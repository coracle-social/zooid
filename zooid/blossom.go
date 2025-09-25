package zooid

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/url"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/khatru/blossom"
	"github.com/gosimple/slug"
	"github.com/spf13/afero"
)

func EnableBlossom(instance *Instance) {
	fs := afero.NewOsFs()

	if err := fs.MkdirAll(Env("DATA"), 0755); err != nil {
		log.Fatal("🚫 error creating blossom path:", err)
	}

	store := &EventStore{
		Schema: &Schema{
			Name: slug.Make(config.Self.Schema) + "__blossom",
		},
	}

	backend := blossom.New(instance.Relay, "https://"+instance.Host)

	backend.Store = blossom.EventStoreBlobIndexWrapper{
		Store:      store,
		ServiceURL: "https://" + instance.Host,
	}

	backend.StoreBlob = func(ctx context.Context, sha256 string, ext string, body []byte) error {
		file, err := fs.Create(instance.Config.Blossom.Directory + "/" + sha256)
		if err != nil {
			return err
		}

		if _, err := io.Copy(file, bytes.NewReader(body)); err != nil {
			return err
		}

		return nil
	}

	backend.LoadBlob = func(ctx context.Context, sha256 string, ext string) (io.ReadSeeker, *url.URL, error) {
		file, err := fs.Open(instance.Config.Blossom.Directory + "/" + sha256)
		if err != nil {
			return nil, nil, err
		}
		return file, nil, nil
	}

	backend.DeleteBlob = func(ctx context.Context, sha256 string, ext string) error {
		return fs.Remove(instance.Config.Blossom.Directory + "/" + sha256)
	}

	backend.RejectUpload = func(ctx context.Context, auth *nostr.Event, size int, ext string) (bool, string, int) {
		if size > 10*1024*1024 {
			return true, "file too large", 413
		}

		if auth == nil || !instance.IsMember(auth.PubKey) {
			return true, "unauthorized", 403
		}

		return false, ext, size
	}

	backend.RejectGet = func(ctx context.Context, auth *nostr.Event, sha256 string, ext string) (bool, string, int) {
		if auth == nil || !instance.IsMember(auth.PubKey) {
			return true, "unauthorized", 403
		}

		return false, "", 200
	}

	backend.RejectList = func(ctx context.Context, auth *nostr.Event, pubkey nostr.PubKey) (bool, string, int) {
		if auth == nil || !instance.IsMember(auth.PubKey) {
			return true, "unauthorized", 403
		}

		return false, "", 200
	}

	backend.RejectDelete = func(ctx context.Context, auth *nostr.Event, sha256 string, ext string) (bool, string, int) {
		if auth == nil || !instance.IsMember(auth.PubKey) {
			return true, "unauthorized", 403
		}

		return false, "", 200
	}
	if err := store.Init(); err != nil {
		panic(err)
	}
}
