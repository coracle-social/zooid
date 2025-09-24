package zooid

import (
	"fiatjaf.com/nostr/khatru"
)

func EnableGroups(config *Config, relay *khatru.Relay) {
	relay.Info.SupportedNIPs = append(relay.Info.SupportedNIPs, 29)
}
