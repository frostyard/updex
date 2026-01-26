# Architecture

**Analysis Date:** 2026-01-26

## Pattern Overview

**Overall:** Library + CLI architecture with clean separation between public API and internal implementation.

**Key Characteristics:**
- Public API package (`updex/`) provides programmatic access to all operations
- CLI commands in `cmd/commands/` are thin wrappers around the public API
- Internal packages provide low-level utilities (config parsing, downloads, version handling)
- Configuration-driven via `.transfer` and `.feature` INI files

## Layers

**CLI Layer:**
- Purpose: Parse arguments, invoke API methods, format output
- Location: `cmd/`
- Contains: Cobra commands, common flags, output formatting
- Depends on: `updex/` (public API), `cmd/common/`
- Used by: End users via `updex` binary

**Public API Layer:**
- Purpose: Expose all operations as a Go library with typed options/results
- Location: `updex/`
- Contains: `Client` struct, operation methods (List, Install, Update, etc.), typed options and results
- Depends on: `internal/config/`, `internal/manifest/`, `internal/sysext/`, `internal/version/`, `internal/download/`
- Used by: CLI layer, external Go programs

**Internal Layer:**
- Purpose: Low-level utilities and domain logic
- Location: `internal/`
- Contains: Config parsing, manifest fetching, download handling, version comparison, sysext management
- Depends on: External libraries (gopkg.in/ini.v1, hashicorp/go-version, etc.)
- Used by: Public API layer only

## Data Flow

**Install Extension Flow:**

1. CLI parses URL and component from args/flags (`cmd/commands/install.go`)
2. CLI creates Client and calls `client.Install()` (`updex/install.go`)
3. Client fetches repository index from `{url}/ext/index`
4. Client downloads `.transfer` file to `/etc/sysupdate.d/{component}.transfer`
5. Client loads transfer config via `config.LoadTransfers()` (`internal/config/transfer.go`)
6. Client fetches manifest from `{source_url}/SHA256SUMS` (`internal/manifest/manifest.go`)
7. Client extracts versions from manifest using patterns (`internal/version/pattern.go`)
8. Client downloads file with hash verification (`internal/download/download.go`)
9. Client updates symlinks and links to `/var/lib/extensions` (`internal/sysext/manager.go`)
10. Client calls `systemd-sysext refresh` to activate

**List Versions Flow:**

1. CLI calls `client.List()` with options
2. Client loads all `.transfer` files from standard paths
3. Client filters transfers by enabled features
4. For each transfer: fetch remote manifest, extract versions, get local installed versions
5. Merge remote + local versions into unified result set
6. Return `[]VersionInfo` to CLI for formatting

**Configuration Loading Flow:**

1. Search paths checked in priority order: `/etc/sysupdate.d/`, `/run/sysupdate.d/`, `/usr/local/lib/sysupdate.d/`, `/usr/lib/sysupdate.d/`
2. Earlier paths take priority (override later paths)
3. `.transfer` files parsed as INI with `[Transfer]`, `[Source]`, `[Target]` sections
4. `.feature` files parsed similarly, with drop-in support (`.feature.d/*.conf`)
5. Transfers filtered by feature enablement (OR logic for Features, AND logic for RequisiteFeatures)

**State Management:**
- No persistent state maintained by updex itself
- State is derived from filesystem: installed versions in target directories
- Transfer/Feature configuration from `.transfer`/`.feature` files in search paths
- Current version tracked via symlinks (e.g., `component.raw` -> `component_1.2.3.raw`)

## Key Abstractions

**Client:**
- Purpose: Entry point for all operations, holds configuration
- Examples: `updex/updex.go`
- Pattern: Functional options via `ClientConfig` struct

**Transfer:**
- Purpose: Represents a single extension update configuration
- Examples: `internal/config/transfer.go`
- Pattern: INI-based configuration with sections (Transfer, Source, Target)

**Feature:**
- Purpose: Groups of transfers that can be enabled/disabled together
- Examples: `internal/config/feature.go`
- Pattern: Systemd-style feature flags with drop-in overrides

**Manifest:**
- Purpose: SHA256SUMS file from remote repository
- Examples: `internal/manifest/manifest.go`
- Pattern: Fetch, parse, optionally verify GPG signature

**Pattern:**
- Purpose: Version extraction from filenames using `@v` and other placeholders
- Examples: `internal/version/pattern.go`
- Pattern: Template-based matching (e.g., `component_@v.raw.xz`)

**Result Types:**
- Purpose: Typed return values for each operation
- Examples: `updex/results.go` (VersionInfo, InstallResult, UpdateResult, etc.)
- Pattern: Struct with JSON tags for serialization

## Entry Points

**CLI Main:**
- Location: `cmd/updex-cli/main.go`
- Triggers: User executes `updex` command
- Responsibilities: Set version info, call `updex.Execute()`

**Root Command:**
- Location: `cmd/updex/root.go`
- Triggers: Cobra command execution
- Responsibilities: Register all subcommands, handle global flags, wrap with fang for signal handling

**Individual Commands:**
- Location: `cmd/commands/*.go`
- Triggers: User runs specific subcommand (list, install, update, etc.)
- Responsibilities: Parse command-specific flags, create Client, call appropriate API method, format output

**API Client:**
- Location: `updex/updex.go`
- Triggers: Programmatic usage via `updex.NewClient()`
- Responsibilities: Execute operations, report progress, return typed results

## Error Handling

**Strategy:** Return errors up the stack with context wrapping

**Patterns:**
- All API methods return `(ResultType, error)` tuples
- Errors wrapped with `fmt.Errorf("context: %w", err)` for stack traces
- Result types include `Error` field for partial failures in batch operations
- Warnings reported via progress reporter, don't halt execution
- Operations that require root check `os.Geteuid() != 0` early

## Cross-Cutting Concerns

**Logging:** Progress reporting via `github.com/frostyard/pm/progress` interface. CLI uses `TextReporter` for human output. JSON mode suppresses progress, outputs structured results only.

**Validation:** Required fields checked in config parsing. API methods validate required options (e.g., component name for Install). Transfer files validated for required sections and fields.

**Authentication:** GPG signature verification optional via `--verify` flag or `Verify=true` in transfer config. Signatures fetched from `SHA256SUMS.gpg`.

---

*Architecture analysis: 2026-01-26*
