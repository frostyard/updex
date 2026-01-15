package config

import (
	"os"
	"path/filepath"
	"testing"
)

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

func TestLoadTransfersEmptyDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	transfers, err := LoadTransfers(tmpDir)
	if err != nil {
		t.Fatalf("LoadTransfers() error = %v", err)
	}

	if transfers != nil && len(transfers) != 0 {
		t.Errorf("expected nil or empty slice, got %d transfers", len(transfers))
	}
}

func TestLoadTransfersNonexistentDirectory(t *testing.T) {
	transfers, err := LoadTransfers("/nonexistent/path/that/should/not/exist")
	if err != nil {
		t.Fatalf("LoadTransfers() error = %v", err)
	}

	if transfers != nil && len(transfers) != 0 {
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
