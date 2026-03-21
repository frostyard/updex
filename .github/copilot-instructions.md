# Copilot Instructions for updex

## Project Overview

updex is a Go library (SDK) and CLI tool for managing systemd-sysext images, replicating `systemd-sysupdate` functionality for `url-file` transfers.

### Architecture

- **Core SDK**: All operations are implemented as a public Go library in the `updex/` package
- **CLI Tool**: `updex` is a thin wrapper around the SDK, providing a CLI interface
- **Reusability**: The SDK can be imported and used by other Go applications for programmatic sysext management

### Purpose and Users

- **Library Users**: Go developers building automation tools or system management applications
- **CLI Users**: System administrators managing systemd-based Linux distributions (especially Debian Trixie) that don't ship with `systemd-sysupdate`
- **Main Use Case**: Automated downloading, verification, and installation of system extension images from remote HTTP sources
- **Key Value**: Provides a lightweight, secure way to manage system extensions with version control, GPG verification, and automatic cleanup

### Tech Stack

- **Language**: Go 1.25
- **CLI Framework**: Cobra (github.com/spf13/cobra) with clix for unified CLI functionality
- **Configuration**: INI files (gopkg.in/ini.v1)
- **Compression**: XZ, gzip, zstd support
- **Security**: GPG signature verification (github.com/ProtonMail/go-crypto/openpgp)
- **Version Management**: Semantic versioning (github.com/hashicorp/go-version)

## Build & Development

### Required Commands

After making any code changes, always run:

```bash
make fmt
```

This formats all Go source files with `gofmt`.

### Common Make Targets

| Target            | Purpose                               |
| ----------------- | ------------------------------------- |
| `make build`      | Build binary to `build/updex`         |
| `make fmt`        | Format Go code (run after edits)      |
| `make lint`       | Run golangci-lint                     |
| `make test`       | Run tests                             |
| `make test-cover` | Run tests with HTML coverage report   |
| `make check`      | Run fmt, lint, and test together      |
| `make clean`      | Remove build artifacts                |
| `make tidy`       | Run go mod tidy                       |

### Build Workflow

1. Make code changes
2. Run `make fmt` to format
3. Run `make build` to compile
4. Test with `./build/updex --help`

## Project Structure

```
updex/
├── updex/                    # PUBLIC SDK - Core library (importable)
│   ├── updex.go              # Main SDK entry point and Client type
│   ├── options.go            # SDK options and configuration
│   ├── results.go            # Result types returned by SDK
│   ├── features.go           # Features operations
│   ├── install.go            # Install operation
│   ├── list.go               # List operation
│   ├── features_test.go      # Tests for features operations
│   └── test_helpers_test.go  # Shared test helpers
├── cmd/                      # CLI layer (thin wrappers)
│   ├── commands/             # Cobra command wrappers
│   │   ├── components.go     # Components command
│   │   ├── features.go       # Features command (list/enable/disable/update/check)
│   │   ├── daemon.go         # Daemon command (enable/disable/status)
│   │   └── completion_test.go
│   ├── common/               # CLI utilities (flags, formatting, etc.)
│   │   ├── common.go
│   │   └── common_test.go
│   ├── updex/                # updex CLI root command
│   │   └── root.go
│   └── updex-cli/            # updex binary entry point
│       └── main.go
├── internal/                 # Internal implementation (used by SDK)
│   ├── config/               # .transfer and .feature file parsing
│   ├── manifest/             # SHA256SUMS handling, GPG verification
│   ├── download/             # HTTP downloads, decompression
│   ├── version/              # Pattern matching, version comparison
│   ├── sysext/               # systemd-sysext integration
│   ├── systemd/              # systemd timer+service unit generation
│   └── testutil/             # Shared test utilities
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

## Go 1.25 Modern Idioms

Use these modern Go patterns throughout the codebase:

- `any` instead of `interface{}`
- `slices`, `maps`, `cmp` packages for collection operations
- `slices.SortFunc` for sorting with a comparator
- `strings.SplitSeq` for iterating over split strings
- `t.Context()` in tests (not `context.Background()`)
- `wg.Go()` for goroutine management with `sync.WaitGroup`
- `omitzero` struct tag for JSON fields that should be omitted when zero (slices, maps, structs)

## Key Patterns

### SDK-First Development

**IMPORTANT**: All operations must be implemented in the public SDK (`updex/` package) first, then wrapped by CLI commands. SDK code must never import CLI packages.

#### SDK Design

- `Client` struct is the main entry point with methods: `Features()`, `EnableFeature()`, `DisableFeature()`, `UpdateFeatures()`, `CheckFeatures()`
- SDK functions accept a `context.Context` and an options struct, return result structs + error
- No CLI dependencies: SDK code must NOT import Cobra, pflag, or CLI-specific packages

#### Adding a New Operation

1. **Implement in SDK** (`updex/<operation>.go`):
   - Define a method on `Client` (e.g., `func (c *Client) MyOperation(ctx context.Context, opts MyOptions) ([]Result, error)`)
   - Implement all business logic in the SDK
   - Return structured results that can be consumed programmatically
   - Document with Go doc comments

2. **Create CLI Wrapper** (`cmd/commands/<operation>.go`):
   - Create a thin Cobra command that calls the SDK method
   - Parse CLI flags into the options struct
   - Call the SDK method via the client
   - Format output (text or JSON) using `cmd/common` utilities
   - Handle errors and exit codes

3. **Register Command**:
   - Register with root command in `cmd/updex/root.go`

### SDK Design Principles

- **No CLI Dependencies**: SDK code must NOT import Cobra, pflag, or CLI-specific packages
- **Structured Returns**: Return typed structs, not formatted strings
- **Context-First**: Accept `context.Context` as the first parameter
- **Error Wrapping**: Use `fmt.Errorf()` to wrap errors with context
- **Pure Functions**: Avoid side effects where possible; use callbacks for progress reporting

### CLI Command Pattern

CLI commands should be thin wrappers:

```go
var myCmd = &cobra.Command{
    Use:   "my-command",
    Short: "Description",
    RunE: func(cmd *cobra.Command, args []string) error {
        // 1. Build options from flags
        opts := updex.MyOptions{
            DefinitionsPath: definitionsPath,
            Component:       component,
        }

        // 2. Call SDK via client
        results, err := client.MyOperation(cmd.Context(), opts)
        if err != nil {
            return err
        }

        // 3. Format output
        if jsonOutput {
            return common.OutputJSON(results)
        }
        outputText(results)
        return nil
    },
}
```

### JSON Output

All SDK functions return structured data. CLI commands handle formatting using `common.OutputJSON()` for `--json` flag, text tables otherwise.

### Transfer Configuration

Configuration is read from `.transfer` and `.feature` INI files from systemd-style search paths:
- `/etc/sysupdate.d/`
- `/run/sysupdate.d/`
- `/usr/local/lib/sysupdate.d/`
- `/usr/lib/sysupdate.d/`

### Using the SDK Programmatically

Other Go applications can import and use updex as a library:

```go
import "github.com/frostyard/updex/updex"

