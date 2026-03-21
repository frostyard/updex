# updex Overview

## Purpose

updex is a Go SDK and CLI for managing systemd-sysext images. It replicates
`systemd-sysupdate` functionality for `url-file` transfers, providing
feature-based grouping of extensions with automatic version management,
SHA256 verification, optional GPG signing, and systemd timer integration
for scheduled updates.

## Architecture

### SDK-First Design

All business logic lives in the `updex/` package as a public Go API. CLI
commands in `cmd/` are thin Cobra wrappers that parse flags, call SDK
methods, and format output. SDK code never imports CLI packages.

### Directory Layout

```
cmd/
  updex-cli/main.go       Entry point, injects build-time version info
  updex/
    root.go               Root Cobra command, global flags (--definitions, --verify, --no-refresh)
    client.go             newClient() helper mapping CLI flags to SDK ClientConfig
    features.go           features subcommand tree (list, enable, disable, update, check)
    features_run.go       RunE implementations calling SDK methods
    daemon.go             daemon subcommand tree (enable, disable, status)

updex/                    Public SDK package
  updex.go                Client struct, NewClient(), core helpers
  features.go             Features(), EnableFeature(), DisableFeature(), UpdateFeatures(), CheckFeatures()
  list.go                 Feature listing logic
  install.go              installTransfer() core download/install flow
  options.go              Option structs for each operation
  results.go              Result types returned by SDK methods

internal/
  config/                 INI parser for .transfer and .feature files
  download/               HTTP download with SHA256 verification and decompression
  manifest/               SHA256SUMS manifest fetch with optional GPG verification
  sysext/                 systemd-sysext integration (refresh, merge, vacuum, symlinks)
  systemd/                systemd timer+service unit generation and management
  version/                Pattern matching (@v placeholder) and semantic version comparison
  testutil/               Test HTTP server helper
```

### Dependency Flow

```
CLI (cmd/updex)
  └── SDK (updex/)
        ├── config      Load .transfer/.feature files
        ├── manifest    Fetch SHA256SUMS, optional GPG verify
        ├── download    HTTP download, hash verify, decompress
        ├── sysext      Extension lifecycle (install, vacuum, refresh)
        │   └── version Pattern matching, version comparison
        └── systemd     Timer/service unit management
```

## Key Patterns

### Feature Model

A **feature** groups related **transfers** (components). Each transfer
describes one downloadable extension image. Features are defined in
`.feature` INI files; transfers in `.transfer` INI files.

- Features can be enabled/disabled via drop-in files at
  `/etc/sysupdate.d/{name}.feature.d/00-updex.conf`
- Transfers reference features via `Features=` (OR logic) and
  `RequisiteFeatures=` (AND logic) in `[Transfer]` section
- Masked features cannot be enabled or disabled

### Update Flow

1. Load all features and their associated transfers from config paths
2. For each enabled feature's transfers:
   a. Fetch `SHA256SUMS` manifest from the transfer's source URL
   b. Match filenames against source patterns to extract available versions
   c. Compare against installed versions to find updates
   d. Download file with SHA256 verification
   e. Decompress if needed (xz, gz, zstd)
   f. Update `CurrentSymlink` in target directory
   g. Symlink into `/var/lib/extensions` for systemd-sysext
3. Vacuum old versions (respects `InstancesMax` and `ProtectVersion`)
4. Run `systemd-sysext refresh` to activate changes

### Version Pattern Matching

Transfer configs use patterns with placeholders. The key placeholder is
`@v` for version extraction. Example: `myext_@v.raw` matches
`myext_1.2.3.raw` and extracts version `1.2.3`.

Other placeholders: `@u` (UUID), `@a` (GPT NoAuto), `@t` (time),
`@h` (SHA256), `@m` (mode), `@s` (size), `@d`/`@l` (tries done/left).

Versions are compared semantically using `hashicorp/go-version` with
string fallback.

### Configuration Loading

Config files use systemd INI format loaded from priority-ordered paths:

1. `/etc/sysupdate.d/` (highest priority)
2. `/run/sysupdate.d/`
3. `/usr/local/lib/sysupdate.d/`
4. `/usr/lib/sysupdate.d/` (lowest priority)

The `--definitions` / `-C` flag overrides these with a single custom path.

Drop-in directories (`.feature.d/*.conf`, `.transfer.d/*.conf`) allow
overriding individual settings. Systemd specifiers (`%w` for VERSION_ID,
`%a` for architecture, etc.) are expanded in paths and patterns.

### Error Conventions

- Lowercase messages, no trailing punctuation
- Wrapped with `fmt.Errorf("context: %w", err)`
- SDK methods return `(result, error)`; CLI translates to exit codes

### Testing Patterns

- `t.TempDir()` for filesystem operations
- Mock runners (`sysext.MockRunner`, `systemd.MockRunner`) for systemd commands
- `internal/testutil` provides an HTTP test server for download/manifest tests
- `t.Context()` for context in tests (Go 1.25)

## CLI Commands

| Command | SDK Method | Root | Description |
|---------|-----------|------|-------------|
| `features list` | `Features()` | No | Show all features with status |
| `features enable NAME` | `EnableFeature()` | Yes | Enable feature (--now to download) |
| `features disable NAME` | `DisableFeature()` | Yes | Disable feature (--now to remove) |
| `features update` | `UpdateFeatures()` | Yes | Download/install updates for enabled features |
| `features check` | `CheckFeatures()` | No | Check for available updates (read-only) |
| `daemon enable` | — | Yes | Install systemd timer for automatic updates |
| `daemon disable` | — | Yes | Remove systemd timer |
| `daemon status` | — | No | Show timer state |

### Global Flags

| Flag | Description |
|------|-------------|
| `-C, --definitions PATH` | Custom config directory |
| `--verify` | Enable GPG signature verification |
| `--no-refresh` | Skip `systemd-sysext refresh` after operations |
| `--json` | Output in JSON format |
| `-v, --verbose` | Enable verbose/debug output |

## Configuration Files

### .feature File Format

```ini
[Feature]
Description=Human-readable description
Documentation=https://example.com/docs
Enabled=true
```

### .transfer File Format

```ini
[Transfer]
MinVersion=1.0.0
ProtectVersion=%w
Verify=false
InstancesMax=3
Features=feature-name
RequisiteFeatures=required-feature

[Source]
Type=url-file
Path=https://example.com/releases/
MatchPattern=myext_@v.raw.xz

[Target]
Type=regular-file
Path=/opt/extensions/
MatchPattern=myext_@v.raw
CurrentSymlink=myext.raw
```

### Key Config Fields

| Field | Section | Description |
|-------|---------|-------------|
| `Features` | Transfer | Feature names this transfer belongs to (OR logic) |
| `RequisiteFeatures` | Transfer | All listed features must be enabled (AND logic) |
| `InstancesMax` | Transfer | Max versions to keep (vacuum removes oldest) |
| `ProtectVersion` | Transfer | Version string to protect from vacuum |
| `MinVersion` | Transfer | Minimum version to consider |
| `Type` | Source/Target | Only `url-file` / `regular-file` supported |
| `Path` | Source | Base URL for manifest and downloads |
| `MatchPattern(s)` | Source/Target | Filename pattern(s) with `@v` placeholder |
| `CurrentSymlink` | Target | Symlink name pointing to current version |

## Detailed Documentation

- [SDK API Reference](sdk-api.md) — Public types, methods, options, and result structures
- [Internal Packages](internal-packages.md) — Config parsing, downloads, manifests, sysext, systemd, versioning
