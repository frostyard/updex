# 0015 — Detect merged sysexts from hierarchy metadata

- **Status:** Accepted
- **Date:** 2026-10-22

## Context

Issue #444 reports that `updex features disable <name> --now` removes a merged
sysext image without the documented `--force` refusal on snosi hosts.

ADR-0011 made each client capture `RuntimePaths.RunExtensionsDir` and treat a
matching entry there as merged state. Verification on a snosi host with
systemd 262 shows that this check cannot fire:

- `sysext.GetActiveVersionIn` considered an image active only when a
  `CurrentSymlink` existed in the target directory, or an entry in
  `RunExtensionsDir` matched the target pattern.
- updex removes the legacy `CurrentSymlink` on every install.
- `/run/extensions` is one of systemd-sysext's image search directories, not a
  record of merged state, and it does not exist on snosi.

systemd-sysext records merged state inside each merged hierarchy, in
`<hierarchy>/.systemd-sysext/`. The default hierarchies are `/usr` and `/opt`.
Real data from the host:

`/usr/.systemd-sysext/extensions` holds one image name per line:

```
bitwarden
docker
vscode
```

`/usr/.systemd-sysext/origin` holds JSON (systemd 258 and newer; older
systemd writes only `extensions`):

```json
{
  "mutable" : { "mode" : "no" },
  "extensions" : {
    "docker" : {
      "path" : "/var/lib/extensions.d/docker_5+29.9.1-1~debian.13~trixie_13_x86-64.raw",
      "onMountId" : 2147483764,
      "fileHandle" : { "type" : 77, "handle" : "6a8d..." },
      "crtime" : 1791467316045655,
      "mtime" : 1791467316149657
    }
  }
}
```

`/opt/.systemd-sysext` does not exist when nothing is merged into `/opt`. The
transfer on that host is `docker.transfer`: `[Target] Path=/var/lib/extensions.d/`,
`MatchPattern=docker_@v_%w_%a.raw docker_@v_%w_%a.raw.xz …`,
`CurrentSymlink=docker.raw`. Its `Component` is `docker`, derived from the file
name. The `/var/lib/extensions/docker.raw` link points at the staged file.

The previous signal also failed open: a read error only warned, then removal
continued.

## Decision

`sysext` gains one active-state reader that consults three signals, in this
order, and the SDK's destructive `--now` guard uses it:

1. the legacy `CurrentSymlink` in the target directory;
2. the merged-state metadata under each configured hierarchy; and
3. the runtime image search directory (`/run/extensions`), when it is not
   empty. An image there is merged on the next refresh, so it stays a
   conservative signal.

`ActiveStatePaths{DefaultDir, RunExtensionsDir, Hierarchies}` carries the
paths. `GetActiveVersionWith` implements the order above.
`GetActiveVersion` and `GetActiveVersionAt` use the production
`RunExtensionsDir` and a clone of `MergedHierarchies` (`/usr`, `/opt`).
`GetActiveVersionIn` keeps its signature and its two-signal behavior (no
hierarchy metadata) for compatibility, and its doc comment says so.

`mergedVersionInHierarchy` reads `<hierarchy>/.systemd-sysext/origin` through
a 1 MiB `io.LimitReader`. When it exists and parses, an entry matches when its
name equals `Transfer.Component` (with a non-empty component) or when
`filepath.Base(entry.path)` matches a target pattern. The version comes from
that base name; when extraction fails the result is `UnknownVersion`
(`"unknown"`). When `origin` is absent, does not parse, or does not name the
image, the helper also reads `<hierarchy>/.systemd-sysext/extensions` through a
64 KiB limit, trims each
line, skips empty lines, and matches a line against `Transfer.Component` or
against a target pattern after appending the target extension. The extensions
list has no version, so a match returns `UnknownVersion`. Neither file names
the image means the hierarchy does not record the image. Any other read error
(for example permission denied) returns `read merged state: …`. A malformed or
partial `origin` must not produce a false "not merged" result, because the
guard is destructive.

`updex.RuntimePaths` gains `SysextHierarchies []string`, whose zero value
resolves to a clone of `sysext.MergedHierarchies` once at client construction,
like the other path slices. `DisableFeature` passes the captured values to
`GetActiveVersionWith`.

The guard fails closed: when the active-state check returns an error for a
transfer, `DisableFeature` keeps its warning and also appends
`<component> (merge state unknown)` to the list that requires `--force`.
`--force` behaves as before.

## Consequences

- On hosts where systemd-sysext merged the image, `features disable --now` and
  `catalog remove` now refuse without `--force`, as documented.
- `features disable --now` reports the version `unknown` when the merged image
  is known only from an `extensions` list (systemd older than 258).
- A merged-state read error now refuses removal instead of continuing, so an
  unreadable hierarchy can require `--force` on a host that is not merged.
  `--force` still removes the image.
- Rootless tests cover the snosi layout by injecting temporary hierarchies
  through `RuntimePaths.SysextHierarchies`.
- Compatibility callers of `GetActiveVersionIn` do not read hierarchy
  metadata; they must move to `GetActiveVersionWith` to see the new signal.

## Alternatives considered

- **Shell out to `systemd-sysext status --json`:** rejected because it runs a
  subprocess against the host systemd, so tests cannot isolate it with
  temporary paths, which breaks the ADR-0011 client boundary.
- **Read `/usr/lib/extension-release.d/`:** rejected because it records image
  names only, with no source path and no version of the merged file.
- **Treat the `/var/lib/extensions` link as active:** rejected in ADR-0011,
  and still wrong: the link only makes an image available for a future merge.
- **Keep `/run/extensions` as the only signal:** rejected because systemd-sysext
  never writes merged state there on snosi.

## References

- Supersedes: [ADR-0011](0011-capture-merged-sysext-state-per-client.md)
- Builds on: [ADR-0010](0010-instance-scoped-runtime-paths.md)
- Shapes: [design overview](../design/overview.md),
  [SDK API reference](../specs/sdk-api.md)
- Implements: [`sysext/manager.go`](../../sysext/manager.go),
  [`updex/features.go`](../../updex/features.go),
  [`updex/updex.go`](../../updex/updex.go)
- Fixes: [issue #444](https://github.com/frostyard/updex/issues/444)
