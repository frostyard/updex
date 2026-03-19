package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpandSpecifiersLiteralPercent(t *testing.T) {
	// %% must always expand to a literal %, regardless of system state.
	tests := []struct {
		input string
		want  string
	}{
		{"foo-%%v-bar", "foo-%v-bar"},
		{"%%", "%"},
		{"100%%", "100%"},
		{"%%%%", "%%"},
		{"no-specifiers", "no-specifiers"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := expandSpecifiers(tt.input)
			if got != tt.want {
				t.Errorf("expandSpecifiers(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestExpandSpecifiersUnknownPassThrough(t *testing.T) {
	// Unknown specifiers must be left unchanged.
	got := expandSpecifiers("foo-%Z-bar")
	if got != "foo-%Z-bar" {
		t.Errorf("expandSpecifiers(%q) = %q, want unchanged %q", "foo-%Z-bar", got, "foo-%Z-bar")
	}
}

func TestExpandSpecifiersTemporaryDirs(t *testing.T) {
	// %T and %V are always /tmp and /var/tmp.
	if got := expandSpecifiers("%T"); got != "/tmp" {
		t.Errorf("expandSpecifiers(%%T) = %q, want /tmp", got)
	}
	if got := expandSpecifiers("%V"); got != "/var/tmp" {
		t.Errorf("expandSpecifiers(%%V) = %q, want /var/tmp", got)
	}
}

func TestGoarchToSystemdArchMap(t *testing.T) {
	// Verify the key entries in the mapping table.
	tests := []struct {
		goarch string
		want   string
	}{
		{"amd64", "x86-64"},
		{"386", "x86"},
		{"arm64", "arm64"},
		{"arm", "arm"},
		{"riscv64", "riscv64"},
		{"ppc64", "ppc64"},
		{"ppc64le", "ppc64-le"},
		{"s390x", "s390x"},
		{"loong64", "loongarch64"},
	}
	for _, tt := range tests {
		got := goarchToSystemd[tt.goarch]
		if got != tt.want {
			t.Errorf("goarchToSystemd[%q] = %q, want %q", tt.goarch, got, tt.want)
		}
	}
	// Unknown arch should map to empty string (not present in map).
	if got := goarchToSystemd["unknownarch"]; got != "" {
		t.Errorf("goarchToSystemd[unknownarch] = %q, want empty", got)
	}
}

func TestMatchPatternSpecifierExpansion(t *testing.T) {
	// Using %% (always expands to %) gives us a deterministic test that
	// verifies expandSpecifiers is actually called on MatchPattern values.
	tmpDir := t.TempDir()

	content := `[Source]
Type=url-file
Path=https://example.com
MatchPattern=myext-%%v-@v.raw

[Target]
MatchPattern=myext-%%v-@v.raw
`
	if err := os.WriteFile(filepath.Join(tmpDir, "test.transfer"), []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	transfers, err := LoadTransfers(tmpDir)
	if err != nil {
		t.Fatalf("LoadTransfers() error = %v", err)
	}
	if len(transfers) != 1 {
		t.Fatalf("expected 1 transfer, got %d", len(transfers))
	}

	// %%v in the file should have become %v (literal) in the parsed pattern.
	const want = "myext-%v-@v.raw"
	if got := transfers[0].Source.MatchPattern; got != want {
		t.Errorf("Source.MatchPattern = %q, want %q", got, want)
	}
	if got := transfers[0].Target.MatchPattern; got != want {
		t.Errorf("Target.MatchPattern = %q, want %q", got, want)
	}
}

func TestLoadTransfers(t *testing.T) {
	// Create temp directory with test transfer files
	tmpDir := t.TempDir()

	// Create a valid transfer file
	validTransfer := `[Transfer]
MinVersion=1.0.0
InstancesMax=3

[Source]
Type=url-file
Path=https://example.com/releases
MatchPattern=myext_@v.raw.xz

[Target]
Type=regular-file
Path=/var/lib/extensions
MatchPattern=myext_@v.raw
CurrentSymlink=myext.raw
Mode=0755
`
	if err := os.WriteFile(filepath.Join(tmpDir, "myext.transfer"), []byte(validTransfer), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Load transfers from temp directory
	transfers, err := LoadTransfers(tmpDir)
	if err != nil {
		t.Fatalf("LoadTransfers() error = %v", err)
	}

	if len(transfers) != 1 {
		t.Fatalf("expected 1 transfer, got %d", len(transfers))
	}

	tr := transfers[0]

	// Validate component name (derived from filename)
	if tr.Component != "myext" {
		t.Errorf("Component = %q, want %q", tr.Component, "myext")
	}

	// Validate Transfer section
	if tr.Transfer.MinVersion != "1.0.0" {
		t.Errorf("MinVersion = %q, want %q", tr.Transfer.MinVersion, "1.0.0")
	}
	if tr.Transfer.InstancesMax != 3 {
		t.Errorf("InstancesMax = %d, want %d", tr.Transfer.InstancesMax, 3)
	}

	// Validate Source section
	if tr.Source.Type != "url-file" {
		t.Errorf("Source.Type = %q, want %q", tr.Source.Type, "url-file")
	}
	if tr.Source.Path != "https://example.com/releases" {
		t.Errorf("Source.Path = %q, want %q", tr.Source.Path, "https://example.com/releases")
	}
	if tr.Source.MatchPattern != "myext_@v.raw.xz" {
		t.Errorf("Source.MatchPattern = %q, want %q", tr.Source.MatchPattern, "myext_@v.raw.xz")
	}

	// Validate Target section
	if tr.Target.Type != "regular-file" {
		t.Errorf("Target.Type = %q, want %q", tr.Target.Type, "regular-file")
	}
	if tr.Target.MatchPattern != "myext_@v.raw" {
		t.Errorf("Target.MatchPattern = %q, want %q", tr.Target.MatchPattern, "myext_@v.raw")
	}
	if tr.Target.CurrentSymlink != "myext.raw" {
		t.Errorf("Target.CurrentSymlink = %q, want %q", tr.Target.CurrentSymlink, "myext.raw")
	}
	if tr.Target.Mode != 0755 {
		t.Errorf("Target.Mode = %o, want %o", tr.Target.Mode, 0755)
	}
}

func TestLoadTransfersDefaults(t *testing.T) {
	tmpDir := t.TempDir()

	// Create minimal transfer file to test defaults
	minimalTransfer := `[Source]
Type=url-file
Path=https://example.com
MatchPattern=test_@v.raw

[Target]
MatchPattern=test_@v.raw
`
	if err := os.WriteFile(filepath.Join(tmpDir, "test.transfer"), []byte(minimalTransfer), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	transfers, err := LoadTransfers(tmpDir)
	if err != nil {
		t.Fatalf("LoadTransfers() error = %v", err)
	}

	if len(transfers) != 1 {
		t.Fatalf("expected 1 transfer, got %d", len(transfers))
	}

	tr := transfers[0]

	// Check defaults
	if tr.Transfer.InstancesMax != 2 {
		t.Errorf("default InstancesMax = %d, want 2", tr.Transfer.InstancesMax)
	}
	if tr.Transfer.Verify != false {
		t.Errorf("default Verify = %v, want false", tr.Transfer.Verify)
	}
	if tr.Target.Path != "/var/lib/extensions" {
		t.Errorf("default Target.Path = %q, want /var/lib/extensions", tr.Target.Path)
	}
	if tr.Target.Mode != 0644 {
		t.Errorf("default Target.Mode = %o, want 0644", tr.Target.Mode)
	}
}

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

func TestLoadTransfersMissingTarget(t *testing.T) {
	tmpDir := t.TempDir()

	// Transfer file missing [Target] section
	invalidTransfer := `[Source]
Type=url-file
Path=https://example.com
MatchPattern=test_@v.raw
`
	if err := os.WriteFile(filepath.Join(tmpDir, "bad.transfer"), []byte(invalidTransfer), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	_, err := LoadTransfers(tmpDir)
	if err == nil {
		t.Error("expected error for missing [Target] section, got nil")
	}
}

func TestLoadTransfersMissingRequiredFields(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name: "missing Source.Type",
			content: `[Source]
Path=https://example.com
MatchPattern=test_@v.raw

[Target]
MatchPattern=test_@v.raw
`,
		},
		{
			name: "missing Source.Path",
			content: `[Source]
Type=url-file
MatchPattern=test_@v.raw

[Target]
MatchPattern=test_@v.raw
`,
		},
		{
			name: "missing Source.MatchPattern",
			content: `[Source]
Type=url-file
Path=https://example.com

[Target]
MatchPattern=test_@v.raw
`,
		},
		{
			name: "missing Target.MatchPattern",
			content: `[Source]
Type=url-file
Path=https://example.com
MatchPattern=test_@v.raw

[Target]
Type=regular-file
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			if err := os.WriteFile(filepath.Join(tmpDir, "test.transfer"), []byte(tt.content), 0644); err != nil {
				t.Fatalf("failed to write test file: %v", err)
			}

			_, err := LoadTransfers(tmpDir)
			if err == nil {
				t.Errorf("expected error for %s, got nil", tt.name)
			}
		})
	}
}

func TestMatchPatternMultiplePatternsWithSpecifierExpansion(t *testing.T) {
	// Test MatchPattern with both frostyard and fedora-sysexts patterns
	// Verify that both patterns are parsed, stored in MatchPatterns,
	// and that specifiers are expanded correctly for all of them.
	content := `[Source]
Type=url-file
Path=https://example.com
MatchPattern=docker_@v_%a.raw docker-@v-%w-%a.raw

[Target]
Type=url-file
Path=/var/lib/sysext/docker.raw
MatchPattern=docker_@v_%a.raw docker-@v-%w-%a.raw
`

	// Write test transfer file to temp directory
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "docker.transfer"), []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Load transfers from temp directory
	transfers, err := LoadTransfers(tmpDir)
	if err != nil {
		t.Fatalf("LoadTransfers failed: %v", err)
	}

	if len(transfers) != 1 {
		t.Fatalf("Expected 1 transfer, got %d", len(transfers))
	}

	tr := transfers[0]

	// Verify MatchPatterns contains both patterns (with specifiers expanded)
	if len(tr.Source.MatchPatterns) != 2 {
		t.Errorf("Source.MatchPatterns has %d patterns, want 2", len(tr.Source.MatchPatterns))
	}
	if len(tr.Target.MatchPatterns) != 2 {
		t.Errorf("Target.MatchPatterns has %d patterns, want 2", len(tr.Target.MatchPatterns))
	}

	// First pattern should be frostyard with %a expanded (but not %w since it's not in the pattern)
	// e.g., "docker_@v_x86-64.raw"
	// Second pattern should be fedora-sysexts with both %w and %a expanded
	// e.g., "docker-@v-43-x86-64.raw" (where 43 is the OS version)

	// Verify first pattern (frostyard) has @v placeholder but %a is expanded
	if !strings.Contains(tr.Source.MatchPatterns[0], "docker_@v_") {
		t.Errorf("Source.MatchPatterns[0] should contain 'docker_@v_', got %q", tr.Source.MatchPatterns[0])
	}
	if strings.Contains(tr.Source.MatchPatterns[0], "%a") {
		t.Errorf("Source.MatchPatterns[0] should have %%a expanded, got %q", tr.Source.MatchPatterns[0])
	}
	if strings.Contains(tr.Source.MatchPatterns[0], ".raw") {
		// Good, has expected suffix
	}

	// Verify second pattern (fedora-sysexts) has @v placeholder but %w and %a are expanded
	if !strings.Contains(tr.Source.MatchPatterns[1], "docker-@v-") {
		t.Errorf("Source.MatchPatterns[1] should contain 'docker-@v-', got %q", tr.Source.MatchPatterns[1])
	}
	if strings.Contains(tr.Source.MatchPatterns[1], "%a") {
		t.Errorf("Source.MatchPatterns[1] should have %%a expanded, got %q", tr.Source.MatchPatterns[1])
	}
	if strings.Contains(tr.Source.MatchPatterns[1], "%w") {
		t.Errorf("Source.MatchPatterns[1] should have %%w expanded, got %q", tr.Source.MatchPatterns[1])
	}

	// Verify MatchPattern (first pattern is stored there)
	if tr.Source.MatchPattern != tr.Source.MatchPatterns[0] {
		t.Errorf("Source.MatchPattern should equal first MatchPatterns entry: %q != %q",
			tr.Source.MatchPattern, tr.Source.MatchPatterns[0])
	}

	// Same checks for Target
	if tr.Target.MatchPattern != tr.Target.MatchPatterns[0] {
		t.Errorf("Target.MatchPattern should equal first MatchPatterns entry: %q != %q",
			tr.Target.MatchPattern, tr.Target.MatchPatterns[0])
	}
}

func TestLoadTransfersEmptyDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	transfers, err := LoadTransfers(tmpDir)
	if err != nil {
		t.Fatalf("LoadTransfers() error = %v", err)
	}

	if len(transfers) != 0 {
		t.Errorf("expected nil or empty slice, got %d transfers", len(transfers))
	}
}

func TestLoadTransfersNonexistentDirectory(t *testing.T) {
	transfers, err := LoadTransfers("/nonexistent/path/that/should/not/exist")
	if err != nil {
		t.Fatalf("LoadTransfers() error = %v", err)
	}

	if len(transfers) != 0 {
		t.Errorf("expected nil or empty slice for nonexistent path")
	}
}

func TestLoadTransfersMultipleFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create multiple transfer files
	for _, name := range []string{"alpha", "beta", "gamma"} {
		content := `[Source]
Type=url-file
Path=https://example.com/` + name + `
MatchPattern=` + name + `_@v.raw

[Target]
MatchPattern=` + name + `_@v.raw
`
		if err := os.WriteFile(filepath.Join(tmpDir, name+".transfer"), []byte(content), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}
	}

	transfers, err := LoadTransfers(tmpDir)
	if err != nil {
		t.Fatalf("LoadTransfers() error = %v", err)
	}

	if len(transfers) != 3 {
		t.Fatalf("expected 3 transfers, got %d", len(transfers))
	}

	// Verify sorted order
	expectedOrder := []string{"alpha", "beta", "gamma"}
	for i, expected := range expectedOrder {
		if transfers[i].Component != expected {
			t.Errorf("transfers[%d].Component = %q, want %q", i, transfers[i].Component, expected)
		}
	}
}

func TestLoadTransfersTrailingSlashRemoved(t *testing.T) {
	tmpDir := t.TempDir()

	// Source.Path with trailing slash
	content := `[Source]
Type=url-file
Path=https://example.com/releases/
MatchPattern=test_@v.raw

[Target]
MatchPattern=test_@v.raw
`
	if err := os.WriteFile(filepath.Join(tmpDir, "test.transfer"), []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	transfers, err := LoadTransfers(tmpDir)
	if err != nil {
		t.Fatalf("LoadTransfers() error = %v", err)
	}

	if len(transfers) != 1 {
		t.Fatalf("expected 1 transfer, got %d", len(transfers))
	}

	// Trailing slash should be removed
	if transfers[0].Source.Path != "https://example.com/releases" {
		t.Errorf("Source.Path = %q, want without trailing slash", transfers[0].Source.Path)
	}
}

func TestLoadTransfersVerifyFlag(t *testing.T) {
	tmpDir := t.TempDir()

	content := `[Transfer]
Verify=true

[Source]
Type=url-file
Path=https://example.com
MatchPattern=test_@v.raw

[Target]
MatchPattern=test_@v.raw
`
	if err := os.WriteFile(filepath.Join(tmpDir, "test.transfer"), []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	transfers, err := LoadTransfers(tmpDir)
	if err != nil {
		t.Fatalf("LoadTransfers() error = %v", err)
	}

	if !transfers[0].Transfer.Verify {
		t.Error("Verify = false, want true")
	}
}

func TestLoadTransfersReadOnlyFlag(t *testing.T) {
	tmpDir := t.TempDir()

	content := `[Source]
Type=url-file
Path=https://example.com
MatchPattern=test_@v.raw

[Target]
MatchPattern=test_@v.raw
ReadOnly=true
`
	if err := os.WriteFile(filepath.Join(tmpDir, "test.transfer"), []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	transfers, err := LoadTransfers(tmpDir)
	if err != nil {
		t.Fatalf("LoadTransfers() error = %v", err)
	}

	if !transfers[0].Target.ReadOnly {
		t.Error("ReadOnly = false, want true")
	}
}

func TestLoadTransfers_FrostyardPattern(t *testing.T) {
	// Test loading transfer file with frostyard pattern
	// Frostyard pattern includes OS version (%w) and architecture (%a)
	// Example: docker_@v_%w_%a.raw matches files like docker_1.0.0_39_x86-64.raw
	//
	// The specifiers %w and %a are expanded during parsing:
	// - %w expands to VERSION_ID from /etc/os-release
	// - %a expands to the system architecture (e.g., x86-64, arm64)
	// - The @v placeholder remains for later version matching
	content := `[Transfer]
Protect=no

[Source]
Type=url-file
Path=https://example.com/docker/
MatchPattern=docker_@v_%w_%a.raw

[Target]
Type=url-file
Path=/var/lib/sysext/docker.raw
MatchPattern=docker_@v_%w_%a.raw
`

	// Write test transfer file
	tmpdir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpdir, "docker.transfer"), []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Load transfer file
	transfers, err := LoadTransfers(tmpdir)
	if err != nil {
		t.Fatalf("LoadTransfers failed: %v", err)
	}

	if len(transfers) != 1 {
		t.Fatalf("Expected 1 transfer, got %d", len(transfers))
	}

	tr := transfers[0]

	// Verify MatchPattern contains @v placeholder and has %w, %a expanded
	// Pattern should look like: docker_@v_<osversion>_<arch>.raw
	// Example: docker_@v_43_x86-64.raw on Fedora 43 x86-64
	if !strings.Contains(tr.Source.MatchPattern, "docker_@v_") {
		t.Errorf("Source.MatchPattern = %q, should contain 'docker_@v_'", tr.Source.MatchPattern)
	}
	if strings.Contains(tr.Source.MatchPattern, "%w") {
		t.Errorf("Source.MatchPattern = %q, should have %%w expanded", tr.Source.MatchPattern)
	}
	if strings.Contains(tr.Source.MatchPattern, "%a") {
		t.Errorf("Source.MatchPattern = %q, should have %%a expanded", tr.Source.MatchPattern)
	}
	if !strings.HasSuffix(tr.Source.MatchPattern, ".raw") {
		t.Errorf("Source.MatchPattern = %q, should end with .raw", tr.Source.MatchPattern)
	}

	// Same checks for Target
	if !strings.Contains(tr.Target.MatchPattern, "docker_@v_") {
		t.Errorf("Target.MatchPattern = %q, should contain 'docker_@v_'", tr.Target.MatchPattern)
	}
	if strings.Contains(tr.Target.MatchPattern, "%w") {
		t.Errorf("Target.MatchPattern = %q, should have %%w expanded", tr.Target.MatchPattern)
	}
	if strings.Contains(tr.Target.MatchPattern, "%a") {
		t.Errorf("Target.MatchPattern = %q, should have %%a expanded", tr.Target.MatchPattern)
	}
	if !strings.HasSuffix(tr.Target.MatchPattern, ".raw") {
		t.Errorf("Target.MatchPattern = %q, should end with .raw", tr.Target.MatchPattern)
	}

	// Verify MatchPatterns array contains exactly one pattern
	if len(tr.Source.MatchPatterns) != 1 {
		t.Errorf("Source.MatchPatterns has %d patterns, want 1", len(tr.Source.MatchPatterns))
	}
	if len(tr.Target.MatchPatterns) != 1 {
		t.Errorf("Target.MatchPatterns has %d patterns, want 1", len(tr.Target.MatchPatterns))
	}

	// Verify MatchPattern equals first MatchPatterns entry
	if tr.Source.MatchPattern != tr.Source.MatchPatterns[0] {
		t.Errorf("Source.MatchPattern should equal first MatchPatterns entry: %q != %q",
			tr.Source.MatchPattern, tr.Source.MatchPatterns[0])
	}
	if tr.Target.MatchPattern != tr.Target.MatchPatterns[0] {
		t.Errorf("Target.MatchPattern should equal first MatchPatterns entry: %q != %q",
			tr.Target.MatchPattern, tr.Target.MatchPatterns[0])
	}
}
