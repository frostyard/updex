# Phase 1: Test Foundation - Research

**Researched:** 2026-01-26
**Domain:** Go Testing (unit tests, mocking, HTTP testing)
**Confidence:** HIGH

## Summary

This phase establishes testing infrastructure for the updex project, a Go CLI tool that manages systemd-sysext images. The project already has good test coverage in internal packages (`config`, `sysext`, `manifest`, `version`, `download`) but lacks tests for the core `updex` package that implements the main operations (list, check, update, install, remove).

The existing tests use idiomatic Go patterns: `t.TempDir()` for filesystem isolation, table-driven tests, and direct testing without third-party mocking libraries. This approach should continue. The main challenges are:
1. Core operations call `sysext.Refresh()`, `sysext.Merge()`, `sysext.Unmerge()` which execute `systemd-sysext` requiring root
2. HTTP fetching for manifests and downloads (plain HTTP, not OCI registry)
3. Filesystem operations that write to `/var/lib/extensions` and `/etc/sysupdate.d`

**Primary recommendation:** Use interface abstraction for systemd commands and `httptest.Server` for HTTP mocking, following existing patterns (temp directories for filesystem isolation).

## Standard Stack

The established libraries/tools for this domain:

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `testing` | Go stdlib | Test framework | Built-in, no dependencies |
| `net/http/httptest` | Go stdlib | HTTP server mocking | Built-in, lightweight, perfect for plain HTTP |
| `t.TempDir()` | Go stdlib | Filesystem isolation | Auto-cleanup, test-scoped |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `io/fs` | Go stdlib | Filesystem abstraction (if needed) | Only if temp directories are insufficient |
| `bytes` | Go stdlib | Buffer responses | Building test fixtures |

### Not Needed
| Instead of | Avoid | Reason |
|------------|-------|--------|
| testify | - | Project already uses stdlib assertions, keep consistency |
| gomock | - | Interface mocking is simple enough by hand |
| afero | - | `t.TempDir()` is sufficient and already used |
| dockertest | - | Registry is plain HTTP, not OCI |

**Installation:** None required - all tools are Go stdlib.

## Architecture Patterns

### Recommended Project Structure
```
updex/
├── updex/
│   ├── updex.go           # Client type
│   ├── list.go            # List operation
│   ├── list_test.go       # Co-located tests
│   ├── check.go           # Check operation
│   ├── check_test.go      # Co-located tests
│   └── ...
├── internal/
│   ├── sysext/
│   │   ├── manager.go
│   │   ├── manager_test.go
│   │   └── testdata/      # Test fixtures (if needed)
│   ├── config/
│   │   ├── transfer.go
│   │   ├── transfer_test.go
│   │   └── testdata/      # .transfer file fixtures
│   └── ...
└── testdata/              # Shared fixtures (only if truly shared)
```

### Pattern 1: Table-Driven Tests
**What:** Define test cases as a slice/map of structs, iterate and test each
**When to use:** Testing functions with multiple input/output combinations
**Example:**
```go
// Source: Go Wiki TableDrivenTests + existing project patterns
func TestList(t *testing.T) {
    tests := []struct {
        name      string
        setup     func(*testing.T, *httptest.Server) // Prepare state
        opts      ListOptions
        wantLen   int
        wantErr   bool
    }{
        {
            name: "empty config returns error",
            setup: func(t *testing.T, s *httptest.Server) {},
            opts: ListOptions{},
            wantErr: true,
        },
        // ...more cases
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Create temp directory for config/target
            tmpDir := t.TempDir()
            server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                // Serve test fixtures
            }))
            defer server.Close()
            
            tt.setup(t, server)
            
            client := NewClient(ClientConfig{Definitions: tmpDir})
            results, err := client.List(context.Background(), tt.opts)
            
            if tt.wantErr && err == nil {
                t.Error("expected error, got nil")
            }
            if !tt.wantErr && err != nil {
                t.Errorf("unexpected error: %v", err)
            }
            if len(results) != tt.wantLen {
                t.Errorf("got %d results, want %d", len(results), tt.wantLen)
            }
        })
    }
}
```

