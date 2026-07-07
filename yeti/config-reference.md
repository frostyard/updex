# Configuration Reference

updex uses INI-format configuration files loaded from systemd-style search paths.

## Search Paths

Searched in priority order (first occurrence of a filename wins):

1. `/etc/sysupdate.d/`
2. `/run/sysupdate.d/`
3. `/usr/local/lib/sysupdate.d/`
4. `/usr/lib/sysupdate.d/`

The `-C` / `--definitions` flag overrides all paths with a single custom directory.

## Feature Files (`.feature`)

Define a named feature that groups one or more transfers.

**Filename**: `<name>.feature` (e.g., `devel.feature`)

```ini
[Feature]
Description=Developer tools and headers
Documentation=https://example.com/docs/devel
AppStream=https://example.com/appstream/devel.xml
Enabled=true
```

| Key | Type | Description |
|-----|------|-------------|
| `Description` | string | Human-readable description |
| `Documentation` | string | URL to documentation |
| `AppStream` | string | AppStream catalog XML URL (parsed by `config` package but not surfaced in the SDK's `FeatureInfo` result) |
| `Enabled` | bool | Whether the feature is active (`true`/`false`) |

### Masked features

A feature is **masked** when its file is a symlink to `/dev/null`. `LoadFeatures` returns a masked entry with `Masked=true` and `Enabled=false` so callers can display it, but enable/disable operations reject masked features.

### Drop-in files

Features support drop-in overrides in `<name>.feature.d/*.conf` directories alongside the feature file. Drop-ins are applied in alphabetical order and can override any `[Feature]` setting.

Example: `/etc/sysupdate.d/devel.feature.d/99-override.conf`
```ini
[Feature]
Enabled=false
```

## Transfer Files (`.transfer`)

Define how a single component (e.g., a kernel image, extension image) is downloaded, verified, and installed.

**Filename**: `<component>.transfer` (e.g., `kernel.transfer`)

```ini
[Transfer]
MinVersion=1.0.0
ProtectVersion=2.1.0
Verify=false
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

### `[Transfer]` section

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `MinVersion` | string | — | Only consider versions >= this value |
| `ProtectVersion` | string | — | Never remove this version during vacuum |
| `Verify` | bool | `false` | Require GPG signature on SHA256SUMS |
| `InstancesMax` | int | `2` | Maximum versions to keep; oldest removed first |
| `Features` | string list | — | OR logic: transfer activates if *any* listed feature is enabled |
| `RequisiteFeatures` | string list | — | AND logic: transfer activates only if *all* listed features are enabled |

`config.FilterTransfersByFeatures` implements the full active-transfer rules: standalone transfers are included when no feature requirements are set, `Features` is OR, `RequisiteFeatures` is AND, and both conditions must pass if both fields are set. Current feature-oriented SDK methods use `config.GetTransfersForFeature` instead, which treats a transfer as associated with a feature if the feature name appears in either list.

### `[Source]` section

| Key | Type | Description |
|-----|------|-------------|
| `Type` | string | Source type (currently `url-file` supported) |
| `Path` | string | Base URL for downloads; trailing slashes are trimmed during parsing |
| `MatchPattern` | string | Filename pattern(s) with `@v` placeholder. Space-separated values define compression variants tried in order |

### `[Target]` section

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `Type` | string | — | Target type (`regular-file`, `directory`) |
| `Path` | string | `/var/lib/extensions.d` | Staging directory for downloaded versioned files |
| `MatchPattern` | string | — | Filename pattern with `@v` for installed files |
| `CurrentSymlink` | string | — | Optional legacy staging symlink name; if configured and present, updex reads it for current-version detection before removing it during update, and removes it during disable `--now` cleanup |
| `Mode` | uint32 | `0644` | File permissions |
| `ReadOnly` | bool | `false` | Whether target should be read-only |

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

Specifier values are cached for a single `LoadTransfers` call. `/etc/os-release` is read first with `/usr/lib/os-release` as fallback, and one-line host values are read from `/proc/sys/kernel/random/boot_id`, `/etc/machine-id`, and `/proc/sys/kernel/osrelease`.

## Version Comparison

Versions extracted via `@v` are sorted descending (newest first) when selecting which version to install. `version.Compare` uses a dpkg-compatible comparator for Debian-style versions containing `:`, `~`, or `+` so epochs and tilde pre-release ordering work correctly. `+` is included because semver treats everything after it as ignorable build metadata, which collapsed dpkg-derived sysext versions like `1+7.2-debian13-202607011055` (epoch encoded as `+` because `:` is not filename-safe) to equal precedence and made selection random. Other versions are compared with `hashicorp/go-version` after stripping a leading `v`/`V`; if parsing fails, plain string comparison is used as fallback.

Version candidates extracted from the manifest are deduplicated in a set and returned lexically sorted before `version.Sort` runs. Because `version.Sort` is stable, this keeps selection reproducible even if a comparator gap ever makes two distinct versions compare equal.

## Retention and Active Versions

`InstancesMax` controls how many installed versions are normally retained. During `sysext.VacuumWithDetails`, a legacy active version pointed to by `CurrentSymlink` is always kept even if it would otherwise sort outside the retention window, and `ProtectVersion` is always kept as well. If `InstancesMax <= 0`, vacuum falls back to the default of `2`.

`CurrentSymlink` is legacy staging state, not the sysext-visible link. `/var/lib/extensions/<component>.<ext>` is derived from the transfer filename component and target pattern extension. Update code must inspect any legacy `CurrentSymlink` before removing it, because that symlink is the only signal that a newer staged file is installed but not yet current.

Dry-run updates call `sysext.PlanVacuumAfterInstall` with the would-install version as the active-version override, which lets the SDK report `RemovedVersions` without touching disk. Real installs call `sysext.Vacuum`, so the update result currently does not include removed-version details for non-dry-run runs.
