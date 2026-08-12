# 0006 — RenderTransfer is a byte-preserving line transform

- **Status:** Accepted
- **Date:** 2026-08-12

## Context

`catalog add` turns a catalog-published `.conf` (a genuine
systemd-sysupdate transfer file) into the `.transfer` updex writes locally.
The published files contain systemd `%` specifiers — notably `%w` (OS
version/release) and `%a` (architecture) in `MatchPattern` — plus comments
and formatting chosen upstream. Round-tripping through an INI writer would
re-serialize the whole file: comments dropped, key order changed, and —
fatally — any library-side expansion or quoting of `%` specifiers would
freeze the file to the Fedora release current at add time, breaking the
sysext across OS upgrades. The written file must also stay trivially
diffable against the catalog original so operators can see exactly what
updex changed.

## Decision

`catalog.RenderTransfer` (`catalog/catalog.go`) never round-trips through
an INI writer. It is a line-by-line transform that makes exactly three
changes and preserves every other byte:

1. prepends the `GeneratedMarker` ownership header (ADR-0003),
2. injects `Features=<name>` immediately after `[Transfer]` (replacing any
   existing `Features=` line), tying the transfer to its generated
   feature, and
3. drops `[Target] CurrentSymlink` — updex manages the
   `/var/lib/extensions` link itself and actively removes legacy staging
   symlinks.

`%w`/`%a` and all other specifiers stay unexpanded in the written file;
expansion happens at config load time, so the definition keeps tracking the
running OS release and architecture. `ini.Load` is used only to *validate*
that `[Source]` and `[Target]` sections exist before the transform runs —
its parsed form is never written back.

## Consequences

- A generated `.transfer` diffs against the catalog original by exactly
  the header, the `Features=` line, and any removed `CurrentSymlink` —
  auditable at a glance.
- The file survives Fedora release upgrades without regeneration, because
  the release is resolved at every load rather than baked in at add time.
- The transform must understand just enough INI structure (section
  headers, `Key=value` lines) to place its edits; exotic-but-valid INI
  (line continuations inside `[Transfer]`) could in principle confuse the
  line classifier, a narrower risk than full re-serialization.
- Upstream formatting quirks are propagated verbatim; updex does not
  normalize catalog files.

## Alternatives considered

- **Parse and re-serialize with the ini library:** loses comments and
  ordering, and risks specifier mangling; also makes the written file
  diff-noisy against upstream.
- **Expanding `%w`/`%a` at add time:** pins the transfer to one Fedora
  release; the file silently stops matching new versions after an OS
  upgrade.
- **Storing the original and patching at load time:** two files to keep
  consistent where one self-contained definition suffices.

## References

- Implements: [`catalog/catalog.go`](../../catalog/catalog.go)
  (`RenderTransfer`, `iniKeyOf`)
- Shapes: [design/overview.md — Catalogs](../design/overview.md#catalogs-catalog-updexcataloggo),
  [specs/config-reference.md — Systemd Specifiers](../specs/config-reference.md#systemd-specifiers)
- Builds on: [ADR-0003](0003-catalog-ownership-marker.md),
  [core ADR-0015 — os-release is the image identity surface; resolve VARIANT_ID → IMAGE_ID → ID](https://github.com/frostyard/core/blob/main/docs/adr/0015-os-release-image-identity.md)
