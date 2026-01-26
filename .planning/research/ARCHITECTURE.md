# Architecture Research

**Domain:** Go CLI tool with systemd integration (auto-update daemon, test infrastructure)
**Researched:** 2026-01-26
**Confidence:** HIGH

## Standard Architecture

### System Overview

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           CLI Layer (cmd/)                                   │
│  ┌───────────┐  ┌───────────┐  ┌───────────┐  ┌───────────┐  ┌───────────┐  │
│  │  install  │  │   update  │  │   list    │  │  daemon   │  │  enable   │  │
│  └─────┬─────┘  └─────┬─────┘  └─────┬─────┘  └─────┬─────┘  └─────┬─────┘  │
│        │              │              │              │              │        │
├────────┴──────────────┴──────────────┴──────────────┴──────────────┴────────┤
│                        Public API Layer (updex/)                             │
│  ┌──────────────────────────────────────────────────────────────────────┐   │
│  │  Client: Install(), Update(), List(), Check(), Daemon(), Enable()    │   │
│  └───────────────────────────────────┬──────────────────────────────────┘   │
├──────────────────────────────────────┴──────────────────────────────────────┤
│                        Internal Layer (internal/)                            │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐       │
│  │  config  │  │ manifest │  │  sysext  │  │ download │  │ version  │       │
│  └──────────┘  └──────────┘  └──────────┘  └──────────┘  └──────────┘       │
├─────────────────────────────────────────────────────────────────────────────┤
│                        Systemd Integration Layer (NEW)                       │
│  ┌───────────────────┐  ┌───────────────────┐  ┌───────────────────┐        │
│  │  Timer/Service    │  │  Embedded Assets  │  │  Unit Manager     │        │
│  │  Templates        │  │  (embed.FS)       │  │  (install/remove) │        │
│  └───────────────────┘  └───────────────────┘  └───────────────────┘        │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Component Responsibilities

| Component | Responsibility | Typical Implementation |
|-----------|----------------|------------------------|
| `cmd/commands/` | CLI argument parsing, output formatting | Thin wrappers calling `updex.Client` methods |
| `updex/` | Business logic, operation orchestration | Methods on `Client` struct with typed options/results |
| `internal/sysext/` | Low-level systemd-sysext operations | Direct filesystem and `exec.Command` calls |
| `internal/systemd/` (NEW) | Systemd unit file management | Template rendering, file installation, `systemctl` calls |

## Recommended Project Structure

### Current Structure (Preserved)

```
updex/
├── cmd/
│   ├── commands/           # CLI command implementations
│   │   ├── install.go
│   │   ├── update.go
│   │   ├── daemon.go       # NEW: daemon subcommand
│   │   └── ...
│   ├── common/             # Shared CLI utilities
│   └── updex/              # Root command setup
├── updex/                  # Public API package
│   ├── updex.go            # Client struct
│   ├── install.go
│   ├── update.go
│   ├── daemon.go           # NEW: daemon operation
│   └── ...
├── internal/
│   ├── config/             # Transfer/feature parsing
│   ├── manifest/           # SHA256SUMS handling
│   ├── sysext/             # systemd-sysext operations
│   ├── download/           # HTTP download + decompress
│   ├── version/            # Version pattern matching
│   └── systemd/            # NEW: systemd unit management
└── assets/                 # NEW: embedded assets
    └── systemd/
        ├── updex.service
        └── updex.timer
```

### New Components for Auto-Update

```
internal/systemd/           # NEW: Systemd unit management
├── units.go                # Unit file templates
├── manager.go              # Install/remove/status operations
└── manager_test.go         # Tests with temp directories

assets/systemd/             # NEW: Embedded unit templates
├── updex.service           # Service unit template
└── updex.timer             # Timer unit template
```

### Structure Rationale

- **`internal/systemd/`:** Isolated from sysext operations because it's a different domain (unit management vs extension management). Allows independent testing and clear responsibility boundaries.
- **`assets/`:** Go 1.16+ `embed.FS` for bundling unit templates into the binary. Avoids runtime file dependencies, simplifies deployment.

