# Copilot Instructions for updex

## Project Overview

updex is a Go CLI application that replicates `systemd-sysupdate` functionality for managing systemd-sysext images via `url-file` transfers.

## Build & Development

### Required Commands

After making any code changes, always run:

```bash
make fmt
```

This formats all Go source files with `gofmt`.

### Common Make Targets

| Target       | Purpose                          |
| ------------ | -------------------------------- |
| `make build` | Build the binary                 |
| `make fmt`   | Format Go code (run after edits) |
| `make lint`  | Run go vet and staticcheck       |
| `make test`  | Run tests                        |
| `make check` | Run fmt, lint, and test together |
| `make clean` | Remove build artifacts           |
| `make tidy`  | Run go mod tidy                  |

### Build Workflow

1. Make code changes
2. Run `make fmt` to format
3. Run `make build` to compile
4. Test with `./updex --help`

## Project Structure

```
updex/
├── cmd/
│   ├── commands/             # Shared Cobra subcommands
│   │   ├── check.go          # check-new command
│   │   ├── components.go     # components command
│   │   ├── discover.go       # discover command
│   │   ├── install.go        # install command
│   │   ├── list.go           # list command
│   │   ├── pending.go        # pending command
│   │   ├── update.go         # update command
│   │   └── vacuum.go         # vacuum command
│   ├── common/               # Shared utilities (JSON output, etc.)
│   ├── updex/                # updex root command
│   │   └── root.go
│   └── instex/               # instex root command
│       └── root.go
├── updex/                    # updex binary entry point
│   └── main.go
├── instex/                   # instex binary entry point
│   └── main.go
├── internal/
│   ├── config/               # .transfer file parsing
│   ├── manifest/             # SHA256SUMS handling, GPG verification
│   ├── download/             # HTTP downloads, decompression
│   ├── version/              # Pattern matching, version comparison
│   └── sysext/               # Sysext image management
├── Makefile
├── go.mod
└── go.sum
```

## Code Style

- Use standard Go formatting (`gofmt`)
- Follow Go naming conventions (camelCase for private, PascalCase for exported)
- Error messages should be lowercase and not end with punctuation
- Wrap errors with `fmt.Errorf("context: %w", err)`

## Key Patterns

### Adding a New Command

1. Create `cmd/commands/<command>.go`
2. Define a `*cobra.Command` variable
3. Register with the appropriate root command in `cmd/updex/root.go` or `cmd/instex/root.go`
4. Implement `RunE` function

### JSON Output

All commands support `--json` flag. Use the pattern:

```go
if jsonOutput {
    items := make([]interface{}, len(results))
    for i, r := range results {
        items[i] = r
    }
    outputJSONLines(items)
    return nil
}
```

### Transfer Configuration

Configuration is read from `.transfer` files. The `config.LoadTransfers()` function handles loading from standard paths or a custom `--definitions` path.

## Dependencies

| Package                              | Purpose            |
| ------------------------------------ | ------------------ |
| `github.com/spf13/cobra`             | CLI framework      |
| `gopkg.in/ini.v1`                    | INI file parsing   |
| `github.com/hashicorp/go-version`    | Version comparison |
| `github.com/schollz/progressbar/v3`  | Download progress  |
| `github.com/ulikunitz/xz`            | XZ decompression   |
| `github.com/klauspost/compress/zstd` | Zstd decompression |
| `golang.org/x/crypto/openpgp`        | GPG verification   |

## Testing

Run tests with:

```bash
make test
```

For coverage:

```bash
make test-cover
```

## Common Tasks

### Adding a New Compression Format

1. Add decompressor in `internal/download/decompress.go`
2. Update `detectCompression()` in `internal/download/download.go`
3. Run `make fmt && make build`

### Adding a New Global Flag

1. Add variable in `cmd/updex/root.go` or `cmd/instex/root.go`
2. Register in `init()` with `rootCmd.PersistentFlags()`
3. Run `make fmt && make build`

### Modifying Transfer Config Parsing

1. Update structs in `internal/config/transfer.go`
2. Update `parseTransferFile()` function
3. Run `make fmt && make build`
