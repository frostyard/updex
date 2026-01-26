# Codebase Structure

**Analysis Date:** 2026-01-26

## Directory Layout

```
updex/
├── cmd/                    # CLI application code
│   ├── commands/           # Individual subcommand implementations
│   ├── common/             # Shared CLI utilities (flags, output, reporters)
│   ├── updex/              # Root command definition
│   └── updex-cli/          # Main entry point
├── updex/                  # Public Go API package
├── internal/               # Internal packages (not exported)
│   ├── config/             # Transfer and Feature config parsing
│   ├── download/           # File download with hash verification
│   ├── manifest/           # SHA256SUMS manifest handling
│   ├── sysext/             # systemd-sysext integration
│   └── version/            # Version pattern matching and comparison
├── build/                  # Build artifacts (gitignored)
├── scripts/                # Build helper scripts
├── completions/            # Shell completion files (generated)
├── manpages/               # Man page files (generated)
├── .github/workflows/      # CI/CD pipelines
└── .planning/              # Planning and analysis documents
```

## Directory Purposes

**`cmd/`:**
- Purpose: All CLI-related code
- Contains: Cobra commands, flag handling, output formatting
- Key files: 
  - `cmd/updex-cli/main.go` - binary entry point
  - `cmd/updex/root.go` - root command with subcommand registration
  - `cmd/commands/*.go` - one file per subcommand

**`cmd/commands/`:**
- Purpose: Individual subcommand implementations
- Contains: One file per command (list.go, install.go, update.go, etc.)
- Key files:
  - `cmd/commands/install.go` - install extension from repo
  - `cmd/commands/update.go` - update installed extensions
  - `cmd/commands/list.go` - list versions
  - `cmd/commands/discover.go` - discover available extensions
  - `cmd/commands/features.go` - manage features

**`cmd/common/`:**
- Purpose: Shared CLI utilities
- Contains: Global flags, JSON output helpers, progress reporters
- Key files:
  - `cmd/common/common.go` - global flags (--definitions, --json, --verify, --component, --no-refresh)
  - `cmd/common/reporter.go` - TextReporter for CLI progress output

**`updex/`:**
- Purpose: Public API package for programmatic use
- Contains: Client, operation methods, typed options and results
- Key files:
  - `updex/updex.go` - Client and ClientConfig definitions
  - `updex/options.go` - all *Options structs (ListOptions, InstallOptions, etc.)
  - `updex/results.go` - all result types (VersionInfo, InstallResult, etc.)
  - `updex/list.go` - List operation implementation
  - `updex/install.go` - Install operation implementation
  - `updex/update.go` - Update operation implementation

**`internal/config/`:**
- Purpose: Parse .transfer and .feature configuration files
- Contains: Transfer struct, Feature struct, loading and filtering logic
- Key files:
  - `internal/config/transfer.go` - Transfer config parsing with INI
  - `internal/config/feature.go` - Feature config parsing with drop-in support

**`internal/download/`:**
- Purpose: Download files with progress and hash verification
- Contains: Download function, decompression support (xz, gz, zstd)
- Key files:
  - `internal/download/download.go` - main Download function
  - `internal/download/decompress.go` - decompression helpers

**`internal/manifest/`:**
- Purpose: Fetch and parse SHA256SUMS manifests
- Contains: Manifest struct, hash verification, GPG signature verification
- Key files:
  - `internal/manifest/manifest.go` - Fetch and parse SHA256SUMS
  - `internal/manifest/gpg.go` - GPG signature verification

**`internal/sysext/`:**
- Purpose: Manage systemd-sysext integration
- Contains: Installed version detection, symlink management, vacuum, refresh
- Key files:
  - `internal/sysext/manager.go` - all sysext operations

**`internal/version/`:**
- Purpose: Version pattern matching and semantic comparison
- Contains: Pattern parsing, version extraction, sorting
- Key files:
  - `internal/version/pattern.go` - Pattern struct with @v placeholder support

## Key File Locations

**Entry Points:**
- `cmd/updex-cli/main.go`: Binary entry point, sets version info
- `cmd/updex/root.go`: Root Cobra command, registers all subcommands

