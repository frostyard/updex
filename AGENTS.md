# frostyard/updex

updex is a Go SDK and CLI for managing systemd-sysext images, replicating
`systemd-sysupdate` functionality for `url-file` transfers. Library users are
Go developers building automation or system-management tools; CLI users are
administrators of systemd-based Linux systems (Debian Trixie in particular)
that do not ship `systemd-sysupdate`. Start at [docs/README.md](docs/README.md);
read [docs/design/overview.md](docs/design/overview.md) and the specs under
`docs/specs/` for codebase context before performing tasks.

This file (`AGENTS.md`) is the CANONICAL agent instructions **and** the
contributing guide — `CLAUDE.md`, `GEMINI.md`, `CONTRIBUTING.md`,
`.cursorrules`, and `.github/copilot-instructions.md` are symlinks to it, and
`.claude/skills` symlinks to `.agents/skills/`
([ADR-0012](docs/adr/0012-acmm-conformance-via-canonical-aliases.md); pattern
from
[frostyard/core ADR-0002](https://github.com/frostyard/core/blob/main/docs/adr/0002-agent-portable-instruction-surface.md)
and
[ADR-0029](https://github.com/frostyard/core/blob/main/docs/adr/0029-acmm-conformance-via-canonical-aliases.md)).
Edit only the canonical paths; keep content tool-agnostic.

## Skills (follow these for common tasks)

Step-by-step procedures live in [.agents/skills/](.agents/skills/); follow
them rather than improvising, whichever agent you are. The three
`frostyard-*` skills below are synced from frostyard/core (each directory
carries a `.synced-from-core` marker) — edit them in core, never locally,
per [core ADR-0026](https://github.com/frostyard/core/blob/main/docs/adr/0026-distribute-core-skills-via-sync-prs.md);
repo-specific guidance belongs in this file or `docs/`:

- **Structuring, building, testing, or releasing this repo the frostyard Go
  way** → [.agents/skills/frostyard-go-repo/SKILL.md](.agents/skills/frostyard-go-repo/SKILL.md)
- **Maintaining the `docs/` tree (four-category shape, index, migrations)**
  → [.agents/skills/frostyard-repo-docs/SKILL.md](.agents/skills/frostyard-repo-docs/SKILL.md)
- **Hive ACMM conformance (open `acmm` issues, fleet prerequisites)** →
  [.agents/skills/frostyard-acmm-conformance/SKILL.md](.agents/skills/frostyard-acmm-conformance/SKILL.md)
  — canonical aliases per ADR-0012, never duplicated content

## Getting started

### Prerequisites

- **Go 1.26.6 or newer** (the module targets `go 1.26.6`)
- `make`
- Optional: [`golangci-lint`](https://golangci-lint.run/) for linting —
  `make ci` requires the exact release pinned as `GOLANGCI_LINT_VERSION` in
  the `Makefile` (install with the exact `go install …@v<version>` command
  printed by `make lint-version-check` when it fails),
  [`svu`](https://github.com/caarlos0/svu) for release tagging, Node ≥ 20 for
  the docs-integrity gate

updex manages [systemd-sysext](https://www.freedesktop.org/software/systemd/man/latest/systemd-sysext.html)
images, so end-to-end use requires a systemd-based Linux system and root
privileges. Building and running the unit tests does not — the test suite uses
`t.TempDir()` and mock runners instead of touching the real system.

## Build & Development Commands

```bash
make fmt          # Format code (run after every change)
make build        # Build binary to build/updex
make test         # Run all tests
make lint         # Run golangci-lint (.golangci.yml; skipped with a message if not installed)
make check        # fmt + lint + test
make ci           # Credential-free gate matching CI's fail-fast order
make test-cover   # Tests with HTML coverage report
make coverage-check        # Enforce the absolute floor and committed coverage ratchet
make test-coverage-check   # Self-test scripts/check-coverage.sh with fixture profiles
make tidy         # go mod tidy
make clean        # Remove build artifacts
node scripts/check-docs.mjs   # Docs-integrity gate (index, links, aliases)
```

Run a single test: `go test -v -run TestName ./updex/`

Build workflow: make code changes → `make fmt` → `make build` → smoke-test
with `./build/updex --help`. Use `make check` for the quick development loop.
Run `make ci` before opening a pull request. It checks module tidiness, vet,
formatting, lint, non-E2E unit tests with coverage, the hermetic E2E suite,
non-E2E race tests, and Linux amd64/arm64 builds, and requires
`golangci-lint`. The lint step first runs
`make lint-version-check`, which fails unless the installed `golangci-lint`
matches `GOLANGCI_LINT_VERSION` in the `Makefile` (currently 2.12.2) — the
CI Lint job reads the same variable to install that release, so the Makefile
is the single place to bump it and local and CI lint results cannot drift
(`make lint` only warns on a mismatch). The unit-test step writes `coverage.out`, and
`make ci` (and the Unit Tests CI job) then enforces a total statement-coverage
gate of `max(80.0, baseline - 0.5)` over the non-E2E packages via
`make coverage-check` (`scripts/check-coverage.sh` and the committed
`.coverage-baseline`); a coverage regression below the effective floor fails
the gate. When coverage rises, regenerate the non-E2E atomic profile and bump
the baseline to the observed `go tool cover -func=coverage.out` total in the
same pull request.

End-to-end tests live in `tests/e2e/` (entry point:
[tests/e2e/README.md](tests/e2e/README.md)): black-box tests that build the real `updex` binary and run it as a subprocess against fake files and HTTP sources. Successful operations are read-only (no root required); mutating command variants are covered at the argument-validation boundary. `make ci` runs this suite after the coverage gate and before the race tests. CLI integration tests in `cmd/updex/` additionally override package search roots to exercise default component discovery and fake catalogs safely. Run both with `go test -v ./cmd/updex ./tests/e2e/...`.

## Commits & Pull Requests

Commit messages **and pull request titles** use
[Conventional Commits](https://www.conventionalcommits.org/):
`type(scope): summary`, e.g. `test(cli): cover catalog mutation handlers`,
`fix(catalog): refuse symlinked definitions`, `docs(agents): …`. Allowed
types: `feat`, `fix`, `docs`, `refactor`, `test`, `chore`, `ci`, `build`,
`perf`, `style`, `revert` (optional `(scope)`) — the release tooling derives
version bumps from them. The
repository squash-merges and its squash title default is "commit or PR
title", so the PR title — or, for a single-commit PR, that commit's subject —
becomes the `main` commit that svu versions and the changelog groups by. Make
the first commit conventional too; do not carry an issue's `[quality] …` /
`[scanner] …` / `[sec-check] …` title into the commit or PR. `.github/workflows/pr-title.yml`
fails a PR whose title (or lone commit subject) is not conventional. See
[Pull requests](#pull-requests) below for the full process.

## Release Automation

`.github/workflows/snapshot.yml` publishes the singleton GoReleaser nightly
release under the `dev` tag after successful `main` tests. Its
`goreleaser-nightly` concurrency group must keep `cancel-in-progress: true`:
overlapping runs delete and recreate the same release, then collide while
uploading identically named assets. The newest successful test run supersedes
older snapshot work.

`.github/workflows/release.yml` runs only for tag pushes. After publishing the
tagged release and packages, it must dispatch `event-type: build` to
`frostyard/snosi` without a default-branch ref guard: a tag run's ref is
`refs/tags/<tag>`, never `refs/heads/<default>`.
`updex/release_workflow_contract_test.go` pins the tag-only trigger and
unguarded snosi dispatch required by
[frostyard/core ADR-0013](https://github.com/frostyard/core/blob/main/docs/adr/0013-release-fanout-via-repository-dispatch.md),
and (`TestReleaseWorkflowAttestsBuildProvenance`) that the workflow grants
`id-token: write` + `attestations: write` and runs a SHA-pinned
`actions/attest-build-provenance` over `checksums.txt` and the release assets,
so every tag release carries GitHub build provenance
(`gh attestation verify <artifact> --repo frostyard/updex`).
`updex/workflow_pins_contract_test.go` pins the core ADR-0021 shape of every
workflow under `.github/workflows/`: each external `uses:` is a full
40-character commit SHA with a `# <version>` comment, the same SHA carries
the same label in every file, each workflow declares top-level
`permissions:`, and each `actions/checkout` step sets
`persist-credentials: false` (no job pushes; release and snapshot publish
through the API).
`updex/snapshot_workflow_contract_test.go` pins the snapshot contract above —
the `goreleaser-nightly` group with boolean `cancel-in-progress: true`, the
`workflow_run` trigger on `Tests` for `main`, and the job's
`workflow_run.conclusion == 'success'` guard — required by
[frostyard/core ADR-0034](https://github.com/frostyard/core/blob/main/docs/adr/0034-cancel-stale-rolling-dev-releases.md).
`updex/manpage_hook_contract_test.go` pins the release man-page hook: it runs
`go run ./cmd/updex-cli man` exactly as GoReleaser's before-hook
`scripts/manpages.sh` does and asserts a `.TH UPDEX 1` roff page with a
`.SH NAME` section of at least 1000 bytes, so a dependency bump that drops or
renames the fang-injected hidden `man` subcommand fails the Unit Tests job on
the pull request instead of GoReleaser at tag time (skipped under `-short`).

Releases are tagged with semantic versions (`vMAJOR.MINOR.PATCH`) and built by
GoReleaser. Maintainers run `make bump`, which builds, tests, formats, lints,
and then tags the next version with `svu` from a clean working tree. Build
metadata (version, build time) is embedded via ldflags; `--version` prints it.

## Architecture

updex is a Go SDK and CLI for managing systemd-sysext images. It replicates `systemd-sysupdate` functionality for `url-file` transfers.

**SDK-first design**: All logic lives in the `updex/` package as a public Go API. CLI commands in `cmd/` are thin Cobra wrappers that parse flags, call SDK functions, and format output. SDK code must never import CLI packages. The SDK can be imported by other Go applications for programmatic sysext management.

Key packages:
- `updex/` — Public SDK: `Client` struct with feature, component, catalog, and daemon lifecycle methods (`EnableDaemon()`, `DisableDaemon()`, `DaemonStatus()`)
- `cmd/updex/` — Cobra command handlers calling SDK methods (flags, output formatting, progress bars)
- `config/` — Parses `.transfer` and `.feature` INI files from systemd-style search paths, including systemd-sysupdate "component" discovery (see below)
- `catalog/` — Sysext catalog primitives (see below): `*.catalog` repo config loading, sysext enumeration via a GitHub contents API endpoint, fetching the catalog's published `.conf`, and rendering it into updex `.transfer`/`.feature` files
- `download/` — HTTP downloads with bounded retry for transient failures, SHA256 verification, and decompression (xz, gz, zstd)
- `manifest/` — Fetches/parses SHA256SUMS manifests with bounded retry for transient failures and GPG verification (enabled by default per systemd-sysupdate); response bodies are capped at 4 MiB for manifests and 1 MiB for detached signatures
- `version/` — Pattern matching (`@v` placeholder) and semantic version comparison
- `sysext/` — systemd-sysext integration with mockable `Runner` interface, `/var/lib/extensions` link management, and read-only vacuum planning helpers
- `systemd/` — Generates/installs systemd timer+service units, mockable `Runner` interface
- `internal/retry/`, `internal/testutil/` — HTTP retry policy and shared test helpers (HTTP test server)

Entry point: `cmd/updex-cli/main.go` → `cmd/updex/root.go`

### Project structure

```
updex/
├── updex/                    # Public SDK and Client methods
│   ├── updex.go              # ClientConfig, Client, and NewClient
│   ├── options.go            # Per-operation option types
│   ├── results.go            # Structured SDK result types
│   ├── domain.go             # Definition and component discovery
│   ├── features.go           # Feature operations
│   └── catalog.go            # Catalog operations
├── cmd/
│   ├── updex/                # Cobra commands and SDK adapters
│   │   ├── root.go           # Root command and global flags
│   │   ├── client.go         # ClientConfig construction
│   │   ├── features.go       # Feature command definitions
│   │   ├── features_run.go   # Feature command execution/output
│   │   ├── catalog.go        # Catalog commands
│   │   ├── components.go     # Components command
│   │   └── daemon.go         # Daemon commands
│   └── updex-cli/main.go     # Binary entry point
├── catalog/                  # .catalog parsing, cache, and repositories
├── config/                   # .feature and .transfer parsing
├── download/                 # HTTP downloads and decompression
├── manifest/                 # SHA256SUMS handling and GPG verification
├── sysext/                   # systemd-sysext integration
├── systemd/                  # Timer and service unit management
├── version/                  # Pattern matching and version comparison
├── internal/
│   ├── retry/                # Internal HTTP retry policy
│   └── testutil/             # Shared test utilities
├── tests/e2e/                # End-to-end tests
├── docs/                     # adr/ design/ specs/ plans/ (+ metrics/ real tree)
├── scripts/check-docs.mjs    # Docs-integrity gate
├── Makefile
├── go.mod
└── go.sum
```

### Tech stack and dependencies

Go 1.26.6; Cobra with [clix](https://github.com/frostyard/clix) for unified
CLI functionality; INI configuration; xz/gzip/zstd decompression; GPG
verification; semantic-version comparison. Prefer the standard library — only
add a dependency when it is genuinely necessary, and run `make tidy`
afterwards.

| Package                                   | Purpose                     |
| ----------------------------------------- | --------------------------- |
| `github.com/spf13/cobra`                  | CLI framework               |
| `github.com/frostyard/clix`               | Unified CLI functionality   |
| `github.com/frostyard/std`                | Standard library extensions |
| `gopkg.in/ini.v1`                         | INI file parsing            |
| `github.com/hashicorp/go-version`         | Version comparison          |
| `github.com/schollz/progressbar/v3`       | Download progress display   |
| `github.com/ulikunitz/xz`                 | XZ decompression            |
| `github.com/klauspost/compress/zstd`      | Zstd decompression          |
| `github.com/ProtonMail/go-crypto/openpgp` | GPG verification            |

### CLI commands

- `features list|enable <name>|disable <name>|update|check` — manage
  systemd-sysext features (list with status, enable/disable via drop-ins,
  download and install updates for enabled features, check without
  installing)
- `components` — list discovered systemd-sysext components
- `daemon enable|disable|status` — install/remove/inspect the systemd
  timer+service units
- `catalog list|search <query>|add <name>|remove <name>` — browse and manage
  sysexts from configured catalogs

### Adding a new operation

1. **Implement in the SDK** (`updex/<operation>.go`): a method on `Client`
   (e.g. `func (c *Client) MyOperation(ctx context.Context, opts MyOptions) (MyResult, error)`)
   taking `context.Context` first and a dedicated options struct second,
   returning a dedicated result struct plus an error; all business logic
   here; Go doc comments on exported identifiers.
2. **Create the CLI wrapper** (`cmd/updex/<operation>.go`): a thin Cobra
   command that parses flags into the options struct, constructs the client
   with `newClient()`, calls the SDK method, formats JSON with
   `clix.OutputJSON()` and text with the existing command's output
   conventions, and handles errors and exit codes.
3. **Register** the command with the root command in `cmd/updex/root.go`.
4. Update the documentation (see [Documentation](#documentation)).

```go
func runFeaturesList(cmd *cobra.Command, args []string) error {
    client := newClient()
    features, err := client.Features(cmd.Context(), updex.FeaturesOptions{
        Component: featureComponent,
    })
    if err != nil {
        return err
    }

    if clix.JSONOutput {
        _, err = clix.OutputJSON(features)
        return err
    }

    for _, feature := range features {
        fmt.Println(feature.Name)
    }
    return nil
}
```

Other common tasks follow the same shape: a **new option** is a field on the
relevant options struct in `updex/options.go` plus a flag in the CLI command
file; a **new global flag** is a `ClientConfig` field in `updex/updex.go` (or
an operation option), a CLI variable in `cmd/updex/root.go` registered in
`registerAppFlags()` and passed through `cmd/updex/client.go`; a **new
compression format** is a decompressor in `download/decompress.go` plus
detection in `download/download.go`; **transfer config parsing** changes go
in the `config/` structs and parse functions. Always finish with
`make fmt && make build`.

### Using the SDK programmatically

```go
import (
    "context"
    "fmt"
    "log"

    "github.com/frostyard/updex/updex"
)

func main() {
    client := updex.NewClient(updex.ClientConfig{
        Definitions: "/etc/sysupdate.d",
        Verify:      true,
    })

    ctx := context.Background()

    // Update all features
    results, err := client.UpdateFeatures(ctx, updex.UpdateFeaturesOptions{
        NoVacuum: true,
    })
    if err != nil {
        log.Fatal(err)
    }

    for _, feature := range results {
        for _, result := range feature.Results {
            fmt.Printf("Updated %s to %s\n", result.Component, result.Version)
        }
    }
}
```

### Component Discovery (`config/component.go`)

systemd-sysupdate "components" (sysupdate.d(5) "Components") are named
groupings of `.transfer`/`.feature` files under a `sysupdate.<name>.d/`
directory, searched across the client's immutable definition roots (production
defaults: `/etc`, `/run`, `/usr/local/lib`, `/usr/lib`, in priority order).
`config.SearchRoots` remains a compatibility default for package APIs — same
precedence as the legacy default
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

### Catalog Support (`catalog/`, `updex/catalog.go`, `cmd/updex/catalog.go`)

`updex catalog list|search|add|remove` consumes sysext catalogs like
fedora-sysexts (https://fedora-sysexts.github.io/, served at
extensions.fcos.fr). There are deliberately **no built-in repos** — such
catalogs only apply to specific systems (ucore/Fedora atomic) — so repos
come from `<name>.catalog` INI files searched across each client's captured
catalog roots (production defaults: `/etc/updex/catalogs.d`, `/run/...`,
`/usr/local/lib/...`, `/usr/lib/...`; earlier root wins per filename).
`catalog.ConfigRoots` remains the package-API compatibility default. Each file defines `SiteURL`
(required; artifacts resolve under `<SiteURL>/<sysext>/`), optional
`ListURL` (GitHub contents API endpoint, only used by list/search;
`GITHUB_TOKEN` honored), optional `Component` (default `catalog-<name>`), the
component the generated files land in, and optional `AllowInsecure=yes`.
`SiteURL` and `ListURL` require HTTPS by default. `AllowInsecure=yes` is an
explicit development/test escape hatch; it does not widen token attachment,
which remains restricted to the trusted `https://api.github.com` origin. The
SDK's default HTTP client refuses HTTPS-to-HTTP redirects, while a
caller-supplied `ClientConfig.HTTPClient` keeps its own redirect policy.

Key design points:

- `CatalogAdd` fetches `<SiteURL>/<name>/<name>.conf` (a genuine
  sysupdate transfer file the catalog publishes) and writes it via
  `catalog.RenderTransfer` to `config.EtcComponentDir(repo.Component)`:
  a line-based transform that prepends the `catalog.GeneratedMarker`
  ownership header, injects `Features=<name>`, drops `CurrentSymlink`
  (updex manages `/var/lib/extensions` links itself), and preserves
  non-sensitive content byte-for-byte — critically `%w`/`%a` specifiers stay
  **unexpanded** so the file survives Fedora release upgrades (expansion
  happens at load time in `config`). Security-sensitive fields are canonical:
  the source must be `url-file` at this repo's `<SiteURL>/<name>/`, patterns
  must be basename-only, and the target is always a regular `0644` file under
  trusted `catalog.TargetPath` (default `/var/lib/extensions.d`); alternate
  paths and target shapes are rejected.
- **Ownership and safety** (added after PR #137 review): the marker
  header names its generating repo, and that pair is the ownership
  signal — `catalog.GeneratedFileRepo(path)` must return *this* repo.
  `CatalogAdd` refuses to overwrite a file that is unmarked (hand-written
  or package-shipped) or marked by another repo, which is what isolates
  two catalogs configured with the same `Component`; `CatalogRemove`
  likewise only claims a sysext whose /etc `.feature` is marked by that
  repo, and keeps a `.transfer` it doesn't own. Sysext names are
  validated (`catalog.ValidateSysextName`,
  `^[a-zA-Z0-9_][a-zA-Z0-9._+-]*$`) in the SDK and in `FetchConf`, and
  `CachedList` re-validates `Repo.Name`, so traversal-shaped values never
  reach `filepath.Join` or URLs. Any failure after the `fileSnapshot`s
  are taken — including the definition writes themselves, since
  `os.WriteFile` truncates on open — goes through one `rollback()`
  closure that restores exactly what was there before (fresh add → files
  removed; re-add → previous `.transfer`/`.feature`/drop-in contents
  rewritten). A snapshot distinguishes `existed` from `captured`, so a
  path that exists but cannot be read is never deleted by rollback, and
  `managedFileExists` surfaces non-not-exist stat errors instead of
  treating them as absence. Both it and `snapshotFile` use `os.Lstat` and
  reject anything that is not a regular file: `os.Stat` calls a dangling
  symlink absent, which skipped the ownership guard and let `os.WriteFile`
  create the link's target outside the component directory as root. `CatalogRemove` validates the `.transfer`'s ownership
  *before* calling `DisableFeature{Now}` and refuses outright on a
  mismatch, since that teardown deletes images described by whatever
  transfer claims the feature; it then deletes only updex's
  `00-updex.conf` (`updexDropInName`) from `<name>.feature.d`, leaving
  administrator drop-ins, and `os.Remove`s the directories only when they
  end up empty. `CatalogList` attributes `Installed`/`Enabled` via
  `GeneratedFileRepo(f.FilePath)`, so a shared `Component` doesn't report
  one repo's install under another.
- The generated `.feature` has `Enabled=false`; enabling goes through the
  standard `EnableFeature{Now: true, Component: repo.Component}` drop-in
  path. After `add`, the sysext is indistinguishable from a hand-written
  feature — every `features` operation and the daemon manage it via the
  normal union domain. Only `CatalogRemove` knows about catalogs: it runs
  `DisableFeature{Now, Force}` then deletes the marker-owned
  `.transfer`/`.feature`/`.feature.d` from the /etc component dir.
- Ambiguity: a bare name found in multiple repos (add) or managed by
  multiple repos (remove) errors listing `repo/name` candidates; the CLI
  accepts `REPO/NAME` or `--repo`.
- Listing cache: `CatalogList` goes through `catalog.CachedList` — a
  per-repo TTL (default 60 min, `catalog.DefaultListCacheTTL`) + ETag
  cache in the client's captured catalog cache directory (the
  `catalog.CacheDir` compatibility default is user cache dir /updex; empty
  disables). Within the TTL no network; after expiry a
  conditional GET revalidates (GitHub 304s are rate-limit-free); on live
  fetch failure a stale entry is served with a warning, except for
  context cancellation/deadline errors, which propagate. `--no-cache`
  (`CatalogListOptions.NoCache`) bypasses and rewrites the cache. The
  cache entry stores the repo's ListURL and is invalidated when it
  changes. `add`/`remove`/`FetchConf` never use the cache.
- Catalog listing sends `GITHUB_TOKEN` only to the trusted
  `https://api.github.com` origin and strips authorization from redirects to
  any other origin; custom `ListURL` hosts never receive GitHub credentials.
- Provenance: `FeatureInfo.Origin`/`OriginName` (set by
  `updex.featureOrigin` from the `.feature` path alone) drive the CATALOG
  column of `features list` — a catalog name for marker-bearing files,
  else `image:<config.ImageName()>` for `/usr/lib`, `local:etc|usr|run`
  for the administered roots (`config.SearchRootIndex`), or `unknown`
  outside them. Kind and name stay separate fields in JSON so a catalog
  named `image` can't be confused for one.
- Catalog operations error when `ClientConfig.Definitions` is set
  (component-scoped, incompatible with `-C`) and return setup guidance
  when no catalogs are configured (`catalog.ErrNoCatalogs`).


## Code Patterns

- **The SDK must never import CLI packages.** No Cobra, no pflag, no
  `cmd/...` imports in `updex/` or the supporting packages.
- **Structured returns.** SDK methods return typed structs, never
  preformatted strings; formatting belongs in `cmd/`.
- **Context first.** Every public SDK method takes `context.Context` as its
  first parameter, then an options struct, and returns result structs + error.
- **Options structs.** New knobs go on the relevant `*Options` struct rather
  than as extra positional parameters or package-level state. Feature-scoped
  operations carry a `Component string` field resolved through
  `Client.loadDomain`. Avoid side effects where possible; use callbacks for
  progress reporting.
- Error messages: lowercase, no trailing punctuation, wrap with `fmt.Errorf("context: %w", err)`; never leak credentials or credential-bearing URLs into error messages
- CLI output: `clix.OutputJSON()` when `clix.JSONOutput` is set (`--json`), text tables otherwise; download progress bars are disabled for JSON/silent modes and write to stderr in interactive mode so stdout remains a data-only stream
- Formatting is plain `gofmt` (`make fmt`); CI fails on unformatted files.
  Follow Go naming conventions, use descriptive names (single letters only
  in very short scopes), and add doc comments on exported identifiers.
- Configuration uses INI format with systemd-style priority paths: `/etc/sysupdate.d/`, `/run/sysupdate.d/`, `/usr/local/lib/sysupdate.d/`, `/usr/lib/sysupdate.d/` (plus the same four roots per discovered component, see above)
- Transfer targets default to staging in `/var/lib/extensions.d`; `CurrentSymlink` is optional legacy state and must not be required for `/var/lib/extensions` sysext links

## Go Version

Go 1.26. Use modern idioms: `any`, `slices`/`maps`/`cmp` packages, `t.Context()`, `slices.SortFunc`, `strings.SplitSeq`, `omitzero` for slice/map/struct JSON tags, `wg.Go()`.

## Testing

- Table-driven tests with descriptive names; cover error paths, not just the
  happy path. Keep tests idempotent so they can run in parallel.
- Use `t.TempDir()` for filesystem operations and `t.Context()` for contexts.
- Mock system commands through the `sysext.SysextRunner` and
  `systemd.SystemctlRunner` interfaces; inject the former through
  `ClientConfig.SysextRunner` and the latter through a temporary
  `ClientConfig.SystemdManager`, rather than mutating global state.
- Use `internal/testutil.NewTestServer()` for HTTP sources and manifests.
- Use `ClientConfig.Paths` (`RuntimePaths`) to give a client its own temp
  trees rather than mutating package globals — independently configured
  clients can run in parallel without save/mutate/restore discipline (see
  [ADR-0011](docs/adr/0011-capture-merged-sysext-state-per-client.md);
  overriding package vars such as `config.SearchRoots` with `t.Cleanup`
  restore, per [ADR-0009](docs/adr/0009-overridable-system-path-vars.md), is
  the superseded compatibility path).
- Run `make fmt` before running tests; for verbose output use `go test -v ./...`.

## Security

Changes that touch downloads, configuration parsing, or filesystem writes get
extra scrutiny:

- Always verify SHA256 hashes before installing, keep decompressed image output
  bounded, and keep GPG verification of `SHA256SUMS` working (it is on by
  default, per systemd-sysupdate).
- Validate names and paths (see `catalog.ValidateSysextName`) so nothing
  traversal-shaped reaches `filepath.Join` or a URL; respect and validate the
  file permissions a transfer configures.
- Use `os.Lstat` and reject non-regular files before writing to a managed
  definition path — `os.Stat` reports a dangling symlink as absent, which
  would let a root-privileged write escape its directory. Reuse the shared
  guard in `updex/fsguard.go` (`managedFileExists`; `os.Lstat` a directory
  path the same way before `MkdirAll`) and write through
  `writeManagedFile` (temp-file-plus-rename), as `writeFeatureDropIn`
  does.
- Make multi-file writes recoverable: snapshot before writing and roll back on
  any failure, as `updex.Client.CatalogAdd` does (both rules are recorded in
  [ADR-0005](docs/adr/0005-transactional-writes-lstat-checks.md)).
- Never expose sensitive information (paths, credential-bearing URLs) in
  error messages; never commit secrets or credentials.

AI-assisted contributions must also follow the
[AI security policy](docs/security/SECURITY-AI.md), including its least
privilege, data-handling, prompt-injection, and human-review requirements,
and the machine-readable controls in
[`.github/policies/ai-governance.json`](.github/policies/ai-governance.json).

If you believe you have found a security vulnerability, do not open a public
issue. Report it privately by emailing the maintainer at
[bketelsen@gmail.com](mailto:bketelsen@gmail.com).

## Troubleshooting

- **GPG verification failures:** First check that the systemd import keyring is
  configured for the transfer's signing key. To intentionally use an unsigned
  source, set `Verify=no` in that source's `.transfer` file. The CLI's
  `--verify` flag is force-enable only: effective verification is the logical
  OR of the flag and the transfer setting, and omitting `Verify=` defaults the
  transfer setting to `yes`. Consequently, `--verify=false` cannot disable
  verification for a transfer that enables it. See the
  [README transfer configuration](README.md#transfer-section) for the
  operator-facing reference.
- **Build issues:** `make build` failing → check `go version` is 1.26.6+;
  dependency download failures → `make tidy`; a missing `golangci-lint` is
  optional locally (`make lint` skips with a message) but required for
  `make ci`, which also fails with
  `expected golangci-lint <pinned>, found <installed>` when the installed
  release differs from `GOLANGCI_LINT_VERSION` in the `Makefile` — install
  the pinned release with the `go install` command it prints.
- **Runtime issues:** "configuration not found" → ensure `.transfer` files
  exist under the standard search paths; "permission denied" → most mutating
  operations require root; download failures → check network connectivity
  and source URL reachability.

## Documentation

**update documentation** After any change to source code, update relevant documentation in AGENTS.md, README.md and the `docs/` tree. A task is not complete without reviewing and updating relevant documentation. When behavior changes, update:

- `README.md` — user-facing usage and flags
- `docs/design/overview.md`, `docs/specs/sdk-api.md`,
  `docs/specs/config-reference.md` — the agent-oriented architecture,
  SDK, and configuration references
- `AGENTS.md` (this file; all instruction aliases resolve here) — when
  conventions or the build workflow change
- `docs/` — topic guides such as `docs/patterns.md` and the public
  `docs/metrics/` index (index every new doc in `docs/README.md`)

**docs/ tree** All repository documentation lives in the single `docs/` tree, in frostyard/core's four-category shape per [frostyard/core ADR-0025](https://github.com/frostyard/core/blob/main/docs/adr/0025-consolidate-repository-docs-into-docs.md) (formerly the separate `yeti/` AI-docs directory): `docs/adr/` (why — repo-local decisions), `docs/design/` (how it fits together), `docs/specs/` (exact contracts), `docs/plans/` (order of work), indexed in [docs/README.md](docs/README.md). [docs/design/overview.md](docs/design/overview.md) is the entry point — read it and the specs under `docs/specs/` for codebase context before performing tasks. New repo-local decisions get an ADR in `docs/adr/` (start from its `TEMPLATE.md`); org-wide decisions belong in frostyard/core — see [docs/org-adrs.md](docs/org-adrs.md). Write these docs to be maximally useful to an AI agent understanding the codebase — detailed architecture, patterns, and decision rationale rather than user-facing guides.

**docs-integrity gate** `node scripts/check-docs.mjs` (the `docs-gate` CI
job) fails on any unindexed doc in the four categories, any dead relative
link in any real-blob Markdown file in the repository (alias symlinks and
`TEMPLATE.md` excluded) — Superseded ADRs included, per
[core ADR-0033](https://github.com/frostyard/core/blob/main/docs/adr/0033-link-maintenance-in-immutable-adrs.md),
which permits link-only repairs to immutable ADRs — or any
broken/repo-escaping symlink; thresholds in `.coverage-thresholds.json` are
all 1.0 with `never_relax: true`.

**conformance aliases** Conformance alias symlinks are listed in
[ADR-0012](docs/adr/0012-acmm-conformance-via-canonical-aliases.md) — edit
their canonical targets, never the aliases. `docs/review-rubric.md`,
`docs/metrics.md`, and `docs/quality.md` resolve to
[docs/specs/pr-review-rubric.md](docs/specs/pr-review-rubric.md),
[docs/specs/pr-acceptance-metric.md](docs/specs/pr-acceptance-metric.md), and
[docs/design/quality-loop.md](docs/design/quality-loop.md).

**session handoffs** Use `.claude/session-summary.md` for concise context needed to continue unfinished work in a later session. Fold durable architecture decisions and non-obvious lessons into the right `docs/` page, or drop them in the `.memory/` inbox (the single learnings inbox, append-only `corrections.jsonl`, drained into `docs/`).

## Pull requests

1. Fork the repository and create a branch off `main`; the org
   squash-merges, so never stack on another PR's branch.
2. Keep changes focused; unrelated fixes belong in a separate PR.
3. Use Conventional Commits for commit messages **and the pull request
   title** (see [Commits & Pull Requests](#commits--pull-requests)).
4. Run `make fmt`, `make ci`, and `node scripts/check-docs.mjs` and make
   sure they pass.
5. Update the documentation and add tests for your change.
6. Classify the change using the [risk tier guide](docs/risk-tiers.md) and
   include the tier rationale in the pull request
   ([`.github/pull_request_template.md`](.github/pull_request_template.md)
   walks through it).
7. Open the PR with a description of what changed and why, and link any
   related issue.

Before requesting review, check the change against the
[pull request review rubric](docs/specs/pr-review-rubric.md). Reviewers apply
its rows to every pull request and label findings blocking / non-blocking /
question / nit, explaining the impact of each finding and suggesting a
concrete resolution; the task-shaped form is
[`.github/prompts/review.prompt.md`](.github/prompts/review.prompt.md).
Automated feedback is advisory: never approve changes, weaken required
checks, or claim verification passed without evidence from the pull request,
and never merge, approve, or release your own work (`.claude/settings.json`
denies these at the tool layer).

Pull requests are automatically labeled for documentation, Go, GitHub Actions,
and dependency changes according to [`.github/labeler.yml`](.github/labeler.yml).
The labeler adds matching labels without removing labels applied by contributors
or reviewers.

CI runs on every pull request (`.github/workflows/test.yml`) and must pass:
lint (golangci-lint), security scan (`govulncheck`), unit tests with coverage,
end-to-end tests, race-detector tests, verification (`go mod tidy`
cleanliness, `go vet`, `gofmt`), docs integrity (`scripts/check-docs.mjs`),
and cross-compiled builds for linux/amd64 and linux/arm64. The unit test job
enforces `max(80.0, baseline - 0.5)` total statement coverage
(`make coverage-check`, `scripts/check-coverage.sh`, and
`.coverage-baseline`); that absolute floor plus ratchet is the enforced
coverage gate. It
also uploads `coverage.out` to Codecov on a best-effort basis
(`continue-on-error`) — the upload currently fails because the repository is
not onboarded on codecov.io, so the project (−1% max) and patch (70%)
statuses `codecov.yml` describes are pending onboarding and not required
checks. The whole loop — declare, review, gate,
learn, observe — is described in
[docs/design/quality-loop.md](docs/design/quality-loop.md).

## Org-wide decisions

Org-level conventions this repo follows are recorded as ADRs in
frostyard/core — see [docs/org-adrs.md](docs/org-adrs.md) for the list that
binds this repo. Change the ADR (in core) before changing behavior it covers.

## License

By contributing, you agree that your contributions are licensed under the
[MIT License](LICENSE) that covers this project.
