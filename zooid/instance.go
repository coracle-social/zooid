package zooid

import (
	"context"
	"iter"
	"log"
	"net/http"
	"slices"
	"strings"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/khatru"
	"github.com/gosimple/slug"
)

type Instance struct {
	Relay      *khatru.Relay
	Config     *Config
	Events     *EventStore
	Blossom    *BlossomStore
	Management *ManagementStore
	Groups     *GroupStore
}

func MakeInstance(filename string) (*Instance, error) {
	config, err := LoadConfig(filename)
	if err != nil {
		return nil, err
	}

	relay := khatru.NewRelay()

	events := &EventStore{
		Relay:  relay,
		Config: config,
		Schema: &Schema{
			Name: slug.Make(config.Schema),
		},
	}

	blossom := &BlossomStore{
		Config: config,
		Events: events,
	}

	management := &ManagementStore{
		Config: config,
		Events: events,
	}

	groups := &GroupStore{
		Config:     config,
		Events:     events,
		Management: management,
	}

	instance := &Instance{
		Relay:      relay,
		Config:     config,
		Events:     events,
		Blossom:    blossom,
		Management: management,
		Groups:     groups,
	}

	// NIP 11 info

	// self := config.GetSelf()
	owner := config.GetOwner()

	instance.Relay.Negentropy = true
	instance.Relay.Info.Name = config.Info.Name
	instance.Relay.Info.Icon = config.Info.Icon
	// instance.Relay.Info.Self = &self
	instance.Relay.Info.PubKey = &owner
	instance.Relay.Info.Description = config.Info.Description
	instance.Relay.Info.Software = "https://github.com/coracle-social/zooid"
	instance.Relay.Info.Version = "v0.1.0"
	instance.Relay.Info.SupportedNIPs = append(instance.Relay.Info.SupportedNIPs, 43)

	// Handlers

	instance.Relay.OnConnect = instance.OnConnect
	instance.Relay.PreventBroadcast = instance.PreventBroadcast
	instance.Relay.StoreEvent = instance.StoreEvent
	instance.Relay.ReplaceEvent = instance.ReplaceEvent
	instance.Relay.DeleteEvent = instance.DeleteEvent
	instance.Relay.OnRequest = instance.OnRequest
	instance.Relay.QueryStored = instance.QueryStored
	instance.Relay.OnEvent = instance.OnEvent
	instance.Relay.OnEventSaved = instance.OnEventSaved
	instance.Relay.OnEphemeralEvent = instance.OnEphemeralEvent

	// Todo: when there's a new version of khatru
	// instance.Relay.StartExpirationManager()

	// HTTP request handling

	router := instance.Relay.Router()

	router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "templates/index.html")
	})

	router.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	// Initialize the database

	if err := instance.Events.Init(); err != nil {
		log.Fatal("Failed to initialize event store: ", err)
	}

	// Enable extra functionality

	if config.Blossom.Enabled {
		instance.Blossom.Enable(instance)
	}

	if config.Management.Enabled {
		instance.Management.Enable(instance)
	}

	if config.Groups.Enabled {
		instance.Groups.Enable(instance)
	}

	// Update managed membership/admin lists

	instance.Management.AllowPubkey(config.GetSelf())
	instance.Management.AllowPubkey(config.GetOwner())

	for _, role := range config.Roles {
		for _, hex := range role.Pubkeys {
			if pubkey, err := nostr.PubKeyFromHex(hex); err == nil {
				instance.Management.AllowPubkey(pubkey)
			}
		}
	}

	return instance, nil
}

func (instance *Instance) Cleanup() {
	instance.Events.Close()
}

// Utility methods

func (instance *Instance) StripSignature(ctx context.Context, event nostr.Event) nostr.Event {
	pubkey, _ := khatru.GetAuthed(ctx)

	if instance.Config.Policy.StripSignatures && !instance.Config.CanManage(pubkey) {
		var zeroSig [64]byte
		event.Sig = zeroSig
	}

	return event
}