### Pattern 2: Interface Abstraction for System Commands
**What:** Extract systemd commands behind an interface that can be swapped in tests
**When to use:** Operations that call `exec.Command("systemd-sysext", ...)`
**Example:**
```go
// Source: Go idiomatic pattern for testability

// SysextRunner executes systemd-sysext commands
type SysextRunner interface {
    Refresh() error
    Merge() error
    Unmerge() error
}

// DefaultRunner executes real systemd-sysext commands
type DefaultRunner struct{}

func (r *DefaultRunner) Refresh() error {
    return exec.Command("systemd-sysext", "refresh").Run()
}

// MockRunner for testing
type MockRunner struct {
    RefreshCalled bool
    RefreshErr    error
}

func (m *MockRunner) Refresh() error {
    m.RefreshCalled = true
    return m.RefreshErr
}
```

### Pattern 3: httptest.Server for HTTP Mocking
**What:** Create a test HTTP server that returns controlled responses
**When to use:** Testing code that fetches manifests or downloads files
**Example:**
```go
// Source: Go stdlib net/http/httptest

func TestManifestFetch(t *testing.T) {
    manifestContent := `a1b2c3...  file_1.0.0.raw
b2c3d4...  file_2.0.0.raw`

    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        switch r.URL.Path {
        case "/SHA256SUMS":
            w.Write([]byte(manifestContent))
        case "/SHA256SUMS.gpg":
            w.WriteHeader(http.StatusNotFound) // No signature
        default:
            w.WriteHeader(http.StatusNotFound)
        }
    }))
    defer server.Close()
    
    m, err := manifest.Fetch(server.URL, false)
    if err != nil {
        t.Fatalf("Fetch() error = %v", err)
    }
    if len(m.Files) != 2 {
        t.Errorf("got %d files, want 2", len(m.Files))
    }
}
```

### Pattern 4: Filesystem Isolation with t.TempDir()
**What:** Create temporary directories for each test that auto-cleanup
**When to use:** Any test that reads/writes files
**Example:**
```go
// Source: Existing project patterns (sysext/manager_test.go)

func TestGetInstalledVersions(t *testing.T) {
    tmpDir := t.TempDir()
    
    // Create test extension files
    files := []string{"myext_1.0.0.raw", "myext_1.1.0.raw"}
    for _, f := range files {
        if err := os.WriteFile(filepath.Join(tmpDir, f), []byte("test"), 0644); err != nil {
            t.Fatalf("failed to create test file: %v", err)
        }
    }
    
    transfer := &config.Transfer{
        Target: config.TargetSection{
            Path:         tmpDir,  // Point to temp directory
            MatchPattern: "myext_@v.raw",
        },
    }
    
    versions, current, err := sysext.GetInstalledVersions(transfer)
    // assertions...
}
```

### Anti-Patterns to Avoid
- **Global state modification:** Don't modify package-level variables for testing; use dependency injection
- **Testing private functions directly:** Test through public API; refactor if internals need testing
- **Shared mutable fixtures:** Each test should set up its own state
- **Hardcoded paths:** Always use `t.TempDir()` or configurable paths
- **Sleeping in tests:** Use proper synchronization or mocked time

## Don't Hand-Roll

Problems that look simple but have existing solutions:

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| HTTP test server | Custom listener | `httptest.NewServer()` | TLS support, auto port, clean shutdown |
| Temp directories | `os.MkdirTemp()` with manual cleanup | `t.TempDir()` | Auto-cleanup, unique per test |
| Assertions | Custom helpers | stdlib comparisons | Keep project consistency |
| Parallel file access | Custom locking | Per-test `t.TempDir()` | Isolation is better than synchronization |

**Key insight:** Go's stdlib testing tools are sufficient for this project. The existing tests demonstrate this pattern works well.

## Common Pitfalls

