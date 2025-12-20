package zooid

import (
	"context"
	"log"
	"net/http"
	"slices"
	"strings"

	"github.com/fiatjaf/khatru"
	"github.com/gosimple/slug"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip29"
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
	instance.Relay.Info.PubKey = owner
	instance.Relay.Info.Description = config.Info.Description
	instance.Relay.Info.Software = "https://github.com/coracle-social/zooid"
	instance.Relay.Info.Version = "v0.1.0"

	// Handlers

	instance.Relay.OnConnect = append(instance.Relay.OnConnect, instance.OnConnect)
	instance.Relay.PreventBroadcast = append(instance.Relay.PreventBroadcast, instance.PreventBroadcast)
	instance.Relay.StoreEvent = append(instance.Relay.StoreEvent, instance.StoreEvent)
	instance.Relay.ReplaceEvent = append(instance.Relay.ReplaceEvent, instance.ReplaceEvent)
	instance.Relay.DeleteEvent = append(instance.Relay.DeleteEvent, instance.DeleteEvent)
	instance.Relay.RejectFilter = append(instance.Relay.RejectFilter, instance.OnRequest)
	instance.Relay.QueryEvents = append(instance.Relay.QueryEvents, instance.QueryStored)
	instance.Relay.RejectEvent = append(instance.Relay.RejectEvent, instance.OnEvent)
	instance.Relay.OnEventSaved = append(instance.Relay.OnEventSaved, instance.OnEventSaved)
	instance.Relay.OnEphemeralEvent = append(instance.Relay.OnEphemeralEvent, instance.OnEphemeralEvent)

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
			if nostr.IsValidPublicKey(hex) {
				instance.Management.AllowPubkey(hex)
			}
		}
	}

	return instance, nil
}

func (instance *Instance) Cleanup() {
	instance.Events.Close()
}

// Utility methods

func (instance *Instance) StripSignature(ctx context.Context, event *nostr.Event) *nostr.Event {
	if event == nil {
		return nil
	}

	pubkey := khatru.GetAuthed(ctx)
	if instance.Config.Policy.StripSignatures && !instance.Config.CanManage(pubkey) {
		stripped := *event
		stripped.Sig = ""
		return &stripped
	}

	return event
}

