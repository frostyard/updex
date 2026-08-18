# AGENTS.md

This file provides guidance to AI coding agents when working with code in this
repository. It is the canonical agent instructions file — `CLAUDE.md`,
`GEMINI.md`, and `.cursorrules` are symlinks to it (frostyard/core ADR-0018).

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

End-to-end tests live in `tests/e2e/`: black-box tests that build the real `updex` binary and run it as a subprocess against fake files and HTTP sources. Successful operations are read-only (no root required); mutating command variants are covered at the argument-validation boundary. CLI integration tests in `cmd/updex/` additionally override package search roots to exercise default component discovery and fake catalogs safely. Run both with `go test -v ./cmd/updex ./tests/e2e/...`.

## Commits & Pull Requests

Commit messages **and pull request titles** use
[Conventional Commits](https://www.conventionalcommits.org/):
`type(scope): summary`, e.g. `test(cli): cover catalog mutation handlers`,
`fix(catalog): refuse symlinked definitions`, `docs(agents): …`. The
repository squash-merges and its squash title default is "commit or PR
title", so the PR title — or, for a single-commit PR, that commit's subject —
becomes the `main` commit that svu versions and the changelog groups by. Make
the first commit conventional too; do not carry an issue's `[quality] …` /
`[scanner] …` title into the commit or PR. `.github/workflows/pr-title.yml`
fails a PR whose title (or lone commit subject) is not conventional. See
[CONTRIBUTING.md](CONTRIBUTING.md#pull-requests) for the type list.

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
[frostyard/core ADR-0013](https://github.com/frostyard/core/blob/main/docs/adr/0013-release-fanout-via-repository-dispatch.md).

## Architecture

updex is a Go SDK and CLI for managing systemd-sysext images. It replicates `systemd-sysupdate` functionality for `url-file` transfers.

**SDK-first design**: All logic lives in the `updex/` package as a public Go API. CLI commands in `cmd/` are thin Cobra wrappers that parse flags, call SDK functions, and format output. SDK code must never import CLI packages.

Key packages:
- `updex/` — Public SDK: `Client` struct with `Features()`, `EnableFeature()`, `DisableFeature()`, `UpdateFeatures()`, `CheckFeatures()`, `Components()`
- `cmd/updex/` — Cobra command handlers calling SDK methods (flags, output formatting, progress bars)
- `config/` — Parses `.transfer` and `.feature` INI files from systemd-style search paths, including systemd-sysupdate "component" discovery (see below)
- `catalog/` — Sysext catalog primitives (see below): `*.catalog` repo config loading, sysext enumeration via a GitHub contents API endpoint, fetching the catalog's published `.conf`, and rendering it into updex `.transfer`/`.feature` files
- `download/` — HTTP downloads with bounded retry for transient failures, SHA256 verification, and decompression (xz, gz, zstd)
- `manifest/` — Fetches/parses SHA256SUMS manifests with bounded retry for transient failures and GPG verification (enabled by default per systemd-sysupdate); response bodies are capped at 4 MiB for manifests and 1 MiB for detached signatures
- `version/` — Pattern matching (`@v` placeholder) and semantic version comparison
- `sysext/` — systemd-sysext integration with mockable `Runner` interface, `/var/lib/extensions` link management, and read-only vacuum planning helpers
- `systemd/` — Generates/installs systemd timer+service units, mockable `Runner` interface

Entry point: `cmd/updex-cli/main.go` → `cmd/updex/root.go`

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
`GITHUB_TOKEN` honored), and optional `Component` (default
`catalog-<name>`), the component the generated files land in.

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

## Code Patterns

- Error messages: lowercase, no trailing punctuation, wrap with `fmt.Errorf("context: %w", err)`
- SDK functions accept a `context.Context` and an options struct, return result structs + error
- CLI output: `common.OutputJSON()` for `--json` flag, text tables otherwise
- Tests use `t.TempDir()` for filesystem operations and mock runners for systemd commands; use `ClientConfig.Paths` (`RuntimePaths`) to give a client its own temp trees rather than mutating package globals — independently configured clients can run in parallel without save/mutate/restore discipline (see ADR-0011)
- Configuration uses INI format with systemd-style priority paths: `/etc/sysupdate.d/`, `/run/sysupdate.d/`, `/usr/local/lib/sysupdate.d/`, `/usr/lib/sysupdate.d/` (plus the same four roots per discovered component, see above)
- Transfer targets default to staging in `/var/lib/extensions.d`; `CurrentSymlink` is optional legacy state and must not be required for `/var/lib/extensions` sysext links

## Go Version

Go 1.26. Use modern idioms: `any`, `slices`/`maps`/`cmp` packages, `t.Context()`, `slices.SortFunc`, `strings.SplitSeq`, `omitzero` for slice/map/struct JSON tags, `wg.Go()`.
## Documentation

**update documentation** After any change to source code, update relevant documentation in AGENTS.md, README.md and the `docs/` tree. A task is not complete without reviewing and updating relevant documentation.

**docs/ tree** All repository documentation lives in the single `docs/` tree, in frostyard/core's four-category shape per [frostyard/core ADR-0025](https://github.com/frostyard/core/blob/main/docs/adr/0025-consolidate-repository-docs-into-docs.md) (formerly the separate `yeti/` AI-docs directory): `docs/adr/` (why — repo-local decisions), `docs/design/` (how it fits together), `docs/specs/` (exact contracts), `docs/plans/` (order of work), indexed in [docs/README.md](docs/README.md). [docs/design/overview.md](docs/design/overview.md) is the entry point — read it and the specs under `docs/specs/` for codebase context before performing tasks. New repo-local decisions get an ADR in `docs/adr/` (start from its `TEMPLATE.md`); org-wide decisions belong in frostyard/core — see [docs/org-adrs.md](docs/org-adrs.md). Write these docs to be maximally useful to an AI agent understanding the codebase — detailed architecture, patterns, and decision rationale rather than user-facing guides.

**session handoffs** Use `.claude/session-summary.md` for concise context needed to continue unfinished work in a later session. Fold durable architecture decisions and non-obvious lessons into the right `docs/` page, or drop them in the `.memory/` inbox (the single learnings inbox, drained into `docs/`).

## Org-wide decisions

Org-level conventions this repo follows are recorded as ADRs in
frostyard/core — see [docs/org-adrs.md](docs/org-adrs.md) for the list that
binds this repo. Change the ADR (in core) before changing behavior it covers.
