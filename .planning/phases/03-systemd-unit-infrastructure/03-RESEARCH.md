# Phase 3: Systemd Unit Infrastructure - Research

**Researched:** 2026-01-26
**Domain:** Systemd timer/service unit file generation and management in Go
**Confidence:** HIGH

## Summary

This phase creates an internal package to generate, install, and manage systemd timer and service unit files for scheduling automatic sysext updates. The core challenge is generating valid systemd unit file syntax programmatically and installing them to appropriate paths while maintaining testability without root privileges.

Systemd timers consist of two linked files: a `.timer` file that defines the schedule and a `.service` file that defines the action. For periodic tasks like sysext updates, we need:
1. A timer that triggers on a calendar schedule (e.g., daily)
2. A service that runs `updex update` (or similar)
3. Both files installed to `/etc/systemd/system/` (or user-configurable path)

The existing `go-systemd` library from CoreOS provides a well-tested `unit` package for serializing unit files. However, given the project's pattern of minimal dependencies and the simplicity of unit file format (INI-like sections), hand-rolling generation is equally viable. The project already uses `gopkg.in/ini.v1` for parsing `.transfer` files, but unit file generation is straightforward enough that templates or simple string builders work well.

**Primary recommendation:** Create a new `internal/systemd` package with types for timer/service configuration and simple template-based generation, using configurable paths for testability.

## Standard Stack

The established libraries/tools for this domain:

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| Go stdlib `text/template` | Go stdlib | Generate unit file content | Built-in, familiar, handles escaping |
| Go stdlib `os` | Go stdlib | File operations | Standard for file I/O |
| Go stdlib `os/exec` | Go stdlib | Run `systemctl` commands | Standard for command execution |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `path/filepath` | Go stdlib | Cross-platform path handling | Path construction |
| `fmt` | Go stdlib | String formatting | Simple unit file generation alternative |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| `text/template` | `github.com/coreos/go-systemd/v22/unit` | More robust but adds dependency; project prefers minimal deps |
| `text/template` | String concatenation with `fmt.Sprintf` | Simpler for small files, less maintainable for complex templates |
| `os.WriteFile` | `afero` filesystem abstraction | Adds dependency; `t.TempDir()` pattern works well |

**Installation:** None required - uses Go stdlib only.

## Architecture Patterns

### Recommended Project Structure
```
internal/
├── systemd/           # NEW: Systemd unit management
│   ├── unit.go        # Unit file types and generation
│   ├── unit_test.go   # Unit generation tests
│   ├── manager.go     # Install/remove operations + systemctl
│   └── manager_test.go
├── sysext/            # Existing: sysext management
└── config/            # Existing: .transfer/.feature parsing
```

### Pattern 1: Configuration Struct with Builder
**What:** Define structs that represent timer/service configuration, then generate unit content
**When to use:** When generating systemd unit files with multiple configurable options
**Example:**
```go
// Source: Pattern adapted from go-systemd unit package concepts

// TimerConfig represents configuration for a systemd timer
type TimerConfig struct {
    Name           string        // e.g., "updex-update"
    Description    string        // e.g., "Automatic sysext updates"
    OnCalendar     string        // e.g., "daily" or "*-*-* 04:00:00"
    Persistent     bool          // Whether to run if missed
    RandomDelaySec int           // Randomize start within this window
    Unit           string        // Service to trigger (optional, defaults to Name.service)
}

// ServiceConfig represents configuration for a systemd service
type ServiceConfig struct {
    Name        string   // e.g., "updex-update"
    Description string
    ExecStart   string   // e.g., "/usr/bin/updex update --quiet"
    Type        string   // e.g., "oneshot"
    User        string   // Optional: run as specific user
    Environment []string // Optional: environment variables
}
```

### Pattern 2: Template-Based Generation
**What:** Use Go templates to generate unit file content
**When to use:** When unit files have a predictable structure with variable parts
**Example:**
```go
// Source: Go stdlib text/template

const timerTemplate = `[Unit]
Description={{.Description}}

[Timer]
OnCalendar={{.OnCalendar}}
{{- if .Persistent}}
Persistent=true
{{- end}}
{{- if gt .RandomDelaySec 0}}
RandomizedDelaySec={{.RandomDelaySec}}s
{{- end}}
{{- if .Unit}}
Unit={{.Unit}}
{{- end}}

