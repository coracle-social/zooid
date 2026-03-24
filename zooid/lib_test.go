package zooid

import "testing"

func TestDispatch_IgnoresInactiveInstances(t *testing.T) {
	instancesOnce.Do(func() {})

	instancesMux.Lock()
	instancesByHost = map[string]*Instance{
		"active.example.com": {
			Config: &Config{Host: "active.example.com"},
		},
		"inactive.example.com": {
			Config: &Config{Host: "inactive.example.com", Inactive: true},
		},
	}
	instancesMux.Unlock()

	if _, exists := Dispatch("active.example.com"); !exists {
		t.Fatal("expected active instance to be dispatched")
	}

	if _, exists := Dispatch("inactive.example.com"); exists {
		t.Fatal("expected inactive instance to not be dispatched")
	}
}
