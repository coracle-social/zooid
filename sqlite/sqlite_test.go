package sqlite

import (
	"os"
	"testing"

	"fiatjaf.com/nostr"
	"github.com/stretchr/testify/assert"
)

func TestSqliteFlow(t *testing.T) {
	os.RemoveAll("/tmp/sqlitetest.db")

	sb := &SqliteBackend{
		Path:   "/tmp/sqlitetest.db",
		Prefix: "prefix",
	}
	err := sb.Init()
	assert.NoError(t, err)
	defer sb.Close()

	willDelete := make([]nostr.Event, 0, 3)

	sk := nostr.MustSecretKeyFromHex("0000000000000000000000000000000000000000000000000000000000000001")

	for i, content := range []string{
		"good morning mr paper maker",
		"good night",
		"I'll see you again in the paper house",
		"tonight we dine in my house",
		"the paper in this house if very good, mr",
	} {
		evt := nostr.Event{
			Content:   content,
			Tags:      nostr.Tags{},
			Kind:      1,
			CreatedAt: nostr.Now(),
			PubKey:    sk.Public(),
		}
		evt.ID = evt.GetID()
		// For testing, we'll skip actual signing and just set a dummy signature
		// In real usage, you'd call evt.Sign(sk)

		err := sb.SaveEvent(evt)
		assert.NoError(t, err)

		if i%2 == 0 {
			willDelete = append(willDelete, evt)
		}
	}

	// Test search functionality (if FTS5 is available)
	if sb.FTSAvailable {
		n := 0
		for range sb.QueryEvents(nostr.Filter{Search: "good"}, 400) {
			n++
		}
		assert.Equal(t, 3, n)
	} else {
		// With LIKE fallback, should still work but might be less precise
		n := 0
		for range sb.QueryEvents(nostr.Filter{Search: "good"}, 400) {
			n++
		}
		assert.Equal(t, 3, n)
	}

	// Delete some events
	for _, evt := range willDelete {
		err := sb.DeleteEvent(evt.ID)
		assert.NoError(t, err)
	}

	// Test search after deletion
	if sb.FTSAvailable {
		n := 0
		for res := range sb.QueryEvents(nostr.Filter{Search: "good"}, 400) {
			n++
			assert.Equal(t, res.Content, "good night")
			assert.Equal(t, sk.Public(), res.PubKey)
		}
		assert.Equal(t, 1, n)
	} else {
		// With LIKE fallback, should still work
		n := 0
		for res := range sb.QueryEvents(nostr.Filter{Search: "good"}, 400) {
			n++
			assert.Equal(t, res.Content, "good night")
			assert.Equal(t, sk.Public(), res.PubKey)
		}
		assert.Equal(t, 1, n)
	}

	// Test query by kind
	{
		n := 0
		for range sb.QueryEvents(nostr.Filter{Kinds: []nostr.Kind{1}}, 400) {
			n++
		}
		assert.Equal(t, 2, n)
	}

	// Test query by author
	{
		n := 0
		for range sb.QueryEvents(nostr.Filter{Authors: []nostr.PubKey{sk.Public()}}, 400) {
			n++
		}
		assert.Equal(t, 2, n)
	}
}