[Install]
WantedBy=timers.target
`

func (t *TimerConfig) Generate() (string, error) {
    tmpl, err := template.New("timer").Parse(timerTemplate)
    if err != nil {
        return "", err
    }
    var buf bytes.Buffer
    if err := tmpl.Execute(&buf, t); err != nil {
        return "", err
    }
    return buf.String(), nil
}
```

### Pattern 3: Configurable Base Path for Testability
**What:** Allow unit installation path to be configured, defaulting to `/etc/systemd/system`
**When to use:** Always - enables testing without root
**Example:**
```go
// Source: Existing project pattern from ClientConfig

// Manager handles systemd unit file operations
type Manager struct {
    UnitPath string // Defaults to /etc/systemd/system
    runner   SystemctlRunner
}

// NewManager creates a manager with default paths
func NewManager() *Manager {
    return &Manager{
        UnitPath: "/etc/systemd/system",
        runner:   &DefaultSystemctlRunner{},
    }
}

// For testing
func NewTestManager(unitPath string, runner SystemctlRunner) *Manager {
    return &Manager{
        UnitPath: unitPath,
        runner:   runner,
    }
}
```

### Pattern 4: Interface Abstraction for systemctl Commands
**What:** Abstract systemctl operations behind an interface for testability
**When to use:** Any code that calls `systemctl daemon-reload`, `enable`, `start`, etc.
**Example:**
```go
// Source: Existing SysextRunner pattern in internal/sysext/runner.go

// SystemctlRunner executes systemctl commands
type SystemctlRunner interface {
    DaemonReload() error
    Enable(unit string) error
    Disable(unit string) error
    Start(unit string) error
    Stop(unit string) error
    IsActive(unit string) (bool, error)
}

// DefaultSystemctlRunner executes real systemctl commands
type DefaultSystemctlRunner struct{}

func (r *DefaultSystemctlRunner) DaemonReload() error {
    return exec.Command("systemctl", "daemon-reload").Run()
}

func (r *DefaultSystemctlRunner) Enable(unit string) error {
    return exec.Command("systemctl", "enable", unit).Run()
}
```

### Anti-Patterns to Avoid
- **Hardcoded paths:** Always use configurable `UnitPath`, never hardcode `/etc/systemd/system`
- **Direct file writes without verification:** Validate generated content before writing
- **Missing daemon-reload:** After installing/removing units, must call `systemctl daemon-reload`
- **Ignoring existing files:** Check for existing units before overwriting without warning
- **Root-only tests:** All tests should use temp directories, no root required

## Don't Hand-Roll

Problems that look simple but have existing solutions:

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| INI section ordering | Custom parser/serializer | Template or go-systemd/unit | Sections must be in correct order |
| Path escaping for ExecStart | Manual escaping | Let systemd handle it | Paths with spaces are complex |
| Timer schedule validation | Regex matching | `systemd-analyze calendar` | Systemd has complex calendar syntax |
| User unit directory detection | Hardcoded `~/.config/systemd/user` | `$XDG_CONFIG_HOME` or `systemd-path` | XDG spec compliance |

**Key insight:** Unit file format is simple (INI-like), but the semantics are complex. Keep generation simple, validate with `systemd-analyze verify` if needed.

## Common Pitfalls

### Pitfall 1: Forgetting daemon-reload
**What goes wrong:** Unit files installed but systemd doesn't see them
**Why it happens:** Systemd caches unit file contents; must reload after changes
**How to avoid:** Always call `systemctl daemon-reload` after install/remove
**Warning signs:** "Unit not found" errors even though file exists

### Pitfall 2: Wrong File Permissions
**What goes wrong:** Systemd refuses to load unit files with incorrect permissions
**Why it happens:** Unit files should be 0644, not 0755 or world-writable
**How to avoid:** Use `os.WriteFile(path, content, 0644)` consistently
**Warning signs:** "Bad file permissions" in journal

### Pitfall 3: Timer Without Service
**What goes wrong:** Timer activates but nothing happens
**Why it happens:** Timer's `Unit=` doesn't match existing service, or service file missing
**How to avoid:** Generate both files together; use matching names (foo.timer + foo.service)
**Warning signs:** Timer shows as active but service never runs

### Pitfall 4: Missing [Install] Section
**What goes wrong:** `systemctl enable` fails silently
**Why it happens:** Timer/service must have `[Install]` with `WantedBy=` to be enableable
**How to avoid:** Always include `[Install]` section with appropriate target
**Warning signs:** "Unit is not enabled" after enable command

