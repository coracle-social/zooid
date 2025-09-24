package zooid

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/url"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore/lmdb"
	"fiatjaf.com/nostr/khatru/blossom"
	"github.com/spf13/afero"
)

func EnableBlossom(instance *Instance) {
	fs := afero.NewOsFs()

	if err := fs.MkdirAll(instance.Config.Blossom.Directory, 0755); err != nil {
		log.Fatal("🚫 error creating blossom path:", err)
	}

	backend := &lmdb.LMDBBackend{Path: instance.Config.Data.Blossom}
	if err := backend.Init(); err != nil {
		panic(err)
	}

	blossom := blossom.New(instance.Relay, "https://"+instance.Host)

	blossom.Store = backend

	blossom.StoreBlob = func(ctx context.Context, sha256 string, ext string, body []byte) error {
		file, err := fs.Create(instance.Config.Blossom.Directory + "/" + sha256)
		if err != nil {
			return err
		}

		if _, err := io.Copy(file, bytes.NewReader(body)); err != nil {
			return err
		}

		return nil
	}

	blossom.LoadBlob = func(ctx context.Context, sha256 string, ext string) (io.ReadSeeker, *url.URL, error) {
		file, err := fs.Open(instance.Config.Blossom.Directory + "/" + sha256)
		if err != nil {
			return nil, nil, err
		}
		return file, nil, nil
	}

	blossom.DeleteBlob = func(ctx context.Context, sha256 string, ext string) error {
		return fs.Remove(instance.Config.Blossom.Directory + "/" + sha256)
	}

	blossom.RejectUpload = func(ctx context.Context, auth *nostr.Event, size int, ext string) (bool, string, int) {
		if size > 10*1024*1024 {
			return true, "file too large", 413
		}

		if auth == nil || !instance.IsMember(auth.PubKey) {
			return true, "unauthorized", 403
		}

		return false, ext, size
	}

	blossom.RejectGet = func(ctx context.Context, auth *nostr.Event, sha256 string, ext string) (bool, string, int) {
		if auth == nil || !instance.IsMember(auth.PubKey) {
			return true, "unauthorized", 403
		}

		return false, "", 200
	}

	blossom.RejectList = func(ctx context.Context, auth *nostr.Event, pubkey nostr.PubKey) (bool, string, int) {
		if auth == nil || !instance.IsMember(auth.PubKey) {
			return true, "unauthorized", 403
		}

		return false, "", 200
	}

	blossom.RejectDelete = func(ctx context.Context, auth *nostr.Event, sha256 string, ext string) (bool, string, int) {
		if auth == nil || !instance.IsMember(auth.PubKey) {
			return true, "unauthorized", 403
		}

		return false, "", 200
	}
}
