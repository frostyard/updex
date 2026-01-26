# Stack Research

**Domain:** systemd-related Go CLI with auto-update functionality
**Researched:** 2026-01-26
**Confidence:** HIGH

## Executive Summary

This research covers the 2025 standard stack for Go CLI tools that integrate with systemd for auto-update mechanisms. The project already uses a solid foundation (Cobra, Fang, go-version), and this research identifies what to add for testing infrastructure, systemd timer/service generation, and development tooling.

## Existing Stack (Validated)

These are already in use and should be retained:

| Technology | Version | Purpose | Status |
|------------|---------|---------|--------|
| Go | 1.25+ | Language runtime | Current, excellent |
| github.com/spf13/cobra | v1.10.2 | CLI framework | Industry standard |
| github.com/charmbracelet/fang | v0.4.4 | Configuration unmarshaling | Modern, well-designed |
| github.com/hashicorp/go-version | v1.8.0 | Version comparison | De facto standard |
| gopkg.in/ini.v1 | v1.67.1 | INI file parsing | Stable, matches systemd conventions |
| github.com/schollz/progressbar/v3 | v3.19.0 | Progress indicators | Good choice |
| github.com/klauspost/compress | v1.18.2 | Compression (zstd) | High performance |
| github.com/ulikunitz/xz | v0.5.15 | XZ decompression | Standard for sysext images |

## Recommended Additions

### Testing Infrastructure

| Library | Version | Purpose | Why Recommended | Confidence |
|---------|---------|---------|-----------------|------------|
| github.com/stretchr/testify | v1.11.1 | Assertions and require | 25.7k stars, 637k dependents, de facto Go standard. v1 is stable; v2 not expected soon. Provides `assert`, `require`, `mock` packages. | HIGH |

**Rationale:**
- The project already uses table-driven tests with stdlib `testing` (good)
- Adding testify enables cleaner assertions: `require.NoError(t, err)` vs `if err != nil { t.Fatal(err) }`
- The `require` package stops test execution on failure, preventing cascading failures
- Already verified: latest release v1.11.1 (Aug 2025), supports Go 1.19+
- Source: https://pkg.go.dev/github.com/stretchr/testify, https://github.com/stretchr/testify

**What NOT to use:**
- `testify/suite` — Not needed for this project's simple test patterns, and doesn't support parallel tests
- `testify/mock` — Only needed if you want to mock interfaces; consider it optional for now

### Systemd Unit Generation

| Library | Version | Purpose | Why Recommended | Confidence |
|---------|---------|---------|-----------------|------------|
| github.com/coreos/go-systemd/v22/unit | v22.6.0 | Unit file serialization | CoreOS-maintained, 2.6k stars. Provides `UnitOption` and `Serialize()` for generating valid unit files programmatically. | HIGH |

**Rationale:**
- Specifically the `unit` subpackage — does NOT require cgo
- Provides `NewUnitOption(section, name, value)` and `Serialize(opts)` for clean unit file generation
- Used by CoreOS, etcd, Kubernetes ecosystem
- Latest release v22.6.0 (Aug 2025), minimum Go 1.23
- Source: https://pkg.go.dev/github.com/coreos/go-systemd/v22/unit

**Alternative considered: hand-written templates**
- Could use `text/template` to generate .service/.timer files
- Simpler but error-prone for escaping and edge cases
- go-systemd/unit provides `UnitNameEscape()` for safe escaping

**What to avoid from go-systemd:**
- `sdjournal` — requires cgo and journald headers
- `dbus` — heavy dependency, not needed for file generation
- `activation` — for socket activation, not needed here

### Development & CI Tools

| Tool | Purpose | Why Recommended | Confidence |
|------|---------|-----------------|------------|
| testifylint | Linter for testify assertions | Catches common testify mistakes, integrates with golangci-lint. 164 stars, actively maintained. | MEDIUM |
| golangci-lint | Unified linter | Already industry standard, enables testifylint via configuration | HIGH |

**Installation:**

```bash
# Testing library
go get github.com/stretchr/testify@v1.11.1

# Systemd unit generation
go get github.com/coreos/go-systemd/v22/unit@v22.6.0

# Development tools (install globally or in tools.go)
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
```

## Systemd Timer/Service Patterns

**Confidence: HIGH** — based on official systemd documentation and common patterns.

### Service Unit Pattern

For auto-update, generate a oneshot service:

```ini
[Unit]
Description=updex auto-update
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
ExecStart=/usr/bin/updex update --all --quiet
# Run as root because sysext management requires it
User=root
# Prevent hanging on network issues
TimeoutStartSec=5min
# Security hardening
ProtectSystem=strict
ProtectHome=yes
NoNewPrivileges=yes
ReadWritePaths=/var/lib/extensions /var/cache/updex

[Install]
# Not installed directly - timer activates this
WantedBy=multi-user.target
```

