package zooid

import (
	"context"
	"iter"
	"log"
	"net/http"
	"sync"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore"
	"fiatjaf.com/nostr/khatru"
	"github.com/gosimple/slug"
)

type Instance struct {
	Host       string
	Config     *Config
	Secret     nostr.SecretKey
	Events     eventstore.Store
	Access     *AccessStore
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

	secret, err := nostr.SecretKeyFromHex(config.Self.Secret)
	if err != nil {
		return nil, err
	}

	instance := &Instance{
		Host:   hostname,
		Config: config,
		Secret: secret,
		Events: &EventStore{
			Config: config,
			Schema: &Schema{
				Name: slug.Make(config.Self.Schema) + "__events",
			},
		},
		Access: &AccessStore{
			Config: config,
			Schema: &Schema{
				Name: slug.Make(config.Self.Schema) + "__access",
			},
		},
		Management: &ManagementStore{
			Config: config,
			Schema: &Schema{
				Name: slug.Make(config.Self.Schema) + "__management",
			},
		},
		Relay: khatru.NewRelay(),
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

	if err := instance.Access.Init(); err != nil {
		log.Fatal("Failed to initialize access store:", err)
	}

	if err := instance.Management.Init(); err != nil {
		log.Fatal("Failed to initialize management store:", err)
	}

	if config.Groups.Enabled {
		EnableGroups(instance)
	}

	if config.Blossom.Enabled {
		EnableBlossom(instance)
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
	if instance.Config.IsOwner(pubkey) {
		return true
	}

	if instance.Config.IsSelf(pubkey) {
		return true
	}

	roles := instance.Config.GetRolesForPubkey(pubkey)

	if instance.Config.CanManage(roles) {
		return true
	}

	if len(instance.Access.GetRedemptionsByPubkey(pubkey)) > 0 {
		return true
	}

	return false
}

func (instance *Instance) GenerateInviteEvents(ctx context.Context, filter nostr.Filter) []*nostr.Event {
	pubkey, ok := khatru.GetAuthed(ctx)

	if !ok {
		return []*nostr.Event{}
	}

	var claim string

	invites := instance.Access.GetInvitesByPubkey(pubkey)

	if len(invites) > 0 {
		claim = First(invites).Claim
	} else {
		claim = RandomString(8)
		instance.Access.AddInvite(pubkey, claim)
	}

	event := nostr.Event{
		Kind:      AUTH_INVITE,
		CreatedAt: nostr.Now(),
		Tags: nostr.Tags{
			nostr.Tag{"claim", claim},
		},
	}

	event.Sign(instance.Secret)

	return []*nostr.Event{&event}
}

// Handlers

func (instance *Instance) OnConnect(ctx context.Context) {
	khatru.RequestAuth(ctx)
}

func (instance *Instance) OnEvent(ctx context.Context, event nostr.Event) (reject bool, msg string) {
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
}

func (instance *Instance) OnEphemeralEvent(ctx context.Context, event nostr.Event) {
}

func (instance *Instance) OnRequest(ctx context.Context, filter nostr.Filter) (reject bool, msg string) {
	return false, ""
}

func (instance *Instance) QueryStored(ctx context.Context, filter nostr.Filter) iter.Seq[nostr.Event] {
	return func(yield func(nostr.Event) bool) {
		for evt := range instance.Events.QueryEvents(filter, 400) {
			if !yield(evt) {
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
