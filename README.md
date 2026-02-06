# Zooid

This is a multi-tenant relay based on [Khatru](https://gitworkshop.dev/fiatjaf.com/nostrlib/tree/master/khatru) which implements a range of access controls. It's designed to be used with [Flotilla](https://flotilla.social) as a community relay (complete with NIP 29 support), but it can also be used outside of a community context.

## Architecture

A single zooid instance can run any number of "virtual" relays. The `config` directory can contain any number of configuration files, each of which represents a single virtual relay.

## Environment

Zooid supports a few environment variables, which configure shared resources like the web server or sqlite database.

- `PORT` - the port the server will listen on for all requests. Defaults to `3334`.
- `CONFIG` - where to store relay configuration files. Defaults to `./config`.
- `MEDIA` - where to store blossom media files. Defaults to `./media`.
- `DATA` - where to store databse files. Defaults to `./data`.

## Configuration

Configuration files are written using [toml](https://toml.io). Top level configuration options are required:

- `host` - a hostname to serve this relay on.
- `schema` - a string that identifies this relay. This cannot be changed, and must be usable as a sqlite identifier.
- `secret` - the nostr secret key of the relay. Will be used to populate the relay's NIP 11 `self` field and sign generated events.

### `[info]`

Contains information for populating the relay's `nip11` document.

- `name` - the name of your relay.
- `icon` - an icon for your relay.
- `pubkey` - the public key of the relay owner. Does not affect access controls.
- `description` - your relay's description.

### `[policy]`

Contains policy and access related configuration.

- `public_join` - whether to allow non-members to join the relay without an invite code. Defaults to `false`.
- `strip_signatures` - whether to remove signatures when serving events to non-admins. This requires clients/users to trust the relay to properly authenticate signatures. Be cautious about using this; a malicious relay will be able to execute all kinds of attacks, including potentially serving events unrelated to a community use case.

### `[groups]`

Configures NIP 29 support.

- `enabled` - whether NIP 29 is enabled.
- `auto_join` - whether relay members can join groups without approval. Defaults to `false`.

### `[management]`

Configures NIP 86 support.

- `enabled` - whether NIP 86 is enabled.
- `methods` - a list of [NIP 86](https://github.com/nostr-protocol/nips/blob/master/86.md) relay management methods enabled for this relay.

### `[blossom]`

Configures blossom support.

- `enabled` - whether blossom is enabled.

### `[roles]`

Defines roles that can be assigned to different users and attendant privileges. Each role is defined by a `[roles.{role_name}]` header and has the following options:

- `pubkeys` - a list of nostr pubkeys this role is assigned to.
- `can_invite` - a boolean indicating whether this role can invite new members to the relay by requesting a `kind 28935` claim. Defaults to `false`. See [access requests](https://github.com/nostr-protocol/nips/pull/1079) for more details.
- `can_manage` - a boolean indicating whether this role can use NIP 86 relay management and administer NIP 29 groups. Defaults to `false`.

A special `[roles.member]` heading may be used to configure policies for all relay users (that is, pubkeys assigned to other roles, or who have redeemed an invite code).

### Example

The below config file might be saved as `./config/my-relay.example.com` in order to route requests from `wss://my-relay.example.com` to this virtual relay.

```toml
host = "my-relay.example.com"
schema = "my_relay"
secret = "<hex private key>"

[info]
name = "My relay"
icon = "https://example.com/icon.png"
pubkey = "<hex public key>"
description = "A community relay for my friends"

[policy]
public_join = true
strip_signatures = false

[groups]
enabled = true
auto_join = false

[management]
enabled = true
methods = ["supportedmethods", "banpubkey", "allowpubkey"]

[blossom]
enabled = false

[roles.member]
can_invite = true

[roles.admin]
pubkeys = ["d9254d9898fd4728f7e2b32b87520221a50f6b8b97d935d7da2de8923988aa6d"]
can_manage = true
```

## Scripts

After running `just build`, a number of scripts will be available:

- `./bin/import` takes JSONL events on stdin and imports it into the given virtual relay

## Development

See `justfile` for defined commands.

## Deploying

Zooid can be run using an OCI container:

```sh
podman run -it \
  -p 3334:3334 \
  -v ./config:/app/config \
  -v ./media:/app/media \
  -v ./data:/app/data \
  ghcr.io/coracle-social/zooid
```

