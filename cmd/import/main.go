package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore"
	"zooid/zooid"
)

type ValidationError struct {
	Line      int
	EventID   string
	Signature bool
	Shape     bool
	Message   string
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	var (
		relay = flag.String("relay", "", "Relay name (required)")
		reset  = flag.Bool("reset", false, "Delete all events from the store before importing")
		force  = flag.Bool("force", false, "Skip validation prompts and import valid events only")
	)
	flag.Parse()

	if *relay == "" {
		fmt.Fprintln(os.Stderr, "Error: --relay is required")
		flag.Usage()
		os.Exit(1)
	}

	// Load config for the specified relay
	filename := fmt.Sprintf("%s.toml", *relay)
	config, err := zooid.LoadConfig(filename)
	if err != nil {
		fmt.Fprintln(os.Stderr, "No such config file", filename)
		os.Exit(1)
	}

	// Create event store
	events := &zooid.EventStore{
		Config: config,
		Schema: &zooid.Schema{Name: config.Schema},
	}

	// Initialize the event store (creates tables if needed)
	if err := events.Init(); err != nil {
		log.Fatalf("Failed to initialize event store: %v", err)
	}

	// Reset if requested
	if *reset {
		if err := resetEventStore(events); err != nil {
			log.Fatalf("Failed to reset event store: %v", err)
		}
		fmt.Fprintln(os.Stderr, "Event store reset complete")
	}

	// Read and process events from stdin
	var (
		validationErrors []ValidationError
		validEvents      []nostr.Event
		lineNum          int
	)

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var event nostr.Event
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			validationErrors = append(validationErrors, ValidationError{
				Line:    lineNum,
				Message: fmt.Sprintf("JSON parse error: %v", err),
			})
			continue
		}

		// Validate event
		sigValid, shapeValid, errMsg := validateEvent(event)

		if !sigValid || !shapeValid {
			validationErrors = append(validationErrors, ValidationError{
				Line:      lineNum,
				EventID:   event.ID.Hex(),
				Signature: sigValid,
				Shape:     shapeValid,
				Message:   errMsg,
			})
			continue
		}

		validEvents = append(validEvents, event)
	}

	if err := scanner.Err(); err != nil {
		log.Fatalf("Error reading stdin: %v", err)
	}

	// Handle validation results
	if len(validationErrors) > 0 {
		printValidationSummary(validationErrors, len(validEvents))

		if !*force {
			// Ask user if they want to continue
			fmt.Fprint(os.Stderr, "\nDo you want to continue importing valid events? [y/N]: ")
			var response string
			fmt.Scanln(&response)
			response = strings.ToLower(strings.TrimSpace(response))
			if response != "y" && response != "yes" {
				fmt.Fprintln(os.Stderr, "Import cancelled")
				os.Exit(0)
			}
		} else {
			fmt.Fprintf(os.Stderr, "\nForce mode: skipping %d invalid events\n", len(validationErrors))
		}
	}

	// Import valid events
	imported := 0
	skipped := 0
	for _, event := range validEvents {
		if err := events.StoreEvent(event); err != nil {
			if errors.Is(err, eventstore.ErrDupEvent) {
				skipped++
			} else {
				log.Printf("Failed to import event %s: %v", event.ID.Hex(), err)
			}
			continue
		}
		imported++
	}

	fmt.Fprintf(os.Stderr, "\nImport complete: %d imported, %d skipped (duplicates)\n", imported, skipped)
}

func validateEvent(event nostr.Event) (sigValid bool, shapeValid bool, errMsg string) {
	var zeroID nostr.ID
	var zeroPubKey nostr.PubKey
	var zeroSig [64]byte

	// Check shape validation (required fields)
	if event.ID == zeroID {
		return false, false, "missing event ID"
	}
	if event.PubKey == zeroPubKey {
		return false, false, "missing pubkey"
	}
	if event.Sig == zeroSig {
		return false, false, "missing signature"
	}
	if event.CreatedAt == 0 {
		return false, false, "missing created_at"
	}

	// Verify signature
	if !event.VerifySignature() {
		return false, true, "invalid signature"
	}

	return true, true, ""
}

func printValidationSummary(errors []ValidationError, validCount int) {
	fmt.Fprintf(os.Stderr, "\n=== Validation Summary ===\n")
	fmt.Fprintf(os.Stderr, "Valid events: %d\n", validCount)
	fmt.Fprintf(os.Stderr, "Invalid events: %d\n\n", len(errors))

	sigErrors := 0
	shapeErrors := 0
	parseErrors := 0

	for _, err := range errors {
		if err.Line > 0 && err.EventID == "" {
			parseErrors++
			fmt.Fprintf(os.Stderr, "Line %d: %s\n", err.Line, err.Message)
		} else if !err.Signature {
			sigErrors++
			fmt.Fprintf(os.Stderr, "Line %d (ID: %s): Invalid signature\n", err.Line, err.EventID)
		} else if !err.Shape {
			shapeErrors++
			fmt.Fprintf(os.Stderr, "Line %d (ID: %s): Invalid shape - %s\n", err.Line, err.EventID, err.Message)
		}
	}

	fmt.Fprintf(os.Stderr, "\nBreakdown:\n")
	fmt.Fprintf(os.Stderr, "  - Signature validation errors: %d\n", sigErrors)
	fmt.Fprintf(os.Stderr, "  - Shape validation errors: %d\n", shapeErrors)
	fmt.Fprintf(os.Stderr, "  - Parse errors: %d\n", parseErrors)
}

func resetEventStore(events *zooid.EventStore) error {
	db := zooid.GetDb()

	// Delete all events and event_tags for this relay
	tables := []string{
		events.Schema.Prefix("event_tags"),
		events.Schema.Prefix("events"),
	}

	for _, table := range tables {
		_, err := db.Exec("DELETE FROM " + table)
		if err != nil {
			return fmt.Errorf("failed to delete from %s: %w", table, err)
		}
	}

	return nil
}
