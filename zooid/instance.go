package zooid

import (
	"context"
	"iter"
	"log"
	"net/http"
	"slices"
	"strings"
	"sync"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore"
	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/nostr/nip29"
	"github.com/gosimple/slug"
)

type Instance struct {
	Config     *Config
	Events     eventstore.Store
	Blossom    *BlossomStore
	Management *ManagementStore
	Relay      *khatru.Relay
}

func MakeInstance(hostname string) (*Instance, error) {
	config, err := LoadConfig(hostname)
	if err != nil {
		return nil, err
	}

	pubkey, err := nostr.PubKeyFromHex(config.Self.Pubkey)
	if err != nil {
		return nil, err
	}

	events := &EventStore{
		Config: config,
		Schema: &Schema{
			Name: slug.Make(config.Self.Schema),
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

	instance := &Instance{
		Config:     config,
		Events:     events,
		Blossom:    blossom,
		Management: management,
		Relay:      khatru.NewRelay(),
	}

	instance.Relay.Info.Name = config.Self.Name
	instance.Relay.Info.Icon = config.Self.Icon
	instance.Relay.Info.PubKey = &pubkey
	instance.Relay.Info.Description = config.Self.Description
	// instance.Relay.Info.Self = nostr.GetPublicKey(secret)
	instance.Relay.Info.Software = "https://github.com/coracle-social/zooid"
	instance.Relay.Info.Version = "v0.1.0"

	instance.Relay.UseEventstore(instance.Events, 400)

	instance.Relay.OnConnect = instance.OnConnect
	instance.Relay.OnEvent = instance.OnEvent
	instance.Relay.StoreEvent = instance.StoreEvent
	instance.Relay.ReplaceEvent = instance.ReplaceEvent
	instance.Relay.DeleteEvent = instance.DeleteEvent
	instance.Relay.OnEventSaved = instance.OnEventSaved
	instance.Relay.OnEphemeralEvent = instance.OnEphemeralEvent
	instance.Relay.OnRequest = instance.OnRequest
	instance.Relay.QueryStored = instance.QueryStored
	instance.Relay.RejectConnection = instance.RejectConnection
	instance.Relay.PreventBroadcast = instance.PreventBroadcast

	// Initialize stuff

	if err := instance.Events.Init(); err != nil {
		log.Fatal("Failed to initialize event store:", err)
	}

	if err := instance.Blossom.Init(); err != nil {
		log.Fatal("Failed to initialize blossom store:", err)
	}

	if config.Blossom.Enabled {
		instance.Blossom.Enable(instance)
	}

	if config.Management.Enabled {
		instance.Management.Enable(instance)
	}

	return instance, nil
}

var (
	instances    map[string]*Instance
	instanceOnce sync.Once
)

func GetInstance(hostname string) (*Instance, error) {
	instanceOnce.Do(func() {
		instances = make(map[string]*Instance)
	})

	instance, exists := instances[hostname]
	if !exists {
		newInstance, err := MakeInstance(hostname)
		if err != nil {
			return nil, err
		}

		instances[hostname] = newInstance
		instance = newInstance
	}

	return instance, nil
}

// Utility methods

func (instance *Instance) HasAccess(pubkey nostr.PubKey) bool {
	if instance.Config.IsAdmin(pubkey) {
		return true
	}

	if instance.Management.PubkeyIsBanned(pubkey) {
		return false
	}

	filter := nostr.Filter{
		Kinds:   []nostr.Kind{AUTH_JOIN},
		Authors: []nostr.PubKey{pubkey},
	}

	for range instance.Events.QueryEvents(filter, 1) {
		return true
	}

	return false
}

func (instance *Instance) IsGroupMember(id string, pubkey nostr.PubKey) bool {
	filter := MakeGroupMembershipCheckFilter(id, pubkey)
	events := instance.Events.QueryEvents(filter, 0)
	isMember := CheckGroupMembership(events)

	return isMember
}

func (instance *Instance) HasGroupAccess(id string, pubkey nostr.PubKey) bool {
	filter := MakeGroupMetadataFilter(id)

	for event := range instance.Events.QueryEvents(filter, 1) {
		if !HasTag(event.Tags, "closed") {
			return true
		}
	}

	return instance.IsGroupMember(id, pubkey)
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

			if err == nil && instance.HasAccess(pubkey) {
				return true
			}
		}
	}

	return false
}

