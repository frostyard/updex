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
├── updex/                    # PUBLIC SDK - Core library (importable)
│   ├── updex.go              # Main SDK entry point and types
│   ├── options.go            # SDK options and configuration
│   ├── results.go            # Result types returned by SDK
│   ├── check.go              # Check-new operation
│   ├── components.go         # Components operation
│   ├── discover.go           # Discover operation
│   ├── features.go           # Features operations
│   ├── install.go            # Install operation
│   ├── list.go               # List operation
│   ├── pending.go            # Pending operation
│   ├── remove.go             # Remove operation
│   ├── update.go             # Update operation
│   ├── vacuum.go             # Vacuum operation
│   └── sysext.go             # Sysext utilities
├── cmd/                      # CLI layer (thin wrappers)
│   ├── commands/             # Cobra command wrappers
│   │   ├── check.go          # Wraps updex.CheckNew()
│   │   ├── components.go     # Wraps updex.Components()
│   │   ├── discover.go       # Wraps updex.Discover()
│   │   ├── features.go       # Wraps updex.Features*()
│   │   ├── install.go        # Wraps updex.Install()
│   │   ├── list.go           # Wraps updex.List()
│   │   ├── pending.go        # Wraps updex.Pending()
│   │   ├── remove.go         # Wraps updex.Remove()
│   │   ├── update.go         # Wraps updex.Update()
│   │   └── vacuum.go         # Wraps updex.Vacuum()
│   ├── common/               # CLI utilities (flags, formatting, etc.)
│   ├── updex/                # updex CLI root command
│   │   └── root.go
│   └── updex-cli/            # updex binary entry point
│       └── main.go
├── internal/                 # Internal implementation (used by SDK)
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

### SDK-First Development

**IMPORTANT**: All operations must be implemented in the public SDK (`updex/` package) first, then wrapped by CLI commands.

#### Adding a New Operation

1. **Implement in SDK** (`updex/<operation>.go`):
   - Define a public function (e.g., `func MyOperation(opts Options) ([]Result, error)`)
   - Implement all business logic in the SDK
   - Return structured results that can be consumed programmatically
   - Use the `Options` struct for configuration
   - Document with Go doc comments

2. **Create CLI Wrapper** (`cmd/commands/<operation>.go`):
   - Create a thin Cobra command that calls the SDK function
   - Parse CLI flags into `updex.Options`
   - Call the SDK function: `results, err := updex.MyOperation(opts)`
   - Format output (text or JSON) using `cmd/common` utilities
   - Handle errors and exit codes

3. **Register Command**:
   - Register with root command in `cmd/updex/root.go`

### SDK Design Principles

- **No CLI Dependencies**: SDK code must NOT import Cobra, pflag, or CLI-specific packages
- **Structured Returns**: Return typed structs, not formatted strings
- **Options Pattern**: Use `Options` struct for configuration instead of global variables
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
        opts := updex.Options{
            DefinitionsPath: definitionsPath,
            Component:       component,
            Verify:          verify,
            Reporter:        createReporter(jsonOutput),
        }

        // 2. Call SDK
        results, err := updex.MyOperation(opts)
        if err != nil {
            return err
        }

        // 3. Format output
        if jsonOutput {
            return outputJSON(results)
        }
        outputText(results)
        return nil
    },
}
```

### JSON Output

All SDK functions return structured data. CLI commands handle formatting:

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

### Using the SDK Programmatically

Other Go applications can import and use updex as a library:

```go
import "github.com/frostyard/updex/updex"

func main() {
    opts := updex.Options{
        DefinitionsPath: "/etc/sysupdate.d",
        Component:       "myext",
        Verify:          true,
    }

    // Check for updates
    hasUpdate, err := updex.CheckNew(opts)
    if err != nil {
        log.Fatal(err)
    }

    if hasUpdate {
        // Download and install
        results, err := updex.Update(opts)
        if err != nil {
            log.Fatal(err)
        }

        for _, r := range results {
            fmt.Printf("Updated %s to %s\n", r.Component, r.Version)
        }
    }
}
```

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

### Adding a New Operation

1. **Create SDK function** in `updex/<operation>.go`:
   - Implement public function with `Options` parameter
   - Return structured results and error
   - Add comprehensive doc comments
2. **Create CLI wrapper** in `cmd/commands/<operation>.go`:
   - Create Cobra command that calls SDK function
   - Handle flag parsing and output formatting
3. **Register command** in `cmd/updex/root.go` or `cmd/instex/root.go`
4. Run `make fmt && make build`

### Adding a New Option

1. Add field to `updex.Options` struct in `updex/options.go`
2. Add corresponding flag in CLI command files
3. Update SDK functions to use the new option
4. Run `make fmt && make build`

### Adding a New Compression Format

1. Add decompressor in `internal/download/decompress.go`
2. Update `detectCompression()` in `internal/download/download.go`
3. Run `make fmt && make build`

### Adding a New Global Flag

1. Add field to `updex.Options` in `updex/options.go` (SDK)
2. Add CLI variable in `cmd/updex/root.go` or `cmd/instex/root.go`
3. Register in `init()` with `rootCmd.PersistentFlags()`
4. Pass flag value to SDK via Options struct
5. Run `make fmt && make build`

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
