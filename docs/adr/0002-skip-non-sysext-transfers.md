# 0002 — Silently skip non-sysext transfers in the default domain

- **Status:** Accepted
- **Date:** 2026-08-12

## Context

On native (bootc A/B) images, the legacy default `sysupdate.d/` directory is
shared: systemd-sysupdate's own OS transfers live there alongside any sysext
transfers. Those OS transfers are not sysext-shaped — the A/B root
partitions use `[Target] Type=partition`, and the UKI uses
`Type=regular-file` with `PathRelativeTo=boot` (a target relative to the
ESP). updex only manages sysext images; treating these files as errors would
make every updex command fail on exactly the images updex is built for,
while treating them as sysexts would have updex writing into partitions and
the ESP.

## Decision

updex discriminates sysext-shaped transfers structurally and silently drops
everything else. `config.IsSysextTransfer` (`config/transfer.go`) requires:

- `Source.Type == "url-file"`,
- `Target.Type` empty or `"regular-file"` (empty is treated as
  `regular-file` to match existing sysext `.transfer` files that never set
  it), and
- `Target.PathRelativeTo == ""` — the discriminator that separates the UKI
  transfer from a genuine sysext regular-file target, since both carry
  `Type=regular-file`.

`config.FilterSysextTransfers` applies this predicate wherever a domain is
loaded: `LoadAllTransfers` always filters, and `Client.loadDomain` filters
the `-C` and `--component` paths too. Skipped transfers produce no warning
and no error — they are simply outside updex's domain.

## Consequences

- updex coexists with the OS's own sysupdate.d definitions; `updex features
  update` on a native image can never touch the A/B partitions or the UKI.
- A genuinely misconfigured sysext transfer that trips the predicate (e.g.
  a stray `PathRelativeTo=`) disappears silently rather than erroring;
  operators must know the shape rules to debug it.
- The predicate is a compatibility contract: new systemd-sysupdate target
  types default to "not a sysext", which is the safe direction.

## Alternatives considered

- **Erroring on unrecognized transfers:** breaks every native image, where
  sharing the directory is systemd's documented layout.
- **Filtering by filename or component convention:** the fedora-sysexts
  catalog and hand-written files follow no common naming scheme; the
  transfer's own `[Source]`/`[Target]` shape is the only reliable signal.
- **Requiring sysexts to live in named components only:** would break
  every existing installation with sysext transfers in the legacy default
  directory.

## References

- Implements: [`config/transfer.go`](../../config/transfer.go)
  (`IsSysextTransfer`, `FilterSysextTransfers`),
  [`updex/domain.go`](../../updex/domain.go) (filter applied per scope)
- Shapes: [design/overview.md — Non-sysext transfers](../design/overview.md#non-sysext-transfers),
  [specs/config-reference.md — Non-sysext transfers (skipped, not errored)](../specs/config-reference.md#non-sysext-transfers-skipped-not-errored),
  [specs/sdk-api.md — Component scoping](../specs/sdk-api.md#component-scoping)
- Builds on: [core ADR-0005 — Transport discrimination by marker file and /run update-state contract](https://github.com/frostyard/core/blob/main/docs/adr/0005-native-ab-marker-and-update-state-files.md),
  [core ADR-0008 — Sysext distribution layout and update contract](https://github.com/frostyard/core/blob/main/docs/adr/0008-sysext-distribution-and-update-contract.md)
