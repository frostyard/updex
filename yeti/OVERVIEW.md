# updex Documentation

## Purpose

updex is a Go SDK and CLI for managing [systemd-sysext](https://www.freedesktop.org/software/systemd/man/latest/systemd-sysext.html) images. It replicates `systemd-sysupdate` functionality for `url-file` transfers, providing feature-based management of system extensions with version tracking, SHA256 verification, optional GPG signing, and automatic cleanup.

The project follows an **SDK-first design**: all logic lives in public Go packages, and the CLI is a thin wrapper that parses flags and formats output.

## Architecture

```
cmd/updex-cli/main.go          Entry point (frostyard/clix bootstrap)
cmd/updex/root.go               Cobra root command, global flags
cmd/updex/features.go           features list|enable|disable|update|check
cmd/updex/features_run.go       Run functions for feature subcommands
cmd/updex/daemon.go             daemon enable|disable|status (systemd timers)
cmd/updex/client.go             CLI → SDK client factory

updex/                          Public SDK (Client + methods)
  updex.go                      Client struct, NewClient()
  features.go                   Features(), EnableFeature(), DisableFeature(),
                                UpdateFeatures(), CheckFeatures()
  install.go                    installTransfer() — complete install pipeline
                                (download, symlink, sysext link, refresh, vacuum)
                                Reuses parsed patterns from getAvailableVersions
  list.go                       getAvailableVersions() — returns versions,
                                manifest, and parsed patterns for caller reuse
  options.go                    Option structs for all operations
  results.go                    Result structs for all operations

config/                         .transfer and .feature INI file parsing
                                (shared collectConfigFiles helper for directory scanning)
download/                       HTTP download with SHA256 + decompression
manifest/                       SHA256SUMS manifest fetch/parse + GPG verify
version/                        Pattern matching (@v placeholder) + semver compare
sysext/                         systemd-sysext operations (refresh/merge/vacuum)
systemd/                        systemd unit generation + systemctl management
internal/testutil/              HTTP test server helpers (module-internal)
```

### Package dependency flow

```
CLI (cmd/) → SDK (updex/) → config, download, manifest, version, sysext
                         → sysext → config, version
CLI (cmd/daemon.go) → systemd (direct, bypasses SDK)
```

> Note: `cmd/updex/daemon.go` imports `systemd` directly rather than through the SDK layer. This is a known architectural deviation from the SDK-first pattern.

## Key Patterns

### SDK conventions

- All public SDK methods take `context.Context` as first parameter for cancellation
- Operations use dedicated option structs (e.g., `EnableFeatureOptions`, `UpdateFeaturesOptions`) to allow future expansion without breaking changes
- Return dedicated result structs with status fields + error
- Error messages: lowercase, no trailing punctuation, wrapped with `fmt.Errorf("context: %w", err)`

### Testing patterns

- Mock interfaces for system commands: `sysext.SysextRunner`, `systemd.SystemctlRunner`
- `ClientConfig.SysextRunner` field for injecting mocks into the SDK client — `NewClient` stores the runner directly on the `Client` struct (does not mutate global state)
- `internal/testutil.NewTestServer()` creates `httptest.Server` with configurable manifests and file content
- `t.TempDir()` for filesystem operations, `t.Context()` for context

### CLI output

- Text tables by default, JSON with `--json` flag — both `--json` and `--dry-run` are provided by the `github.com/frostyard/clix` package, not defined in this repo
- Operations requiring filesystem changes call `requireRoot()` to enforce root access

### Public API (Issue #13)

All core packages (`config`, `version`, `download`, `manifest`, `sysext`, `systemd`) are exported as public API at `github.com/frostyard/updex/<package>`. Only `internal/testutil` remains internal. This was an intentional decision: the types in these packages (e.g., `Transfer`, `Feature`, `Pattern`, `Manifest`) were designed with exported fields and are suitable for external consumption.

## Configuration

### Search paths (priority order)

1. `/etc/sysupdate.d/` (highest priority)
2. `/run/sysupdate.d/`
3. `/usr/local/lib/sysupdate.d/`
4. `/usr/lib/sysupdate.d/`

Only the first occurrence of a given filename is used. The `-C` flag overrides all search paths with a custom directory.

### File types

See [Configuration Reference](config-reference.md) for detailed format documentation.

- **`.feature`** files define features (name, description, enabled state)
- **`.transfer`** files define how components are downloaded and installed
- **`.feature.d/`** drop-in directories override feature settings (applied alphabetically)

### Key transfer settings

| Setting | Section | Default | Description |
|---------|---------|---------|-------------|
| `InstancesMax` | `[Transfer]` | `2` | Max versions to keep on disk |
| `ProtectVersion` | `[Transfer]` | — | Version that is never removed |
| `MinVersion` | `[Transfer]` | — | Minimum version to consider |
| `Verify` | `[Transfer]` | `false` | Require GPG signature verification |
| `Features` | `[Transfer]` | — | OR list: any enabled feature activates this transfer |
| `RequisiteFeatures` | `[Transfer]` | — | AND list: all must be enabled |
| `CurrentSymlink` | `[Target]` | — | Symlink name pointing to current version |

### GPG verification

When enabled, fetches `SHA256SUMS.gpg` (detached signature) and verifies against keyrings at:
1. `/etc/systemd/import-pubring.gpg`
2. `/usr/lib/systemd/import-pubring.gpg`

Uses `github.com/ProtonMail/go-crypto/openpgp` for signature verification. Supports both binary and armored keyring formats.

### Systemd specifiers

Transfer file values support systemd-style `%` specifiers. See [Configuration Reference](config-reference.md#systemd-specifiers) for the full list.

## Data Flow

### Feature update (end-to-end)

1. Load all `.feature` and `.transfer` files from search paths
2. Filter transfers to those matching enabled features
3. For each transfer:
   - Fetch `SHA256SUMS` manifest from source URL (+ GPG verify if configured); manifests are cached by source URL across transfers so that multiple transfers sharing the same source make only one HTTP request
   - Parse source patterns and extract available versions using pattern matching (`@v` placeholder); parsed patterns are returned to callers so `installTransfer` reuses them without re-parsing
   - Select newest version via semver comparison
   - Skip if already installed (check target directory)
   - Download file, verify SHA256 hash of compressed bytes during transfer
   - Decompress if needed (xz, gz, zstd — detected from filename)
   - Atomically rename to final path, update `CurrentSymlink`
   - Create symlink in `/var/lib/extensions/` pointing to extension
   - Vacuum old versions per `InstancesMax`
4. Call `systemd-sysext refresh` to reload all extensions (unless `--no-refresh`). Callers batch this — `installTransfer` is called with `NoRefresh: true` per-component, and a single refresh runs at the end

### Enable/disable feature

- **Enable**: Creates drop-in at `/etc/sysupdate.d/<name>.feature.d/00-updex.conf` setting `Enabled=true`. With `--now`, also downloads extensions immediately.
- **Disable**: Creates drop-in setting `Enabled=false`. With `--now`, calls `Unmerge()`, removes symlinks from `/var/lib/extensions/`, and deletes all versioned files. `--force` required if extensions are currently active/merged (changes take effect after reboot).

## CLI Commands

```
updex features list                     List all features with status (alias: updex feature)
updex features enable <name>            Enable a feature
  --now                                 Download extensions immediately
updex features disable <name>           Disable a feature
  --now                                 Unmerge and remove files immediately
  --force                               Allow removal of merged extensions
updex features update                   Download and install new versions
  --no-vacuum                           Skip removing old versions
updex features check                    Check for available updates

updex daemon enable                     Install daily auto-update timer
updex daemon disable                    Remove auto-update timer
updex daemon status                     Show timer status

Global flags:
  -C, --definitions <path>              Custom path to config files
  --verify                              Enable GPG verification
  --no-refresh                          Skip systemd-sysext refresh
  --json                                Output as JSON (from clix)
  --dry-run                             Preview without modifying filesystem (from clix)
  --verbose                             Enable debug output (from clix)
```

## Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/spf13/cobra` | CLI framework |
| `github.com/frostyard/clix` | CLI utilities (output formatting, reporters) |
| `github.com/frostyard/std` | Standard library extensions |
| `github.com/hashicorp/go-version` | Semantic version comparison |
| `github.com/schollz/progressbar/v3` | Download progress bars |
| `gopkg.in/ini.v1` | INI file parsing |
| `github.com/ulikunitz/xz` | XZ decompression |
| `github.com/klauspost/compress` | ZSTD decompression |
| `github.com/ProtonMail/go-crypto` | GPG signature verification (openpgp) |
