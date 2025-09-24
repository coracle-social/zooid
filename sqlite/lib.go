package sqlite

import (
	"bytes"
	"database/sql"
	"fmt"
	"text/template"

	"fiatjaf.com/nostr/eventstore"
	_ "github.com/mattn/go-sqlite3"
)

var _ eventstore.Store = (*SqliteBackend)(nil)

type SqliteBackend struct {
	db           *sql.DB
	Path         string
	Prefix       string
	FTSAvailable bool
}

func (s *SqliteBackend) Close() {
	if s.db != nil {
		s.db.Close()
	}
}

func (s *SqliteBackend) Init() error {
	if s.Path == "" {
		return fmt.Errorf("missing Path")
	}

	var err error
	s.db, err = sql.Open("sqlite3", s.Path+"?_journal_mode=WAL&_sync=NORMAL&_cache_size=1000&_foreign_keys=true")
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	// Create tables and indexes
	if err := s.createSchema(); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	return nil
}

func (s *SqliteBackend) tmpl(t string) string {
	var buf bytes.Buffer
	err := template.Must(template.New("schema").Parse(t)).Execute(&buf, s)
	if err != nil {
		panic(err)
	}

	return buf.String()
}

func (s *SqliteBackend) createSchema() error {
	// Create basic schema first
	basicSchema := s.tmpl(`
	CREATE TABLE IF NOT EXISTS {{.Prefix}}events (
		id TEXT PRIMARY KEY,
		created_at INTEGER NOT NULL,
		kind INTEGER NOT NULL,
		pubkey TEXT NOT NULL,
		content TEXT NOT NULL,
		tags TEXT NOT NULL,
		sig TEXT NOT NULL
	);

	CREATE INDEX IF NOT EXISTS {{.Prefix}}idx_events_created_at ON {{.Prefix}}events(created_at);
	CREATE INDEX IF NOT EXISTS {{.Prefix}}idx_events_kind ON {{.Prefix}}events(kind);
	CREATE INDEX IF NOT EXISTS {{.Prefix}}idx_events_pubkey ON {{.Prefix}}events(pubkey);
	CREATE INDEX IF NOT EXISTS {{.Prefix}}idx_events_kind_pubkey ON {{.Prefix}}events(kind, pubkey);
	CREATE INDEX IF NOT EXISTS {{.Prefix}}idx_events_kind_pubkey_created_at ON {{.Prefix}}events(kind, pubkey, created_at DESC);

	CREATE TABLE IF NOT EXISTS {{.Prefix}}event_tags (
		event_id TEXT NOT NULL,
		key TEXT NOT NULL,
		value TEXT NOT NULL,
		FOREIGN KEY (event_id) REFERENCES {{.Prefix}}events(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS {{.Prefix}}idx_event_tags_event_id ON {{.Prefix}}event_tags(event_id);
	CREATE INDEX IF NOT EXISTS {{.Prefix}}idx_event_tags_key ON {{.Prefix}}event_tags(key);
	CREATE INDEX IF NOT EXISTS {{.Prefix}}idx_event_tags_key_value ON {{.Prefix}}event_tags(key, value);
	`)

	if _, err := s.db.Exec(basicSchema); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	// Try to create FTS5 schema - if it fails, continue without it
	ftsSchema := `
	CREATE VIRTUAL TABLE IF NOT EXISTS {{.Prefix}}events_fts USING fts5(
		content,
		content='{{.Prefix}}events',
		content_rowid='rowid'
	);

	CREATE TRIGGER IF NOT EXISTS {{.Prefix}}events_ai AFTER INSERT ON {{.Prefix}}events BEGIN
		INSERT INTO {{.Prefix}}events_fts(rowid, content) VALUES (new.rowid, new.content);
	END;

	CREATE TRIGGER IF NOT EXISTS {{.Prefix}}events_ad AFTER DELETE ON {{.Prefix}}events BEGIN
		INSERT INTO {{.Prefix}}events_fts({{.Prefix}}events_fts, rowid, content)
		VALUES('delete', old.rowid, old.content);
	END;

	CREATE TRIGGER IF NOT EXISTS {{.Prefix}}events_au AFTER UPDATE ON {{.Prefix}}events BEGIN
		INSERT INTO {{.Prefix}}events_fts({{.Prefix}}events_fts, rowid, content)
		VALUES('delete', old.rowid, old.content);
		INSERT INTO {{.Prefix}}events_fts(rowid, content)
		VALUES (new.rowid, new.content);
	END;
	`

	if _, err := s.db.Exec(ftsSchema); err != nil {
		// FTS5 not available, continue without full-text search
		s.FTSAvailable = false
	} else {
		s.FTSAvailable = true
	}

	return nil
}
