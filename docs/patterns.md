# Transfer File Patterns

updex accepts any sysext transfer pattern containing the required `@v`
version placeholder. This document describes two common naming styles:

## Pattern Styles

### Frostyard Pattern
```
<name>_@v_%w_%a.raw[.zst|.xz|.gz]
```

**Example:** `docker_@v_%w_%a.raw`

**After specifier expansion on Fedora 39/x86-64:**
```
docker_@v_39_x86-64.raw
```

**Components:**
- `<name>`: Package name (e.g., `docker`, `htop`)
- `_`: Underscore separator
- `@v`: Version placeholder (extracted from filename)
- `_`: Underscore separator
- `%w`: OS version specifier (expands to VERSION_ID from `/etc/os-release`)
- `_`: Underscore separator
- `%a`: Architecture specifier (expands to systemd architecture: x86-64, arm64, etc.)
- `.raw[.zst|.xz|.gz]`: File extension with optional compression

**Characteristics:**
- OS-version-specific: only downloads updates for your current OS version
- Arch-specific: uses systemd architecture naming
- Underscore-based naming convention (unlike fedora-sysexts which uses hyphens)
- Explicit specifiers in configuration: visible in `.transfer` files
- `%w` and `%a` are optional; the pattern shown above uses them to scope
  artifacts to the current OS version and architecture

### Fedora-Sysexts Pattern
```
<name>-@v-%w-%a.raw[.zst|.xz|.gz]
```

**Example:** `docker-@v-%w-%a.raw`

**After specifier expansion on Fedora 39/x86-64:**
```
docker-@v-39-x86-64.raw
```

**Components:**
- `<name>`: Package name (e.g., `docker`, `htop`)
- `-`: Hyphen separator
- `@v`: Version placeholder (extracted from filename)
- `-`: Hyphen separator
- `%w`: OS version specifier (expands to VERSION_ID from `/etc/os-release`)
- `-`: Hyphen separator
- `%a`: Architecture specifier (expands to systemd architecture: x86-64, arm64, etc.)
- `.raw[.zst|.xz|.gz]`: File extension with optional compression

**Characteristics:**
- OS-version-specific: only downloads updates for your current OS version
- Arch-specific: uses systemd architecture naming
- Hyphen-based naming convention (standard for fedora-sysexts)
- Explicit specifiers in configuration: visible in `.transfer` files

## Placeholder Reference

### `@` Placeholders (File Content)

Placeholders matched directly in filenames. Only `@v` is required; all other
`@` placeholders are optional:

| Placeholder | Matches | Regex |
|-------------|---------|-------|
| `@v` | Version string (required) | `[a-zA-Z0-9._+:~-]+` |
| `@a` | GPT NoAuto flag | `[01]` (0 or 1 only) |
| `@u` | UUID | `[a-fA-F0-9-]+` |
| `@g` | GrowFileSystem flag | `[01]` |
| `@r` | ReadOnly flag | `[01]` |

### `%` Specifiers (Config-Time Expansion)

Specifiers expanded when loading `.transfer` files. All `%` specifiers are
optional and constrain a pattern only when included:

| Specifier | Expands To | Example |
|-----------|-----------|---------|
| `%a` | Systemd architecture | x86-64, arm64, riscv64 |
| `%w` | OS version (VERSION_ID) | 39 (Fedora), 22.04 (Ubuntu) |
| `%H` | Hostname | localhost |
| `%T` | Temporary directory | /tmp |
| `%V` | Persistent temporary | /var/tmp |
| `%%` | Literal % | % (for escaping) |

## Examples

### Frostyard Pattern (Fedora 39, x86-64)

Configuration:
```ini
MatchPattern=docker_@v_%w_%a.raw
```

After expansion on Fedora 39/x86-64:
```
docker_@v_39_x86-64.raw
```

Matches filenames like:
```
docker_1.0.0_39_x86-64.raw      ✓
docker_29.0.0-rc1_39_x86-64.raw ✓
docker_1.0.0_38_x86-64.raw      ✗ (wrong OS version)
docker_1.0.0_39_arm64.raw       ✗ (wrong architecture)
```

### Frostyard Pattern (Ubuntu 22.04, arm64)

Configuration:
```ini
MatchPattern=htop_@v_%w_%a.raw
```

After expansion on Ubuntu 22.04/arm64:
```
htop_@v_22.04_arm64.raw
```

Matches filenames like:
```
htop_7.2.0_22.04_arm64.raw      ✓
htop_8.0.1_22.04_arm64.raw      ✓
htop_7.2.0_22.04_x86-64.raw     ✗ (wrong architecture)
htop_7.2.0_20.04_arm64.raw      ✗ (wrong OS version)
```

### Fedora-Sysexts Pattern (Fedora 39, x86-64)

Configuration:
```ini
MatchPattern=docker-@v-%w-%a.raw
```

After expansion:
```
docker-@v-39-x86-64.raw
```

Matches filenames like:
```
docker-7.2.0-39-x86-64.raw      ✓
docker-1.0.0-rc1-39-x86-64.raw  ✓
docker-1.0.0-38-x86-64.raw      ✗ (wrong OS version)
docker-1.0.0-39-arm64.raw       ✗ (wrong architecture)
```

### Multiple Patterns with Compression

Configuration:
```ini
MatchPattern=docker_@v_%w_%a.raw.xz docker-@v-%w-%a.raw.gz
```

Matches:
```
docker_1.0.0_39_x86-64.raw.xz       ✓ (frostyard, xz compressed)
docker-1.0.0-39-x86-64.raw.gz       ✓ (fedora-sysexts, gzip compressed)
```

## Pattern Comparison

The table compares the OS- and architecture-specific forms shown above. The
`%w` and `%a` specifiers are optional in both styles.

| Aspect | Frostyard | Fedora-Sysexts |
|--------|---------------------|-----------------|
| Pattern | `<name>_@v_%w_%a.raw` | `<name>-@v-%w-%a.raw` |
| Delimiter Style | Underscores (`_`) | Hyphens (`-`) |
| OS Version | Shown with `%w` (optional) | Shown with `%w` (optional) |
| Architecture | Shown with `%a` (optional) | Shown with `%a` (optional) |
| Example filename (Fedora 39/x86-64) | `docker_1.0.0_39_x86-64.raw` | `docker-1.0.0-39-x86-64.raw` |
| Example filename (Ubuntu 22.04/arm64) | `htop_7.2.0_22.04_arm64.raw` | `htop-7.2.0-22.04-arm64.raw` |

## Migration Note

There is no required migration to `<name>_@v_%w_%a.raw`. Existing patterns
such as `<name>_@v.raw` remain valid because `@v` is the only required
placeholder. Add `%w` only when filenames include an OS version, and add `%a`
only when they include an architecture. Note that `@a` is a GPT NoAuto flag
placeholder, not an architecture specifier.

Frostyard and Fedora-Sysexts naming styles can coexist in a single `.transfer`
file by space-separating patterns in `MatchPattern` when multiple naming
conventions are needed.
