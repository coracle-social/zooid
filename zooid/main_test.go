package zooid

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestMain(m *testing.M) {
	base, err := os.MkdirTemp("", "zooid-test-*")
	if err != nil {
		panic(err)
	}

	// Ensure sqlite parent dir exists
	_ = os.MkdirAll(filepath.Join(base, "data"), 0o755)
	_ = os.MkdirAll(filepath.Join(base, "media"), 0o755)
	_ = os.MkdirAll(filepath.Join(base, "config"), 0o755)

	_ = os.Setenv("DATA", filepath.Join(base, "data"))
	_ = os.Setenv("MEDIA", filepath.Join(base, "media"))
	_ = os.Setenv("CONFIG", filepath.Join(base, "config"))

	// Reset Env() cache in case it was initialized already
	env = nil
	envOnce = sync.Once{}

	// Reset DB singleton in case it was initialized already
	if db != nil {
		_ = db.Close()
		db = nil
	}
	dbOnce = sync.Once{}

	code := m.Run()

	if db != nil {
		_ = db.Close()
	}
	_ = os.RemoveAll(base)

	os.Exit(code)
}