func main() {
    client := updex.NewClient(updex.ClientOptions{
        DefinitionsPath: "/etc/sysupdate.d",
    })

    // Update all features
    results, err := client.UpdateFeatures(ctx, updex.UpdateFeaturesOptions{
        Verify: true,
    })
    if err != nil {
        log.Fatal(err)
    }

    for _, r := range results {
        fmt.Printf("Updated %s to %s\n", r.Feature, r.Version)
    }
}
```

## Dependencies

| Package                              | Purpose                        |
| ------------------------------------ | ------------------------------ |
| `github.com/spf13/cobra`             | CLI framework                  |
| `github.com/frostyard/clix`          | Unified CLI functionality      |
| `github.com/frostyard/std`           | Standard library extensions    |
| `gopkg.in/ini.v1`                    | INI file parsing               |
| `github.com/hashicorp/go-version`    | Version comparison             |
| `github.com/schollz/progressbar/v3`  | Download progress display      |
| `github.com/ulikunitz/xz`            | XZ decompression               |
| `github.com/klauspost/compress/zstd` | Zstd decompression             |
| `github.com/ProtonMail/go-crypto/openpgp` | GPG verification          |

## Testing

Run tests with:

```bash
make test
```

For coverage:

```bash
make test-cover
```

Run a single test:

```bash
go test -v -run TestName ./updex/
```

### Testing Best Practices

- Write table-driven tests for functions with multiple test cases
- Use descriptive test names that explain what is being tested
- Test error cases in addition to happy paths
- Use temporary directories (`t.TempDir()`) for file system operations in tests
- Use `t.Context()` for test contexts (not `context.Background()`)
- Mock external dependencies and HTTP requests using mockable `Runner` interfaces
- Ensure tests are idempotent and can run in parallel where possible

## CLI Commands

### `features` command

Manages systemd-sysext features:

- `features list` — List all available features and their status
- `features enable <name>` — Enable a feature
- `features disable <name>` — Disable a feature
- `features update` — Download and install updates for enabled features
- `features check` — Check for available updates without installing

### `components` command

Lists available systemd-sysext components.

### `daemon` command

Manages the updex systemd daemon:

- `daemon enable` — Install and enable systemd timer+service units
- `daemon disable` — Remove systemd timer+service units
- `daemon status` — Show daemon status

## Common Tasks

### Adding a New Option

1. Add field to the relevant options struct in `updex/options.go`
2. Add corresponding flag in CLI command files
3. Update SDK methods to use the new option
4. Run `make fmt && make build`

### Adding a New Compression Format

1. Add decompressor in `internal/download/decompress.go`
2. Update `detectCompression()` in `internal/download/download.go`
3. Run `make fmt && make build`

### Adding a New Global Flag

1. Add field to the relevant options struct in `updex/options.go` (SDK)
2. Add CLI variable in `cmd/updex/root.go`
3. Register in `init()` with `rootCmd.PersistentFlags()`
4. Pass flag value to SDK via options struct
5. Run `make fmt && make build`

### Modifying Transfer Config Parsing

1. Update structs in `internal/config/`
2. Update the relevant parse functions
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
