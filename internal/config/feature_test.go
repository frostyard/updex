package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFeatures(t *testing.T) {
	// Create temp directory with test feature files
	tmpDir := t.TempDir()

	// Create a valid feature file
	validFeature := `[Feature]
Description=Development Tools
Documentation=https://example.com/docs
AppStream=https://example.com/appstream.xml
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

	if len(features) != 1 {
		t.Fatalf("expected 1 feature, got %d", len(features))
	}

	f := features[0]

	// Validate feature name (derived from filename)
	if f.Name != "devel" {
		t.Errorf("Name = %q, want %q", f.Name, "devel")
	}

	// Validate fields
	if f.Description != "Development Tools" {
		t.Errorf("Description = %q, want %q", f.Description, "Development Tools")
	}
	if f.Documentation != "https://example.com/docs" {
		t.Errorf("Documentation = %q, want %q", f.Documentation, "https://example.com/docs")
	}
	if f.AppStream != "https://example.com/appstream.xml" {
		t.Errorf("AppStream = %q, want %q", f.AppStream, "https://example.com/appstream.xml")
	}
	if !f.Enabled {
		t.Errorf("Enabled = %v, want %v", f.Enabled, true)
	}
	if f.Masked {
		t.Errorf("Masked = %v, want %v", f.Masked, false)
	}
}

func TestLoadFeaturesDefaults(t *testing.T) {
	tmpDir := t.TempDir()

	// Create minimal feature file to test defaults
	minimalFeature := `[Feature]
Description=Minimal Feature
`
	if err := os.WriteFile(filepath.Join(tmpDir, "minimal.feature"), []byte(minimalFeature), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	features, err := LoadFeatures(tmpDir)
	if err != nil {
		t.Fatalf("LoadFeatures() error = %v", err)
	}

	if len(features) != 1 {
		t.Fatalf("expected 1 feature, got %d", len(features))
	}

	f := features[0]

	// Enabled should default to false
	if f.Enabled {
		t.Errorf("Enabled = %v, want %v (default)", f.Enabled, false)
	}
}

func TestLoadFeaturesDropIn(t *testing.T) {
	tmpDir := t.TempDir()

	// Create base feature file with Enabled=false
	baseFeature := `[Feature]
Description=Test Feature
Enabled=false
`
	if err := os.WriteFile(filepath.Join(tmpDir, "test.feature"), []byte(baseFeature), 0644); err != nil {
		t.Fatalf("failed to write base feature file: %v", err)
	}

	// Create drop-in directory
	dropInDir := filepath.Join(tmpDir, "test.feature.d")
	if err := os.MkdirAll(dropInDir, 0755); err != nil {
		t.Fatalf("failed to create drop-in directory: %v", err)
	}

	// Create drop-in file that enables the feature
	dropIn := `[Feature]
Enabled=true
`
	if err := os.WriteFile(filepath.Join(dropInDir, "00-enable.conf"), []byte(dropIn), 0644); err != nil {
		t.Fatalf("failed to write drop-in file: %v", err)
	}

	features, err := LoadFeatures(tmpDir)
	if err != nil {
		t.Fatalf("LoadFeatures() error = %v", err)
	}

	if len(features) != 1 {
		t.Fatalf("expected 1 feature, got %d", len(features))
	}

	f := features[0]

	// Drop-in should override Enabled to true
	if !f.Enabled {
		t.Errorf("Enabled = %v, want %v (from drop-in)", f.Enabled, true)
	}
}

func TestLoadFeaturesMasked(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a symlink to /dev/null (masked feature)
	featurePath := filepath.Join(tmpDir, "masked.feature")
	if err := os.Symlink("/dev/null", featurePath); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	features, err := LoadFeatures(tmpDir)
	if err != nil {
		t.Fatalf("LoadFeatures() error = %v", err)
	}

	if len(features) != 1 {
		t.Fatalf("expected 1 feature, got %d", len(features))
	}

	f := features[0]

	if !f.Masked {
		t.Errorf("Masked = %v, want %v", f.Masked, true)
	}
	if f.Enabled {
		t.Errorf("Enabled = %v, want %v (masked features should be disabled)", f.Enabled, false)
	}
}

func TestLoadFeaturesEmpty(t *testing.T) {
	tmpDir := t.TempDir()

	// Load from empty directory
	features, err := LoadFeatures(tmpDir)
	if err != nil {
		t.Fatalf("LoadFeatures() error = %v", err)
	}

	if features != nil && len(features) != 0 {
		t.Errorf("expected nil or empty slice, got %d features", len(features))
	}
}

func TestGetEnabledFeatureNames(t *testing.T) {
	features := []*Feature{
		{Name: "enabled1", Enabled: true, Masked: false},
		{Name: "enabled2", Enabled: true, Masked: false},
		{Name: "disabled", Enabled: false, Masked: false},
		{Name: "masked", Enabled: true, Masked: true}, // Masked should be excluded even if Enabled
	}

	enabled := GetEnabledFeatureNames(features)

	if len(enabled) != 2 {
		t.Fatalf("expected 2 enabled features, got %d", len(enabled))
	}

	// Check that enabled1 and enabled2 are in the list
	found := make(map[string]bool)
	for _, name := range enabled {
		found[name] = true
	}

	if !found["enabled1"] {
		t.Errorf("expected enabled1 in list")
	}
	if !found["enabled2"] {
		t.Errorf("expected enabled2 in list")
	}
	if found["disabled"] {
		t.Errorf("disabled should not be in list")
	}
	if found["masked"] {
		t.Errorf("masked should not be in list")
	}
}

func TestIsFeatureEnabled(t *testing.T) {
	features := []*Feature{
		{Name: "enabled", Enabled: true, Masked: false},
		{Name: "disabled", Enabled: false, Masked: false},
		{Name: "masked", Enabled: true, Masked: true},
	}

	tests := []struct {
		name     string
		expected bool
	}{
		{"enabled", true},
		{"disabled", false},
		{"masked", false}, // Masked should return false even if Enabled=true
		{"nonexistent", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsFeatureEnabled(features, tt.name)
			if got != tt.expected {
				t.Errorf("IsFeatureEnabled(%q) = %v, want %v", tt.name, got, tt.expected)
			}
		})
	}
}

func TestFilterTransfersByFeatures(t *testing.T) {
	features := []*Feature{
		{Name: "enabled", Enabled: true, Masked: false},
		{Name: "disabled", Enabled: false, Masked: false},
		{Name: "other", Enabled: true, Masked: false},
	}

	transfers := []*Transfer{
		{Component: "standalone", Transfer: TransferSection{}}, // No features
		{Component: "feat-enabled", Transfer: TransferSection{Features: []string{"enabled"}}},
		{Component: "feat-disabled", Transfer: TransferSection{Features: []string{"disabled"}}},
		{Component: "feat-or", Transfer: TransferSection{Features: []string{"disabled", "enabled"}}}, // OR: one enabled
		{Component: "req-enabled", Transfer: TransferSection{RequisiteFeatures: []string{"enabled"}}},
		{Component: "req-disabled", Transfer: TransferSection{RequisiteFeatures: []string{"disabled"}}},
		{Component: "req-and", Transfer: TransferSection{RequisiteFeatures: []string{"enabled", "other"}}},         // AND: both enabled
		{Component: "req-and-fail", Transfer: TransferSection{RequisiteFeatures: []string{"enabled", "disabled"}}}, // AND: one disabled
	}

	filtered := FilterTransfersByFeatures(transfers, features)

	// Build map of results
	included := make(map[string]bool)
	for _, t := range filtered {
		included[t.Component] = true
	}

	tests := []struct {
		component string
		expected  bool
	}{
		{"standalone", true},     // No features = always included
		{"feat-enabled", true},   // Features has enabled feature
		{"feat-disabled", false}, // Features has only disabled feature
		{"feat-or", true},        // OR logic: one enabled is enough
		{"req-enabled", true},    // RequisiteFeatures has enabled feature
		{"req-disabled", false},  // RequisiteFeatures has disabled feature
		{"req-and", true},        // AND logic: all enabled
		{"req-and-fail", false},  // AND logic: one disabled = excluded
	}

	for _, tt := range tests {
		t.Run(tt.component, func(t *testing.T) {
			if included[tt.component] != tt.expected {
				t.Errorf("FilterTransfersByFeatures: %q included = %v, want %v", tt.component, included[tt.component], tt.expected)
			}
		})
	}
}

func TestFilterTransfersByFeaturesEmpty(t *testing.T) {
	// Empty features should include all transfers
	transfers := []*Transfer{
		{Component: "a", Transfer: TransferSection{Features: []string{"any"}}},
		{Component: "b", Transfer: TransferSection{}},
	}

	filtered := FilterTransfersByFeatures(transfers, nil)

	if len(filtered) != len(transfers) {
		t.Errorf("expected all %d transfers, got %d", len(transfers), len(filtered))
	}

	filtered = FilterTransfersByFeatures(transfers, []*Feature{})

	if len(filtered) != len(transfers) {
		t.Errorf("expected all %d transfers with empty features, got %d", len(transfers), len(filtered))
	}
}

func TestGetTransfersForFeature(t *testing.T) {
	transfers := []*Transfer{
		{Component: "standalone", Transfer: TransferSection{}},
		{Component: "devel-1", Transfer: TransferSection{Features: []string{"devel"}}},
		{Component: "devel-2", Transfer: TransferSection{Features: []string{"devel", "other"}}},
		{Component: "req-devel", Transfer: TransferSection{RequisiteFeatures: []string{"devel"}}},
		{Component: "other-only", Transfer: TransferSection{Features: []string{"other"}}},
	}

	develTransfers := GetTransfersForFeature(transfers, "devel")

	if len(develTransfers) != 3 {
		t.Fatalf("expected 3 transfers for devel feature, got %d", len(develTransfers))
	}

	included := make(map[string]bool)
	for _, t := range develTransfers {
		included[t.Component] = true
	}

	if !included["devel-1"] {
		t.Errorf("expected devel-1 in list")
	}
	if !included["devel-2"] {
		t.Errorf("expected devel-2 in list")
	}
	if !included["req-devel"] {
		t.Errorf("expected req-devel in list")
	}
	if included["standalone"] {
		t.Errorf("standalone should not be in list")
	}
	if included["other-only"] {
		t.Errorf("other-only should not be in list")
	}
}

func TestTransferSectionFeaturesParsing(t *testing.T) {
	tmpDir := t.TempDir()

	// Create transfer file with Features and RequisiteFeatures
	transferContent := `[Transfer]
Features=devel extra
RequisiteFeatures=base core

[Source]
Type=url-file
Path=https://example.com
MatchPattern=test_@v.raw

[Target]
MatchPattern=test_@v.raw
`
	if err := os.WriteFile(filepath.Join(tmpDir, "test.transfer"), []byte(transferContent), 0644); err != nil {
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

	// Validate Features parsing
	if len(tr.Transfer.Features) != 2 {
		t.Errorf("Features length = %d, want 2", len(tr.Transfer.Features))
	}
	if len(tr.Transfer.Features) >= 2 {
		if tr.Transfer.Features[0] != "devel" {
			t.Errorf("Features[0] = %q, want %q", tr.Transfer.Features[0], "devel")
		}
		if tr.Transfer.Features[1] != "extra" {
			t.Errorf("Features[1] = %q, want %q", tr.Transfer.Features[1], "extra")
		}
	}

	// Validate RequisiteFeatures parsing
	if len(tr.Transfer.RequisiteFeatures) != 2 {
		t.Errorf("RequisiteFeatures length = %d, want 2", len(tr.Transfer.RequisiteFeatures))
	}
	if len(tr.Transfer.RequisiteFeatures) >= 2 {
		if tr.Transfer.RequisiteFeatures[0] != "base" {
			t.Errorf("RequisiteFeatures[0] = %q, want %q", tr.Transfer.RequisiteFeatures[0], "base")
		}
		if tr.Transfer.RequisiteFeatures[1] != "core" {
			t.Errorf("RequisiteFeatures[1] = %q, want %q", tr.Transfer.RequisiteFeatures[1], "core")
		}
	}
}

func TestLoadFeaturesDropInOrder(t *testing.T) {
	tmpDir := t.TempDir()

	// Create base feature file
	baseFeature := `[Feature]
Description=Base Description
Enabled=false
`
	if err := os.WriteFile(filepath.Join(tmpDir, "test.feature"), []byte(baseFeature), 0644); err != nil {
		t.Fatalf("failed to write base feature file: %v", err)
	}

	// Create drop-in directory
	dropInDir := filepath.Join(tmpDir, "test.feature.d")
	if err := os.MkdirAll(dropInDir, 0755); err != nil {
		t.Fatalf("failed to create drop-in directory: %v", err)
	}

	// Create multiple drop-ins (should be applied in alphabetical order)
	dropIn1 := `[Feature]
Enabled=true
Description=First Drop-in
`
	if err := os.WriteFile(filepath.Join(dropInDir, "10-first.conf"), []byte(dropIn1), 0644); err != nil {
		t.Fatalf("failed to write drop-in file: %v", err)
	}

	dropIn2 := `[Feature]
Description=Second Drop-in
`
	if err := os.WriteFile(filepath.Join(dropInDir, "20-second.conf"), []byte(dropIn2), 0644); err != nil {
		t.Fatalf("failed to write drop-in file: %v", err)
	}

	features, err := LoadFeatures(tmpDir)
	if err != nil {
		t.Fatalf("LoadFeatures() error = %v", err)
	}

	if len(features) != 1 {
		t.Fatalf("expected 1 feature, got %d", len(features))
	}

	f := features[0]

	// Enabled should be true from 10-first.conf
	if !f.Enabled {
		t.Errorf("Enabled = %v, want %v", f.Enabled, true)
	}

	// Description should be from 20-second.conf (last applied)
	if f.Description != "Second Drop-in" {
		t.Errorf("Description = %q, want %q", f.Description, "Second Drop-in")
	}
}