func (instance *Instance) AllowRecipientEvent(event nostr.Event) bool {
	// For zap receipts and gift wraps, authorize the recipient instead of the author.
	// For everything else, make sure the authenticated user is the same as the event author
	recipientAuthKinds := []nostr.Kind{
		nostr.KindZap,
		nostr.KindGiftWrap,
	}

	if slices.Contains(recipientAuthKinds, event.Kind) {
		recipientTag := event.Tags.Find("p")

		if recipientTag != nil {
			pubkey, err := nostr.PubKeyFromHex(recipientTag[1])

			if err == nil && instance.Management.IsMember(pubkey) {
				return true
			}
		}
	}

	return false
}

func (instance *Instance) IsInternalEvent(event nostr.Event) bool {
	if event.Kind == nostr.KindApplicationSpecificData {
		tag := event.Tags.Find("d")

		if tag != nil && strings.HasPrefix(tag[1], "zooid/") {
			return true
		}
	}

	return false
}

func (instance *Instance) IsReadOnlyEvent(event nostr.Event) bool {
	readOnlyEventKinds := []nostr.Kind{
		RELAY_ADD_MEMBER,
		RELAY_REMOVE_MEMBER,
		RELAY_MEMBERS,
	}

	return slices.Contains(readOnlyEventKinds, event.Kind)
}

func (instance *Instance) IsWriteOnlyEvent(event nostr.Event) bool {
	writeOnlyEventKinds := []nostr.Kind{
		RELAY_JOIN,
		RELAY_LEAVE,
	}

	return slices.Contains(writeOnlyEventKinds, event.Kind)
}

func (instance *Instance) GenerateInviteEvent(pubkey nostr.PubKey) nostr.Event {
	filter := nostr.Filter{
		Kinds: []nostr.Kind{RELAY_INVITE},
		Tags: nostr.TagMap{
			"#p": []string{pubkey.Hex()},
		},
	}

	for event := range instance.Events.QueryEvents(filter, 1) {
		return event
	}

	event := nostr.Event{
		Kind:      RELAY_INVITE,
		CreatedAt: nostr.Now(),
		Tags: nostr.Tags{
			[]string{"claim", RandomString(8)},
			[]string{"p", pubkey.Hex()},
		},
	}

	if err := instance.Events.SignAndStoreEvent(&event, false); err != nil {
		log.Printf("Failed to sign invite event: %v", err)
	}

	return event
}

// Handlers

func (instance *Instance) OnConnect(ctx context.Context) {
	khatru.RequestAuth(ctx)
}

func (instance *Instance) PreventBroadcast(ws *khatru.WebSocket, event nostr.Event) bool {
	return instance.IsWriteOnlyEvent(event)
}

func (instance *Instance) StoreEvent(ctx context.Context, event nostr.Event) error {
	return instance.Events.StoreEvent(event)
}

func (instance *Instance) ReplaceEvent(ctx context.Context, event nostr.Event) error {
	return instance.Events.ReplaceEvent(event)
}

func (instance *Instance) DeleteEvent(ctx context.Context, id nostr.ID) error {
	return instance.Events.DeleteEvent(id)
}

// Requests

func (instance *Instance) OnRequest(ctx context.Context, filter nostr.Filter) (reject bool, msg string) {
	pubkey, ok := khatru.GetAuthed(ctx)

	if !ok {
		return true, "auth-required: authentication is required for access"
	}

	if !instance.Management.IsMember(pubkey) {
		return true, "restricted: you are not a member of this relay"
	}

	return false, ""
}

