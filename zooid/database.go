package zooid

import (
	"database/sql"
	"log"
	"sync"
)

var (
	db     *sql.DB
	dbOnce sync.Once
)

func GetDb() *sql.DB {
	dbOnce.Do(func() {
		newDb, err := sql.Open("sqlite3", Env("DATA")+"/db?_journal_mode=WAL&_sync=NORMAL&_cache_size=1000&_foreign_keys=true")

		if err != nil {
			log.Fatal("Failed to open database: %w", err)
		}

		db = newDb
	})

	return db
}
