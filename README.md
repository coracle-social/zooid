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

- `strip_signatures` - whether to remove signatures when serving events to non-admins. This requires clients/users to trust the relay to properly authenticate signatures. Be cautious about using this; a malicious relay will be able to execute all kinds of attacks, including potentially serving events unrelated to a community use case.

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
secret = "ce30b1831a4551f4cb7a984033c34ab96d8cf56ff50df9d0c27d9fa5422f2278"

[info]
name = "My relay"
icon = "https://example.com/icon.png"
pubkey = "d9254d9898fd4728f7e2b32b87520221a50f6b8b97d935d7da2de8923988aa6d"
description = "A community relay for my friends"

[policy]
strip_signatures = false

[groups]
enabled = true
auto_join = false
auto_leave = true

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

## Deploying on ubuntu

```sh
# Add a user
adduser zooid

# Install system dependencies
sudo apt update
apt install nginx git certbot python3-certbot-nginx sqlite3 gcc

# Install go and add it to path
wget -qO- https://go.dev/dl/go1.25.1.linux-amd64.tar.gz | sudo tar -C /usr/local -xzf -
echo 'export PATH=$PATH:/usr/local/go/bin' >> /etc/profile

# Log in as your user
su --login zooid

# Clone the repository and build
git clone https://github.com/coracle-social/zooid.git ~/zooid && cd ~/zooid
CGO_ENABLED=1 go build -o bin/zooid cmd/relay/main.go

# Back to root
exit

# Add a service file - edit if needed
cp /home/zooid/zooid/zooid.service /etc/systemd/system/zooid.service

# Start the service
systemctl enable zooid
service zooid start

# Set up a SSL certificate
# Use another method or --manual-auth-hook and --manual-cleanup-hook to automate renewal
certbot certonly --manual -d '*.yourdomain.com' --preferred-challenges=dns

# Set up nginx - be sure to edit the server_name to your domain
cp /home/zooid/zooid/nginx.conf /etc/nginx/sites-available/zooid.conf
ln -s /etc/nginx/sites-{available,enabled}/zooid.conf

# Enable the site and restart nginx
service nginx restart
```

## Deploying via container

Zooid can be run using an OCI container:

```sh
podman run -it \
  -p 3334:3334 \
  -v ./config:/tmp/config \
  -v ./media:/tmp/media \
  -v ./data:/tmp/data \
  ghcr.io/coracle-social/zooid
```