### Timer Unit Pattern

```ini
[Unit]
Description=updex auto-update timer
Documentation=man:updex(1)

[Timer]
# Daily at a randomized time between 6:00-7:00
OnCalendar=*-*-* 06:00:00
RandomizedDelaySec=1h
# If missed, run on next boot
Persistent=true

[Install]
WantedBy=timers.target
```

### Key Configuration Options

| Setting | Purpose | Recommendation |
|---------|---------|----------------|
| `Type=oneshot` | Single execution, not daemon | Required for update task |
| `Persistent=true` | Run missed executions | Important for laptops |
| `RandomizedDelaySec` | Spread load across systems | Good for shared registries |
| `TimeoutStartSec` | Prevent hanging | 5min reasonable for network ops |

### Installation Paths

| File | Location | Notes |
|------|----------|-------|
| `updex-update.service` | `/etc/systemd/system/` | User-installed |
| `updex-update.timer` | `/etc/systemd/system/` | User-installed |

Generate via CLI command: `updex install-timer` (proposed)

## What NOT to Use

| Technology | Why Avoid | Use Instead |
|------------|-----------|-------------|
| uber-go/mock (gomock) | More complex than testify/mock, overkill for this project | testify/mock if needed, or just use table tests |
| vektra/mockery | Code generator for mocks — adds build complexity | Hand-write mocks if needed, or use interfaces directly |
| go-systemd/dbus | Heavy dependency, requires dbus connection at runtime | Use `systemctl` via exec, or just generate unit files |
| go-systemd/sdjournal | Requires cgo and libsystemd-dev | Log to stdout, let systemd capture to journal |
| testify/suite | Doesn't support parallel tests, adds complexity | Standard `func TestXxx(t *testing.T)` patterns |
| text/template for units | Error-prone escaping, reinvents wheel | go-systemd/v22/unit |

## Testing Patterns for This Project

### Recommended Approach

1. **Keep existing stdlib patterns** — table-driven tests work well
2. **Add testify for assertions** — cleaner error checking
3. **Use `t.TempDir()`** — already used in existing tests, excellent for filesystem tests
4. **Test real behavior where possible** — existing tests already create real files

### Example Migration

```go
// Before (stdlib only)
if len(versions) != 3 {
    t.Errorf("got %d versions, want 3", len(versions))
}

// After (with testify/require)
require.Len(t, versions, 3)

// Or with assert (continues on failure)
assert.Len(t, versions, 3)
```

### When to Use require vs assert

| Use `require` | Use `assert` |
|---------------|--------------|
| Error checking: `require.NoError(t, err)` | Multiple independent assertions |
| Prerequisites for later assertions | When you want all failures reported |
| Setup that must succeed | Non-critical comparisons |

### Mocking Strategy

For this project, **prefer real filesystem and real systemd unit files** over mocks:
- Sysext operations are filesystem-based
- `t.TempDir()` provides isolated test directories
- Unit file generation can be tested by parsing the output

If mocking becomes necessary later:
- Define small interfaces at the point of use
- Implement mocks manually (small scope)
- Consider testify/mock only if interface is complex

## Version Compatibility

| Package | Requires | Compatible With |
|---------|----------|-----------------|
| testify v1.11.x | Go 1.19+ | Go 1.25 (current) |
| go-systemd/v22 v22.6.0 | Go 1.23+ | Go 1.25 (current) |
| cobra v1.10.x | Go 1.18+ | Go 1.25 (current) |

No compatibility concerns identified.

## Implementation Priorities

1. **Add testify** — immediate value for existing and new tests
2. **Add go-systemd/v22/unit** — required for timer/service generation feature
3. **Add testifylint to golangci-lint config** — catches common mistakes

## Sources

- https://pkg.go.dev/github.com/stretchr/testify@v1.11.1 — Official Go package docs (HIGH confidence)
- https://github.com/stretchr/testify — GitHub README, v1.11.1 release Aug 2025 (HIGH confidence)
- https://pkg.go.dev/github.com/coreos/go-systemd/v22 — Official Go package docs (HIGH confidence)
- https://github.com/coreos/go-systemd — GitHub README, v22.6.0 release Aug 2025 (HIGH confidence)
- https://pkg.go.dev/github.com/coreos/go-systemd/v22/unit — Unit package API docs (HIGH confidence)
- https://github.com/Antonboom/testifylint — testifylint documentation (MEDIUM confidence)
- Existing codebase test patterns — `internal/manifest/manifest_test.go`, `internal/sysext/manager_test.go` (HIGH confidence)

---
*Stack research for: updex milestone (auto-update, testing, UX polish)*
*Researched: 2026-01-26*
