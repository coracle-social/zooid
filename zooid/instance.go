package zooid

import (
	"sync"
	"iter"
	"net/http"
	"context"

	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/nostr"
)

type Instance struct {
	Host   string
	Config *Config
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
		Relay:  khatru.NewRelay(),
	}

	instance.Relay.Info.Name = config.Self.Name
	instance.Relay.Info.Icon = config.Self.Icon
	instance.Relay.Info.PubKey = &pubkey
	instance.Relay.Info.Description = config.Self.Description
	// instance.Relay.Info.Self = nostr.GetPublicKey(secret)
	instance.Relay.Info.Software = "https://github.com/coracle-social/zooid"
	instance.Relay.Info.Version = "v0.1.0"

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
  return nil
}

func (instance *Instance) ReplaceEvent(ctx context.Context, event nostr.Event) error {
  return nil
}

func (instance *Instance) DeleteEvent(ctx context.Context, id nostr.ID) error {
  return nil
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
		// TODO: Implement actual event querying logic
		// For now, return empty sequence
	}
}

func (instance *Instance) RejectConnection(r *http.Request) bool {
  return false
}

func (instance *Instance) PreventBroadcast(ws *khatru.WebSocket, event nostr.Event) bool {
  return event.Kind == 28934
}
