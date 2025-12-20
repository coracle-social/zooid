package zooid

import (
	"bytes"
	"context"
	"io"

	"github.com/fiatjaf/eventstore"
	"github.com/fiatjaf/khatru/blossom"
	"github.com/nbd-wtf/go-nostr"
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

	backend.StoreBlob = append(backend.StoreBlob, func(ctx context.Context, sha256 string, ext string, body []byte) error {
		file, err := fs.Create(dir + "/" + sha256)
		if err != nil {
			return err
		}

		if _, err := io.Copy(file, bytes.NewReader(body)); err != nil {
			return err
		}

		return nil
	})

	backend.LoadBlob = append(backend.LoadBlob, func(ctx context.Context, sha256 string, ext string) (io.ReadSeeker, error) {
		file, err := fs.Open(dir + "/" + sha256)
		if err != nil {
			return nil, err
		}
		return file, nil
	})

	backend.DeleteBlob = append(backend.DeleteBlob, func(ctx context.Context, sha256 string, ext string) error {
		return fs.Remove(dir + "/" + sha256)
	})

	backend.RejectUpload = append(backend.RejectUpload, func(ctx context.Context, auth *nostr.Event, size int, ext string) (bool, string, int) {
		if size > 10*1024*1024 {
			return true, "file too large", 413
		}

		if auth == nil || !instance.Management.IsMember(auth.PubKey) {
			return true, "unauthorized", 403
		}

		return false, ext, size
	})

	backend.RejectGet = append(backend.RejectGet, func(ctx context.Context, auth *nostr.Event, sha256 string, ext string) (bool, string, int) {
		if auth == nil || !instance.Management.IsMember(auth.PubKey) {
			return true, "unauthorized", 403
		}

		return false, "", 200
	})

	backend.RejectList = append(backend.RejectList, func(ctx context.Context, auth *nostr.Event, pubkey string) (bool, string, int) {
		if auth == nil || !instance.Management.IsMember(auth.PubKey) {
			return true, "unauthorized", 403
		}

		return false, "", 200
	})

	backend.RejectDelete = append(backend.RejectDelete, func(ctx context.Context, auth *nostr.Event, sha256 string, ext string) (bool, string, int) {
		if auth == nil || !instance.Management.IsMember(auth.PubKey) {
			return true, "unauthorized", 403
		}

		return false, "", 200
	})
}
