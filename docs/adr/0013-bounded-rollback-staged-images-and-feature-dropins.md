# 0013 — Bounded-memory rollback for staged images and guarded feature drop-ins

- **Status:** Accepted
- **Date:** 2026-08-24

## Context

ADR-0005 made `CatalogAdd`/`CatalogRemove` transactional over the
`.transfer`, `.feature`, and `00-updex.conf` drop-in, and required
`os.Lstat`-only existence checks so a planted symlink cannot redirect a
privileged write. Since then `CatalogAdd` grew to manage two more kinds of
state in the same transaction — downloaded staging images and the active
sysext link — and `writeFeatureDropIn` gained its own root-privileged write
path outside `catalog.go`. Extending the original mechanism unchanged would
have reopened both problems ADR-0005 closed:

- `fileSnapshot`/`snapshotFile` held every snapshot's bytes in memory. Once
  staged image backups (multi-megabyte sysext images) went through the same
  path, heap use would scale with retained image size instead of staying
  bounded, and a rollback failure still only returned the original operation
  error, silently discarding which restoration step left debris.
- `writeFeatureDropIn` wrote `<feature>.feature.d/00-updex.conf` with
  `os.MkdirAll` + `os.WriteFile`, so a symlink planted at the drop-in
  directory or file was followed by a root write — the same class of bug
  ADR-0005 closed for `catalog.go`, but the guard had not been applied here.

## Decision

`CatalogAdd`'s transaction and the shared managed-file guard are extended
rather than reworked:

- Before mutating each part of the operation, `CatalogAdd` snapshots the
  generated definitions and drop-in, all staged entries matching the
  transfer's target patterns, and the transfer's sysext link
  (`catalogManagedStateSnapshot`). Every failure past that point runs a
  single `rollback()` closure that restores each snapshot exactly: a fresh
  add rolls back to nothing, while a re-add restores its previous
  definitions, staged images, and link target. Unrelated staging entries are
  never removed.
- Matching regular staged images are streamed to same-directory temporary
  backups instead of retained in memory. Heap use is therefore bounded
  independently of image size, while temporary disk use scales with retained
  image size; backup files are removed after success or rollback. Matching
  staged entries and link destinations that are neither regular files nor
  symlinks are refused before install rather than accepted with a rollback
  strategy that cannot reconstruct them.
- The original operation error is always preserved. If restoration itself
  fails, `CatalogAdd` joins the rollback error into the returned error so
  callers can identify both the original failure and the state that could
  not be restored.
- `managedFileExists` and the temp-file-plus-rename writer move from
  `catalog.go` into a shared `updex/fsguard.go`, and `writeFeatureDropIn`
  adopts them: the `<feature>.feature.d/` directory is `os.Lstat`-checked
  and created only when absent (present but not a real directory is an
  error, nothing written); the drop-in path itself is checked with
  `managedFileExists`, so a symlink there, dangling or live, is refused
  before any write; and the file is written via `writeManagedFile`
  (create-temp, write, sync, close, rename), so the write itself never
  follows a link that appears between check and write.

## Consequences

- No enabled-but-broken state and no destroyed working definition: a failed
  add leaves definitions, matching staging images, and the sysext link as
  they were, and a failed re-add restores the previous working install.
- Rollback memory use no longer scales with staged image size, so a large
  sysext image cannot turn a failed add into an out-of-memory failure.
- A rollback failure can still leave debris after an I/O environment has
  failed, but it is never silent: the returned joined error names the
  restoration step while retaining the original operation error.
- Symlink-planting at the feature drop-in path cannot redirect a root write,
  closing the gap ADR-0005 left between `catalog.go` and `features.go`.
- Every new caller of the managed-file guard now shares one implementation
  (`updex/fsguard.go`) instead of each package reimplementing the Lstat
  check and atomic write.

## Alternatives considered

- **Keep unbounded in-memory image backups:** simplest, but heap use would
  scale with the largest staged sysext image, unbounded by the size of the
  actual definitions being changed.
- **Best-effort rollback with the restoration error dropped, as before:**
  keeps `CatalogAdd`'s signature unchanged but leaves debris invisible;
  joining the error costs nothing at call sites, which already handle a
  single wrapped error.
- **Guard `writeFeatureDropIn` with its own ad hoc Lstat check instead of
  sharing `fsguard.go`:** rejected because it would duplicate the check
  `catalog.go` and `systemd/manager.go` already share and could drift from
  it independently.

## References

- Supersedes: [ADR-0005](0005-transactional-writes-lstat-checks.md)
- Builds on: [ADR-0003](0003-catalog-ownership-marker.md)
- Implements: [`updex/catalog.go`](../../updex/catalog.go)
  (`fileSnapshot`, `catalogManagedStateSnapshot`, `snapshotFile`, `restore`,
  `CatalogAdd` rollback closure); [`updex/fsguard.go`](../../updex/fsguard.go)
  (`managedFileExists`, `writeManagedFile` — the shared Lstat-only file
  guard and temp-file-plus-rename write); [`updex/features.go`](../../updex/features.go)
  (`writeFeatureDropIn` — the `<feature>.feature.d/` directory and the
  `00-updex.conf` drop-in are both Lstat-checked before
  `EnableFeature`/`DisableFeature` write)
- Shapes: [design/overview.md — Catalogs](../design/overview.md#catalogs-catalog-updexcataloggo),
  [design/overview.md — Enable/disable feature](../design/overview.md#enabledisable-feature),
  [CONTRIBUTING.md — Security](../../CONTRIBUTING.md#security)