## Architectural Patterns

### Pattern 1: Embedded Assets for Systemd Units

**What:** Use Go's `embed` directive to bundle systemd unit files into the binary, then render them with `text/template` at install time.

**When to use:** When shipping configuration files that may need customization at install time (binary paths, intervals, etc.).

**Trade-offs:**
- Pros: Single binary distribution, no runtime file dependencies, customizable at install
- Cons: Requires rebuild to change default templates (but templates are replaceable via flags)

**Example:**
```go
package systemd

import (
    "embed"
    "text/template"
)

//go:embed assets/*.service assets/*.timer
var assets embed.FS

type UnitConfig struct {
    ExecStart   string // Path to updex binary
    OnCalendar  string // Timer schedule (e.g., "daily")
    Description string
}

func RenderService(cfg UnitConfig) ([]byte, error) {
    tmpl, err := template.ParseFS(assets, "assets/updex.service")
    if err != nil {
        return nil, err
    }
    var buf bytes.Buffer
    if err := tmpl.Execute(&buf, cfg); err != nil {
        return nil, err
    }
    return buf.Bytes(), nil
}
```

### Pattern 2: Library Method for Each Operation

**What:** Each CLI command maps to a single public API method on `Client`. The method accepts a typed options struct and returns a typed result struct.

**When to use:** Always for this project — maintains the existing Library + CLI separation.

**Trade-offs:**
- Pros: Clean API, testable without CLI, supports JSON output, enables programmatic use
- Cons: Slightly more boilerplate (options/result types for each operation)

**Example:**
```go
// updex/daemon.go
type EnableAutoUpdateOptions struct {
    OnCalendar string // systemd OnCalendar spec (default: "daily")
    Component  string // Optional: only auto-update specific component
}

type EnableAutoUpdateResult struct {
    ServicePath string // Where service file was installed
    TimerPath   string // Where timer file was installed
    Enabled     bool   // Whether timer was enabled
    Started     bool   // Whether timer was started
    Error       string
}

func (c *Client) EnableAutoUpdate(ctx context.Context, opts EnableAutoUpdateOptions) (*EnableAutoUpdateResult, error) {
    // 1. Render unit files from templates
    // 2. Install to /etc/systemd/system/ (or user location)
    // 3. systemctl daemon-reload
    // 4. systemctl enable updex.timer
    // 5. systemctl start updex.timer
    return result, nil
}
```

### Pattern 3: Filesystem Abstraction for Testing

**What:** Use interfaces or helper functions that allow filesystem operations to be redirected to temp directories during tests.

**When to use:** For code that writes to `/etc/systemd/system/` or other system paths.

**Trade-offs:**
- Pros: Enables comprehensive testing without root, tests actual file operations
- Cons: Requires careful design of path configuration

**Example:**
```go
// internal/systemd/manager.go
type Manager struct {
    SystemdDir string // Default: /etc/systemd/system, override in tests
    BinaryPath string // Path to updex binary for ExecStart
}

func NewManager() *Manager {
    return &Manager{
        SystemdDir: "/etc/systemd/system",
        BinaryPath: "/usr/local/bin/updex",
    }
}

// In tests:
func TestInstallUnits(t *testing.T) {
    tmpDir := t.TempDir()
    mgr := &Manager{
        SystemdDir: tmpDir,
        BinaryPath: "/tmp/fake-updex",
    }
    // Test file creation without root access
}
```

## Data Flow

### Auto-Update Enable Flow

```
User: updex daemon enable --schedule daily
    ↓
CLI: Parse flags, call client.EnableAutoUpdate()
    ↓
Client: Render service template with binary path
    ↓
Client: Render timer template with OnCalendar=daily
    ↓
Manager: Write /etc/systemd/system/updex.service
    ↓
Manager: Write /etc/systemd/system/updex.timer
    ↓
Manager: exec systemctl daemon-reload
    ↓
Manager: exec systemctl enable --now updex.timer
    ↓
Result: Return paths and status to CLI
```

### Auto-Update Execution Flow (Timer Triggered)

