# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Development Commands

```bash
make fmt          # Format code (run after every change)
make build        # Build binary to build/updex
make test         # Run all tests
make lint         # Run golangci-lint
make check        # fmt + lint + test
make test-cover   # Tests with HTML coverage report
make tidy         # go mod tidy
```

Run a single test: `go test -v -run TestName ./updex/`

## Architecture

updex is a Go SDK and CLI for managing systemd-sysext images. It replicates `systemd-sysupdate` functionality for `url-file` transfers.

**SDK-first design**: All logic lives in the `updex/` package as a public Go API. CLI commands in `cmd/` are thin Cobra wrappers that parse flags, call SDK functions, and format output. SDK code must never import CLI packages.

Key packages:
- `updex/` — Public SDK: `Client` struct with `Features()`, `EnableFeature()`, `DisableFeature()`, `UpdateFeatures()`, `CheckFeatures()`, `Components()`
- `cmd/updex/` — Cobra command handlers calling SDK methods (flags, output formatting, progress bars)
- `config/` — Parses `.transfer` and `.feature` INI files from systemd-style search paths, including systemd-sysupdate "component" discovery (see below)
- `download/` — HTTP downloads with bounded retry for transient failures, SHA256 verification, and decompression (xz, gz, zstd)
- `manifest/` — Fetches/parses SHA256SUMS manifests with bounded retry for transient failures and optional GPG verification
- `version/` — Pattern matching (`@v` placeholder) and semantic version comparison
- `sysext/` — systemd-sysext integration with mockable `Runner` interface, `/var/lib/extensions` link management, and read-only vacuum planning helpers
- `systemd/` — Generates/installs systemd timer+service units, mockable `Runner` interface

Entry point: `cmd/updex-cli/main.go` → `cmd/updex/root.go`

### Component Discovery (`config/component.go`)

systemd-sysupdate "components" (sysupdate.d(5) "Components") are named
groupings of `.transfer`/`.feature` files under a `sysupdate.<name>.d/`
directory, searched across `config.SearchRoots` (`/etc`, `/run`,
`/usr/local/lib`, `/usr/lib`, in priority order — a package var, overridable
in tests like `sysext.SysextDir`) — same precedence as the legacy default
`sysupdate.d/` directory (`ComponentSearchPaths("")`). This exists so a
sysext's transfer/feature files can move out of the shared default directory
into their own versioning scope (native images now put OS A/B partition and
UKI transfers in the default directory, which must not intersect with
package-versioned sysext transfers) without updex losing track of them.

- `DiscoverComponents()` scans `SearchRoots` for `sysupdate.<name>.d`
  directories (`[a-zA-Z0-9_-]+` names only; dotted/empty names ignored) and
  returns them sorted by name. It does not include the legacy default
  component.
- `LoadComponentFeatures(name)` / `LoadComponentTransfers(name)` load a
  single named component (pass `""` for the legacy default).
- `LoadAllFeatures(customPath)` / `LoadAllTransfers(customPath)` load the
  domain updex operates on by default: the union of the legacy default
  directory and every discovered component. A name collision (same feature
  or transfer name from more than one source) resolves to the most specific
  source — a named component beats the legacy default directory — and is
  reported as a warning string rather than an error. `customPath` (mirrors
  the `-C`/`--definitions` flag) bypasses discovery entirely, matching plain
  `LoadFeatures`/`LoadTransfers(customPath)` behavior.
- `IsSysextTransfer(t)` / `FilterSysextTransfers(transfers)` keep only
  url-file-source, regular-file-target transfers, silently dropping the
  non-sysext transfer shapes native images ship in the legacy default
  directory: `Target Type=partition` (A/B root) and a `regular-file` target
  with `PathRelativeTo` set (the UKI, relative to the ESP). `LoadAllTransfers`
  always applies this filter; plain `LoadTransfers`/`LoadComponentTransfers`
  do not (callers that want the filter must apply it themselves).
- `ComponentOfPath(path)` recovers the component name (or `false` for the
  legacy default / a `-C` override directory) from a loaded `Feature`'s
  `FilePath`, used by `updex.Client.writeFeatureDropIn` to decide whether an
  enable/disable drop-in goes under `/etc/sysupdate.<name>.d/` (via
  `EtcComponentDir(name)`) or the legacy `/etc/sysupdate.d/`.
- `updex.Client.loadDomain(component string)` is the single entry point
  SDK methods use to resolve their read domain: `Definitions` set → that one
  directory (component must be empty, else error); `component` set → just
  that component; otherwise → the full union, with collision warnings routed
  through the client's reporter. All `*FeatureOptions`/`*FeaturesOptions`
  structs carry a `Component string` field for this; extend the options
  struct for new component-scoped operations, never add package-level flag
  state to the SDK.

## Code Patterns

- Error messages: lowercase, no trailing punctuation, wrap with `fmt.Errorf("context: %w", err)`
- SDK functions accept a `context.Context` and an options struct, return result structs + error
- CLI output: `common.OutputJSON()` for `--json` flag, text tables otherwise
- Tests use `t.TempDir()` for filesystem operations and mock runners for systemd commands
- Configuration uses INI format with systemd-style priority paths: `/etc/sysupdate.d/`, `/run/sysupdate.d/`, `/usr/local/lib/sysupdate.d/`, `/usr/lib/sysupdate.d/` (plus the same four roots per discovered component, see above)
- Transfer targets default to staging in `/var/lib/extensions.d`; `CurrentSymlink` is optional legacy state and must not be required for `/var/lib/extensions` sysext links

## Go Version

Go 1.26. Use modern idioms: `any`, `slices`/`maps`/`cmp` packages, `t.Context()`, `slices.SortFunc`, `strings.SplitSeq`, `omitzero` for slice/map/struct JSON tags, `wg.Go()`.
## Documentation

**update documentation** After any change to source code, update relevant documentation in CLAUDE.md, README.md and the yeti/ folder. A task is not complete without reviewing and updating relevant documentation.

**yeti/ directory** The `yeti/` directory contains documentation written for AI consumption and context enhancement, not primarily for humans. Jobs like `doc-maintainer` and `issue-worker` instruct the AI to read `yeti/OVERVIEW.md` and related files for codebase context before performing tasks. Write content in this directory to be maximally useful to an AI agent understanding the codebase — detailed architecture, patterns, and decision rationale rather than user-facing guides.
