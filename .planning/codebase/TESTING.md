# Testing Patterns

**Analysis Date:** 2026-01-26

## Test Framework

**Runner:**
- Go standard `testing` package
- No config file (uses Go defaults)

**Assertion Library:**
- Standard library only (`testing.T.Errorf`, `testing.T.Fatalf`)
- No external assertion libraries

**Run Commands:**
```bash
make test              # Run all tests (go test -v ./...)
make test-cover        # Run with coverage (outputs coverage.html)
go test ./...          # Direct invocation
go test -v ./...       # Verbose mode
go test -cover ./...   # Quick coverage summary
```

## Test File Organization

**Location:**
- Co-located with source files (same package directory)
- Test files named `*_test.go`

**Naming:**
- `common_test.go` tests `common.go`
- `pattern_test.go` tests `pattern.go`
- `transfer_test.go` tests `transfer.go`

**Structure:**
```
internal/
├── config/
│   ├── feature.go
│   ├── feature_test.go        # 452 lines
│   ├── transfer.go
│   └── transfer_test.go       # 370 lines
├── download/
│   ├── decompress.go
│   ├── decompress_test.go     # 281 lines
│   └── download.go
├── manifest/
│   ├── gpg.go
│   ├── manifest.go
│   └── manifest_test.go       # 240 lines
├── sysext/
│   ├── manager.go
│   └── manager_test.go        # 408 lines
└── version/
    ├── pattern.go
    └── pattern_test.go        # 378 lines
cmd/
└── common/
    ├── common.go
    └── common_test.go         # 27 lines
```

## Test Structure

**Suite Organization:**
```go
func TestFunctionName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  error
	}{
		{
			name:     "descriptive test case name",
			input:    "value",
			expected: "result",
			wantErr:  nil,
		},
		// ... more cases
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// test implementation
		})
	}
}
```

**Patterns from `internal/version/pattern_test.go`:**
```go
func TestParsePattern(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		wantErr error
	}{
		{
			name:    "simple version pattern",
			pattern: "myext_@v.raw",
			wantErr: nil,
		},
		{
			name:    "empty pattern",
			pattern: "",
			wantErr: ErrEmptyPattern,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParsePattern(tt.pattern)
			if err != tt.wantErr {
				t.Errorf("ParsePattern() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
```

**Setup/Teardown:**
- Use `t.TempDir()` for temporary directories (auto-cleaned)
- Use `defer` for cleanup of created resources
- No global setup/teardown functions

## Mocking

**Framework:** No mocking framework used

**Patterns:**
- File system tests use temporary directories: `t.TempDir()`
- Write test files directly: `os.WriteFile(filepath.Join(tmpDir, "test.transfer"), []byte(content), 0644)`
- No interface-based mocking for external services

**Filesystem mocking example from `internal/config/feature_test.go`:**
```go
func TestLoadFeatures(t *testing.T) {
	// Create temp directory with test feature files
	tmpDir := t.TempDir()

	// Create a valid feature file
	validFeature := `[Feature]
Description=Development Tools
Documentation=https://example.com/docs
Enabled=true
`
	if err := os.WriteFile(filepath.Join(tmpDir, "devel.feature"), []byte(validFeature), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Load features from temp directory
	features, err := LoadFeatures(tmpDir)
	if err != nil {
		t.Fatalf("LoadFeatures() error = %v", err)
	}
	// ... assertions
}
```

**What to Mock:**
- Filesystem operations (via temp directories)
- Configuration files (written as test fixtures)

**What NOT to Mock:**
- No HTTP mocking in current tests
- No external service mocking
- Compression libraries tested with real data

## Fixtures and Factories

**Test Data:**
- Inline string literals for config file content
- Computed values for hashes (generated at runtime)

**Pattern from `internal/manifest/manifest_test.go`:**
```go
func TestParseManifest(t *testing.T) {
	// SHA256 hashes are exactly 64 hex characters
	hash1 := "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"
	hash2 := "b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3"

	tests := []struct {
		name     string
		content  string
		expected map[string]string
	}{
		{
			name:    "standard format",
			content: hash1 + "  file1.raw\n" + hash2 + "  file2.raw.xz",
			expected: map[string]string{
				"file1.raw":    hash1,
				"file2.raw.xz": hash2,
			},
		},
	}
	// ...
}
```

**Dynamic hash computation:**
```go
func TestVerifyHash(t *testing.T) {
	content := []byte("hello world\n")
	// Compute expected hash at runtime
	h := sha256.New()
	h.Write(content)
	expectedHash := fmt.Sprintf("%x", h.Sum(nil))
	// ...
}
```

**Location:**
- No separate fixtures directory
- Test data created inline in test functions

## Coverage

**Requirements:** No enforced minimum (advisory only)

**Current coverage by package:**
| Package | Coverage |
|---------|----------|
| `internal/config` | 80.8% |
| `internal/version` | 75.0% |
| `internal/sysext` | 42.4% |
| `internal/manifest` | 42.4% |
| `internal/download` | 40.5% |
| `cmd/common` | 5.7% |
| `cmd/commands` | 0.0% |
| `cmd/updex` | 0.0% |
| `updex` | 0.0% |

**View Coverage:**
```bash
make test-cover      # Generates coverage.html
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
```

## Test Types

**Unit Tests:**
- Focus on individual functions and packages
- Located in `internal/` packages
- High coverage for core logic (`config`, `version`)

**Integration Tests:**
- Not present (no test tags or separate test directories)

**E2E Tests:**
- Not present

## Common Patterns

**Async Testing:**
- Not applicable (no concurrent test patterns observed)

**Error Testing:**
```go
func TestLoadTransfersMissingSource(t *testing.T) {
	tmpDir := t.TempDir()

	// Transfer file missing [Source] section
	invalidTransfer := `[Target]
MatchPattern=test_@v.raw
`
	if err := os.WriteFile(filepath.Join(tmpDir, "bad.transfer"), []byte(invalidTransfer), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	_, err := LoadTransfers(tmpDir)
	if err == nil {
		t.Error("expected error for missing [Source] section, got nil")
	}
}
```

**Subtests with t.Run:**
```go
for _, tt := range tests {
	t.Run(tt.name, func(t *testing.T) {
		got := FunctionUnderTest(tt.input)
		if got != tt.expected {
			t.Errorf("FunctionUnderTest() = %v, want %v", got, tt.expected)
		}
	})
}
```

**Fatal vs Error:**
- Use `t.Fatalf` for setup failures that prevent further testing
- Use `t.Errorf` for assertion failures that don't block other checks

```go
// Fatal for setup issues
if err := os.WriteFile(...); err != nil {
	t.Fatalf("failed to write test file: %v", err)
}

// Error for assertion failures
if got != want {
	t.Errorf("Function() = %v, want %v", got, want)
}
```

## Test Gaps

**Untested Packages:**
- `cmd/commands/` - CLI command implementations (0% coverage)
- `cmd/updex/` - Root command setup (0% coverage)
- `updex/` - Public API client methods (0% coverage)

**Risk Areas:**
- Download functionality with real HTTP (only compression tested)
- GPG verification (`internal/manifest/gpg.go`)
- Public API (`updex/` package)

---

*Testing analysis: 2026-01-26*