```
systemd: Timer fires based on OnCalendar schedule
    ↓
systemd: Starts updex.service
    ↓
Service: Runs "updex update --all"
    ↓
updex: For each configured transfer:
    ↓
    Check for newer version → Download → Install → Vacuum
    ↓
updex: Exit with success/failure code
    ↓
systemd: Logs exit status, schedules next timer
```

### Key Data Flows

1. **Unit Installation:** CLI → Client → Manager → filesystem + systemctl
2. **Timer Execution:** systemd timer → updex binary → standard update flow
3. **Status Query:** CLI → Client → Manager → systemctl status parsing → result

## Scaling Considerations

| Scale | Architecture Adjustments |
|-------|--------------------------|
| Single system | Default: timer runs updex update, logs to journal |
| Fleet (10-1000) | No binary changes needed; use Ansible/Puppet to deploy timer with consistent schedule |
| Large fleet (1000+) | Consider staggered schedules (RandomizedDelaySec in timer) to avoid thundering herd |

### Scaling Priorities

1. **First bottleneck:** Concurrent downloads from same repository — Not a problem for single-system use, but timer's RandomizedDelaySec helps for fleets
2. **Second bottleneck:** Log management — Journal handles this; no changes needed

## Anti-Patterns

### Anti-Pattern 1: Hardcoded Paths in Multiple Places

**What people do:** Scatter `/etc/systemd/system/` and binary paths throughout the code.

**Why it's wrong:** Harder to test, harder to support user installs vs system installs.

**Do this instead:** Centralize paths in a config struct passed to the Manager. Test with temp directories.

```go
// Bad
func InstallTimer() {
    path := "/etc/systemd/system/updex.timer"
    // ...
}

// Good
func (m *Manager) InstallTimer() {
    path := filepath.Join(m.SystemdDir, "updex.timer")
    // ...
}
```

### Anti-Pattern 2: Calling systemctl Without Checking Availability

**What people do:** Assume systemctl exists and works.

**Why it's wrong:** Fails cryptically in containers, WSL, or non-systemd systems.

**Do this instead:** Check for systemd availability before operations, return clear errors.

```go
func (m *Manager) checkSystemd() error {
    _, err := exec.LookPath("systemctl")
    if err != nil {
        return fmt.Errorf("systemctl not found: auto-update requires systemd")
    }
    return nil
}
```

### Anti-Pattern 3: Testing External Commands by Running Them

**What people do:** Test Manager by actually calling systemctl (requires root, modifies system).

**Why it's wrong:** Tests can't run in CI, require cleanup, may break the system.

**Do this instead:** Test file generation separately from systemctl execution. Use interface for command execution if needed.

```go
// Test file generation (no root needed)
func TestRenderService(t *testing.T) {
    content, err := RenderService(UnitConfig{...})
    // Assert content contains expected strings
}

// Test file installation (temp dir, no root)
func TestInstallUnits(t *testing.T) {
    mgr := &Manager{SystemdDir: t.TempDir()}
    err := mgr.InstallUnits(UnitConfig{...})
    // Check files exist with correct content
}

// Skip systemctl tests unless root
func TestEnableTimer(t *testing.T) {
    if os.Geteuid() != 0 {
        t.Skip("requires root")
    }
    // Actual integration test
}
```

## Test Architecture for CLI Tools

### Test Organization

```
updex/
├── internal/
│   ├── config/
│   │   └── transfer_test.go       # Unit tests for config parsing
│   ├── systemd/
│   │   └── manager_test.go        # Unit tests for unit management
│   └── ...
├── updex/
│   └── updex_test.go              # Integration tests for Client methods
└── test/                          # NEW: Test helpers and fixtures
    ├── helpers.go                 # Shared test utilities
    └── fixtures/
        └── transfers/             # Sample .transfer files
```

### Test Patterns for This Codebase

**1. Temp Directory Pattern (existing, continue using)**
```go
func TestSomething(t *testing.T) {
    tmpDir := t.TempDir() // Auto-cleaned after test
    // Create test files
    // Run test
}
```

