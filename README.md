# trawl

A terminal SFTP browser. Dual-panel TUI for browsing local and remote
filesystems and moving files between them over SSH.

Status: rebuilding from a prototype. Not yet usable.

## Build

    make build         # → bin/trawl
    make build-all     # cross-compile to bin/trawl-<os>-<arch>

## Develop

    make run           # go run ./cmd/trawl
    make test
    make fmt vet tidy

## Layout

- `cmd/trawl/`   — entry point
- `internal/`    — private packages (added as features land)
- `bin/`         — build output (gitignored)