func (instance *Instance) GenerateInviteEvent(pubkey nostr.PubKey) nostr.Event {
	filter := nostr.Filter{
		Kinds:   []nostr.Kind{AUTH_INVITE},
		Authors: []nostr.PubKey{pubkey},
	}

	for event := range instance.Events.QueryEvents(filter, 1) {
		return event
	}

	event := nostr.Event{
		Kind:      AUTH_INVITE,
		CreatedAt: nostr.Now(),
		Tags: nostr.Tags{
			[]string{"claim", RandomString(8)},
			[]string{"p", pubkey.Hex()},
		},
	}

	event.Sign(instance.Config.Secret)

	err := instance.Events.SaveEvent(event)
	if err != nil {
		log.Printf("Failed to generate invite event: %v", err)
	}

	return event
}

func (instance *Instance) OnJoinEvent(event nostr.Event) (reject bool, msg string) {
	claimTag := event.Tags.Find("claim")

	if claimTag == nil {
		return true, "invalid: no claim tag"
	}

	filter := nostr.Filter{
		Kinds: []nostr.Kind{AUTH_INVITE},
	}

	for event := range instance.Events.QueryEvents(filter, 0) {
		if event.Tags.FindWithValue("claim", claimTag[1]) != nil {
			return false, ""
		}
	}

	return true, "invalid: failed to validate invite code"
}

func (instance *Instance) GetGroupMetadataEvent(h string) nostr.Event {
	for event := range instance.Events.QueryEvents(MakeGroupMetadataFilter(h), 1) {
		return event
	}

	return nostr.Event{}
}

// Handlers

func (instance *Instance) OnConnect(ctx context.Context) {
	khatru.RequestAuth(ctx)
}

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

	if event.Kind == AUTH_JOIN {
		return instance.OnJoinEvent(event)
	}

	if !instance.HasAccess(pubkey) {
		return true, "restricted: you are not a member of this relay"
	}

	if instance.IsInternalEvent(event) {
		return true, "invalid: this event is not accepted"
	}

	if slices.Contains(nip29.MetadataEventKinds, event.Kind) {
		return true, "invalid: group metadata cannot be set directly"
	}

	if slices.Contains(nip29.ModerationEventKinds, event.Kind) && !instance.Config.IsAdmin(event.PubKey) {
		return true, "restricted: you are not authorized to manage groups"
	}

	allGroupKinds := append(
		nip29.ModerationEventKinds,
		nostr.KindSimpleGroupJoinRequest,
		nostr.KindSimpleGroupLeaveRequest,
	)

	h := GetGroupIDFromEvent(event)

	if slices.Contains(allGroupKinds, event.Kind) {
		if !instance.Config.Groups.Enabled {
			return true, "invalid: group events not accepted on this relay"
		}

		if h == "" {
			return true, "invalid: h tag is required"
		}

		meta := instance.GetGroupMetadataEvent(h)

		if event.Kind == nostr.KindSimpleGroupCreateGroup && !IsEmptyEvent(meta) {
			return true, "invalid: that group already exists"
		} else if IsEmptyEvent(meta) {
			return true, "invalid: no such group exists"
		}

		if event.Kind == nostr.KindSimpleGroupJoinRequest && instance.IsGroupMember(h, event.PubKey) {
			return true, "duplicate: already a member"
		}

		if event.Kind == nostr.KindSimpleGroupLeaveRequest && !instance.IsGroupMember(h, event.PubKey) {
			return true, "duplicate: not currently a member"
		}
	} else if h != "" {
		meta := instance.GetGroupMetadataEvent(h)

		if IsEmptyEvent(meta) {
			return true, "invalid: no such group exists"
		}

		if HasTag(meta.Tags, "closed") && !instance.IsGroupMember(h, pubkey) {
			return true, "restricted: you are not a member of that group"
		}
	}

	if instance.Management.EventIsBanned(event.ID) {
		return true, "restricted: this event has been banned from this relay"
	}

	return false, ""
}

func (instance *Instance) StoreEvent(ctx context.Context, event nostr.Event) error {
	return instance.Events.SaveEvent(event)
}

