package zooid

import (
	"testing"
)

func TestSchema_Render(t *testing.T) {
	schema := Schema{Name: "test_db"}
	result := schema.Render("CREATE TABLE {{.Name}}_events")
	expected := "CREATE TABLE test_db_events"

	if result != expected {
		t.Errorf("Schema.Render() = %q, expected %q", result, expected)
	}
}

func TestSchema_Prefix(t *testing.T) {
	schema := Schema{Name: "test_db"}
	result := schema.Prefix("events")
	expected := "test_db__events"

	if result != expected {
		t.Errorf("Schema.Prefix() = %q, expected %q", result, expected)
	}
}
