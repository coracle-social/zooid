# Zooid

This is a multi-tenant relay based on [Khatru](https://gitworkshop.dev/fiatjaf.com/nostrlib/tree/master/khatru) which implements a range of access controls. It's designed to be used with [Flotilla](https://flotilla.social) as a community relay (complete with NIP 29 support), but it can also be used outside of a community context.

## Architecture

A single zooid instance can run any number of "virtual" relays. The `config` directory can contain any number of configuration files, each of which represents a single virtual relay.

## Configuration

Configuration files are written using [toml](https://toml.io). The name of the configuration file should be the hostname the relay serves, for example `relay.example.com`. Config files contain the following sections:

### `[self]`

Contains information for populating the relay's `nip11` document.

Required:

- `secret` - the nostr secret key of the relay. Will be used to populate the relay's NIP 11 `self` field and sign generated events.

Optional:

- `name` - the name of your relay.
- `icon` - an icon for your relay.
- `pubkey` - the public key of the relay owner. Does not affect access controls.
- `description` - your relay's description.

### `[groups]`

Configures NIP 29 support.

- `enabled` - whether NIP 29 is enabled.
- `auto_join` - whether relay members can join `open` groups without approval. Defaults to `true`.
- `auto_leave` - whether relay members can leave groups without approval. Defaults to `true`.

### `[management]`

Configures NIP 86 support.

- `enabled` - whether NIP 86 is enabled.
- `methods` - a list of [NIP 86](https://github.com/nostr-protocol/nips/blob/master/86.md) relay management methods enabled for this relay.

### `[blossom]`

Configures blossom support.

- `enabled` - whether blossom is enabled.
- `directory` - where to store files. Defaults to `./data/{my-relay}/media`.

### `[roles]`

Defines roles that can be assigned to different users and attendant privileges. Each role is defined by a `[roles.{role_name}]` header and has the following options:

- `pubkeys` - a list of nostr pubkeys this role is assigned to.
- `can_invite` - a boolean indicating whether this role can invite new members to the relay by requesting a `kind 28935` claim. Defaults to `false`. See [access requests](https://github.com/nostr-protocol/nips/pull/1079) for more details.

A special `[roles.member]` heading may be used to configure policies for all relay users (that is, pubkeys assigned to other roles, or who have redeemed an invite code).

### `[data]`

Contains information related to data persistence.

- `events` - the location of the sqlite database file used to store events. Defaults to `./data/{my-relay}/events`.
- `media` - the location of the sqlite database file used to store file metadata. Defaults to `./data/{my-relay}/media`.

### Example

The below config file might be saved as `./config/my-relay.example.com` in order to route requests from `wss://my-relay.example.com` to this virtual relay.

```toml
[self]
name = "My relay"
secret = "ce30b1831a4551f4cb7a984033c34ab96d8cf56ff50df9d0c27d9fa5422f2278"

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

[data]
events = "./data/my-relay/events"
```

## Development

See `justfile` for defined commands.
