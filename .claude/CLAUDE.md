# CLAUDE.md

Guidance for contributors working in this repository.

## Project

`trawl` is a dual-panel terminal SFTP file manager. It applies proper Go conventions: package
boundaries, tests, structured logging, secure defaults.

## Module

- Module path: `github.com/liam-od/trawl`
- Go version: `1.26`
- No CGO; the resulting binary is fully static.

## Commands

All day-to-day work goes through `make`. Binaries land in `bin/` (gitignored).

| Target           | Purpose                                                     |
|------------------|-------------------------------------------------------------|
| `make build`     | Build `bin/trawl` for the host platform                     |
| `make run`       | `go run ./cmd/trawl` (no binary written)                    |
| `make test`      | `go test ./...` across the module                           |
| `make fmt`       | `go fmt ./...`                                              |
| `make vet`       | `go vet ./...`                                              |
| `make tidy`      | `go mod tidy`                                               |
| `make clean`     | Remove `bin/`                                               |
| `make build-all` | Cross-compile: `bin/trawl-linux-amd64`, `bin/trawl-windows-amd64.exe` |
| `make help`      | List the targets above with their descriptions              |

## Layout

```
trawl/
├── cmd/trawl/main.go      # entry-point glue only — no business logic
├── internal/<feature>/    # private packages, one per concern
├── bin/                   # build output (gitignored)
└── go.mod
```

- `cmd/trawl/main.go` parses flags, wires dependencies, calls into `internal/`. Keep it thin.
- `internal/<feature>/` is where real code lives. Create packages **lazily** as features land.
- Tests live next to source as `<file>_test.go`. Use `t.TempDir()` for filesystem fixtures.
- Platform-specific code must use `//go:build` constraints and `*_unix.go` / `*_windows.go` files.

## Code style

- `gofmt` enforced via `make fmt`; `go vet` must be clean.
- Errors: wrap with `fmt.Errorf("doing X: %w", err)`; never swallow. Return up the stack.
- Logging: surface errors through return values, not stderr prints. The TUI status bar handles
  user-facing messages. (`log/slog` in `internal/log` is post-v1.)
- No package-level mutable state. Pass dependencies explicitly into constructors.
- Prefer the standard library. Each external dependency must be justified in the commit message.
- Naming: idiomatic Go (short, lower-case, no stutter). Export only across package boundaries.

## Adding a dependency

```bash
go get <module>@<version>   # pin to a tagged release
make tidy
```

State the reason in the commit message.

