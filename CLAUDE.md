`README.md` contains high-level project information.
`justfile` contains common commands.

## Codebase Overview

- **zooid/config.go**: Defines `Config` struct with TOML tags for relay configuration (self, groups, management, blossom, roles, data). Contains `LoadConfig()` function and `IsMember()` method.

- **zooid/http.go**: Simple HTTP handler that calls `GetInstance()` and delegates to khatru relay.

- **zooid/instance.go**: Core instance management. `Instance` struct holds config and khatru relay. `MakeInstance()` creates configured relay instances with handlers. `GetInstance()` provides singleton access with lazy loading.

- **zooid/blossom.go**: Blossom file storage integration with member-only access controls.

- **zooid/util.go**: Environment variable utilities with `Env()` function.

- **cmd/relay/main.go**: HTTP server entry point with graceful shutdown.

## SQLite EventStore

The `sqlite/` directory contains a complete SQLite-based khatru eventstore implementation.

### nostrlib API Compatibility
- `Event.Sig` is `[64]byte`, not a separate Signature type
- `Event.CreatedAt` is `nostr.Timestamp` (int64), not `time.Time`
- Use `hex.EncodeToString(evt.Sig[:])` for signature serialization
- Use `hex.DecodeString()` and `copy()` for signature parsing
