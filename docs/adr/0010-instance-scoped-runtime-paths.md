# 0010 — Scope runtime paths to each SDK client

- **Status:** Accepted
- **Date:** 2026-08-12

## Context

ADR-0009 made system path anchors exported package variables so tests could
exercise real filesystem operations in temporary directories. That avoided
constructor plumbing, but it also made those paths mutable process-wide state.

`updex.Client` otherwise owns its runtime dependencies: HTTP transport,
reporting, download progress, and the systemd-sysext runner. Path-dependent
behavior is the exception. Definition discovery, catalog discovery, catalog
caching, os-release reads, and sysext link writes still consult package globals
at call time. Two clients in one process therefore cannot use different roots,
and changing a path while another goroutine reads it is a data race.

The SDK is intended for embedding in long-running Go programs, not only for the
single-client CLI. Client isolation and safe concurrent use are therefore part
of the abstraction rather than test-only concerns.

## Decision

Each `updex.Client` owns an immutable runtime-path configuration captured by
`NewClient`. Zero values resolve to the existing production paths, so the CLI
and existing SDK callers retain their current behavior.

The path configuration covers:

- systemd-sysupdate definition search roots;
- os-release lookup paths;
- catalog configuration roots and listing cache directory; and
- the systemd-sysext link directory.

SDK methods pass that configuration to path-dependent collaborators in
`config`, `catalog`, and `sysext`. Those packages expose explicit,
value-parameterized operations for SDK use. Existing package-level functions
remain compatibility wrappers over production defaults during migration, but
SDK internals do not read mutable package variables.

The migration proceeds from read-only discovery to writes:

1. Add an immutable path value with production defaults and defensive copies
   for slices.
2. Parameterize config and catalog loading, cache access, provenance, and
   derived write paths.
3. Parameterize sysext link operations.
4. Wire the value through `ClientConfig` and all SDK operations.
5. Replace save/mutate/restore tests with independently configured clients and
   run concurrent isolation tests under the race detector.
6. Deprecate exported mutable path variables after all compatibility wrappers
   can use immutable defaults.

No path may fall back to a package global after client construction. Tests must
prove that mutating a compatibility variable cannot redirect an existing
client.

## Consequences

- Multiple clients can safely target independent filesystem trees in one
  process.
- Tests can run path-dependent cases in parallel without process-global
  save/restore discipline.
- The public client matches its implied isolation boundary: all runtime
  dependencies are captured at construction.
- Public supporting packages gain explicit parameterized entry points in
  addition to compatibility wrappers, increasing API surface during migration.
- Path values must be threaded through several packages, adding deliberate
  plumbing that ADR-0009 previously avoided.
- Mutable globals cannot be removed immediately without a compatibility cycle;
  deprecation and removal require normal public-API versioning.

## Alternatives considered

- **Keep ADR-0009 and document clients as process-global:** rejected because it
  contradicts the SDK-first embedding model and leaves concurrent use unsafe.
- **Protect globals with a mutex:** rejected because synchronization prevents a
  data race but cannot give two clients different roots.
- **Use one chroot-style root prefix:** rejected because user cache paths and
  os-release overrides do not necessarily share one filesystem root.
- **Inject an `fs.FS`:** rejected as the primary boundary because updex performs
  privileged writes, renames, symlink operations, and command execution that
  are not represented by the read-only `fs.FS` interface.

## References

- Supersedes: [ADR-0009](0009-overridable-system-path-vars.md)
- Shapes: [design overview](../design/overview.md),
  [SDK API reference](../specs/sdk-api.md)
- Tracks: [updex issue #279](https://github.com/frostyard/updex/issues/279)
