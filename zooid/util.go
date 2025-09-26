package zooid

import (
	"fiatjaf.com/nostr"
	"math/rand"
	"strings"
)

const (
	AUTH_JOIN      = 28934
	AUTH_INVITE    = 28935
	BANNED_PUBKEYS = "zooid/banned_pubkeys"
	BANNED_EVENTS  = "zooid/banned_events"
)

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