**Configuration Parsing:**
- `internal/config/transfer.go`: LoadTransfers(), parseTransferFile()
- `internal/config/feature.go`: LoadFeatures(), parseFeatureFile()

**Core Operations:**
- `updex/list.go`: Client.List() - list versions
- `updex/install.go`: Client.Install() - install from repository
- `updex/update.go`: Client.Update() - update extensions
- `updex/discover.go`: Client.Discover() - discover extensions
- `updex/remove.go`: Client.Remove() - remove extensions
- `updex/vacuum.go`: Client.Vacuum() - clean old versions
- `updex/check.go`: Client.Check() - check for updates
- `updex/pending.go`: Client.Pending() - check pending activations
- `updex/features.go`: Client.Features(), Client.EnableFeature(), Client.DisableFeature()

**Testing:**
- `cmd/common/common_test.go`: CLI utility tests
- `internal/config/transfer_test.go`: Transfer parsing tests
- `internal/config/feature_test.go`: Feature parsing tests
- `internal/download/decompress_test.go`: Decompression tests
- `internal/manifest/manifest_test.go`: Manifest parsing tests
- `internal/sysext/manager_test.go`: Sysext manager tests
- `internal/version/pattern_test.go`: Pattern matching tests

**Build Configuration:**
- `go.mod`: Go module definition
- `Makefile`: Build, test, install targets
- `.goreleaser.yaml`: Release automation config

## Naming Conventions

**Files:**
- Go files: lowercase with underscores (`transfer_test.go`)
- Single-purpose files named after primary type/function (`pattern.go`, `manifest.go`)
- Test files: `*_test.go` suffix

**Directories:**
- All lowercase, no underscores or hyphens
- `internal/` for non-exported packages
- `cmd/{binary-name}/` for entry points

**Packages:**
- Package name matches directory name
- Public API package: `updex`
- Internal packages: `config`, `download`, `manifest`, `sysext`, `version`

**Functions/Methods:**
- Exported: PascalCase (`LoadTransfers`, `NewClient`)
- Private: camelCase (`parseTransferFile`, `loadSingleTransfer`)

**Types:**
- Exported structs: PascalCase (`Transfer`, `Client`, `VersionInfo`)
- Options structs: `{Operation}Options` (e.g., `ListOptions`, `InstallOptions`)
- Result structs: `{Operation}Result` (e.g., `InstallResult`, `UpdateResult`)

**Variables/Constants:**
- Package-level vars: camelCase for private (`defaultSearchPaths`)
- Constants: PascalCase for exported (`SysextDir`)

## Where to Add New Code

**New CLI Command:**
- Create `cmd/commands/{command}.go`
- Implement `New{Command}Cmd() *cobra.Command`
- Add to `cmd/updex/root.go` via `rootCmd.AddCommand()`

**New API Operation:**
- Add method to Client in `updex/{operation}.go`
- Define `{Operation}Options` in `updex/options.go`
- Define `{Operation}Result` in `updex/results.go`
- Create CLI wrapper in `cmd/commands/{operation}.go`

**New Internal Utility:**
- Add to existing internal package if it fits
- Create new `internal/{package}/` if distinct concern
- Never import internal packages from outside project

**New Configuration Section:**
- Extend parsing in `internal/config/transfer.go` or `internal/config/feature.go`
- Add fields to appropriate struct
- Update parsing logic in `parseTransferFile()` or `parseFeatureFile()`

**New Tests:**
- Co-locate with implementation: `foo_test.go` next to `foo.go`
- Use `go test ./...` to run all

## Special Directories

**`build/`:**
- Purpose: Compiled binaries
- Generated: Yes (by `make build`)
- Committed: No (gitignored)

**`completions/`:**
- Purpose: Shell completion scripts
- Generated: Yes (by `scripts/completions.sh`)
- Committed: Yes (for distribution)

**`manpages/`:**
- Purpose: Man page documentation
- Generated: Yes (by `scripts/manpages.sh`)
- Committed: Yes (for distribution)

**`.planning/`:**
- Purpose: Planning documents and codebase analysis
- Generated: No (created by GSD commands)
- Committed: Optional (project preference)

**`internal/`:**
- Purpose: Go convention for non-importable packages
- Generated: No
- Committed: Yes

---

*Structure analysis: 2026-01-26*
