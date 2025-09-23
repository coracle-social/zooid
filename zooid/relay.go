package zooid

import (
	"fiatjaf.com/nostr/khatru"
	"sync"
)

func MakeRelay(hostname string, config *Config) *khatru.Relay {
	relay := khatru.NewRelay()

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
