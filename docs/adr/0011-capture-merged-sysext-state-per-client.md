# 0011 — Capture merged sysext state per SDK client

- **Status:** Accepted
- **Date:** 2026-08-17

## Context

ADR-0010 moved path-dependent SDK behavior from mutable package globals into
an immutable `RuntimePaths` value captured by each `updex.Client`. That
decision covered definition discovery, os-release lookup, catalog paths, and
the `/var/lib/extensions` link directory, but `sysext.GetActiveVersionAt`
still read `/run/extensions` as a literal path.

`DisableFeature` consults active state before destructive `--now` removal. The
hard-coded merged-image directory prevented independently configured clients
from isolating that check and forced tests to rely on the legacy
`CurrentSymlink` signal. It also violated ADR-0010's intended client boundary:
an SDK operation still consulted process-global filesystem state after client
construction.

The two sysext directories represent different facts. `/var/lib/extensions`
contains links that make images available to systemd-sysext, while
`/run/extensions` records images in the current merged state. Availability is
not proof that an image is active.

## Decision

Each `updex.Client` captures the merged-image state directory alongside its
other runtime paths. `RuntimePaths.RunExtensionsDir` defaults to the
`sysext.RunExtensionsDir` production constant (`/run/extensions`) and is
resolved once by `NewClient`.

The sysext package exposes:

- `GetActiveVersion`, which uses the production target and merged-state
  defaults;
- `GetActiveVersionAt`, which preserves the existing explicit target fallback
  while using the production merged-state default; and
- `GetActiveVersionIn`, which accepts both the target fallback and
  merged-state directories explicitly for SDK use.

`updex.Client.DisableFeature` calls `GetActiveVersionIn` with its captured
`SysextLinkDir` and `RunExtensionsDir`. An extension is considered active for
the `--force` gate when its version is identified by either:

1. the transfer's legacy `CurrentSymlink`; or
2. a matching entry in `RunExtensionsDir`.

The `SysextLinkDir` link is not an active-state signal. It only supplies the
fallback location for a legacy `CurrentSymlink` when `Target.Path` is absent
and otherwise represents an image available for a future merge.

No SDK active-state check may read a hard-coded runtime directory after client
construction. Compatibility wrappers may use the single production constant.

## Consequences

- Rootless tests can construct an isolated merged-state directory and exercise
  `DisableFeature` refusal and forced removal without touching `/run`.
- Multiple clients can inspect independent merged-state trees without
  process-global mutation.
- The public `RuntimePaths` and sysext helper APIs gain one additional path
  field and one explicit-directory function.
- Callers of `GetActiveVersionIn` must provide a valid merged-state directory;
  the SDK always supplies its resolved non-empty value.
- A link in `/var/lib/extensions` alone does not require `--force`, because it
  does not establish that the image is currently merged.

## Alternatives considered

- **Treat `SysextLinkDir` as active:** rejected because the link expresses
  availability, not current merged state, and would require `--force` for
  inactive extensions.
- **Keep `/run/extensions` hard-coded:** rejected because it breaks client
  isolation and prevents rootless coverage of the destructive-operation gate.
- **Remove the legacy `CurrentSymlink` signal:** rejected in this change to
  preserve compatibility for transfers that still define and maintain it.
- **Add a runner method that shells out to systemd-sysext:** rejected because
  the existing filesystem snapshot is sufficient and keeps the check
  rootless-testable.

## References

- Supersedes: [ADR-0010](0010-instance-scoped-runtime-paths.md)
- Builds on: [ADR-0009](0009-overridable-system-path-vars.md)
- Shapes: [design overview](../design/overview.md),
  [SDK API reference](../specs/sdk-api.md)
- Implements: [`sysext/manager.go`](../../sysext/manager.go),
  [`updex/features.go`](../../updex/features.go),
  [`updex/updex.go`](../../updex/updex.go)
