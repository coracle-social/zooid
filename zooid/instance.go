package zooid

import (
	"context"
	"iter"
	"log"
	"net/http"
	"os"
	"sync"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore"
	"fiatjaf.com/nostr/khatru"
	"zooid/sqlite"
)

type Instance struct {
	Host   string
	Config *Config
	Events eventstore.Store
	Relay  *khatru.Relay
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

	// secret, err := nostr.SecretKeyFromHex(config.Self.Secret)
	// if err != nil {
	// 	return nil, err
	// }

	instance := &Instance{
		Host:   hostname,
		Config: config,
		Events: &sqlite.SqliteBackend{Path: config.Data.Events},
		Relay:  khatru.NewRelay(),
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

	if config.Groups.Enabled {
		EnableGroups(instance)
	}

	if config.Blossom.Enabled {
		EnableBlossom(instance)
	}

	if config.Management.Enabled {
		EnableManagement(instance)
	}

	// Initialize stuff

	if err := os.MkdirAll(instance.Config.Data.Events, 0755); err != nil {
		log.Fatal("Failed to create event store path:", err)
	}

	if err := instance.Events.Init(); err != nil {
		log.Fatal("Failed to initialize event store:", err)
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

func (instance *Instance) IsMember(pubkey nostr.PubKey) bool {
	pubkeyStr := pubkey.String()
	for _, role := range instance.Config.Roles {
		for _, pk := range role.Pubkeys {
			if pk == pubkeyStr {
				return true
			}
		}
	}
	return false
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
