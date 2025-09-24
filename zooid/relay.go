package zooid

import (
	"sync"

	"fiatjaf.com/nostr/khatru"
)

func MakeRelay(hostname string, config *Config) *khatru.Relay {
	relay := khatru.NewRelay()
	relay.Info.Name = config.Name
	relay.Info.Icon = config.Icon
	relay.Info.PubKey = config.Pubkey
	relay.Info.Description = config.Description
	relay.Info.Self = nostr.GetPublicKey(config.Secret)
	relay.Info.Software = "https://github.com/coracle-social/zooid"
	relay.Info.Version = "v0.1.0"

	relay.OnConnect = append(relay.OnConnect, khatru.RequestAuth)
	relay.RejectFilter = append(relay.RejectFilter, RejectFilter)
	relay.QueryEvents = append(relay.QueryEvents, QueryEvents)
	relay.DeleteEvent = append(relay.DeleteEvent, DeleteEvent)
	relay.RejectEvent = append(relay.RejectEvent, RejectEvent)
	relay.StoreEvent = append(relay.StoreEvent, SaveEvent)
	relay.OnEventSaved = append(relay.OnEventSaved, OnEventSaved)

	if config.Groups.Enabled {
  	EnableGroups(config, relay)
	}

	if config.Blossom.Enabled {
  	EnableBlossom(config, relay)
	}

	if config.Management.Enabled {
  	EnableManagement(config, relay)
	}

	return relay
}

var (
	relays    map[string]*khatru.Relay
	relayOnce sync.Once
)

func GetRelay(hostname string) (*khatru.Relay, error) {
	relayOnce.Do(func() {
		relays = make(map[string]*khatru.Relay)
	})

	relay, exists := relays[hostname]
	if !exists {
		config, err := LoadConfig(hostname)
		if err != nil {
			return nil, err
		}

		newRelay := MakeRelay(hostname, config)

		relays[hostname] = newRelay
		relay = newRelay
	}

	return relay, nil
}
