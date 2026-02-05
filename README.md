# Zooid (Unicity Fork)

This is a fork of [Zooid](https://github.com/coracle-social/zooid), a multi-tenant relay based on [Khatru](https://gitworkshop.dev/fiatjaf.com/nostrlib/tree/master/khatru) which implements a range of access controls. This fork is customized for use with [Unicity Sphere](https://github.com/unicitylabs/sphere) for NIP-29 group chat functionality.

## Unicity Fork Modifications

This fork includes the following changes from upstream Zooid:

### Open Policy for Public Groups

Modified `groups.go` to allow authenticated users to read messages from public groups with open policy, without requiring explicit group membership:

```go
// In CanRead function - allows public group access
if g.Config.Policy.Open && !HasTag(meta.Tags, "private") {
    return true
}
```

This enables the "browse and join" workflow where users can discover public groups, view their messages, and then join if interested.

### Configuration for Sphere

The relay is configured with:
- `public_join = true` — allows anyone to join without invite
- `groups.enabled = true` — enables NIP-29 support
- `groups.auto_join = true` — members can join groups without approval
- Open policy for public group message access

## Original Zooid Documentation

---

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

## Development

See `justfile` for defined commands.

### Running Tests

**Unit tests:**
```bash
just test
# or directly:
go test -v ./...
```

**BDD tests (Cucumber/Gherkin scenarios):**
```bash
just test-bdd
# or directly:
go test -v ./zooid -run TestFeatures
```

The BDD tests use [godog](https://github.com/cucumber/godog) and cover:
- Event storage and querying
- Group lifecycle, membership, and access control
- Role-based permissions
- Blossom media handling
- Key-value store operations
- Multi-instance dispatch
- NIP-86 management API
- Special events (zaps, gift wraps)

Feature files are located in `zooid/features/`.

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

## Running with Unicity Sphere

For local development with Sphere, use Docker Compose:

```bash
cd /path/to/groupchat  # Contains docker-compose.yml and config/
docker compose up -d
```

This starts Zooid on `ws://localhost:3334` with the pre-configured `localhost` relay.

### Default Configuration

The `config/localhost` file provides a development configuration:

```toml
host = "localhost"
schema = "localhost"
secret = "<relay-private-key>"

[info]
name = "Localhost Relay"
description = "Local development NIP-29 relay for Sphere"

[policy]
public_join = true

[groups]
enabled = true
auto_join = true

[roles.member]
can_invite = true
```

### Verifying the Relay

Check relay status:
```bash
curl http://localhost:3334
```

View logs:
```bash
docker compose logs -f zooid
```

## CI/CD

### GitHub Actions Workflows

This fork includes automated CI/CD:

**Build and Push (`docker-build.yml`):**
- Triggers on push to main/master or tags
- Builds Docker image
- Pushes to GitHub Container Registry (`ghcr.io/unicitylabs/zooid`)
- Tags: `latest`, `sha-<commit>`, semver tags

**Deploy to AWS (`deploy-aws.yml`):**
- Triggers after successful build or manual dispatch
- Forces new ECS deployment
- Waits for service stability

### Required Secrets

Add these to GitHub repository secrets for AWS deployment:
- `AWS_ACCESS_KEY_ID`
- `AWS_SECRET_ACCESS_KEY`

### Manual Deployment

```bash
# Force new deployment
aws ecs update-service \
    --cluster sphere-zooid-relay-cluster \
    --service sphere-zooid-relay-zooid-relay \
    --force-new-deployment \
    --region me-central-1
```

## AWS Infrastructure

See `/Users/pavelg/work/unicity/sphere-infra/aws/` for CloudFormation templates and deployment scripts.

### Environment Variables

When running in AWS ECS, these environment variables configure the relay:

| Variable | Description |
|----------|-------------|
| `RELAY_HOST` | Domain name (e.g., `sphere-relay.unicity.network`) |
| `RELAY_SECRET` | Nostr private key (64-char hex) |
| `RELAY_NAME` | Display name |
| `RELAY_DESCRIPTION` | Description |
| `ADMIN_PUBKEYS` | Admin pubkeys (quoted, comma-separated) |

