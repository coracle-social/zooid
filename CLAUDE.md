`README.md` contains high-level project information.
`justfile` contains common commands.

## Codebase Overview

- **zooid/config.go**: Defines `Config` struct with TOML tags for relay configuration (self info, groups, roles, data paths). Contains `loadConfig()` function that parses hostname-based config files from `configs/` directory.

- **zooid/http.go**: Core HTTP handling with dynamic instance creation. `getInstance()` function loads config and creates khatru relay instances on-demand. `ServeHTTP()` function routes requests to appropriate relay instances based on hostname.

- **zooid/util.go**: Environment variable utilities. `Env()` function with fallback support for configuration.

- **cmd/relay/main.go**: Main entry point that starts HTTP server with graceful shutdown handling. Uses `zooid.ServeHTTP` as the handler wrapped in `http.HandlerFunc`.
