# Copilot Instructions for updex

## Project Overview

updex is a Go CLI application that replicates `systemd-sysupdate` functionality for managing systemd-sysext images via `url-file` transfers.

### Purpose and Users

- **Target Users**: System administrators managing systemd-based Linux distributions (especially Debian Trixie) that don't ship with `systemd-sysupdate`
- **Main Use Case**: Automated downloading, verification, and installation of system extension images from remote HTTP sources
- **Key Value**: Provides a lightweight, secure way to manage system extensions with version control, GPG verification, and automatic cleanup

### Tech Stack

- **Language**: Go 1.25+
- **CLI Framework**: Cobra (github.com/spf13/cobra)
- **Configuration**: INI files (gopkg.in/ini.v1)
- **Compression**: XZ, gzip, zstd support
- **Security**: GPG signature verification (golang.org/x/crypto/openpgp)
- **Version Management**: Semantic versioning (github.com/hashicorp/go-version)

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
- Prefer standard library functions over external dependencies when possible
- Use descriptive variable names; avoid single-letter variables except for very short scopes (loop indices, etc.)
- Add comments for exported functions and types following Go documentation conventions

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

### Testing Best Practices

- Write table-driven tests for functions with multiple test cases
- Use descriptive test names that explain what is being tested
- Test error cases in addition to happy paths
- Use temporary directories (`t.TempDir()`) for file system operations in tests
- Mock external dependencies and HTTP requests
- Ensure tests are idempotent and can run in parallel where possible

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

## Security Considerations

- **GPG Verification**: Always support and test GPG signature verification for SHA256SUMS files
- **File Permissions**: Respect and validate file permissions specified in transfer configurations
- **Path Validation**: Validate all file paths to prevent directory traversal attacks
- **Hash Verification**: Always verify SHA256 hashes after downloads before installation
- **Input Sanitization**: Sanitize all user inputs and configuration values
- **Error Handling**: Never expose sensitive information (paths, URLs with credentials) in error messages
- **Symlink Safety**: Validate symlink targets to prevent attacks via malicious symlinks

## Troubleshooting

### Build Issues

- If `make build` fails, ensure Go 1.25+ is installed: `go version`
- If dependencies fail to download, try: `make tidy` or `go mod tidy`
- If golangci-lint is not found, it's optional; the build will continue

### Test Failures

- Run `make fmt` before running tests to ensure code is properly formatted
- If tests fail on file system operations, check directory permissions
- For verbose test output, use: `go test -v ./...`

### Common Runtime Issues

- **Configuration not found**: Ensure `.transfer` files exist in standard paths (`/etc/sysupdate.d/`, etc.)
- **Permission denied**: Most operations require root privileges; use `sudo`
- **GPG verification fails**: Ensure GPG keyring is properly configured or use `--verify=false`
- **Download failures**: Check network connectivity and source URL accessibility

## Version Management and Releases

- Version numbers follow semantic versioning (MAJOR.MINOR.PATCH)
- Git tags are used to mark releases
- Use `make bump` to create a new version tag (requires `svu` tool)
- Build metadata (version, build time) is embedded during compilation via ldflags
- The `--version` flag displays the embedded version information
