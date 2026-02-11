package zooid

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"time"

	"fiatjaf.com/nostr"
)

// Struct definition

type PushManager struct {
	Config       *Config
	Events       *EventStore
	Management   *ManagementStore
	Groups       *GroupStore
	client       *http.Client
	errorCounts  map[string]int // tracks consecutive errors per callback URL
	errorCountMu sync.Mutex     // protects errorCounts map
}

type PushPayload struct {
	ID    string       `json:"id"`
	Relay string       `json:"relay"`
	Event *nostr.Event `json:"event,omitempty"`
}

// Handlers

func (p *PushManager) ValidatePushSubscription(event nostr.Event) (reject bool, msg string) {
	if event.Tags.GetD() == "" {
		return true, "invalid: missing or empty d tag"
	}

	if event.Tags.FindWithValue("relay", "wss://"+p.Config.Host+"/") == nil {
		return true, "invalid: relay tag does not match this relay's URL"
	}

	filterTags := slices.Collect(event.Tags.FindAll("filter"))
	if len(filterTags) == 0 {
		return true, "invalid: at least one filter tag is required"
	}

	for _, filterTag := range filterTags {
		if len(filterTag) < 2 {
			return true, "invalid: filter tag is malformed"
		}

		var filter nostr.Filter
		if err := json.Unmarshal([]byte(filterTag[1]), &filter); err != nil {
			return true, "invalid: filter tag contains invalid JSON: " + err.Error()
		}
	}

	for ignoreTag := range event.Tags.FindAll("ignore") {
		if len(ignoreTag) < 2 {
			return true, "invalid: ignore tag is malformed"
		}

		var filter nostr.Filter
		if err := json.Unmarshal([]byte(ignoreTag[1]), &filter); err != nil {
			return true, "invalid: ignore tag contains invalid JSON: " + err.Error()
		}
	}

	callbackTags := slices.Collect(event.Tags.FindAll("callback"))

	if len(callbackTags) < 1 {
		return true, "invalid: missing callback tag"
	}

	if len(callbackTags) > 1 {
		return true, "invalid: too many callback tags"
	}

	for _, callbackTag := range callbackTags {
		if len(callbackTag) < 2 || callbackTag[1] == "" {
			return true, "invalid: empty callback tag"
		}

		callbackURL := callbackTag[1]
		if parsedURL, err := url.Parse(callbackURL); err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
			return true, "invalid: callback must be a valid HTTP or HTTPS URL"
		}
	}

	filter := nostr.Filter{
		Kinds:   []nostr.Kind{PUSH_SUBSCRIPTION},
		Authors: []nostr.PubKey{event.PubKey},
	}

	count, err := p.Events.CountEvents(filter)
	if err != nil {
		return true, "internal: failed to query database"
	}

	if count > 10 {
		return true, "invalid: too many subscriptions registered"
	}

	return false, ""
}

func (p *PushManager) HandleEvent(event nostr.Event) {
	if !IsReadableEvent(event) {
		return
	}

	filter := nostr.Filter{
		Kinds: []nostr.Kind{PUSH_SUBSCRIPTION},
	}

	for subscriptionEvent := range p.Events.QueryEvents(filter, 0) {
		if event.PubKey == subscriptionEvent.PubKey {
			continue
		}

		if p.Groups.IsGroupEvent(event) && !p.Groups.CanRead(subscriptionEvent.PubKey, event) {
			continue
		}

		filterTags := subscriptionEvent.Tags.FindAll("filter")
		matched := false
		for filterTag := range filterTags {
			if len(filterTag) < 2 {
				continue
			}

			var filter nostr.Filter
			if err := json.Unmarshal([]byte(filterTag[1]), &filter); err != nil {
				continue
			}

			if filter.Matches(event) {
				matched = true
				break
			}
		}

		if !matched {
			continue
		}

		ignoreTags := subscriptionEvent.Tags.FindAll("ignore")
		ignored := false
		for ignoreTag := range ignoreTags {
			if len(ignoreTag) < 2 {
				continue
			}

			var ignore nostr.Filter
			if err := json.Unmarshal([]byte(ignoreTag[1]), &ignore); err != nil {
				continue
			}

			if ignore.Matches(event) {
				ignored = true
				break
			}
		}

		if ignored {
			continue
		}

		callbackTag := subscriptionEvent.Tags.Find("callback")

		if callbackTag == nil || len(callbackTag) < 2 {
			continue
		}

		callback := callbackTag[1]

		payload := PushPayload{
			ID:    event.ID.Hex(),
			Relay: "wss://" + p.Config.Host + "/",
		}

		if subscriptionEvent.Tags.Find("include_event") != nil {
			payload.Event = &event
		}

		payloadBytes, err := json.Marshal(payload)
		if err != nil {
			continue
		}

		go p.sendCallback(subscriptionEvent.ID, callback, payloadBytes)
	}
}

func (p *PushManager) sendCallback(subscriptionID nostr.ID, callback string, payloadBytes []byte) {
	resp, err := p.client.Post(callback, "application/json", bytes.NewReader(payloadBytes))
	if resp != nil {
		defer resp.Body.Close()
	}

	incrementError := func() (count int) {
		p.errorCountMu.Lock()
		p.errorCounts[callback]++
		count = p.errorCounts[callback]
		p.errorCountMu.Unlock()

		return count
	}

	clearError := func() {
		p.errorCountMu.Lock()
		delete(p.errorCounts, callback)
		p.errorCountMu.Unlock()
	}

	if err == nil && resp.StatusCode == 200 {
		clearError()
	} else if err == nil && resp.StatusCode == 404 {
		log.Printf("Callback returned 404, deleting subscription %s", subscriptionID.Hex())
		p.Events.DeleteEvent(subscriptionID)
		clearError()
	} else {
		count := incrementError()

		if count >= 10 {
			log.Printf("Deleting subscription %s due to 10 consecutive failures", subscriptionID.Hex())
			p.Events.DeleteEvent(subscriptionID)
			clearError()
		}
	}
}

// Middleware

func (p *PushManager) Enable(instance *Instance) {
	p.client = &http.Client{
		Timeout: 10 * time.Second,
	}
	p.errorCounts = make(map[string]int)

	instance.Relay.Info.SupportedNIPs = append(instance.Relay.Info.SupportedNIPs, "9a")
}