### Pitfall 1: Testing Against Real systemd-sysext
**What goes wrong:** Tests fail without root, tests fail on non-systemd systems, tests modify real system state
**Why it happens:** Direct calls to `exec.Command("systemd-sysext", ...)` without abstraction
**How to avoid:** 
1. Extract `sysext.Refresh()`, `sysext.Merge()`, `sysext.Unmerge()` behind an interface
2. Pass the interface to `Client` or use a package-level variable for testing
3. In tests, inject a mock implementation
**Warning signs:** Tests require `sudo`, tests skip on CI, tests leave state behind

### Pitfall 2: Flaky HTTP Tests
**What goes wrong:** Tests pass locally but fail in CI due to timing or network issues
**Why it happens:** Using real HTTP endpoints, race conditions in test server setup
**How to avoid:**
1. Always use `httptest.Server` - never real endpoints
2. Create server before making requests, close with `defer`
3. Use synchronous request/response patterns
**Warning signs:** Tests pass on retry, tests fail only in CI

### Pitfall 3: Test Pollution from Shared Fixtures
**What goes wrong:** One test modifies fixture files, affecting subsequent tests
**Why it happens:** Fixtures in static `testdata/` directories that get modified
**How to avoid:**
1. Copy fixtures to `t.TempDir()` before modification
2. Use inline fixture strings for small data
3. Create fixtures programmatically in test setup
**Warning signs:** Test order affects results, parallel tests fail

### Pitfall 4: Missing Error Case Coverage
**What goes wrong:** Happy path works, but errors cause panics or incorrect behavior
**Why it happens:** Only testing success cases
**How to avoid:**
1. Test HTTP errors (timeouts, 404s, 500s, malformed responses)
2. Test filesystem errors (permission denied, disk full, missing files)
3. Test config errors (missing sections, invalid values)
**Warning signs:** Code has error returns but tests don't verify them

### Pitfall 5: Over-Mocking Implementation Details
**What goes wrong:** Tests pass but real code is broken because mocks are too lenient
**Why it happens:** Mocking internals instead of boundaries
**How to avoid:**
1. Mock at system boundaries (HTTP, exec, filesystem)
2. Let internal functions call each other naturally
3. Verify mock interactions sparingly (was it called?) not strictly (exact call order)
**Warning signs:** Refactoring breaks many tests, tests don't catch bugs

## Code Examples

Verified patterns from existing project and Go stdlib:

### HTTP Server Mock for Registry
```go
// Create a test HTTP server that serves SHA256SUMS manifest
func newTestServer(t *testing.T, files map[string]string) *httptest.Server {
    t.Helper()
    return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.URL.Path == "/SHA256SUMS" {
            var lines []string
            for filename, hash := range files {
                lines = append(lines, fmt.Sprintf("%s  %s", hash, filename))
            }
            w.Write([]byte(strings.Join(lines, "\n")))
            return
        }
        // Serve file content for downloads
        filename := strings.TrimPrefix(r.URL.Path, "/")
        if _, ok := files[filename]; ok {
            w.Write([]byte("file content for " + filename))
            return
        }
        w.WriteHeader(http.StatusNotFound)
    }))
}
```

### Transfer Config Fixture
```go
// Create a test transfer config file
func createTransferFile(t *testing.T, dir, component, baseURL string) {
    t.Helper()
    content := fmt.Sprintf(`[Source]
Type=url-file
Path=%s
MatchPattern=%s_@v.raw

[Target]
MatchPattern=%s_@v.raw
`, baseURL, component, component)
    
    path := filepath.Join(dir, component+".transfer")
    if err := os.WriteFile(path, []byte(content), 0644); err != nil {
        t.Fatalf("failed to create transfer file: %v", err)
    }
}
```

