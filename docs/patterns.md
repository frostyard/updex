# Transfer File Patterns

updex supports two pattern styles for sysext transfers:

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
- OS version is now required (unlike the old frostyard pattern)

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

Placeholders matched directly in filenames:

| Placeholder | Matches | Regex |
|-------------|---------|-------|
| `@v` | Version string (required) | `[a-zA-Z0-9._+:~-]+` |
| `@a` | GPT NoAuto flag | `[01]` (0 or 1 only) |
| `@u` | UUID | `[a-fA-F0-9-]+` |
| `@g` | GrowFileSystem flag | `[01]` |
| `@r` | ReadOnly flag | `[01]` |

### `%` Specifiers (Config-Time Expansion)

Specifiers expanded when loading `.transfer` files:

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

| Aspect | Frostyard | Fedora-Sysexts |
|--------|---------------------|-----------------|
| Pattern | `<name>_@v_%w_%a.raw` | `<name>-@v-%w-%a.raw` |
| Delimiter Style | Underscores (`_`) | Hyphens (`-`) |
| OS Version | Included (`%w`) | Included (`%w`) |
| Architecture | Included (`%a`) | Included (`%a`) |
| Example (Fedora 39/x86-64) | `docker_1.0.0_39_x86-64.raw` | `docker-1.0.0-39-x86-64.raw` |
| Example (Ubuntu 22.04/arm64) | `htop_7.2.0_22.04_arm64.raw` | `htop-7.2.0-22.04-arm64.raw` |

## Migration Note

The new Frostyard pattern (`<name>_@v_%w_%a.raw`) is a replacement for the older Frostyard pattern (`<name>_@v_@a.raw`). No backwards compatibility is maintained. If you were previously using the older pattern without OS version specificity, you must update your `.transfer` files to use the new pattern with `%w` to specify OS version requirements.

Both patterns (Frostyard and Fedora-Sysexts) can coexist in a single `.transfer` file by space-separating them in `MatchPattern` if you need to support multiple naming conventions.