func (instance *Instance) QueryStored(ctx context.Context, filter nostr.Filter) iter.Seq[nostr.Event] {
	return func(yield func(nostr.Event) bool) {
		if khatru.IsInternalCall(ctx) {
			for event := range instance.Events.QueryEvents(filter, 0) {
				if !yield(event) {
					return
				}
			}
		} else {
			pubkey, _ := khatru.GetAuthed(ctx)
			generated := make([]nostr.Event, 0)

			if slices.Contains(filter.Kinds, RELAY_INVITE) && instance.Config.CanInvite(pubkey) {
				generated = append(generated, instance.GenerateInviteEvent(pubkey))
			}

			for _, event := range generated {
				if !filter.Matches(event) {
					continue
				}

				if !yield(instance.StripSignature(ctx, event)) {
					return
				}
			}

			for event := range instance.Events.QueryEvents(filter, 1000) {
				if event.Kind == RELAY_INVITE {
					continue
				}

				if instance.IsInternalEvent(event) {
					continue
				}

				if instance.IsWriteOnlyEvent(event) {
					continue
				}

				if instance.Groups.IsGroupEvent(event) && !instance.Groups.CanRead(pubkey, event) {
					continue
				}

				if !yield(instance.StripSignature(ctx, event)) {
					return
				}
			}
		}
	}
}

// Event publishing

func (instance *Instance) OnEvent(ctx context.Context, event nostr.Event) (reject bool, msg string) {
	if instance.AllowRecipientEvent(event) {
		return false, ""
	}

	pubkey, isAuthenticated := khatru.GetAuthed(ctx)

	if !isAuthenticated {
		return true, "auth-required: authentication is required for access"
	} else if pubkey != event.PubKey {
		return true, "restricted: you cannot publish events on behalf of others"
	}

	if event.Kind == RELAY_JOIN {
		return instance.Management.ValidateJoinRequest(event)
	}

	if !instance.Management.IsMember(pubkey) {
		return true, "restricted: you are not a member of this relay"
	}

	if instance.IsInternalEvent(event) {
		return true, "invalid: this event's kind is not accepted"
	}

	if instance.IsReadOnlyEvent(event) {
		return true, "invalid: this event's kind is not accepted"
	}

	if instance.Groups.IsGroupEvent(event) {
		if err := instance.Groups.CheckWrite(event); err != "" {
			return true, err
		}
	}

	if instance.Management.EventIsBanned(event.ID) {
		return true, "restricted: this event has been banned from this relay"
	}

	return false, ""
}

func (instance *Instance) OnEventSaved(ctx context.Context, event nostr.Event) {
	var groupMeta nostr.Event
	var groupFound bool

	h := GetGroupIDFromEvent(event)

	if h != "" {
		groupMeta, groupFound = instance.Groups.GetMetadata(h)

		if !groupFound && event.Kind != nostr.KindSimpleGroupCreateGroup {
			log.Printf("Attempted to process event for nonexistent group %s", h)
			return
		}
	}

	if event.Kind == nostr.KindSimpleGroupJoinRequest && instance.Config.Groups.AutoJoin {
		if !HasTag(groupMeta.Tags, "closed") {
			instance.Groups.AddMember(h, event.PubKey)
			instance.Groups.UpdateMembersList(h)
		}
	}

	if event.Kind == nostr.KindSimpleGroupLeaveRequest {
		instance.Groups.RemoveMember(h, event.PubKey)
		instance.Groups.UpdateMembersList(h)
	}

	if event.Kind == nostr.KindSimpleGroupPutUser {
		instance.Groups.UpdateMembersList(h)
	}

	if event.Kind == nostr.KindSimpleGroupRemoveUser {
		instance.Groups.UpdateMembersList(h)
	}

	if event.Kind == nostr.KindSimpleGroupCreateGroup {
		instance.Groups.UpdateMetadata(event)
		instance.Groups.UpdateAdminsList(h)
	}

	if event.Kind == nostr.KindSimpleGroupEditMetadata {
		instance.Groups.UpdateMetadata(event)
		instance.Groups.UpdateAdminsList(h)
	}

	if event.Kind == nostr.KindSimpleGroupDeleteGroup {
		instance.Groups.DeleteGroup(h)
	}
}

func (instance *Instance) OnEphemeralEvent(ctx context.Context, event nostr.Event) {
	if event.Kind == RELAY_JOIN {
		instance.Management.AddMember(event.PubKey)
	}

	if event.Kind == RELAY_LEAVE {
		instance.Management.RemoveMember(event.PubKey)
	}
}