### Pitfall 5: Unescaped Special Characters in Values
**What goes wrong:** Unit file parse errors
**Why it happens:** Values containing `%`, `\`, or quotes need escaping
**How to avoid:** For ExecStart, use full absolute paths; for descriptions, sanitize input
**Warning signs:** "Invalid escape sequence" or "Line continuation without continuation"

### Pitfall 6: Testing with Real systemctl
**What goes wrong:** Tests fail without root, or modify real system state
**Why it happens:** Direct calls to `systemctl` without abstraction
**How to avoid:** Use interface abstraction (SystemctlRunner) and inject mock in tests
**Warning signs:** Tests require sudo, tests leave units installed

## Code Examples

Verified patterns from official sources and project conventions:

### Minimal Timer Unit File
```ini
# Source: ArchWiki Systemd/Timers + freedesktop.org systemd docs
# This is what we need to generate for updex

[Unit]
Description=Automatic sysext update timer

[Timer]
OnCalendar=daily
Persistent=true
RandomizedDelaySec=1h

[Install]
WantedBy=timers.target
```

### Minimal Service Unit File
```ini
# Source: ArchWiki + systemd.service(5)
# Paired with the timer above

[Unit]
Description=Automatic sysext update

[Service]
Type=oneshot
ExecStart=/usr/bin/updex update --quiet
```

### Timer/Service Generator Functions
```go
// Source: Adapted from go-systemd patterns + project conventions

func GenerateTimer(cfg *TimerConfig) string {
    var b strings.Builder
    
    // [Unit] section
    b.WriteString("[Unit]\n")
    b.WriteString(fmt.Sprintf("Description=%s\n", cfg.Description))
    b.WriteString("\n")
    
    // [Timer] section
    b.WriteString("[Timer]\n")
    b.WriteString(fmt.Sprintf("OnCalendar=%s\n", cfg.OnCalendar))
    if cfg.Persistent {
        b.WriteString("Persistent=true\n")
    }
    if cfg.RandomDelaySec > 0 {
        b.WriteString(fmt.Sprintf("RandomizedDelaySec=%ds\n", cfg.RandomDelaySec))
    }
    b.WriteString("\n")
    
    // [Install] section
    b.WriteString("[Install]\n")
    b.WriteString("WantedBy=timers.target\n")
    
    return b.String()
}

func GenerateService(cfg *ServiceConfig) string {
    var b strings.Builder
    
    // [Unit] section
    b.WriteString("[Unit]\n")
    b.WriteString(fmt.Sprintf("Description=%s\n", cfg.Description))
    b.WriteString("\n")
    
    // [Service] section
    b.WriteString("[Service]\n")
    b.WriteString(fmt.Sprintf("Type=%s\n", cfg.Type))
    b.WriteString(fmt.Sprintf("ExecStart=%s\n", cfg.ExecStart))
    if cfg.User != "" {
        b.WriteString(fmt.Sprintf("User=%s\n", cfg.User))
    }
    
    return b.String()
}
```

### File Installation with Temp Directory Testing
```go
// Source: Existing project test patterns + os package

func (m *Manager) Install(timer *TimerConfig, service *ServiceConfig) error {
    // Generate content
    timerContent := GenerateTimer(timer)
    serviceContent := GenerateService(service)
    
    timerPath := filepath.Join(m.UnitPath, timer.Name+".timer")
    servicePath := filepath.Join(m.UnitPath, service.Name+".service")
    
    // Write timer file
    if err := os.WriteFile(timerPath, []byte(timerContent), 0644); err != nil {
        return fmt.Errorf("failed to write timer: %w", err)
    }
    
    // Write service file
    if err := os.WriteFile(servicePath, []byte(serviceContent), 0644); err != nil {
        // Clean up timer if service write fails
        os.Remove(timerPath)
        return fmt.Errorf("failed to write service: %w", err)
    }
    
    // Reload systemd (only if real system)
    if err := m.runner.DaemonReload(); err != nil {
        return fmt.Errorf("daemon-reload failed: %w", err)
    }
    
    return nil
}

