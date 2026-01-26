# Coding Conventions

**Analysis Date:** 2026-01-26

## Naming Patterns

**Files:**
- Snake_case for test files: `*_test.go`
- Lowercase with no separators for package files: `transfer.go`, `manifest.go`, `pattern.go`
- Main entry points: `main.go` in `cmd/updex-cli/`

**Functions:**
- Exported functions use PascalCase: `LoadTransfers()`, `GetInstalledVersions()`, `VacuumWithDetails()`
- Unexported functions use camelCase: `parseTransferFile()`, `expandSpecifiers()`, `normalizeVersion()`
- Constructor functions: `NewClient()`, `NewInstallCmd()`, `NewListCmd()`

**Variables:**
- Local variables use camelCase: `targetDir`, `tmpFile`, `actualHash`
- Package-level variables use camelCase for unexported: `placeholders`, `defaultSearchPaths`
- Package-level exported variables use PascalCase: `Definitions`, `JSONOutput`, `SysextDir`

**Types:**
- Structs use PascalCase: `Transfer`, `Client`, `Pattern`, `VersionInfo`
- Section structs append "Section": `TransferSection`, `SourceSection`, `TargetSection`
- Result types append "Result": `CheckResult`, `UpdateResult`, `VacuumResult`
- Info types append "Info": `VersionInfo`, `ComponentInfo`, `FeatureInfo`

**Errors:**
- Custom error types use camelCase with "Error" suffix or descriptive name: `patternError`
- Error variables use "Err" prefix: `ErrEmptyPattern`, `ErrMissingVersionPlaceholder`

## Code Style

**Formatting:**
- Standard `gofmt` (invoked via `make fmt`)
- No custom formatting tools beyond standard Go tooling

**Linting:**
- `golangci-lint` (invoked via `make lint`)
- May not be installed in all environments (Makefile gracefully handles missing linter)

## Import Organization

**Order:**
1. Standard library packages
2. External third-party packages
3. Internal project packages

**Example from `cmd/commands/install.go`:**
```go
import (
	"context"
	"fmt"

	"github.com/frostyard/updex/cmd/common"
	"github.com/frostyard/updex/updex"
	"github.com/spf13/cobra"
)
```

**Path Aliases:**
- Import aliasing used when package names conflict: `goversion "github.com/hashicorp/go-version"`

## Error Handling

**Patterns:**
- Return error as last value: `func LoadTransfers(customPath string) ([]*Transfer, error)`
- Wrap errors with context using `fmt.Errorf("context: %w", err)`
- Check `os.IsNotExist(err)` for graceful handling of missing files/directories
- Return `nil, nil` for empty results (not found scenarios)

**Error wrapping example from `internal/config/transfer.go`:**
```go
if err != nil {
	return nil, fmt.Errorf("failed to read directory %s: %w", dir, err)
}
```

**Validation errors:**
```go
if t.Source.Type == "" {
	return nil, fmt.Errorf("Source.Type is required")
}
```

**Custom error types in `internal/version/pattern.go`:**
```go
type patternError string

func (e patternError) Error() string {
	return string(e)
}

var (
	ErrEmptyPattern              = patternError("pattern cannot be empty")
	ErrMissingVersionPlaceholder = patternError("pattern must contain @v placeholder")
)
```

## Logging

**Framework:** Direct `fmt.Printf` and `fmt.Fprintf` to stdout/stderr

**Patterns:**
- Error output goes to stderr: `fmt.Fprintf(os.Stderr, "Error encoding JSON: %v\n", err)`
- User-facing output goes to stdout: `fmt.Printf("Successfully installed %s version %s\n", ...)`
- No structured logging framework in use

## Comments

**When to Comment:**
- Package documentation at top of primary package file
- Exported functions have doc comments describing purpose
- Inline comments for complex logic or non-obvious behavior

**Package documentation example from `updex/updex.go`:**
```go
// Package updex provides a programmatic API for managing systemd-sysext images.
//
// This package allows you to download, verify, and manage sysext images from remote sources.
// It replicates the functionality of systemd-sysupdate for url-file transfers.
//
// Basic usage:
//
//	client := updex.NewClient(updex.ClientConfig{
//	    Verify: true,
//	})
```

**Function documentation:**
```go
// GetInstalledVersions returns the list of installed versions for a transfer config
// Also returns the current version (pointed to by symlink or newest)
func GetInstalledVersions(t *config.Transfer) ([]string, string, error) {
```

## Function Design

**Size:** 
- Functions kept focused on single responsibility
- Larger functions broken into helper functions (e.g., `VacuumWithDetails` internally uses sorted version logic)

**Parameters:**
- Struct types for complex configuration: `ClientConfig`, `InstallOptions`
- Pointers for mutable structs: `*Transfer`, `*Feature`

**Return Values:**
- Multiple return values for data + error: `([]string, string, error)`
- Named result types for API responses: `CheckResult`, `UpdateResult`
- Return slices as `nil` when empty (not empty slice)

## Module Design

**Exports:**
- Only export what is needed by other packages
- Unexported helper functions with lowercase names
- Exported types in separate files (e.g., `results.go` for result types)

**Barrel Files:**
- Not used; Go convention of direct package imports followed

## Defer Patterns

**Resource cleanup:**
```go
defer func() { _ = tmpFile.Close() }()
defer func() { _ = resp.Body.Close() }()
```

**Cleanup on failure:**
```go
defer func() {
	_ = tmpFile.Close()
	_ = os.Remove(tmpPath) // Clean up on failure
}()
```

## JSON Tags

**Struct field tagging for JSON output:**
```go
type VersionInfo struct {
	Version   string `json:"version"`
	Installed bool   `json:"installed"`
	Available bool   `json:"available"`
	Current   bool   `json:"current"`
	Protected bool   `json:"protected,omitempty"`
	Component string `json:"component,omitempty"`
}
```

- Use `omitempty` for optional fields
- Use snake_case for JSON keys

## Context Usage

**Pattern:** Pass `context.Context` as first parameter to operations that may be cancelled:
```go
func (c *Client) Install(ctx context.Context, repoURL string, opts InstallOptions) (*InstallResult, error)
```

## Cobra Command Pattern

**Command construction in `cmd/commands/`:**
```go
func NewInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install URL",
		Short: "Install an extension from a remote repository",
		Long:  `Install an extension from a remote repository...`,
		Args:  cobra.ExactArgs(1),
		RunE:  runInstall,
	}
}

func runInstall(cmd *cobra.Command, args []string) error {
	// Implementation
}
```

---

*Convention analysis: 2026-01-26*
