#!/bin/sh
set -e

# Zooid NIP-29 Relay Entrypoint
# Generates config from environment variables if not present

CONFIG_DIR="${CONFIG:-/app/config}"
DATA_DIR="${DATA:-/app/data}"
MEDIA_DIR="${MEDIA:-/app/media}"

# Default values
RELAY_HOST="${RELAY_HOST:-localhost}"
RELAY_SCHEMA="${RELAY_SCHEMA:-sphere_relay}"
RELAY_SECRET="${RELAY_SECRET:-}"
RELAY_NAME="${RELAY_NAME:-Sphere Relay}"
RELAY_DESCRIPTION="${RELAY_DESCRIPTION:-NIP-29 Group Chat Relay for Unicity Sphere}"
RELAY_PUBKEY="${RELAY_PUBKEY:-}"
ADMIN_PUBKEYS="${ADMIN_PUBKEYS:-}"
GROUPS_ADMIN_CREATE_ONLY="${GROUPS_ADMIN_CREATE_ONLY:-true}"
GROUPS_PRIVATE_ADMIN_ONLY="${GROUPS_PRIVATE_ADMIN_ONLY:-true}"

# Create directories
mkdir -p "$CONFIG_DIR" "$DATA_DIR" "$MEDIA_DIR"

# Generate config file if it doesn't exist
CONFIG_FILE="$CONFIG_DIR/$RELAY_HOST"

if [ ! -f "$CONFIG_FILE" ]; then
    echo "Generating config for host: $RELAY_HOST"

    if [ -z "$RELAY_SECRET" ]; then
        echo "ERROR: RELAY_SECRET environment variable is required"
        exit 1
    fi

    cat > "$CONFIG_FILE" << EOF
# Auto-generated Zooid configuration
host = "$RELAY_HOST"
schema = "$RELAY_SCHEMA"
secret = "$RELAY_SECRET"

[info]
name = "$RELAY_NAME"
description = "$RELAY_DESCRIPTION"
EOF

    if [ -n "$RELAY_PUBKEY" ]; then
        echo "pubkey = \"$RELAY_PUBKEY\"" >> "$CONFIG_FILE"
    fi

    cat >> "$CONFIG_FILE" << EOF

[policy]
open = true
public_join = true
strip_signatures = false

[groups]
enabled = true
auto_join = true
admin_create_only = $GROUPS_ADMIN_CREATE_ONLY
private_admin_only = $GROUPS_PRIVATE_ADMIN_ONLY
EOF

    # Add admin role if pubkeys provided
    if [ -n "$ADMIN_PUBKEYS" ]; then
        cat >> "$CONFIG_FILE" << EOF

[roles.admin]
pubkeys = [$ADMIN_PUBKEYS]
can_invite = true
can_manage = true
EOF
    fi

    cat >> "$CONFIG_FILE" << EOF

[roles.member]
can_invite = true
EOF

    echo "Config generated at: $CONFIG_FILE"
    cat "$CONFIG_FILE"
else
    echo "Using existing config: $CONFIG_FILE"
fi

echo ""
echo "Starting Zooid relay..."
exec /bin/zooid "$@"
