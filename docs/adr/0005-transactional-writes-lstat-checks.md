# 0005 — Snapshot-and-rollback writes with Lstat-only file checks

- **Status:** Accepted
- **Date:** 2026-08-12

## Context

`CatalogAdd` mutates several paths as root in one logical operation: a
`.transfer`, a `.feature`, the `00-updex.conf` enable drop-in, downloaded
staging images, and the active sysext link. A failure partway through —
including a download, link replacement, final refresh, or definition write —
would leave a half-installed state: definitions present with a broken enable,
a mismatched old/new pair that every later `features update` retries, or
staged/link state that no longer matches the definitions.
Separately, these privileged writes resolve paths an unprivileged user may
have planted symlinks at: `os.Stat` reports a *dangling* symlink as absent,
so a naive existence check would skip the ownership guard and the following
root write would follow the link and create its target anywhere on the
filesystem.

## Decision

Privileged multi-file mutations are transactional and refuse to operate
through non-regular files:

- Before mutating each part of the operation, `CatalogAdd` snapshots the
  generated definitions and drop-in, all staged entries matching the
  transfer's target patterns, and the transfer's sysext link. Every failure
  past that point runs a single `rollback()` closure that restores each
  snapshot exactly: a fresh add rolls back to nothing, while a re-add restores
  its previous definitions, staged images, and link target. Unrelated staging
  entries are never removed. A definition path that existed but could not be
  read (`existed && !captured`) is left strictly alone, since removing it
  would destroy state the snapshot cannot rebuild.
- The original operation error is always preserved. If restoration itself
  fails, `CatalogAdd` joins the rollback error into the returned error so
  callers can identify both the original failure and the state that could not
  be restored.
- Existence checks at managed definition paths use `os.Lstat`, never
  `os.Stat`, and anything present that is not a regular file is an error
  (`managedFileExists`) — resolved deliberately by the operator, never
  written through. Stat failures other than not-exist are errors too,
  so an unreadable path can never be misread as absent and skip a guard.
- `CatalogRemove` validates both definitions this way *before* its
  destructive `DisableFeature{Now}` step, so a symlink or foreign file
  cannot produce a half-completed teardown.

CONTRIBUTING.md's Security section makes both rules binding for any new
code that writes to managed definition paths.

## Consequences

- No enabled-but-broken state and no destroyed working definition: a failed
  add leaves definitions, matching staging images, and the sysext link as they
  were, and a failed re-add restores the previous working install.
- Symlink-planting cannot redirect a root write outside the component
  directory (fixed during PR #137 review, fourth round).
- Every new multi-file mutation must snapshot first and route all failure
  paths through rollback — forgetting one write call silently reopens the
  truncation hazard, which reviews must watch for.
- A rollback failure can still leave debris after an I/O environment has
  failed, but it is never silent: the returned joined error names the
  restoration step while retaining the original operation error.

## Alternatives considered

- **Write-to-temp + rename per file:** makes each file atomic but not the
  *set*; the failure mode being prevented is a mismatched pair, not a
  torn single file.
- **`os.Stat` existence checks:** reports dangling symlinks as absent —
  precisely the case that turns a root write into an arbitrary-path
  create.
- **Following symlinks but verifying the resolved target:** more code and
  still surprising; updex manages definitions in place, and a symlinked
  definition is unusual enough to demand operator resolution.

## References

- Implements: [`updex/catalog.go`](../../updex/catalog.go)
  (`fileSnapshot`, `catalogManagedStateSnapshot`, `snapshotFile`, `restore`,
  `CatalogAdd` rollback closure); [`updex/fsguard.go`](../../updex/fsguard.go)
  (`managedFileExists`, `writeManagedFile` — the shared Lstat-only
  file guard and temp-file-plus-rename write);
  [`updex/features.go`](../../updex/features.go) (`writeFeatureDropIn` —
  the `<feature>.feature.d/` directory and the `00-updex.conf` drop-in
  are both Lstat-checked before `EnableFeature`/`DisableFeature` write, a
  symlink or other non-regular entry at either path is refused rather
  than written through, and the drop-in is written via
  temp-file-plus-rename);
  [`systemd/manager.go`](../../systemd/manager.go) (`unitFileState`,
  `writeUnitFile`, `Install`, `Exists` — the Lstat-only existence rule
  applied to the root-written `updex-update.timer`/`.service` unit files:
  a symlink, directory, or other non-regular entry at a unit path is
  refused rather than written through, and units are written via
  temp-file-plus-rename so the write itself never follows a link)
- Shapes: [design/overview.md — Catalogs](../design/overview.md#catalogs-catalog-updexcataloggo),
  [design/overview.md — Auto-update daemon](../design/overview.md#auto-update-daemon),
  [CONTRIBUTING.md — Security](../../CONTRIBUTING.md#security)
- Builds on: [ADR-0003](0003-catalog-ownership-marker.md)
