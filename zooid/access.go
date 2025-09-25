package zooid

import (
	"fmt"
	"time"

	"fiatjaf.com/nostr"
	"github.com/Masterminds/squirrel"
)

type Invite struct {
	ID        string
	CreatedAt int
	Pubkey    nostr.PubKey
	Claim     string
}

type Redemption struct {
	ID        string
	InviteID  string
	CreatedAt int
	Pubkey    nostr.PubKey
}

type AccessStore struct {
	Config *Config
	Schema *Schema
}

func (access *AccessStore) Init() error {
	schema := access.Schema.Render(`
	CREATE TABLE IF NOT EXISTS {{.Prefix}}__invites (
		id TEXT PRIMARY KEY,
		created_at INTEGER NOT NULL,
		pubkey TEXT NOT NULL,
		claim TEXT NOT NULL
	);

	CREATE INDEX IF NOT EXISTS {{.Prefix}}__idx_invites_created_at ON {{.Prefix}}__invites(created_at);
	CREATE INDEX IF NOT EXISTS {{.Prefix}}__idx_invites_pubkey ON {{.Prefix}}__invites(pubkey);
	CREATE INDEX IF NOT EXISTS {{.Prefix}}__idx_invites_claim ON {{.Prefix}}__invites(claim);

	CREATE TABLE IF NOT EXISTS {{.Prefix}}__redemptions (
		id TEXT PRIMARY KEY,
		invite_id TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		pubkey TEXT NOT NULL,
		FOREIGN KEY (invite_id) REFERENCES {{.Prefix}}__invites(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS {{.Prefix}}__idx_redemptions_invite_id ON {{.Prefix}}__redemptions(invite_id);
	CREATE INDEX IF NOT EXISTS {{.Prefix}}__idx_redemptions_created_at ON {{.Prefix}}__redemptions(created_at);
	CREATE INDEX IF NOT EXISTS {{.Prefix}}__idx_redemptions_pubkey ON {{.Prefix}}__redemptions(pubkey);
	`)

	if _, err := GetDb().Exec(schema); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	return nil
}

// Invite utils

func (access *AccessStore) SelectInvites() squirrel.SelectBuilder {
	return squirrel.Select("id", "created_at", "pubkey", "claim").From(access.Schema.Prefix("invites"))
}

func (access *AccessStore) QueryInvites(builder squirrel.SelectBuilder) []Invite {
	rows, err := builder.RunWith(GetDb()).Query()
	if err != nil {
		return []Invite{}
	}
	defer rows.Close()

	var invites []Invite
	for rows.Next() {
		var invite Invite
		var pubkeyStr string
		err := rows.Scan(&invite.ID, &invite.CreatedAt, &pubkeyStr, &invite.Claim)
		if err != nil {
			continue
		}

		if pubkey, err := nostr.PubKeyFromHex(pubkeyStr); err == nil {
			invite.Pubkey = pubkey
		} else {
			continue
		}

		invites = append(invites, invite)
	}

	return invites
}

func (access *AccessStore) AddInvite(pubkey nostr.PubKey, claim string) error {
	id := RandomString(32)
	createdAt := int(time.Now().Unix())

	insertQb := squirrel.Insert(access.Schema.Prefix("invites")).
		Columns("id", "created_at", "pubkey", "claim").
		Values(id, createdAt, pubkey.Hex(), claim)

	_, err := insertQb.RunWith(GetDb()).Exec()
	if err != nil {
		return fmt.Errorf("failed to add invite: %w", err)
	}

	return nil
}

func (access *AccessStore) GetInvitesByClaim(claim string) []Invite {
	return access.QueryInvites(access.SelectInvites().Where(squirrel.Eq{"claim": claim}))
}

func (access *AccessStore) GetInvitesByPubkey(pubkey nostr.PubKey) []Invite {
	return access.QueryInvites(access.SelectInvites().Where(squirrel.Eq{"pubkey": pubkey.Hex()}))
}

// Redemption utils

func (access *AccessStore) SelectRedemptions() squirrel.SelectBuilder {
	return squirrel.Select("id", "invite_id", "created_at", "pubkey").From(access.Schema.Prefix("redemptions"))
}

func (access *AccessStore) QueryRedemptions(builder squirrel.SelectBuilder) []Redemption {
	rows, err := builder.RunWith(GetDb()).Query()
	if err != nil {
		return []Redemption{}
	}
	defer rows.Close()

	var redemptions []Redemption
	for rows.Next() {
		var redemption Redemption
		var pubkeyStr string

		err := rows.Scan(&redemption.ID, &redemption.InviteID, &redemption.CreatedAt, &pubkeyStr)
		if err != nil {
			continue
		}

		if pubkey, err := nostr.PubKeyFromHex(pubkeyStr); err == nil {
			redemption.Pubkey = pubkey
		} else {
			continue
		}

		redemptions = append(redemptions, redemption)
	}

	return redemptions
}

func (access *AccessStore) AddRedemption(pubkey nostr.PubKey, invite Invite) error {
	id := RandomString(32)
	createdAt := int(time.Now().Unix())

	insertQb := squirrel.Insert(access.Schema.Prefix("redemptions")).
		Columns("id", "invite_id", "created_at", "pubkey").
		Values(id, invite.ID, createdAt, pubkey.Hex())

	_, err := insertQb.RunWith(GetDb()).Exec()
	if err != nil {
		return fmt.Errorf("failed to add invite: %w", err)
	}

	return nil
}

func (access *AccessStore) GetRedemptionsByPubkey(pubkey nostr.PubKey) []Invite {
	return access.QueryInvites(access.SelectRedemptions().Where(squirrel.Eq{"pubkey": pubkey.Hex()}))
}