### Filesystem Test Setup
```go
// Setup test environment with config and target directories
func setupTestEnv(t *testing.T) (configDir, targetDir string) {
    t.Helper()
    configDir = t.TempDir()
    targetDir = t.TempDir()
    return configDir, targetDir
}

// Create fake installed extension files
func createInstalledVersions(t *testing.T, targetDir, pattern string, versions []string) {
    t.Helper()
    p, err := version.ParsePattern(pattern)
    if err != nil {
        t.Fatalf("invalid pattern: %v", err)
    }
    for _, v := range versions {
        filename := p.BuildFilename(v)
        path := filepath.Join(targetDir, filename)
        if err := os.WriteFile(path, []byte("fake extension"), 0644); err != nil {
            t.Fatalf("failed to create test file: %v", err)
        }
    }
}
```

### Error Response Testing
```go
// Test HTTP error scenarios
func TestManifestFetchErrors(t *testing.T) {
    tests := []struct {
        name       string
        handler    http.HandlerFunc
        wantErrMsg string
    }{
        {
            name: "404 not found",
            handler: func(w http.ResponseWriter, r *http.Request) {
                w.WriteHeader(http.StatusNotFound)
            },
            wantErrMsg: "404 Not Found",
        },
        {
            name: "timeout",
            handler: func(w http.ResponseWriter, r *http.Request) {
                time.Sleep(35 * time.Second) // Exceeds default timeout
            },
            wantErrMsg: "context deadline exceeded",
        },
        {
            name: "malformed response",
            handler: func(w http.ResponseWriter, r *http.Request) {
                w.Write([]byte("not a valid manifest"))
            },
            wantErrMsg: "", // Should parse but return empty
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            server := httptest.NewServer(tt.handler)
            defer server.Close()
            
            _, err := manifest.Fetch(server.URL, false)
            if tt.wantErrMsg != "" {
                if err == nil || !strings.Contains(err.Error(), tt.wantErrMsg) {
                    t.Errorf("got error %v, want containing %q", err, tt.wantErrMsg)
                }
            }
        })
    }
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `os.MkdirTemp()` + defer cleanup | `t.TempDir()` | Go 1.15 | Auto-cleanup, unique per test |
| Third-party test runners | `go test` subtest | Go 1.7 | Better output, subtests |
| Manual loop var capture | Loop var fix | Go 1.22 | No more `tt := tt` in parallel tests |

**Deprecated/outdated:**
- `ioutil.TempDir()`: Use `t.TempDir()` instead (ioutil deprecated in Go 1.16)
- `ioutil.WriteFile()`: Use `os.WriteFile()` instead

## Open Questions

Things that couldn't be fully resolved:

1. **How deeply to mock systemd commands?**
   - What we know: Need to mock `Refresh()`, `Merge()`, `Unmerge()` to run without root
   - What's unclear: Should we inject the runner into Client, or use a package-level setter?
   - Recommendation: Inject into Client via config or use a package variable with SetRunner() for tests

2. **Whether to reorganize existing tests?**
   - What we know: Existing tests work well, follow good patterns
   - What's unclear: Should we consolidate test helpers or keep them inline?
   - Recommendation: Add helpers as needed, don't reorganize working code

3. **Integration test strategy?**
   - What we know: Unit tests are the focus of this phase
   - What's unclear: Will there be e2e tests in a future phase?
   - Recommendation: Focus on unit tests; design for future integration test expansion

## Sources

### Primary (HIGH confidence)
- Existing project tests (`internal/config/*_test.go`, `internal/sysext/manager_test.go`, etc.)
- Go stdlib `net/http/httptest` documentation
- Go stdlib `testing` package documentation
- Go Wiki TableDrivenTests (https://go.dev/wiki/TableDrivenTests)

### Secondary (MEDIUM confidence)
- Go 1.22 release notes (loop variable semantics change)
- Go 1.15 release notes (t.TempDir addition)

### Tertiary (LOW confidence)
- None

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH - Uses only Go stdlib, verified against existing project patterns
- Architecture: HIGH - Patterns match existing project code
- Pitfalls: HIGH - Based on common Go testing issues and project-specific concerns

**Research date:** 2026-01-26
**Valid until:** 2026-02-26 (Go testing patterns are stable)
