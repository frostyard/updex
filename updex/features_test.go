package updex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostyard/updex/internal/sysext"
	"github.com/frostyard/updex/internal/testutil"
)

// createFeatureFile creates a .feature file in the config directory
func createFeatureFile(t *testing.T, configDir, featureName string, enabled bool) string {
	t.Helper()
	enabledStr := "false"
	if enabled {
		enabledStr = "true"
	}
	content := `[Feature]
Description=Test feature
Enabled=` + enabledStr + `
`
	path := filepath.Join(configDir, featureName+".feature")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create feature file: %v", err)
	}
	return path
}

// createMaskedFeatureFile creates a masked .feature file (symlink to /dev/null)
func createMaskedFeatureFile(t *testing.T, configDir, featureName string) string {
	t.Helper()
	path := filepath.Join(configDir, featureName+".feature")
	// For testing, we create a regular file instead of symlink to /dev/null
	// since the actual masking logic checks for symlink target
	content := `[Feature]
Description=Masked feature
Enabled=false
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create feature file: %v", err)
	}
	return path
}

// createFeatureTransferFile creates a .transfer file with Features set
func createFeatureTransferFile(t *testing.T, configDir, component, featureName, baseURL string) {
	t.Helper()
	content := `[Transfer]
Features=` + featureName + `

[Source]
Type=url-file
Path=` + baseURL + `
MatchPattern=` + component + `_@v.raw

[Target]
MatchPattern=` + component + `_@v.raw
CurrentSymlink=` + component + `.raw
`
	path := filepath.Join(configDir, component+".transfer")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create transfer file: %v", err)
	}
}

// hashContent returns the SHA256 hash of the given content
func hashContent(content []byte) string {
	h := sha256.Sum256(content)
	return hex.EncodeToString(h[:])
}

// TestEnableFeature_DryRun_ShowsDownloads verifies that --dry-run with --now shows what would be downloaded
func TestEnableFeature_DryRun_ShowsDownloads(t *testing.T) {
	configDir := t.TempDir()
	targetDir := t.TempDir()

	// Set up mock runner
	mockRunner := &sysext.MockRunner{}
	cleanup := sysext.SetRunner(mockRunner)
	defer cleanup()

	// Create test extension content
	extContent := []byte("fake extension content for dry run test")
	extHash := hashContent(extContent)

	// Set up HTTP server
	server := testutil.NewTestServer(t, testutil.TestServerFiles{
		Files: map[string]string{
			"testext_1.0.0.raw": extHash,
		},
		Content: map[string][]byte{
			"testext_1.0.0.raw": extContent,
		},
	})
	defer server.Close()

	// Create feature file (disabled)
	createFeatureFile(t, configDir, "testfeature", false)

	// Create transfer file
	createFeatureTransferFile(t, configDir, "testext", "testfeature", server.URL)

	// Update transfer target path
	updateTransferTargetPath(t, configDir, targetDir)

	// Act
	client := NewClient(ClientConfig{Definitions: configDir})
	result, err := client.EnableFeature(context.Background(), "testfeature", EnableFeatureOptions{
		Now:    true,
		DryRun: true,
	})

	// Assert
	if err != nil {
		t.Fatalf("EnableFeature failed: %v", err)
	}
	if !result.DryRun {
		t.Error("expected DryRun=true in result")
	}
	if !result.Success {
		t.Errorf("expected Success=true, got false. Error: %s", result.Error)
	}

	// DryRun should list what would be downloaded
	if len(result.DownloadedFiles) == 0 {
		t.Error("expected DownloadedFiles to list what would be downloaded")
	}

	// Check no files were downloaded
	extPath := filepath.Join(targetDir, "testext_1.0.0.raw")
	if _, err := os.Stat(extPath); !os.IsNotExist(err) {
		t.Error("expected extension file to NOT exist in dry-run mode")
	}
}

// TestEnableFeature_DryRun_NoNow_ShowsConfig verifies that --dry-run without --now shows config only
func TestEnableFeature_DryRun_NoNow_ShowsConfig(t *testing.T) {
	configDir := t.TempDir()
	targetDir := t.TempDir()

	// Set up mock runner
	mockRunner := &sysext.MockRunner{}
	cleanup := sysext.SetRunner(mockRunner)
	defer cleanup()

	// Create test extension content
	extContent := []byte("fake extension content")
	extHash := hashContent(extContent)

	// Set up HTTP server (shouldn't be called without --now)
	server := testutil.NewTestServer(t, testutil.TestServerFiles{
		Files: map[string]string{
			"testext_1.0.0.raw": extHash,
		},
		Content: map[string][]byte{
			"testext_1.0.0.raw": extContent,
		},
	})
	defer server.Close()

	// Create feature file
	createFeatureFile(t, configDir, "testfeature", false)

	// Create transfer file
	createFeatureTransferFile(t, configDir, "testext", "testfeature", server.URL)

	// Update transfer target path
	updateTransferTargetPath(t, configDir, targetDir)

	// Act
	client := NewClient(ClientConfig{Definitions: configDir})
	result, err := client.EnableFeature(context.Background(), "testfeature", EnableFeatureOptions{
		Now:    false, // without --now
		DryRun: true,
	})

	// Assert
	if err != nil {
		t.Fatalf("EnableFeature failed: %v", err)
	}
	if !result.DryRun {
		t.Error("expected DryRun=true in result")
	}
	if !result.Success {
		t.Errorf("expected Success=true, got false. Error: %s", result.Error)
	}

	// Without --now, no downloads should be listed
	if len(result.DownloadedFiles) > 0 {
		t.Error("expected no DownloadedFiles without --now flag")
	}

	// Check no files were downloaded
	extPath := filepath.Join(targetDir, "testext_1.0.0.raw")
	if _, err := os.Stat(extPath); !os.IsNotExist(err) {
		t.Error("expected extension file to NOT exist")
	}
}

// TestEnableFeature_FeatureNotFound verifies error when feature doesn't exist
func TestEnableFeature_FeatureNotFound(t *testing.T) {
	configDir := t.TempDir()

	// Set up mock runner
	mockRunner := &sysext.MockRunner{}
	cleanup := sysext.SetRunner(mockRunner)
	defer cleanup()

	// No features created

	// Act
	client := NewClient(ClientConfig{Definitions: configDir})
	result, err := client.EnableFeature(context.Background(), "nonexistent", EnableFeatureOptions{})

	// Assert
	if err == nil {
		t.Error("expected error for non-existent feature")
	}
	if result.Error == "" {
		t.Error("expected result.Error to be set")
	}
	if !strings.Contains(result.Error, "not found") {
		t.Errorf("expected error to contain 'not found', got: %s", result.Error)
	}
}

// TestDisableFeature_DryRun_ShowsRemovals verifies --dry-run shows what would be removed
func TestDisableFeature_DryRun_ShowsRemovals(t *testing.T) {
	configDir := t.TempDir()
	targetDir := t.TempDir()

	// Set up mock runner
	mockRunner := &sysext.MockRunner{}
	cleanup := sysext.SetRunner(mockRunner)
	defer cleanup()

	// Create feature file (enabled)
	createFeatureFile(t, configDir, "testfeature", true)

	// Create transfer file
	createFeatureTransferFile(t, configDir, "testext", "testfeature", "http://localhost")

	// Update transfer target path
	updateTransferTargetPath(t, configDir, targetDir)

	// Create extension file
	extPath := filepath.Join(targetDir, "testext_1.0.0.raw")
	if err := os.WriteFile(extPath, []byte("extension content"), 0644); err != nil {
		t.Fatalf("failed to create extension file: %v", err)
	}

	// Act
	client := NewClient(ClientConfig{Definitions: configDir})
	result, err := client.DisableFeature(context.Background(), "testfeature", DisableFeatureOptions{
		Now:    true,
		DryRun: true,
	})

	// Assert
	if err != nil {
		t.Fatalf("DisableFeature failed: %v", err)
	}
	if !result.DryRun {
		t.Error("expected DryRun=true in result")
	}
	if !result.Success {
		t.Errorf("expected Success=true, got false. Error: %s", result.Error)
	}

	// DryRun should list what would be removed
	if len(result.RemovedFiles) == 0 {
		t.Error("expected RemovedFiles to list what would be removed")
	}

	// Check extension file still exists
	if _, err := os.Stat(extPath); os.IsNotExist(err) {
		t.Error("expected extension file to still exist in dry-run mode")
	}

	// Check Unmerge was NOT called
	if mockRunner.UnmergeCalled {
		t.Error("expected Unmerge to NOT be called in dry-run mode")
	}
}

// TestDisableFeature_DryRun_NoNow_ShowsConfig verifies --dry-run without --now shows config only
func TestDisableFeature_DryRun_NoNow_ShowsConfig(t *testing.T) {
	configDir := t.TempDir()
	targetDir := t.TempDir()

	// Set up mock runner
	mockRunner := &sysext.MockRunner{}
	cleanup := sysext.SetRunner(mockRunner)
	defer cleanup()

	// Create feature file (enabled)
	createFeatureFile(t, configDir, "testfeature", true)

	// Create transfer file
	createFeatureTransferFile(t, configDir, "testext", "testfeature", "http://localhost")

	// Update transfer target path
	updateTransferTargetPath(t, configDir, targetDir)

	// Create extension file
	extPath := filepath.Join(targetDir, "testext_1.0.0.raw")
	if err := os.WriteFile(extPath, []byte("extension content"), 0644); err != nil {
		t.Fatalf("failed to create extension file: %v", err)
	}

	// Act without --now
	client := NewClient(ClientConfig{Definitions: configDir})
	result, err := client.DisableFeature(context.Background(), "testfeature", DisableFeatureOptions{
		Now:    false, // without --now
		DryRun: true,
	})

	// Assert
	if err != nil {
		t.Fatalf("DisableFeature failed: %v", err)
	}
	if !result.DryRun {
		t.Error("expected DryRun=true in result")
	}
	if !result.Success {
		t.Errorf("expected Success=true, got false. Error: %s", result.Error)
	}

	// Without --now, no files should be listed for removal
	if len(result.RemovedFiles) > 0 {
		t.Error("expected no RemovedFiles without --now flag")
	}

	// Check extension file still exists
	if _, err := os.Stat(extPath); os.IsNotExist(err) {
		t.Error("expected extension file to still exist")
	}

	// Check Unmerge was NOT called
	if mockRunner.UnmergeCalled {
		t.Error("expected Unmerge to NOT be called without --now flag")
	}
}

// TestDisableFeature_MergedExtension_RequiresForce verifies merge state check blocks removal
func TestDisableFeature_MergedExtension_RequiresForce(t *testing.T) {
	configDir := t.TempDir()
	targetDir := t.TempDir()

	// Set up mock runner
	mockRunner := &sysext.MockRunner{}
	cleanup := sysext.SetRunner(mockRunner)
	defer cleanup()

	// Create feature file (enabled)
	createFeatureFile(t, configDir, "testfeature", true)

	// Create transfer file with CurrentSymlink (indicates installable extension)
	content := `[Transfer]
Features=testfeature

[Source]
Type=url-file
Path=http://localhost
MatchPattern=testext_@v.raw

[Target]
MatchPattern=testext_@v.raw
CurrentSymlink=testext.raw
Path=` + targetDir + `
`
	transferPath := filepath.Join(configDir, "testext.transfer")
	if err := os.WriteFile(transferPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create transfer file: %v", err)
	}

	// Create extension file in target directory
	extPath := filepath.Join(targetDir, "testext_1.0.0.raw")
	if err := os.WriteFile(extPath, []byte("extension content"), 0644); err != nil {
		t.Fatalf("failed to create extension file: %v", err)
	}

	// Create symlink (this triggers GetActiveVersion to return a version)
	symlinkPath := filepath.Join(targetDir, "testext.raw")
	if err := os.Symlink("testext_1.0.0.raw", symlinkPath); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	// Act with Force=false and DryRun=false (but we'll get blocked by merge check before /etc write)
	client := NewClient(ClientConfig{Definitions: configDir})
	result, err := client.DisableFeature(context.Background(), "testfeature", DisableFeatureOptions{
		Now:   true,
		Force: false,
	})

	// Assert - Since we have a CurrentSymlink pointing to a version, GetActiveVersion returns that version
	// and the function should require --force (returning error before trying to write to /etc)
	if err == nil {
		t.Error("expected error for merged extension without --force")
	}
	if result.Error == "" {
		t.Error("expected result.Error to be set")
	}
	if !strings.Contains(result.Error, "active") && !strings.Contains(result.Error, "force") {
		t.Errorf("expected error to mention 'active' or 'force', got: %s", result.Error)
	}

	// Extension file should still exist since removal was blocked
	if _, err := os.Stat(extPath); os.IsNotExist(err) {
		t.Error("expected extension file to still exist (removal blocked)")
	}
}

// TestDisableFeature_Force_DryRun_WithMerged verifies --force with --dry-run shows what would be removed
func TestDisableFeature_Force_DryRun_WithMerged(t *testing.T) {
	configDir := t.TempDir()
	targetDir := t.TempDir()

	// Set up mock runner
	mockRunner := &sysext.MockRunner{}
	cleanup := sysext.SetRunner(mockRunner)
	defer cleanup()

	// Create feature file (enabled)
	createFeatureFile(t, configDir, "testfeature", true)

	// Create transfer file with CurrentSymlink
	content := `[Transfer]
Features=testfeature

[Source]
Type=url-file
Path=http://localhost
MatchPattern=testext_@v.raw

[Target]
MatchPattern=testext_@v.raw
CurrentSymlink=testext.raw
Path=` + targetDir + `
`
	transferPath := filepath.Join(configDir, "testext.transfer")
	if err := os.WriteFile(transferPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create transfer file: %v", err)
	}

	// Create extension file in target directory
	extPath := filepath.Join(targetDir, "testext_1.0.0.raw")
	if err := os.WriteFile(extPath, []byte("extension content"), 0644); err != nil {
		t.Fatalf("failed to create extension file: %v", err)
	}

	// Create symlink
	symlinkPath := filepath.Join(targetDir, "testext.raw")
	if err := os.Symlink("testext_1.0.0.raw", symlinkPath); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	// Act with Force=true and DryRun=true
	client := NewClient(ClientConfig{Definitions: configDir})
	result, err := client.DisableFeature(context.Background(), "testfeature", DisableFeatureOptions{
		Now:    true,
		Force:  true,
		DryRun: true,
	})

	// Assert
	if err != nil {
		t.Fatalf("DisableFeature with Force+DryRun failed: %v", err)
	}
	if !result.DryRun {
		t.Error("expected DryRun=true in result")
	}
	if !result.Success {
		t.Errorf("expected Success=true, got false. Error: %s", result.Error)
	}

	// Extension file should still exist (dry-run)
	if _, err := os.Stat(extPath); os.IsNotExist(err) {
		t.Error("expected extension file to still exist in dry-run mode")
	}

	// RemovedFiles should list what would be removed
	if len(result.RemovedFiles) == 0 {
		t.Error("expected RemovedFiles to list what would be removed")
	}
}

// TestDisableFeature_FeatureNotFound verifies error when feature doesn't exist
func TestDisableFeature_FeatureNotFound(t *testing.T) {
	configDir := t.TempDir()

	// Set up mock runner
	mockRunner := &sysext.MockRunner{}
	cleanup := sysext.SetRunner(mockRunner)
	defer cleanup()

	// No features created

	// Act
	client := NewClient(ClientConfig{Definitions: configDir})
	result, err := client.DisableFeature(context.Background(), "nonexistent", DisableFeatureOptions{})

	// Assert
	if err == nil {
		t.Error("expected error for non-existent feature")
	}
	if result.Error == "" {
		t.Error("expected result.Error to be set")
	}
	if !strings.Contains(result.Error, "not found") {
		t.Errorf("expected error to contain 'not found', got: %s", result.Error)
	}
}

// TestEnableFeature_NoTransfers verifies enable works when feature has no transfers
func TestEnableFeature_NoTransfers(t *testing.T) {
	configDir := t.TempDir()

	// Set up mock runner
	mockRunner := &sysext.MockRunner{}
	cleanup := sysext.SetRunner(mockRunner)
	defer cleanup()

	// Create feature file (disabled) with no associated transfers
	createFeatureFile(t, configDir, "testfeature", false)

	// Act (dry-run to avoid /etc access)
	client := NewClient(ClientConfig{Definitions: configDir})
	result, err := client.EnableFeature(context.Background(), "testfeature", EnableFeatureOptions{
		Now:    true,
		DryRun: true,
	})

	// Assert
	if err != nil {
		t.Fatalf("EnableFeature failed: %v", err)
	}
	if !result.Success {
		t.Errorf("expected Success=true, got false. Error: %s", result.Error)
	}
	// With no transfers, no files should be downloaded
	if len(result.DownloadedFiles) > 0 {
		t.Errorf("expected no DownloadedFiles for feature with no transfers, got: %v", result.DownloadedFiles)
	}
}

// TestDisableFeature_NoTransfers verifies disable works when feature has no transfers
func TestDisableFeature_NoTransfers(t *testing.T) {
	configDir := t.TempDir()

	// Set up mock runner
	mockRunner := &sysext.MockRunner{}
	cleanup := sysext.SetRunner(mockRunner)
	defer cleanup()

	// Create feature file (enabled) with no associated transfers
	createFeatureFile(t, configDir, "testfeature", true)

	// Act (dry-run to avoid /etc access)
	client := NewClient(ClientConfig{Definitions: configDir})
	result, err := client.DisableFeature(context.Background(), "testfeature", DisableFeatureOptions{
		Now:    true,
		DryRun: true,
	})

	// Assert
	if err != nil {
		t.Fatalf("DisableFeature failed: %v", err)
	}
	if !result.Success {
		t.Errorf("expected Success=true, got false. Error: %s", result.Error)
	}
	// With no transfers, no files should be removed
	if len(result.RemovedFiles) > 0 {
		t.Errorf("expected no RemovedFiles for feature with no transfers, got: %v", result.RemovedFiles)
	}
}

// TestFeatures_ListAllFeatures verifies Features() returns all configured features
func TestFeatures_ListAllFeatures(t *testing.T) {
	configDir := t.TempDir()

	// Set up mock runner
	mockRunner := &sysext.MockRunner{}
	cleanup := sysext.SetRunner(mockRunner)
	defer cleanup()

	// Create multiple feature files
	createFeatureFile(t, configDir, "feature1", true)
	createFeatureFile(t, configDir, "feature2", false)

	// Create transfers for features
	createFeatureTransferFile(t, configDir, "ext1", "feature1", "http://localhost")
	createFeatureTransferFile(t, configDir, "ext2", "feature2", "http://localhost")

	// Update transfer target paths (not strictly needed for listing, but good practice)
	targetDir := t.TempDir()
	updateTransferTargetPath(t, configDir, targetDir)

	// Act
	client := NewClient(ClientConfig{Definitions: configDir})
	features, err := client.Features(context.Background())

	// Assert
	if err != nil {
		t.Fatalf("Features failed: %v", err)
	}
	if len(features) != 2 {
		t.Errorf("expected 2 features, got %d", len(features))
	}

	// Check feature states
	var foundEnabled, foundDisabled bool
	for _, f := range features {
		if f.Name == "feature1" {
			if !f.Enabled {
				t.Error("expected feature1 to be enabled")
			}
			foundEnabled = true
		}
		if f.Name == "feature2" {
			if f.Enabled {
				t.Error("expected feature2 to be disabled")
			}
			foundDisabled = true
		}
	}
	if !foundEnabled || !foundDisabled {
		t.Error("expected to find both features")
	}
}
