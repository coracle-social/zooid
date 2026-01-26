package zooid

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	// Create required directories for tests
	os.MkdirAll("./data", 0755)
	os.MkdirAll("./media", 0755)
	os.MkdirAll("./config", 0755)

	// Run tests
	code := m.Run()

	// Cleanup test databases (keep directories for potential debugging)
	files, _ := os.ReadDir("./data")
	for _, f := range files {
		if f.Name() != ".gitkeep" {
			os.Remove("./data/" + f.Name())
		}
	}

	os.Exit(code)
}
