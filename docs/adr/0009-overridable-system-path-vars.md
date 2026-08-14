# 0009 — System paths are exported package vars, overridable in tests

- **Status:** Superseded by [0010](0010-instance-scoped-runtime-paths.md)
- **Date:** 2026-08-12

## Context

updex's behavior is anchored to absolute system paths: the four
`sysupdate.d` search roots, `/var/lib/extensions`, the
`updex/catalogs.d` config roots, the listing cache directory, and the
os-release files. Tests must exercise the real read/write code paths —
component discovery, drop-in writes, catalog add/remove, cache
round-trips — without root privileges or touching the live system.
Dependency-injecting every path through constructors would thread
plumbing through every package for values that are constants in
production.

## Decision

System path anchors are exported package `var`s, not `const`s, and tests
override them with `t.Cleanup` restoration:

- `config.SearchRoots` (`/etc`, `/run`, `/usr/local/lib`, `/usr/lib`) —
  `config/component.go`
- `config.OSReleasePaths` — `config/transfer.go`
- `sysext.SysextDir` (`/var/lib/extensions`) — `sysext/manager.go`
- `catalog.ConfigRoots` (`<root>/updex/catalogs.d`) — `catalog/repo.go`
- `catalog.CacheDir` — `catalog/cache.go`

Derived paths are computed from the vars at call time, not init time —
e.g. `config.EtcComponentDir` derives from `SearchRoots[0]` so tests that
override `SearchRoots` exercise real write paths. CONTRIBUTING.md's
testing rules make the pattern binding: override the package vars instead
of writing to real system paths, and restore them with `t.Cleanup`.
Process-external dependencies (running commands, HTTP) use interface
injection instead (`sysext.SysextRunner`, `systemd.SystemctlRunner`,
`ClientConfig.HTTPClient`); the var pattern is only for path anchors.

## Consequences

- Unit tests cover the genuine filesystem logic — discovery, priority
  order, drop-in application, transactional writes — in `t.TempDir()`
  sandboxes with no mocking layer between the code and the filesystem.
- The vars are public API a library consumer could mutate; this is a
  deliberate trade of API immutability for testability. Consumers are
  expected to treat them as constants, and the docs describe them as
  test-overridable rather than as configuration.
- Tests that override the vars must restore them and cannot run in
  parallel with tests reading the same var; the `t.Cleanup` rule in
  CONTRIBUTING.md carries that discipline.

## Alternatives considered

- **Constructor/dependency injection of paths:** threads plumbing through
  `config`, `sysext`, and `catalog` for values with exactly one
  production value; the interfaces stay noisier forever to serve tests.
- **A process-wide root prefix (chroot-style "rootdir" parameter):**
  cleaner in principle but invasive to retrofit, and awkward for paths
  that are not all under one root (user cache dir vs `/etc`).
- **`const` paths with filesystem abstraction (fs.FS):** read-side only;
  the write paths (drop-ins, catalog files, unit files) are the ones that
  most need real coverage.

## References

- Implements: [`config/component.go`](../../config/component.go)
  (`SearchRoots`, `EtcComponentDir`),
  [`config/transfer.go`](../../config/transfer.go) (`OSReleasePaths`),
  [`sysext/manager.go`](../../sysext/manager.go) (`SysextDir`),
  [`catalog/repo.go`](../../catalog/repo.go) (`ConfigRoots`),
  [`catalog/cache.go`](../../catalog/cache.go) (`CacheDir`)
- Shapes: [design/overview.md — Components](../design/overview.md#components-configcomponentgo),
  [CONTRIBUTING.md — Testing](../../CONTRIBUTING.md#testing),
  [specs/sdk-api.md — Supporting Packages](../specs/sdk-api.md#supporting-packages)
- Builds on: [core ADR-0004 — Product-namespaced filesystem paths, split by lifetime tier](https://github.com/frostyard/core/blob/main/docs/adr/0004-product-namespaced-filesystem-tiers.md)