**2. Table-Driven Tests (existing, continue using)**
```go
func TestVersionParsing(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    string
        wantErr bool
    }{
        {"valid", "1.2.3", "1.2.3", false},
        {"invalid", "abc", "", true},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // ...
        })
    }
}
```

**3. Skip for Root-Required Tests (add for systemd tests)**
```go
func TestSystemctlEnable(t *testing.T) {
    if os.Geteuid() != 0 {
        t.Skip("requires root privileges")
    }
    // Integration test that actually calls systemctl
}
```

**4. HTTP Test Server for Download Tests (add for API tests)**
```go
func TestDownload(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.URL.Path == "/SHA256SUMS" {
            fmt.Fprintln(w, "abc123  file.raw")
        }
    }))
    defer srv.Close()
    
    // Test download against srv.URL
}
```

**5. Fixture Files for Complex Configs**
```go
//go:embed testdata/valid.transfer
var validTransfer string

func TestParseTransfer(t *testing.T) {
    // Use embedded fixture
}
```

### Test Coverage Priorities

| Priority | Package | Current | Target | Rationale |
|----------|---------|---------|--------|-----------|
| 1 | `internal/systemd/` | 0% (new) | 80%+ | Core new feature |
| 2 | `updex/` | 0% | 60%+ | Public API, enables downstream confidence |
| 3 | `cmd/commands/` | 0% | 40%+ | E2E coverage for user flows |
| 4 | `internal/sysext/` | 42% | 60%+ | Critical operations |

## Integration Points

### External Services

| Service | Integration Pattern | Notes |
|---------|---------------------|-------|
| systemd (systemctl) | exec.Command | Check availability before use, parse exit codes |
| systemd (journal) | stdout/stderr | Service output goes to journal automatically |
| HTTP registries | net/http client | Existing pattern, no changes needed |

### Internal Boundaries

| Boundary | Communication | Notes |
|----------|---------------|-------|
| CLI → Client | Method calls | Typed options/results, maintains current pattern |
| Client → Manager | Method calls | New Manager struct in internal/systemd/ |
| Manager → systemctl | exec.Command | Isolated for testing, check exit codes |
| Client → internal/sysext | Method calls | Existing pattern, no changes |

## Build Order Implications

Based on this architecture, suggested build order for the milestone:

### Phase 1: Test Infrastructure Foundation
**Rationale:** Adding tests first enables confidence in subsequent changes
- Add test helpers package (`test/helpers.go`)
- Add HTTP test server helpers for download tests
- Add fixtures for .transfer files
- Improve existing test coverage (internal/sysext, internal/config)

### Phase 2: Systemd Unit Management Core
**Rationale:** Build the internal package before exposing via CLI
- Create `internal/systemd/` package
- Implement unit template rendering
- Implement file installation (with configurable paths)
- Add comprehensive unit tests with temp directories

### Phase 3: Public API Integration
**Rationale:** Expose via Client after internals are solid
- Add `EnableAutoUpdate()` method to Client
- Add `DisableAutoUpdate()` method to Client
- Add `AutoUpdateStatus()` method to Client
- Add result types with JSON tags

### Phase 4: CLI Commands
**Rationale:** Thin wrappers over tested API
- Add `updex daemon enable` command
- Add `updex daemon disable` command
- Add `updex daemon status` command
- Add help text and documentation

### Phase 5: Integration Testing
**Rationale:** End-to-end validation after components complete
- Add root-required integration tests (skipped in CI)
- Document manual testing procedure
- Update Makefile with test targets

## Sources

- Existing codebase analysis (2026-01-26)
- Go standard library documentation: https://pkg.go.dev/testing
- Go embed package: https://pkg.go.dev/embed
- Project ARCHITECTURE.md: .planning/codebase/ARCHITECTURE.md
- Project TESTING.md: .planning/codebase/TESTING.md
- systemd.timer(5) man page (documented patterns, not fetched due to site blocking)
- systemd.service(5) man page (documented patterns, not fetched due to site blocking)

---

*Architecture research for: Go CLI auto-update daemon and test infrastructure*
*Researched: 2026-01-26*
