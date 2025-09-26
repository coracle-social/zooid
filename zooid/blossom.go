package zooid

import (
	"bytes"
	"context"
	"io"
	"net/url"
	"os"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore"
	"fiatjaf.com/nostr/khatru/blossom"
	"github.com/spf13/afero"
)

type BlossomStore struct {
	Config *Config
	Schema *Schema
	Store  eventstore.Store
}

func (bl *BlossomStore) Init() error {
	dir := Env("DATA") + "/media"

	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Blossom uses a wrapped event store for metadata
	bl.Store = &EventStore{Schema: bl.Schema}

	if err := bl.Store.Init(); err != nil {
		return err
	}

	return nil
}

func (bl *BlossomStore) Enable(instance *Instance) {
	fs := afero.NewOsFs()
	dir := Env("DATA") + "/media"
	backend := blossom.New(instance.Relay, "https://"+instance.Host)

	backend.Store = blossom.EventStoreBlobIndexWrapper{
		Store:      bl.Store,
		ServiceURL: "https://" + instance.Host,
	}

	backend.StoreBlob = func(ctx context.Context, sha256 string, ext string, body []byte) error {
		file, err := fs.Create(dir + "/" + sha256)
		if err != nil {
			return err
		}

		if _, err := io.Copy(file, bytes.NewReader(body)); err != nil {
			return err
		}

		return nil
	}

	backend.LoadBlob = func(ctx context.Context, sha256 string, ext string) (io.ReadSeeker, *url.URL, error) {
		file, err := fs.Open(dir + "/" + sha256)
		if err != nil {
			return nil, nil, err
		}
		return file, nil, nil
	}

	backend.DeleteBlob = func(ctx context.Context, sha256 string, ext string) error {
		return fs.Remove(dir + "/" + sha256)
	}

	backend.RejectUpload = func(ctx context.Context, auth *nostr.Event, size int, ext string) (bool, string, int) {
		if size > 10*1024*1024 {
			return true, "file too large", 413
		}

		if auth == nil || !instance.HasAccess(auth.PubKey) {
			return true, "unauthorized", 403
		}

		return false, ext, size
	}

	backend.RejectGet = func(ctx context.Context, auth *nostr.Event, sha256 string, ext string) (bool, string, int) {
		if auth == nil || !instance.HasAccess(auth.PubKey) {
			return true, "unauthorized", 403
		}

		return false, "", 200
	}

	backend.RejectList = func(ctx context.Context, auth *nostr.Event, pubkey nostr.PubKey) (bool, string, int) {
		if auth == nil || !instance.HasAccess(auth.PubKey) {
			return true, "unauthorized", 403
		}

		return false, "", 200
	}

	backend.RejectDelete = func(ctx context.Context, auth *nostr.Event, sha256 string, ext string) (bool, string, int) {
		if auth == nil || !instance.HasAccess(auth.PubKey) {
			return true, "unauthorized", 403
		}

		return false, "", 200
	}
}
