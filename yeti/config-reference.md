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

A feature is **masked** when its file is a symlink to `/dev/null`. Masked features are always treated as disabled regardless of the `Enabled` setting.

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
Path=/var/lib/extensions
MatchPattern=component_@v.raw
CurrentSymlink=component.raw
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

### `[Source]` section

| Key | Type | Description |
|-----|------|-------------|
| `Type` | string | Source type (currently `url-file` supported) |
| `Path` | string | Base URL for downloads (must end with `/`) |
| `MatchPattern` | string | Filename pattern(s) with `@v` placeholder. Space-separated values define compression variants tried in order |

### `[Target]` section

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `Type` | string | — | Target type (`regular-file`, `directory`) |
| `Path` | string | `/var/lib/extensions` | Target directory for downloaded files |
| `MatchPattern` | string | — | Filename pattern with `@v` for installed files |
| `CurrentSymlink` | string | — | Symlink name pointing to the active version |
| `Mode` | uint32 | `0644` | File permissions |
| `ReadOnly` | bool | `false` | Whether target should be read-only |

### Multiple `MatchPattern` values

`MatchPattern` accepts space-separated patterns on a single line. The first is the primary pattern; additional entries are compression variants. During download, patterns are tried in order to find a matching file in the manifest.

```ini
MatchPattern=component_@v.raw.xz component_@v.raw.gz component_@v.raw
```

Specifiers (`%a`, `%v`, `%w`, etc.) are expanded on each pattern after splitting.

In Go code, the `Transfer` struct stores both `MatchPattern` (first pattern, for backward compatibility) and `MatchPatterns` (all patterns). The `Patterns()` method on `SourceSection`/`TargetSection` returns the canonical list.

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

String values in transfer files support systemd-style `%` specifiers, expanded at parse time:

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

## Version Comparison

Versions extracted via `@v` are compared using `hashicorp/go-version` (semantic versioning). If a version string cannot be parsed as semver, string comparison is used as fallback. Versions are sorted descending (newest first) when selecting which version to install.
