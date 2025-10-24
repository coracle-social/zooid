package zooid

import (
	"bytes"
	"context"
	"io"
	"net/url"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore"
	"fiatjaf.com/nostr/khatru/blossom"
	"github.com/spf13/afero"
)

type BlossomStore struct {
	Config *Config
	Events eventstore.Store
}

func (bl *BlossomStore) Enable(instance *Instance) {
	dir := Env("MEDIA")
	fs := afero.NewOsFs()
	backend := blossom.New(instance.Relay, "https://"+bl.Config.Host)

	backend.Store = blossom.EventStoreBlobIndexWrapper{
		Store:      bl.Events,
		ServiceURL: "https://" + bl.Config.Host,
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

		if auth == nil || !instance.Management.IsMember(auth.PubKey) {
			return true, "unauthorized", 403
		}

		return false, ext, size
	}

	backend.RejectGet = func(ctx context.Context, auth *nostr.Event, sha256 string, ext string) (bool, string, int) {
		if auth == nil || !instance.Management.IsMember(auth.PubKey) {
			return true, "unauthorized", 403
		}

		return false, "", 200
	}

	backend.RejectList = func(ctx context.Context, auth *nostr.Event, pubkey nostr.PubKey) (bool, string, int) {
		if auth == nil || !instance.Management.IsMember(auth.PubKey) {
			return true, "unauthorized", 403
		}

		return false, "", 200
	}

	backend.RejectDelete = func(ctx context.Context, auth *nostr.Event, sha256 string, ext string) (bool, string, int) {
		if auth == nil || !instance.Management.IsMember(auth.PubKey) {
			return true, "unauthorized", 403
		}

		return false, "", 200
	}
}