// Test example
func TestInstall(t *testing.T) {
    tmpDir := t.TempDir()
    mockRunner := &MockSystemctlRunner{}
    
    mgr := NewTestManager(tmpDir, mockRunner)
    
    timer := &TimerConfig{
        Name:        "updex-update",
        Description: "Test timer",
        OnCalendar:  "daily",
        Persistent:  true,
    }
    service := &ServiceConfig{
        Name:        "updex-update",
        Description: "Test service",
        Type:        "oneshot",
        ExecStart:   "/usr/bin/updex update",
    }
    
    err := mgr.Install(timer, service)
    if err != nil {
        t.Fatalf("Install() error = %v", err)
    }
    
    // Verify files exist
    if _, err := os.Stat(filepath.Join(tmpDir, "updex-update.timer")); err != nil {
        t.Error("timer file not created")
    }
    if _, err := os.Stat(filepath.Join(tmpDir, "updex-update.service")); err != nil {
        t.Error("service file not created")
    }
    
    // Verify daemon-reload was called
    if !mockRunner.DaemonReloadCalled {
        t.Error("DaemonReload() not called")
    }
}
```

### Unit Removal
```go
// Source: Project patterns

func (m *Manager) Remove(name string) error {
    timerPath := filepath.Join(m.UnitPath, name+".timer")
    servicePath := filepath.Join(m.UnitPath, name+".service")
    
    // Stop and disable first
    _ = m.runner.Stop(name + ".timer")     // Ignore errors if not running
    _ = m.runner.Disable(name + ".timer")  // Ignore errors if not enabled
    
    // Remove files
    var errs []error
    if err := os.Remove(timerPath); err != nil && !os.IsNotExist(err) {
        errs = append(errs, fmt.Errorf("remove timer: %w", err))
    }
    if err := os.Remove(servicePath); err != nil && !os.IsNotExist(err) {
        errs = append(errs, fmt.Errorf("remove service: %w", err))
    }
    
    // Reload daemon
    if err := m.runner.DaemonReload(); err != nil {
        errs = append(errs, fmt.Errorf("daemon-reload: %w", err))
    }
    
    if len(errs) > 0 {
        return fmt.Errorf("errors during removal: %v", errs)
    }
    return nil
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Cron for scheduling | systemd timers | ~2012 (systemd adoption) | Better logging, dependencies, calendar syntax |
| SysV init scripts | systemd services | ~2012 | Declarative, dependency-aware |
| `go-systemd/unit.Serialize()` | Direct templates | Still valid | Both work; templates are simpler for our use case |

**Deprecated/outdated:**
- Cron: Still works but systemd timers are preferred for systemd systems (better integration, logging)
- `ioutil.WriteFile`: Use `os.WriteFile` (deprecated in Go 1.16)

## Open Questions

Things that couldn't be fully resolved:

1. **User vs system units?**
   - What we know: System units go to `/etc/systemd/system`, user units to `~/.config/systemd/user`
   - What's unclear: Should updex support user-level timers or only system-level?
   - Recommendation: Start with system-level only (requires root for install, but that's expected for sysext management)

2. **Should we validate OnCalendar syntax?**
   - What we know: `systemd-analyze calendar "daily"` can validate timer specs
   - What's unclear: Worth the complexity of calling external command?
   - Recommendation: Accept any string; let systemd report errors on load. Document valid syntax in help.

3. **What happens if timer already exists?**
   - What we know: Need to decide: overwrite, error, or prompt
   - What's unclear: User expectation
   - Recommendation: Default to error with `--force` to overwrite (consistent with project's careful-by-default approach)

## Sources

### Primary (HIGH confidence)
- ArchWiki Systemd/Timers (https://wiki.archlinux.org/title/Systemd/Timers) - timer/service syntax and examples
- CoreOS go-systemd unit package (https://github.com/coreos/go-systemd) - serialization patterns
- Existing project patterns (internal/sysext/runner.go) - interface abstraction pattern
- Go stdlib testing, os, text/template documentation

### Secondary (MEDIUM confidence)
- freedesktop.org systemd documentation (access blocked, referenced via ArchWiki)

### Tertiary (LOW confidence)
- None

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH - Uses Go stdlib only, matches project conventions
- Architecture: HIGH - Pattern directly adapted from existing sysext runner pattern
- Pitfalls: HIGH - Based on well-documented systemd behaviors and project experience
- Code examples: HIGH - Adapted from verified sources (go-systemd, ArchWiki, project code)

**Research date:** 2026-01-26
**Valid until:** 2026-02-26 (systemd unit format is stable; Go stdlib is stable)