func (instance *Instance) AllowRecipientEvent(event *nostr.Event) bool {
	if event == nil {
		return false
	}
	// For zap receipts and gift wraps, authorize the recipient instead of the author.
	// For everything else, make sure the authenticated user is the same as the event author
	recipientAuthKinds := []int{
		nostr.KindZap,
		nostr.KindGiftWrap,
	}

	if slices.Contains(recipientAuthKinds, event.Kind) {
		recipientTag := event.Tags.Find("p")

		if recipientTag != nil && len(recipientTag) >= 2 {
			pubkey := recipientTag[1]
			if nostr.IsValidPublicKey(pubkey) && instance.Management.IsMember(pubkey) {
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
	readOnlyEventKinds := []int{
		RELAY_ADD_MEMBER,
		RELAY_REMOVE_MEMBER,
		RELAY_MEMBERS,
	}

	return slices.Contains(readOnlyEventKinds, event.Kind)
}

func (instance *Instance) IsWriteOnlyEvent(event nostr.Event) bool {
	writeOnlyEventKinds := []int{
		RELAY_JOIN,
		RELAY_LEAVE,
	}

	return slices.Contains(writeOnlyEventKinds, event.Kind)
}

func (instance *Instance) GenerateInviteEvent(pubkey string) nostr.Event {
	filter := nostr.Filter{
		Kinds:   []int{RELAY_INVITE},
		Authors: []string{pubkey},
	}

	ch, err := instance.Events.QueryEvents(context.Background(), filter)
	if err == nil {
		if event := <-ch; event != nil {
			return *event
		}
	}

	event := nostr.Event{
		Kind:      RELAY_INVITE,
		CreatedAt: nostr.Now(),
		Tags: nostr.Tags{
			[]string{"claim", RandomString(8)},
			[]string{"p", pubkey},
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

func (instance *Instance) PreventBroadcast(ws *khatru.WebSocket, event *nostr.Event) bool {
	if event == nil {
		return false
	}
	return instance.IsWriteOnlyEvent(*event)
}

func (instance *Instance) StoreEvent(ctx context.Context, event *nostr.Event) error {
	return instance.Events.StoreEvent(ctx, event)
}

func (instance *Instance) ReplaceEvent(ctx context.Context, event *nostr.Event) error {
	return instance.Events.ReplaceEvent(ctx, event)
}

func (instance *Instance) DeleteEvent(ctx context.Context, event *nostr.Event) error {
	return instance.Events.DeleteEvent(ctx, event)
}

// Requests

func (instance *Instance) OnRequest(ctx context.Context, filter nostr.Filter) (reject bool, msg string) {
	pubkey := khatru.GetAuthed(ctx)
	if pubkey == "" {
		return true, "auth-required: authentication is required for access"
	}

	if !instance.Management.IsMember(pubkey) {
		return true, "restricted: you are not a member of this relay"
	}

	return false, ""
}

func (instance *Instance) QueryStored(ctx context.Context, filter nostr.Filter) (chan *nostr.Event, error) {
	if khatru.IsInternalCall(ctx) {
		return instance.Events.QueryEvents(ctx, filter)
	}

	pubkey := khatru.GetAuthed(ctx)
	out := make(chan *nostr.Event)

	go func() {
		defer close(out)

		send := func(event *nostr.Event) bool {
			if event == nil {
				return true
			}
			select {
			case out <- event:
				return true
			case <-ctx.Done():
				return false
			}
		}

		generated := make([]*nostr.Event, 0)

		if slices.Contains(filter.Kinds, RELAY_INVITE) && instance.Config.CanInvite(pubkey) {
			event := instance.GenerateInviteEvent(pubkey)
			generated = append(generated, &event)
		}

		if slices.Contains(filter.Kinds, nostr.KindSimpleGroupAdmins) {
			filter = nostr.Filter{
				Kinds: []int{nostr.KindSimpleGroupMetadata},
			}

			ch, err := instance.Events.QueryEvents(ctx, filter)
			if err == nil {
				for event := range ch {
					if event == nil {
						continue
					}
					if tag := event.Tags.Find("d"); tag != nil {
						gen := instance.Groups.GenerateAdminsEvent(tag[1])
						generated = append(generated, &gen)
					}
				}
			}
		}

		if slices.Contains(filter.Kinds, nostr.KindSimpleGroupMembers) {
			filter = nostr.Filter{
				Kinds: []int{nostr.KindSimpleGroupMetadata},
			}

			ch, err := instance.Events.QueryEvents(ctx, filter)
			if err == nil {
				for event := range ch {
					if event == nil {
						continue
					}
					if tag := event.Tags.Find("d"); tag != nil {
						gen := instance.Groups.GenerateMembersEvent(tag[1])
						generated = append(generated, &gen)
					}
				}
			}
		}

		for _, event := range generated {
			if !filter.Matches(event) {
				continue
			}

			if !send(instance.StripSignature(ctx, event)) {
				return
			}
		}

		storeFilter := filter
		if storeFilter.Limit > 0 && storeFilter.Limit > 1000 {
			storeFilter.Limit = 1000
		}

		ch, err := instance.Events.QueryEvents(ctx, storeFilter)
		if err != nil {
			return
		}
		for event := range ch {
			if event == nil {
				continue
			}

			if event.Kind == RELAY_INVITE {
				continue
			}

			if instance.IsInternalEvent(*event) {
				continue
			}

			if instance.IsWriteOnlyEvent(*event) {
				continue
			}

			h := GetGroupIDFromEvent(*event)

			if h != "" {
				if !instance.Config.Groups.Enabled {
					continue
				}

				if !instance.Groups.IsMember(h, pubkey) {
					continue
				}
			}

			if !instance.Config.Groups.Enabled && slices.Contains(nip29.MetadataEventKinds, event.Kind) {
				continue
			}

			if !send(instance.StripSignature(ctx, event)) {
				return
			}
		}
	}()

	return out, nil
}

// Event publishing

func (instance *Instance) OnEvent(ctx context.Context, event *nostr.Event) (reject bool, msg string) {
	if instance.AllowRecipientEvent(event) {
		return false, ""
	}

	if event == nil {
		return true, "invalid: missing event"
	}

	pubkey := khatru.GetAuthed(ctx)
	if pubkey == "" {
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

	if instance.IsInternalEvent(*event) {
		return true, "invalid: this event's kind is not accepted"
	}

	if instance.IsReadOnlyEvent(*event) {
		return true, "invalid: this event's kind is not accepted"
	}

	if slices.Contains(nip29.MetadataEventKinds, event.Kind) {
		return true, "invalid: group metadata cannot be set directly"
	}

	if slices.Contains(nip29.ModerationEventKinds, event.Kind) && !instance.Config.CanManage(event.PubKey) {
		return true, "restricted: you are not authorized to manage groups"
	}

	allGroupKinds := append(
		nip29.ModerationEventKinds,
		nostr.KindSimpleGroupJoinRequest,
		nostr.KindSimpleGroupLeaveRequest,
	)

	h := GetGroupIDFromEvent(*event)

	if slices.Contains(allGroupKinds, event.Kind) {
		if !instance.Config.Groups.Enabled {
			return true, "invalid: group events not accepted on this relay"
		}

		if h == "" {
			return true, "invalid: h tag is required"
		}

		meta := instance.Groups.GetMetadata(h)

		if event.Kind == nostr.KindSimpleGroupCreateGroup {
			if !IsEmptyEvent(meta) {
				return true, "invalid: that group already exists"
			}
		} else if IsEmptyEvent(meta) {
			return true, "invalid: no such group exists"
		}

		if event.Kind == nostr.KindSimpleGroupJoinRequest && instance.Groups.IsMember(h, event.PubKey) {
			return true, "duplicate: already a member"
		}

		if event.Kind == nostr.KindSimpleGroupLeaveRequest && !instance.Groups.IsMember(h, event.PubKey) {
			return true, "duplicate: not currently a member"
		}
	} else if h != "" {
		meta := instance.Groups.GetMetadata(h)

		if IsEmptyEvent(meta) {
			return true, "invalid: no such group exists"
		}

		if HasTag(meta.Tags, "closed") && !instance.Groups.IsMember(h, pubkey) {
			return true, "restricted: you are not a member of that group"
		}
	}

	if instance.Management.EventIsBanned(event.ID) {
		return true, "restricted: this event has been banned from this relay"
	}

	return false, ""
}

func (instance *Instance) OnEventSaved(ctx context.Context, event *nostr.Event) {
	if event == nil {
		return
	}
	if event.Kind == nostr.KindSimpleGroupJoinRequest && instance.Config.Groups.AutoJoin {
		h := GetGroupIDFromEvent(*event)
		meta := instance.Groups.GetMetadata(h)

		if !HasTag(meta.Tags, "closed") {
			instance.Groups.AddMember(h, event.PubKey)
		}
	}

	if event.Kind == nostr.KindSimpleGroupLeaveRequest && instance.Config.Groups.AutoLeave {
		instance.Groups.RemoveMember(GetGroupIDFromEvent(*event), event.PubKey)
	}

	if event.Kind == nostr.KindSimpleGroupCreateGroup || event.Kind == nostr.KindSimpleGroupEditMetadata {
		instance.Groups.SetMetadataFromEvent(*event)
	}

	if event.Kind == nostr.KindSimpleGroupDeleteGroup {
		instance.Groups.DeleteGroup(GetGroupIDFromEvent(*event))
	}
}

func (instance *Instance) OnEphemeralEvent(ctx context.Context, event *nostr.Event) {
	if event == nil {
		return
	}
	if event.Kind == RELAY_JOIN {
		instance.Management.AddMember(event.PubKey)
	}

	if event.Kind == RELAY_LEAVE {
		instance.Management.RemoveMember(event.PubKey)
	}
}
