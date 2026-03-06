package zooid

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"slices"
	"strings"

	"fiatjaf.com/nostr"
)

const (
	RELAY_ADD_MEMBER    = 8000
	RELAY_REMOVE_MEMBER = 8001
	ROOM_PRESENCE       = 10312
	RELAY_MEMBERS       = 13534
	RELAY_JOIN          = 28934
	RELAY_INVITE        = 28935
	RELAY_LEAVE         = 28936
	PUSH_SUBSCRIPTION   = 30390
	BANNED_PUBKEYS      = "zooid/banned_pubkeys"
	BANNED_EVENTS       = "zooid/banned_events"
)

func IsInternalEvent(event nostr.Event) bool {
	if event.Kind == nostr.KindApplicationSpecificData {
		tag := event.Tags.Find("d")

		if tag != nil && strings.HasPrefix(tag[1], "zooid/") {
			return true
		}
	}

	return false
}

func IsReadOnlyEvent(event nostr.Event) bool {
	readOnlyEventKinds := []nostr.Kind{
		RELAY_ADD_MEMBER,
		RELAY_REMOVE_MEMBER,
		RELAY_MEMBERS,
	}

	return slices.Contains(readOnlyEventKinds, event.Kind)
}

func IsWriteOnlyEvent(event nostr.Event) bool {
	writeOnlyEventKinds := []nostr.Kind{
		RELAY_JOIN,
		RELAY_LEAVE,
		PUSH_SUBSCRIPTION,
	}

	return slices.Contains(writeOnlyEventKinds, event.Kind)
}

func IsReadableEvent(event nostr.Event) bool {
	if event.Kind == RELAY_INVITE {
		return false
	}

	if IsInternalEvent(event) {
		return false
	}

	if IsWriteOnlyEvent(event) {
		return false
	}

	return true
}

func First[T any](s []T) T {
	if len(s) == 0 {
		var zero T
		return zero
	}

	return s[0]
}

func Keys[K comparable, V any](m map[K]V) []K {
	ks := make([]K, len(m))

	i := 0
	for k := range m {
		ks[i] = k
		i++
	}

	return ks
}

func Filter[T any](ss []T, test func(T) bool) (ret []T) {
	for _, s := range ss {
		if test(s) {
			ret = append(ret, s)
		}
	}

	return
}

func Remove[T comparable](slice []T, element T) []T {
	for i, v := range slice {
		if v == element {
			return append(slice[:i], slice[i+1:]...)
		}
	}

	return slice
}

func Reversed[T any](slice []T) []T {
	slices.Reverse(slice)

	return slice
}

const letters = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"

func RandomString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}

	return string(b)
}

func Split(s string, delim string) []string {
	if s == "" {
		return []string{}
	} else {
		return strings.Split(s, delim)
	}
}

func HasTag(tags nostr.Tags, key string) bool {
	for _, v := range tags {
		if len(v) >= 1 && v[0] == key {
			return true
		}
	}
	return false
}

func IsEmptyEvent(event nostr.Event) bool {
	var zeroID nostr.ID

	return event.ID == zeroID
}

func validateNIP98Auth(r *http.Request) (nostr.PubKey, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return nostr.PubKey{}, fmt.Errorf("missing authorization header")
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || strings.ToLower(parts[0]) != "nostr" {
		return nostr.PubKey{}, fmt.Errorf("invalid authorization header format")
	}

	eventJSON, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return nostr.PubKey{}, fmt.Errorf("invalid base64 encoding: %w", err)
	}

	var event nostr.Event
	if err := json.Unmarshal(eventJSON, &event); err != nil {
		return nostr.PubKey{}, fmt.Errorf("invalid event json: %w", err)
	}

	if event.Kind != nostr.KindHTTPAuth {
		return nostr.PubKey{}, fmt.Errorf("invalid event kind: expected %d, got %d", nostr.KindHTTPAuth, event.Kind)
	}

	if !event.VerifySignature() {
		return nostr.PubKey{}, fmt.Errorf("invalid event signature")
	}

	expectedURL := fmt.Sprintf("%s://%s%s", scheme(r), r.Host, r.URL.Path)
	var hasURL, hasMethod bool

	for _, tag := range event.Tags {
		if len(tag) < 2 {
			continue
		}
		switch tag[0] {
		case "u":
			if tag[1] == expectedURL {
				hasURL = true
			}
		case "method":
			if strings.ToUpper(tag[1]) == r.Method {
				hasMethod = true
			}
		}
	}

	if !hasURL {
		return nostr.PubKey{}, fmt.Errorf("event missing or invalid u tag")
	}
	if !hasMethod {
		return nostr.PubKey{}, fmt.Errorf("event missing or invalid method tag")
	}

	return event.PubKey, nil
}
