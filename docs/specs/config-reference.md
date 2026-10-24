# Configuration Reference

updex uses INI-format configuration files loaded from systemd-style search paths.

## Search Paths

Searched in priority order (first occurrence of a filename wins):

1. `/etc/sysupdate.d/`
2. `/run/sysupdate.d/`
3. `/usr/local/lib/sysupdate.d/`
4. `/usr/lib/sysupdate.d/`

The `-C` / `--definitions` flag overrides all paths with a single custom directory.

### Components

The four roots above (`/etc`, `/run`, `/usr/local/lib`, `/usr/lib` —
`config.SearchRoots`) are also searched for named `sysupdate.<name>.d/`
directories (systemd-sysupdate "components", sysupdate.d(5) "Components"),
with the same priority order per component: e.g.
`/etc/sysupdate.docker.d/` overrides `/usr/lib/sysupdate.docker.d/`. `<name>`
must match `[a-zA-Z0-9_-]+`.
Moving a sysext's files from `/usr/lib/sysupdate.d/` to a named directory
gives it an independent versioning scope, for example:

```
/usr/lib/sysupdate.docker.d/docker.transfer
/usr/lib/sysupdate.docker.d/docker.feature
```

`updex components` lists discovered component names, source directories and
feature counts.

By default (no `-C`/`--definitions`, no `--component`), updex's config
domain is the **union** of the legacy default `sysupdate.d/` directory above
and every discovered component. Feature/transfer names are expected to be
globally unique across that union; a name defined in more than one source
resolves to the most specific one (a named component beats the legacy
default directory) and is reported as a warning. `--component=<name>` scopes
an operation to a single component's own search paths instead. `-C` bypasses
component discovery entirely and behaves exactly as before components
existed. See [design/overview.md — Components](../design/overview.md#components-configcomponentgo)
for the full design; the scope-resolution contract is pinned by
[ADR-0001](../adr/0001-read-domain-resolution-via-loaddomain.md).
Enabling or disabling a named-component feature writes
`/etc/sysupdate.<name>.d/<feature>.feature.d/00-updex.conf`; a legacy default
or `-C`-loaded feature writes
`/etc/sysupdate.d/<feature>.feature.d/00-updex.conf`.

## Feature Files (`.feature`)

Define a named feature that groups one or more transfers.

**Filename**: `<name>.feature` (e.g., `devel.feature`)
For a packaged optional feature, `/usr/lib/sysupdate.d/devel.feature` can
start disabled and be enabled by a local drop-in:

```ini
[Feature]
Description=Developer tools and headers
Documentation=https://example.com/docs/devel
AppStream=https://example.com/appstream/devel.xml
Enabled=false
```

| Key | Type | Description |
|-----|------|-------------|
| `Description` | string | Human-readable description |
| `Documentation` | string | URL to documentation |
| `AppStream` | string | AppStream catalog XML URL (parsed by `config` package but not surfaced in the SDK's `FeatureInfo` result) |
| `Enabled` | bool | Whether the feature is active (`true`/`false`); default `false` |

### Masked features

A feature is **masked** when its file is a symlink to `/dev/null`. `LoadFeatures` returns a masked entry with `Masked=true` and `Enabled=false` so callers can display it, but enable/disable operations reject masked features.

### Drop-in files

Features support drop-in overrides in `<name>.feature.d/*.conf` directories alongside the feature file. Drop-ins are applied in alphabetical order and can override any `[Feature]` setting. updex itself writes exactly one drop-in, `00-updex.conf`, which sorts first so administrator drop-ins always override it (see [ADR-0004](../adr/0004-single-updex-drop-in.md)).
Use `sudo updex features enable devel` to enable a feature; add `--now` to
download its extensions immediately. A manual drop-in with `[Feature]` and
`Enabled=true` can also enable it.

Example: `/etc/sysupdate.d/devel.feature.d/99-override.conf`
```ini
[Feature]
Enabled=false
```

## Transfer Files (`.transfer`)

Define how a single component (e.g., a kernel image, extension image) is downloaded, verified, and installed.

**Filename**: `<component>.transfer` (e.g., `kernel.transfer`)
For example, `/etc/sysupdate.d/myext.transfer` can use
`Source.Path=https://example.com/sysexts` and
`MatchPattern=myext_@v.raw.xz` to install decompressed
`myext_@v.raw` files under `/var/lib/extensions.d` (the sections and keys
are shown below).

### Masked transfers

A transfer is **masked** when its file is a symlink to `/dev/null`. The
masked transfer is ignored, but its higher-priority filename still suppresses
lower-priority definitions of the same name.

```ini
[Transfer]
MinVersion=1.0.0
ProtectVersion=2.1.0
Verify=true
InstancesMax=2
Features=devel

[Source]
Type=url-file
Path=https://example.com/releases/
MatchPattern=component_@v.raw.xz component_@v.raw.gz component_@v.raw

[Target]
Type=regular-file
Path=/var/lib/extensions.d
MatchPattern=component_@v.raw
Mode=0644
```

### [Transfer] Section

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `MinVersion` | string | — | Only consider versions >= this value |
| `ProtectVersion` | string | — | Never remove this version during vacuum |
| `Verify` | bool | `true` | Require GPG signature on SHA256SUMS; set false to opt out |
| `InstancesMax` | int | `2` | Maximum versions to keep; oldest removed first. The linked and the merged version are kept in addition |
| `Features` | string list | — | OR logic: transfer activates if *any* listed feature is enabled |
| `RequisiteFeatures` | string list | — | AND logic: transfer activates only if *all* listed features are enabled |

`config.FilterTransfersByFeatures` implements the full active-transfer rules: standalone transfers are included when no feature requirements are set, `Features` is OR, `RequisiteFeatures` is AND, and both conditions must pass if both fields are set. Current feature-oriented SDK methods use `config.GetTransfersForFeature` instead, which treats a transfer as associated with a feature if the feature name appears in either list.
For example, `Features=devel` associates the transfer with `devel.feature`.
Omitting `Verify=` requires a GPG signature;
`Verify=no` explicitly opts out, but the CLI `--verify` flag forces verification
even for that transfer. A transfer generated by `updex catalog add` honors
`Verify=no` only while its catalog sets `AllowUnsigned=yes` (see
[Signature policy for catalog sysexts](#signature-policy-for-catalog-sysexts)).

### `[Source]` section

| Key | Type | Description |
|-----|------|-------------|
| `Type` | string | Source type (currently `url-file` supported) |
| `Path` | string | Base URL for downloads; trailing slashes are trimmed during parsing |
| `MatchPattern` | string | Filename pattern(s) with `@v` placeholder. Space-separated values define compression variants tried in order |

### [Target] Section

| Option | Type | Default | Description |
| ------ | ---- | ------- | ----------- |
| `Type` | string | (none) | May be omitted (implicit `regular-file`); non-`regular-file` values are silently skipped by sysext filtering |
| `Path` | string | `/var/lib/extensions.d` | Staging directory for downloaded versioned files |
| `PathRelativeTo` | string | (none) | Base directory `Path` is relative to (e.g. `boot` for a UKI); when set, the non-sysext transfer is skipped entirely |
| `MatchPattern` | string | — | Filename pattern with `@v` for installed files |
| `CurrentSymlink` | string | — | Optional legacy staging symlink name; if configured and present, updex removes it during update |
| `Mode` | uint32 | `0644` | File permissions (octal) |
| `ReadOnly` | bool | `false` | Parsed for sysupdate.d(5) compatibility; updex does not currently act on it |

### Non-sysext transfers (skipped, not errored)

Native (bootc A/B) images share the legacy default `sysupdate.d/` directory
between sysext transfers and the OS's own A/B partition and UKI transfers.
`config.FilterSysextTransfers` (applied by the default union loader,
`LoadAllTransfers`) keeps only `Source.Type == "url-file"` transfers whose
target is empty-or-`regular-file` with no `PathRelativeTo` set, silently
dropping `Target Type=partition` entries and the UKI's
`Type=regular-file`+`PathRelativeTo=boot` entry rather than erroring on them.
This compatibility contract is pinned by
[ADR-0002](../adr/0002-skip-non-sysext-transfers.md).

### Multiple `MatchPattern` values

`MatchPattern` accepts space-separated patterns on a single line. The first is the primary pattern kept in `MatchPattern` for backward-compatible callers; the full ordered list is stored in `MatchPatterns`. During download, all valid source patterns are used to find matching files in the manifest.

Downloads are always stored decompressed, so the installed filename is derived from the first target pattern that yields a name without a compression suffix; if every target pattern carries one (e.g. a target list mirroring the source list), the suffix is stripped. A target `MatchPattern` list like `component_@v.raw.zst component_@v.raw` therefore installs `component_<version>.raw` no matter which source variant matched.

```ini
MatchPattern=component_@v.raw.xz component_@v.raw.gz component_@v.raw
```

Specifiers (`%a`, `%v`, `%w`, etc.) are expanded on each pattern after splitting.

In Go code, the `Transfer` struct stores both `MatchPattern` (first pattern, for backward compatibility) and `MatchPatterns` (all patterns). The `Patterns()` method on `SourceSection`/`TargetSection` returns the canonical list. Invalid patterns are skipped by `version.ParsePatterns`; callers fail only when no valid pattern remains.

## Pattern Placeholders

The `@v` placeholder is required in every `MatchPattern`. Additional placeholders are available:

For example, `myext_@v.raw.xz` matches `myext_1.2.3.raw.xz` and
`myext_2.0.0-rc1.raw.xz`; `kernel_@v.efi` matches `kernel_6.1.0.efi`.

| Placeholder | Captures | Description |
|-------------|----------|-------------|
| `@v` | `[a-zA-Z0-9._+:~-]+` | Version string (required) |
| `@u` | `[a-fA-F0-9-]+` | UUID |
| `@f` | `[0-9]+` | Flags |
| `@a` | `[01]` | GPT NoAuto flag |
| `@g` | `[01]` | GrowFileSystem flag |
| `@r` | `[01]` | Read-only flag |
| `@t` | `[0-9]+` | Modification time |
| `@m` | `[0-7]+` | File mode |
| `@s` | `[0-9]+` | File size |
| `@d` | `[0-9]+` | Tries done |
| `@l` | `[0-9]+` | Tries left |
| `@h` | `[a-fA-F0-9]+` | SHA256 hash |

## Systemd Specifiers

Selected transfer values support systemd-style `%` specifiers, expanded at parse time. Current expansion applies to `Source.MatchPattern`, `Target.MatchPattern`, and `Transfer.ProtectVersion`; it does not apply to `Source.Path`, `Target.Path`, or `CurrentSymlink`.

| Specifier | Source | Description |
|-----------|--------|-------------|
| `%A` | `/etc/os-release` `IMAGE_VERSION=` | Image version |
| `%a` | Go `GOARCH` → systemd | Architecture (e.g., `x86-64`, `arm64`) |
| `%B` | `/etc/os-release` `BUILD_ID=` | Build ID |
| `%b` | `/proc/sys/kernel/random/boot_id` | Boot ID |
| `%H` | `os.Hostname()` | Full hostname |
| `%l` | `os.Hostname()` | Short hostname (before first `.`) |
| `%M` | `/etc/os-release` `IMAGE_ID=` | Image ID |
| `%m` | `/etc/machine-id` | Machine ID |
| `%o` | `/etc/os-release` `ID=` | OS ID |
| `%T` | — | `/tmp` |
| `%V` | — | `/var/tmp` |
| `%v` | `/proc/sys/kernel/osrelease` | Kernel version |
| `%w` | `/etc/os-release` `VERSION_ID=` | OS version ID |
| `%W` | `/etc/os-release` `VARIANT_ID=` | OS variant ID |
| `%%` | — | Literal `%` |

Expansion is a single left-to-right pass. Unknown specifiers are preserved literally, and `%%` becomes `%` without triggering a second expansion pass.

Expansion happens at config load time only: catalog-generated `.transfer`
files keep `%w`/`%a` unexpanded on disk so definitions track the running OS
release (see [ADR-0006](../adr/0006-byte-preserving-render-transfer.md)).

Specifier values are cached for a single `LoadTransfers` call. `/etc/os-release` is read first with `/usr/lib/os-release` as fallback, and one-line host values are read from `/proc/sys/kernel/random/boot_id`, `/etc/machine-id`, and `/proc/sys/kernel/osrelease`.

## Catalog Files (`.catalog`)

Catalog repos for `updex catalog` are defined by `<name>.catalog` INI files
(the filename stem is the repo name, `[a-zA-Z0-9_-]+`), searched in
priority order (first occurrence of a filename wins):

1. `/etc/updex/catalogs.d/`
2. `/run/updex/catalogs.d/`
3. `/usr/local/lib/updex/catalogs.d/`
4. `/usr/lib/updex/catalogs.d/`

There are no built-in repos; without at least one `.catalog` file every
catalog operation fails with setup guidance.

Files written by `updex catalog add` begin with the repo-scoped ownership
header `# Generated by updex catalog (repo: <name>); managed by 'updex
catalog'`; add and remove refuse to touch definition files not marked with
the acting repo (see [ADR-0003](../adr/0003-catalog-ownership-marker.md)).

```ini
# /etc/updex/catalogs.d/fedora.catalog
[Catalog]
SiteURL=https://extensions.fcos.fr/fedora
ListURL=https://api.github.com/repos/fedora-sysexts/fedora/contents/
# Component=catalog-fedora
```

| Key | Required | Description |
|-----|----------|-------------|
| `SiteURL` | yes | Base URL artifacts resolve under (`<SiteURL>/<sysext>/{<sysext>.conf,SHA256SUMS,*.raw}`); trailing slash trimmed |
| `ListURL` | no | GitHub contents API endpoint for `catalog list`/`search`; without it the repo is skipped in listings (add/remove unaffected) |
| `Component` | no | systemd-sysupdate component for generated files; default `catalog-<name>`, validated against `[a-zA-Z0-9_-]+` |
| `AllowInsecure` | no | bool, default `no`; permits non-HTTPS `SiteURL`/`ListURL` values for explicitly trusted development/test endpoints. Does not affect `GITHUB_TOKEN` transmission |
| `AllowUnsigned` | no | bool, default `no`; lets this catalog's published `Verify=` decide whether its `SHA256SUMS` must be GPG-signed. Without it, signatures are always required for this repo's sysexts. Cannot be combined with `AllowInsecure=yes` (the file fails to load): HTTPS and SHA256 checks still apply |

### Signature policy for catalog sysexts

The catalog's own `[Transfer] Verify=` is never copied verbatim. When the
repo sets `AllowUnsigned=yes`, `catalog add` parses it the way `config` loads
transfers (absent or not a boolean → `yes`) and writes a canonical
`Verify=yes` or `Verify=no` after the injected `Features=` line; otherwise the
generated transfer carries no `Verify=` line.

Whenever a generated transfer is checked, updated, or installed, the
effective setting is recomputed from local configuration
([ADR-0014](../adr/0014-local-catalog-signature-policy.md)):

| Condition | Signature required |
|-----------|--------------------|
| `ClientConfig.Verify` / `--verify` | always |
| Owning repo lacks `AllowUnsigned=yes`, is no longer configured, or its `.catalog` fails to load | always, whatever the transfer says |
| Transfer starts with the catalog marker but names no parseable repo, or its header cannot be read | always |
| Owning repo sets `AllowUnsigned=yes`, but the transfer's `Source.Path` is not `<SiteURL>/<name>` for its current `SiteURL` | always (re-run `catalog add`) |
| Owning repo sets `AllowUnsigned=yes` | the transfer's `Verify=` (default `yes`) |

A missing `SHA256SUMS.gpg` on a transfer that requires one fails with an
error naming the rule that applied (`manifest.ErrSignatureNotFound` is in the
chain). SHA256 hash checks are unconditional.

`catalog add` writes the generated `<sysext>.transfer`/`<sysext>.feature`
into `/etc/sysupdate.<Component>.d/`, which is discovered as a normal named
component (see "Components" above).

## Sysext image files (`extension-release`)

updex reads one key of the `extension-release.<name>` file of a downloaded
sysext image (`usr/lib/extension-release.d/extension-release.<name>`, an
os-release style file; see extension-release.d(5)). The image is read without
mounting it: a GPT disk image holding an EROFS partition (the snosi shape), or
a bare EROFS image. `systemd-sysext` ignores the key.

An uncompressed EROFS image is read by the pure-Go reader. A compressed EROFS
image (lz4, lz4hc, zstd, ...) is read with the erofs-utils `dump.erofs`
program, which prints the file without mounting. That fallback needs
erofs-utils **1.8.5** or newer, because it uses `--cat`; `--offset` and `--ls`
exist since 1.8. A host without a new enough `dump.erofs` cannot check a
compressed image and fails closed: the feature is not linked and the error
says `erofs-utils >= 1.8.5 is required to read compressed EROFS images;
install erofs-utils`. Uncompressed images need no erofs-utils. The packaged
`updex` (deb, rpm, apk) depends on erofs-utils; a standalone binary needs it
on the host when any configured sysext ships a compressed image.

| Key | Description |
|-----|-------------|
| `UPDEX_MIN_IMAGE_VERSION` | The lowest `IMAGE_VERSION` of the base image that this sysext revision works with ([ADR-0016](../adr/0016-automatic-sysext-updates-apply-at-next-boot.md), "Minimum image version"). Absent: no check. Digits only (`^[0-9]+$`, for example a 14-digit snosi timestamp `20261022021319`); no sign, no space, not empty |

**The version that it is compared with depends on what merges the image.**

The *link* is what the next boot merges, so it is compared with the
`IMAGE_VERSION` of the image that boots next: `.status.rollback.image.version`
of `bootc status --json` when bootc reports `rollbackQueued: true` (the next
boot is the rollback deployment; a queued rollback without that version is an
error), else `.status.staged.image.version` when bootc reports a staged
deployment (a staged deployment without that version is an error: the next
boot is known to be the staged image, its version is not), else
`IMAGE_VERSION` from the first readable of the client's os-release files
(`/etc/os-release`, `/usr/lib/os-release`; `RuntimePaths.OSReleasePaths`). A
host without `bootc`, or with no staged deployment, uses the os-release.
bootc reads `image.version` from the OCI manifest annotation
`org.opencontainers.image.version` of the OS image (not from the config label
of the same name), so an OS image that uses the key on its sysexts must carry
that annotation.
Both values are compared as numbers of any length (leading zeros ignored).
When the key is set and the image version is lower, missing or not digits
only, or the key value itself is not digits only, the revision is not linked
and updex reports `needs image >= <value>`; see `README.md` for what stays and
what continues.

An immediate `systemd-sysext refresh` merges into the *running* image, so
before every refresh updex runs it compares the key of each sysext the refresh
would merge with `IMAGE_VERSION` from the first readable of the same
os-release files — the booted deployment's, which needs no `bootc` call and no
staged deployment. A refresh only activates a link that the next-boot check
already allowed; this second check decides whether the running base can run it
now. When it fails (or the running version is missing or not numeric), updex
keeps the links, skips the refresh, and reports `refresh skipped: <component>
needs image >= <value> (running image <running>); it takes effect at the next
boot` (JSON `refresh_skipped`). `features disable --now` refuses the whole
command instead, before it unmerges anything, and asks for a run without
`--now` and a reboot. `--no-refresh` (the timer) does not run the check.

If several `extension-release.*` files exist, the one named after the transfer
(its file name without `.transfer`), then the one named after the feature, is
used; with no match the image cannot be judged and the feature is not linked
(an error). A file larger than 64 KiB is refused.

**Fail open or closed.** A candidate region counts as an EROFS filesystem only
when its superblock at offset 1024 starts with the EROFS magic
(`0xE0F5E1E2`). When no candidate has it (a squashfs sysext, random bytes, a
broken GPT), `imagecompat.ErrUnsupportedImage` says so and the image is
treated as having no key: it is linked. Once a candidate has the magic, every
later problem is an error and the feature is not linked: a failed EROFS open,
a release file that cannot be opened, inspected or read, no release file,
several without a match, a file over 64 KiB, a parser failure, or a
compressed image whose `dump.erofs` fallback is missing, too old, or fails.

## Version Comparison

Versions extracted via `@v` are sorted descending (newest first) when selecting which version to install. `version.Compare` uses a dpkg-compatible comparator for Debian-style versions containing `:`, `~`, or `+` so epochs and tilde pre-release ordering work correctly. `+` is included because semver treats everything after it as ignorable build metadata, which collapsed dpkg-derived sysext versions like `1+7.2-debian13-202607011055` (epoch encoded as `+` because `:` is not filename-safe) to equal precedence and made selection random. Other versions are compared with `hashicorp/go-version` after stripping a leading `v`/`V`; if parsing fails, plain string comparison is used as fallback.

Version candidates extracted from the manifest are deduplicated in a set and returned lexically sorted before `version.Sort` runs. Because `version.Sort` is stable, this keeps selection reproducible even if a comparator gap ever makes two distinct versions compare equal.

## Retention and Active Versions

`InstancesMax` controls how many installed versions are normally retained. The vacuum that `updex features update` (and `features enable --now`) runs after a feature's links succeeded always keeps, besides the newest `InstancesMax` versions: the version that the sysext link `/var/lib/extensions/<component>.<ext>` names (linked), the version merged now (read from the `.systemd-sysext` metadata of the merged hierarchies, ADR-0015), `ProtectVersion`, the version a rollback record rejected for the transfer while the record exists (`/var/lib/updex/rollback/<feature>`, see `updex features rollback`), and a legacy active version pointed to by `CurrentSymlink`. They are kept in addition to the newest `InstancesMax` versions (one that is among the newest counts as one of them), so a host that has linked a new revision but still runs an older one keeps up to `InstancesMax` + 1 images per transfer until its next reboot. If updex cannot read the link or the merged state, it deletes nothing. If `InstancesMax <= 0`, vacuum falls back to the default of `2`.

Dry-run updates call `sysext.PlanVacuumWith` with the would-install version as the active-version override, which lets the SDK report `RemovedVersions` without touching disk. Real installs call `sysext.VacuumWith`, so the update result currently does not include removed-version details for non-dry-run runs.

## Remote Manifest Format

The source base URL must contain `SHA256SUMS` with a SHA256 hash and filename
per entry. Two spaces between the hash and filename are the standard format
illustrated below, but the parser accepts any whitespace as a separator,
including the `hash *filename` binary form (the leading `*` is removed):

```
0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef  myext_1.0.0.raw.xz
abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789  myext_1.1.0.raw.xz
```

When verification is enabled, provide a detached `SHA256SUMS.gpg` signature
too. The manifest and detached signature fetches retry transient network
failures and HTTP 5xx/429 with exponential backoff; response bodies are
limited to 4 MiB and 1 MiB respectively. Oversized responses are rejected
before parsing or signature verification. Signature verification follows the
manifest fetch.