func (instance *Instance) ReplaceEvent(ctx context.Context, event nostr.Event) error {
	return instance.Events.ReplaceEvent(event)
}

func (instance *Instance) DeleteEvent(ctx context.Context, id nostr.ID) error {
	return instance.Events.DeleteEvent(id)
}

func (instance *Instance) OnEventSaved(ctx context.Context, event nostr.Event) {
	addEvent := func(newEvent nostr.Event) {
		if err := newEvent.Sign(instance.Config.Secret); err != nil {
			log.Println(err)
		} else {
			if err := instance.Events.SaveEvent(newEvent); err != nil {
				log.Println(err)
			} else {
				instance.Relay.BroadcastEvent(newEvent)
			}
		}
	}

	if event.Kind == nostr.KindSimpleGroupJoinRequest && instance.Config.Groups.AutoJoin {
		h := GetGroupIDFromEvent(event)
		meta := instance.GetGroupMetadataEvent(h)

		if !HasTag(meta.Tags, "closed") {
			addEvent(MakePutUserEvent(h, event.PubKey))
		}
	}

	if event.Kind == nostr.KindSimpleGroupLeaveRequest && instance.Config.Groups.AutoLeave {
		addEvent(MakeRemoveUserEvent(GetGroupIDFromEvent(event), event.PubKey))
	}

	if event.Kind == nostr.KindSimpleGroupCreateGroup {
		addEvent(MakeMetadataEvent(event))
	}

	if event.Kind == nostr.KindSimpleGroupEditMetadata {
		addEvent(MakeMetadataEvent(event))
	}

	if event.Kind == nostr.KindSimpleGroupDeleteGroup {
		for _, filter := range MakeGroupEventFilters(GetGroupIDFromEvent(event)) {
			for event := range instance.Events.QueryEvents(filter, 0) {
				instance.Events.DeleteEvent(event.ID)
			}
		}
	}
}

func (instance *Instance) OnEphemeralEvent(ctx context.Context, event nostr.Event) {
	if slices.Contains([]nostr.Kind{AUTH_INVITE, AUTH_JOIN}, event.Kind) {
		instance.Events.SaveEvent(event)
	}
}

func (instance *Instance) OnRequest(ctx context.Context, filter nostr.Filter) (reject bool, msg string) {
	pubkey, ok := khatru.GetAuthed(ctx)

	if !ok {
		return true, "auth-required: authentication is required for access"
	}

	if !instance.HasAccess(pubkey) {
		return true, "restricted: you are not a member of this relay"
	}

	return false, ""
}

func (instance *Instance) QueryStored(ctx context.Context, filter nostr.Filter) iter.Seq[nostr.Event] {
	return func(yield func(nostr.Event) bool) {
		pubkey, ok := khatru.GetAuthed(ctx)

		if !ok {
			log.Fatal("Unauthenticated user was allowed to query events")
		}

		stripSignature := func(event nostr.Event) nostr.Event {
			if instance.Config.Policy.StripSignatures && !instance.Config.IsAdmin(pubkey) {
				var zeroSig [64]byte
				event.Sig = zeroSig
			}

			return event
		}

		if slices.Contains(filter.Kinds, AUTH_INVITE) && instance.Config.CanInvite(pubkey) {
			if !yield(stripSignature(instance.GenerateInviteEvent(pubkey))) {
				return
			}
		}

		for event := range instance.Events.QueryEvents(filter, 1000) {
			// We save some ephemeral events for bookkeeping, don't return them
			if event.Kind.IsEphemeral() {
				continue
			}

			h := GetGroupIDFromEvent(event)

			if h != "" {
				if !instance.Config.Groups.Enabled {
					continue
				}

				if !instance.HasGroupAccess(h, pubkey) {
					continue
				}
			}

			if !instance.Config.Groups.Enabled && slices.Contains(nip29.MetadataEventKinds, event.Kind) {
				continue
			}

			if !yield(event) {
				return
			}
		}
	}
}

func (instance *Instance) RejectConnection(r *http.Request) bool {
	return false
}

func (instance *Instance) PreventBroadcast(ws *khatru.WebSocket, event nostr.Event) bool {
	return event.Kind == AUTH_JOIN
}
