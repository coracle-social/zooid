package zooid

import (
	"fmt"
	"github.com/Masterminds/squirrel"
	"log"
	"sync"
)

var (
	kv     *KeyValueStore
	kvOnce sync.Once
)

type KeyValueStore struct{}

func GetKeyValueStore() *KeyValueStore {
	dbOnce.Do(func() {
		kv = &KeyValueStore{}
		kv.Migrate()
	})

	return kv
}

func (kv *KeyValueStore) Migrate() {
	sql := `
	CREATE TABLE IF NOT EXISTS kv (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_kv_key ON kv(key);
	`

	if _, err := GetDb().Exec(sql); err != nil {
		log.Fatal("failed to migrate database: %w", err)
	}
}

func (kv *KeyValueStore) Get(key string) (string, error) {
	rows, err := squirrel.Select("value").
		From("kv").
		Where(squirrel.Eq{"key": key}).
		RunWith(GetDb()).
		Query()

	if err != nil {
		return "", err
	}

	defer rows.Close()

	for rows.Next() {
		var value string

		err := rows.Scan(&value)
		if err != nil {
			return "", err
		}

		return value, nil
	}

	return "", fmt.Errorf("%s not found", key)
}

func (kv *KeyValueStore) Set(key string, value string) error {
	_, err := squirrel.Insert("kv").
		Columns("key", "value").
		Values(key, value).
		Suffix("ON CONFLICT(key) DO UPDATE SET value = excluded.value").
		RunWith(GetDb()).
		Exec()

	return err
}

// Namespaced kv

type KV struct {
	Name string
}

func (kv *KV) Key(key string) string {
	return fmt.Sprintf("%s:%s", kv.Name, key)
}

func (kv *KV) Get(key string) (string, error) {
	return GetKeyValueStore().Get(kv.Key(key))
}

func (kv *KV) Set(key string, value string) error {
	return GetKeyValueStore().Set(kv.Key(key), value)
}
