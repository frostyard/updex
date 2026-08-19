package updex

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/frostyard/updex/config"
	"github.com/frostyard/updex/internal/testutil"
	"github.com/frostyard/updex/sysext"
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

// createFeatureTransferFile creates a .transfer file with Features set
func createFeatureTransferFile(t *testing.T, configDir, component, featureName, baseURL string) {
	t.Helper()
	content := `[Transfer]
Features=` + featureName + `
Verify=false

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

func createFeatureTransferFileWithoutCurrentSymlink(t *testing.T, configDir, component, featureName, baseURL, targetDir string) {
	t.Helper()
	content := `[Transfer]
Features=` + featureName + `
Verify=false

[Source]
Type=url-file
Path=` + baseURL + `
MatchPattern=` + component + `_@v.raw

[Target]
Path=` + targetDir + `
MatchPattern=` + component + `_@v.raw
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
	client := NewClient(ClientConfig{Definitions: configDir, SysextRunner: mockRunner})
	result, err := client.EnableFeature(t.Context(), "testfeature", EnableFeatureOptions{
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
	client := NewClient(ClientConfig{Definitions: configDir, SysextRunner: mockRunner})
	result, err := client.EnableFeature(t.Context(), "testfeature", EnableFeatureOptions{
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

	// No features created

	// Act
	client := NewClient(ClientConfig{Definitions: configDir, SysextRunner: mockRunner})
	result, err := client.EnableFeature(t.Context(), "nonexistent", EnableFeatureOptions{})

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
	client := NewClient(ClientConfig{Definitions: configDir, SysextRunner: mockRunner})
	result, err := client.DisableFeature(t.Context(), "testfeature", DisableFeatureOptions{
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
	client := NewClient(ClientConfig{Definitions: configDir, SysextRunner: mockRunner})
	result, err := client.DisableFeature(t.Context(), "testfeature", DisableFeatureOptions{
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

	// Create feature file (enabled)
	createFeatureFile(t, configDir, "testfeature", true)

	// Create transfer file with a legacy CurrentSymlink so GetActiveVersion sees it.
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
	client := NewClient(ClientConfig{Definitions: configDir, SysextRunner: mockRunner})
	result, err := client.DisableFeature(t.Context(), "testfeature", DisableFeatureOptions{
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

func TestDisableFeature_MergedImageWithoutCurrentSymlinkRequiresForce(t *testing.T) {
	configDir := t.TempDir()
	definitionRoot := t.TempDir()
	targetDir := t.TempDir()
	sysextLinkDir := t.TempDir()
	runExtensionsDir := t.TempDir()
	mockRunner := &sysext.MockRunner{}

	createFeatureFile(t, configDir, "testfeature", true)
	content := `[Transfer]
Features=testfeature

[Source]
Type=url-file
Path=http://localhost
MatchPattern=testext_@v.raw

[Target]
MatchPattern=testext_@v.raw
Path=` + targetDir + `
`
	if err := os.WriteFile(filepath.Join(configDir, "testext.transfer"), []byte(content), 0644); err != nil {
		t.Fatalf("write transfer: %v", err)
	}

	extPath := filepath.Join(targetDir, "testext_1.0.0.raw")
	if err := os.WriteFile(extPath, []byte("extension content"), 0644); err != nil {
		t.Fatalf("write installed image: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(runExtensionsDir, "testext_1.0.0.raw"),
		[]byte("merged image"),
		0644,
	); err != nil {
		t.Fatalf("write merged image: %v", err)
	}

	client := NewClient(ClientConfig{
		Definitions:  configDir,
		SysextRunner: mockRunner,
		Paths: RuntimePaths{
			DefinitionRoots:  []string{definitionRoot},
			SysextLinkDir:    sysextLinkDir,
			RunExtensionsDir: runExtensionsDir,
		},
	})

	blocked, err := client.DisableFeature(t.Context(), "testfeature", DisableFeatureOptions{
		Now: true,
	})
	if err == nil {
		t.Fatal("expected merged image removal without force to fail")
	}
	if !strings.Contains(blocked.Error, "--force") {
		t.Errorf("blocked error = %q, want --force guidance", blocked.Error)
	}
	if _, err := os.Stat(extPath); err != nil {
		t.Fatalf("installed image changed after refusal: %v", err)
	}
	if mockRunner.UnmergeCalled {
		t.Error("unmerge ran before force approval")
	}

	result, err := client.DisableFeature(t.Context(), "testfeature", DisableFeatureOptions{
		Now:   true,
		Force: true,
	})
	if err != nil {
		t.Fatalf("DisableFeature with force: %v", err)
	}
	if !result.Success {
		t.Errorf("result.Success = false, error = %q", result.Error)
	}
	if !strings.Contains(result.NextActionMessage, "Reboot required") {
		t.Errorf("NextActionMessage = %q, want reboot requirement", result.NextActionMessage)
	}
	if !mockRunner.UnmergeCalled {
		t.Error("expected forced disable to unmerge")
	}
	if _, err := os.Stat(extPath); !os.IsNotExist(err) {
		t.Errorf("installed image still exists after forced disable: %v", err)
	}
}

// TestDisableFeature_Force_DryRun_WithMerged verifies --force with --dry-run shows what would be removed
func TestDisableFeature_Force_DryRun_WithMerged(t *testing.T) {
	configDir := t.TempDir()
	targetDir := t.TempDir()

	// Set up mock runner
	mockRunner := &sysext.MockRunner{}

	// Create feature file (enabled)
	createFeatureFile(t, configDir, "testfeature", true)

	// Create transfer file with a legacy CurrentSymlink.
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
	client := NewClient(ClientConfig{Definitions: configDir, SysextRunner: mockRunner})
	result, err := client.DisableFeature(t.Context(), "testfeature", DisableFeatureOptions{
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

	// No features created

	// Act
	client := NewClient(ClientConfig{Definitions: configDir, SysextRunner: mockRunner})
	result, err := client.DisableFeature(t.Context(), "nonexistent", DisableFeatureOptions{})

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

	// Create feature file (disabled) with no associated transfers
	createFeatureFile(t, configDir, "testfeature", false)

	// Act (dry-run to avoid /etc access)
	client := NewClient(ClientConfig{Definitions: configDir, SysextRunner: mockRunner})
	result, err := client.EnableFeature(t.Context(), "testfeature", EnableFeatureOptions{
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

	// Create feature file (enabled) with no associated transfers
	createFeatureFile(t, configDir, "testfeature", true)

	// Act (dry-run to avoid /etc access)
	client := NewClient(ClientConfig{Definitions: configDir, SysextRunner: mockRunner})
	result, err := client.DisableFeature(t.Context(), "testfeature", DisableFeatureOptions{
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

	// Create multiple feature files
	createFeatureFile(t, configDir, "feature1", true)
	createFeatureFile(t, configDir, "feature2", false)

	// Create transfers for features
	createFeatureTransferFile(t, configDir, "ext1", "feature1", "http://localhost")
	createFeatureTransferFile(t, configDir, "ext2", "feature2", "http://localhost")

	// Update transfer target paths (not strictly needed for listing, but good practice)
	targetDir := t.TempDir()
	updateTransferTargetPath(t, configDir, targetDir)

	// Act: call with no options at all, exercising the backward-compatible
	// variadic signature (opts is optional).
	client := NewClient(ClientConfig{Definitions: configDir, SysextRunner: mockRunner})
	features, err := client.Features(t.Context())

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

// TestFeatures_VariadicOptions verifies the backward-compatible variadic
// signature: Features(ctx) works with zero options, Features(ctx, opts)
// works with exactly one (the pre-existing call form), and when multiple
// options are passed only the first is honored.
func TestFeatures_VariadicOptions(t *testing.T) {
	configDir := t.TempDir()
	mockRunner := &sysext.MockRunner{}

	createFeatureFile(t, configDir, "feature1", true)

	client := NewClient(ClientConfig{Definitions: configDir, SysextRunner: mockRunner})

	// Zero options: should behave like the zero value (Component == "").
	if _, err := client.Features(t.Context()); err != nil {
		t.Fatalf("Features() with no opts failed: %v", err)
	}

	// One option: the pre-existing call form must still compile and work.
	if _, err := client.Features(t.Context(), FeaturesOptions{}); err != nil {
		t.Fatalf("Features(opts) failed: %v", err)
	}

	// Multiple options: only the first is used. A non-empty Component on a
	// Definitions-scoped client is rejected by loadDomain, so passing it
	// first should surface that error; passing it second should not.
	if _, err := client.Features(t.Context(), FeaturesOptions{Component: "bogus"}, FeaturesOptions{}); err == nil {
		t.Error("expected error from first opts element with Component set on a Definitions-scoped client")
	}
	if _, err := client.Features(t.Context(), FeaturesOptions{}, FeaturesOptions{Component: "bogus"}); err != nil {
		t.Errorf("expected second opts element to be ignored, got error: %v", err)
	}
}

// TestUpdateFeatures_DownloadsForEnabledFeatures verifies UpdateFeatures downloads for enabled features
func TestUpdateFeatures_DownloadsForEnabledFeatures(t *testing.T) {
	configDir := t.TempDir()
	targetDir := t.TempDir()

	// Set up mock runner
	mockRunner := &sysext.MockRunner{}

	// Create test extension content
	extContent := []byte("fake extension content for update test")
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

	// Create enabled feature
	createFeatureFile(t, configDir, "testfeature", true)

	// Create transfer file
	createFeatureTransferFile(t, configDir, "testext", "testfeature", server.URL)

	// Update transfer target path
	updateTransferTargetPath(t, configDir, targetDir)

	// Act
	client := NewClient(ClientConfig{Definitions: configDir, SysextRunner: mockRunner})
	results, err := client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{
		NoRefresh: true,
	})

	// Assert
	if err != nil {
		t.Fatalf("UpdateFeatures failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 feature result, got %d", len(results))
	}
	if results[0].Feature != "testfeature" {
		t.Errorf("expected feature 'testfeature', got %q", results[0].Feature)
	}
	if len(results[0].Results) != 1 {
		t.Fatalf("expected 1 component result, got %d", len(results[0].Results))
	}
	r := results[0].Results[0]
	if !r.Downloaded {
		t.Error("expected Downloaded=true")
	}
	if r.Version != "1.0.0" {
		t.Errorf("expected Version=1.0.0, got %q", r.Version)
	}

	// Verify file was downloaded
	extPath := filepath.Join(targetDir, "testext_1.0.0.raw")
	if _, err := os.Stat(extPath); os.IsNotExist(err) {
		t.Error("expected extension file to exist after update")
	}
}

func TestUpdateFeatures_DownloadsWithoutCurrentSymlink(t *testing.T) {
	configDir := t.TempDir()
	targetDir := t.TempDir()
	sysextDir := t.TempDir()
	oldSysextDir := sysext.SysextDir
	sysext.SysextDir = sysextDir
	t.Cleanup(func() { sysext.SysextDir = oldSysextDir })

	extContent := []byte("fake extension content without current symlink")
	extHash := hashContent(extContent)

	server := testutil.NewTestServer(t, testutil.TestServerFiles{
		Files: map[string]string{
			"testext_1.0.0.raw": extHash,
		},
		Content: map[string][]byte{
			"testext_1.0.0.raw": extContent,
		},
	})
	defer server.Close()

	createFeatureFile(t, configDir, "testfeature", true)
	createFeatureTransferFileWithoutCurrentSymlink(t, configDir, "testext", "testfeature", server.URL, targetDir)

	client := NewClient(ClientConfig{Definitions: configDir})
	results, err := client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{
		NoRefresh: true,
	})
	if err != nil {
		t.Fatalf("UpdateFeatures failed: %v", err)
	}
	if len(results) != 1 || len(results[0].Results) != 1 {
		t.Fatalf("expected 1 feature result with 1 component, got %+v", results)
	}
	r := results[0].Results[0]
	if r.Error != "" {
		t.Fatalf("component update failed: %s", r.Error)
	}
	if !r.Downloaded {
		t.Error("expected Downloaded=true")
	}

	extPath := filepath.Join(targetDir, "testext_1.0.0.raw")
	if _, err := os.Stat(extPath); err != nil {
		t.Fatalf("expected extension file to exist after update: %v", err)
	}
	linkPath := filepath.Join(sysextDir, "testext.raw")
	target, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatalf("expected sysext link to exist: %v", err)
	}
	if target != extPath {
		t.Errorf("sysext link target = %q, want %q", target, extPath)
	}
	if _, err := os.Lstat(filepath.Join(targetDir, "testext.raw")); !os.IsNotExist(err) {
		t.Errorf("expected no staging current symlink, stat err = %v", err)
	}
}

func TestUpdateFeatures_RemovesLegacyCurrentSymlinkWhenAlreadyCurrent(t *testing.T) {
	configDir := t.TempDir()
	targetDir := t.TempDir()
	mockRunner := &sysext.MockRunner{}

	extContent := []byte("already installed extension content")
	extHash := hashContent(extContent)

	server := testutil.NewTestServer(t, testutil.TestServerFiles{
		Files: map[string]string{
			"testext_1.0.0.raw": extHash,
		},
		Content: map[string][]byte{
			"testext_1.0.0.raw": extContent,
		},
	})
	defer server.Close()

	createFeatureFile(t, configDir, "testfeature", true)
	createFeatureTransferFile(t, configDir, "testext", "testfeature", server.URL)
	updateTransferTargetPath(t, configDir, targetDir)

	extPath := filepath.Join(targetDir, "testext_1.0.0.raw")
	if err := os.WriteFile(extPath, extContent, 0644); err != nil {
		t.Fatalf("failed to create installed extension: %v", err)
	}
	legacyLink := filepath.Join(targetDir, "testext.raw")
	if err := os.Symlink("testext_1.0.0.raw", legacyLink); err != nil {
		t.Fatalf("failed to create legacy symlink: %v", err)
	}
	// The instance sysext link dir already carries a correct link, so the
	// already-current path has nothing to restore.
	sysextDir := t.TempDir()
	if err := os.Symlink(extPath, filepath.Join(sysextDir, "testext.raw")); err != nil {
		t.Fatalf("failed to create sysext link: %v", err)
	}

	client := NewClient(ClientConfig{Definitions: configDir, SysextRunner: mockRunner, Paths: RuntimePaths{SysextLinkDir: sysextDir}})
	results, err := client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{NoRefresh: true})
	if err != nil {
		t.Fatalf("UpdateFeatures failed: %v", err)
	}
	if len(results) != 1 || len(results[0].Results) != 1 {
		t.Fatalf("expected 1 feature result with 1 component, got %+v", results)
	}
	r := results[0].Results[0]
	if r.Error != "" {
		t.Fatalf("component update failed: %s", r.Error)
	}
	if r.Downloaded {
		t.Error("expected already-current component not to download")
	}
	if _, err := os.Lstat(legacyLink); !os.IsNotExist(err) {
		t.Errorf("expected legacy current symlink to be removed, stat err = %v", err)
	}
	if mockRunner.LinkToSysextCalled {
		t.Error("expected no sysext relink when no update was needed")
	}
}

func TestUpdateFeatures_LegacyCurrentSymlinkToOlderVersionStillUpdates(t *testing.T) {
	configDir := t.TempDir()
	targetDir := t.TempDir()
	mockRunner := &sysext.MockRunner{}

	extContent := []byte("new extension content")
	extHash := hashContent(extContent)

	server := testutil.NewTestServer(t, testutil.TestServerFiles{
		Files: map[string]string{
			"testext_2.0.0.raw": extHash,
		},
		Content: map[string][]byte{
			"testext_2.0.0.raw": extContent,
		},
	})
	defer server.Close()

	createFeatureFile(t, configDir, "testfeature", true)
	createFeatureTransferFile(t, configDir, "testext", "testfeature", server.URL)
	updateTransferTargetPath(t, configDir, targetDir)

	for _, filename := range []string{"testext_1.0.0.raw", "testext_2.0.0.raw"} {
		if err := os.WriteFile(filepath.Join(targetDir, filename), []byte("old content"), 0644); err != nil {
			t.Fatalf("failed to create installed extension %s: %v", filename, err)
		}
	}
	legacyLink := filepath.Join(targetDir, "testext.raw")
	if err := os.Symlink("testext_1.0.0.raw", legacyLink); err != nil {
		t.Fatalf("failed to create legacy symlink: %v", err)
	}

	client := NewClient(ClientConfig{Definitions: configDir, SysextRunner: mockRunner})
	results, err := client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{NoRefresh: true})
	if err != nil {
		t.Fatalf("UpdateFeatures failed: %v", err)
	}
	if len(results) != 1 || len(results[0].Results) != 1 {
		t.Fatalf("expected 1 feature result with 1 component, got %+v", results)
	}
	r := results[0].Results[0]
	if r.Error != "" {
		t.Fatalf("component update failed: %s", r.Error)
	}
	if !r.Downloaded {
		t.Error("expected update because legacy symlink pointed at an older version")
	}
	if !mockRunner.LinkToSysextCalled {
		t.Error("expected sysext link update")
	}
	if _, err := os.Lstat(legacyLink); !os.IsNotExist(err) {
		t.Errorf("expected legacy current symlink to be removed, stat err = %v", err)
	}
}

// TestUpdateFeatures_DryRun_NoMutations verifies dry-run reports planned work
// without downloading, cleaning legacy symlinks, linking sysexts, refreshing, or vacuuming.
func TestUpdateFeatures_DryRun_NoMutations(t *testing.T) {
	configDir := t.TempDir()
	targetDir := t.TempDir()

	mockRunner := &sysext.MockRunner{}

	extContent := []byte("fake extension content for dry-run update test")
	extHash := hashContent(extContent)

	server := testutil.NewTestServer(t, testutil.TestServerFiles{
		Files: map[string]string{
			"testext_2.0.0.raw": extHash,
		},
		Content: map[string][]byte{
			"testext_2.0.0.raw": extContent,
		},
	})
	defer server.Close()

	createFeatureFile(t, configDir, "testfeature", true)
	createFeatureTransferFile(t, configDir, "testext", "testfeature", server.URL)
	updateTransferTargetPath(t, configDir, targetDir)

	for _, filename := range []string{"testext_0.9.0.raw", "testext_1.0.0.raw"} {
		if err := os.WriteFile(filepath.Join(targetDir, filename), []byte("old extension"), 0644); err != nil {
			t.Fatalf("failed to write %s: %v", filename, err)
		}
	}

	client := NewClient(ClientConfig{Definitions: configDir, SysextRunner: mockRunner})
	results, err := client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{
		DryRun: true,
	})

	if err != nil {
		t.Fatalf("UpdateFeatures failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 feature result, got %d", len(results))
	}
	if len(results[0].Results) != 1 {
		t.Fatalf("expected 1 component result, got %d", len(results[0].Results))
	}

	r := results[0].Results[0]
	if !r.DryRun {
		t.Error("expected DryRun=true")
	}
	if !r.Downloaded {
		t.Error("expected Downloaded=true to mean would download")
	}
	if r.Installed {
		t.Error("expected Installed=false in dry-run would-install result")
	}
	if r.Version != "2.0.0" {
		t.Errorf("expected Version=2.0.0, got %q", r.Version)
	}
	if len(r.RemovedVersions) != 1 || r.RemovedVersions[0] != "0.9.0" {
		t.Errorf("expected RemovedVersions=[0.9.0], got %v", r.RemovedVersions)
	}

	if _, err := os.Stat(filepath.Join(targetDir, "testext_2.0.0.raw")); !os.IsNotExist(err) {
		t.Error("expected extension file to NOT exist in dry-run mode")
	}
	if _, err := os.Lstat(filepath.Join(targetDir, "testext.raw")); !os.IsNotExist(err) {
		t.Error("expected current symlink to NOT exist in dry-run mode")
	}
	if _, err := os.Stat(filepath.Join(targetDir, "testext_0.9.0.raw")); err != nil {
		t.Errorf("expected old extension to remain after dry-run vacuum preview: %v", err)
	}
	if mockRunner.LinkToSysextCalled {
		t.Error("expected LinkToSysext to NOT be called in dry-run mode")
	}
	if mockRunner.RefreshCalled {
		t.Error("expected Refresh to NOT be called in dry-run mode")
	}
}

// TestUpdateFeatures_SkipsDisabledFeatures verifies UpdateFeatures skips disabled features
func TestUpdateFeatures_SkipsDisabledFeatures(t *testing.T) {
	configDir := t.TempDir()
	targetDir := t.TempDir()

	// Set up mock runner
	mockRunner := &sysext.MockRunner{}

	extContent := []byte("fake extension content")
	extHash := hashContent(extContent)

	server := testutil.NewTestServer(t, testutil.TestServerFiles{
		Files: map[string]string{
			"testext_1.0.0.raw": extHash,
		},
		Content: map[string][]byte{
			"testext_1.0.0.raw": extContent,
		},
	})
	defer server.Close()

	// Create DISABLED feature
	createFeatureFile(t, configDir, "testfeature", false)
	createFeatureTransferFile(t, configDir, "testext", "testfeature", server.URL)
	updateTransferTargetPath(t, configDir, targetDir)

	// Act
	client := NewClient(ClientConfig{Definitions: configDir, SysextRunner: mockRunner})
	results, err := client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{
		NoRefresh: true,
	})

	// Assert - no results because feature is disabled
	if err != nil {
		t.Fatalf("UpdateFeatures failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 feature results for disabled feature, got %d", len(results))
	}
}

// TestCheckFeatures_FindsUpdates verifies CheckFeatures finds available updates
func TestCheckFeatures_FindsUpdates(t *testing.T) {
	configDir := t.TempDir()
	targetDir := t.TempDir()

	// Set up mock runner
	mockRunner := &sysext.MockRunner{}

	// Server has v1 and v2 available
	server := testutil.NewTestServer(t, testutil.TestServerFiles{
		Files: map[string]string{
			"testext_1.0.0.raw": "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
			"testext_2.0.0.raw": "b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3",
		},
	})
	defer server.Close()

	// Create enabled feature
	createFeatureFile(t, configDir, "testfeature", true)
	createFeatureTransferFile(t, configDir, "testext", "testfeature", server.URL)
	updateTransferTargetPath(t, configDir, targetDir)

	// Install v1 locally
	if err := os.WriteFile(filepath.Join(targetDir, "testext_1.0.0.raw"), []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Act
	client := NewClient(ClientConfig{Definitions: configDir, SysextRunner: mockRunner})
	results, err := client.CheckFeatures(t.Context(), CheckFeaturesOptions{})

	// Assert
	if err != nil {
		t.Fatalf("CheckFeatures failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 feature result, got %d", len(results))
	}
	if results[0].Feature != "testfeature" {
		t.Errorf("expected feature 'testfeature', got %q", results[0].Feature)
	}
	if len(results[0].Results) != 1 {
		t.Fatalf("expected 1 component result, got %d", len(results[0].Results))
	}
	r := results[0].Results[0]
	if !r.UpdateAvailable {
		t.Error("expected UpdateAvailable=true")
	}
	if r.CurrentVersion != "1.0.0" {
		t.Errorf("expected CurrentVersion=1.0.0, got %q", r.CurrentVersion)
	}
	if r.NewestVersion != "2.0.0" {
		t.Errorf("expected NewestVersion=2.0.0, got %q", r.NewestVersion)
	}
}

// writeCheckTransfer writes a transfer file for CheckFeatures failure-path
// tests. verify controls Verify= so a test can force the detached-signature
// fetch against a server that has no SHA256SUMS.gpg.
func writeCheckTransfer(t *testing.T, configDir, component, featureName, baseURL, targetDir string, verify bool) {
	t.Helper()
	verifyStr := "false"
	if verify {
		verifyStr = "true"
	}
	content := `[Transfer]
Features=` + featureName + `
Verify=` + verifyStr + `

[Source]
Type=url-file
Path=` + baseURL + `
MatchPattern=` + component + `_@v.raw

[Target]
Path=` + targetDir + `
MatchPattern=` + component + `_@v.raw
`
	if err := os.WriteFile(filepath.Join(configDir, component+".transfer"), []byte(content), 0644); err != nil {
		t.Fatalf("failed to create transfer file: %v", err)
	}
}

// TestCheckFeatures_ManifestFetchFailure_ReportsError verifies that a
// component whose manifest cannot be fetched is reported with Error set
// (rather than silently dropped) and that CheckFeatures returns an
// aggregate error, mirroring UpdateFeatures. A 404 is used because it is
// non-transient: a 5xx would exercise the same path after bounded retries.
func TestCheckFeatures_ManifestFetchFailure_ReportsError(t *testing.T) {
	configDir := t.TempDir()
	targetDir := t.TempDir()

	server := testutil.NewErrorServer(t, 404)
	defer server.Close()

	createFeatureFile(t, configDir, "testfeature", true)
	writeCheckTransfer(t, configDir, "testext", "testfeature", server.URL, targetDir, false)

	client := NewClient(ClientConfig{Definitions: configDir, SysextRunner: &sysext.MockRunner{}})
	results, err := client.CheckFeatures(t.Context(), CheckFeaturesOptions{})

	if err == nil {
		t.Fatal("expected CheckFeatures to return an error when a manifest fetch fails")
	}
	if !strings.Contains(err.Error(), "failed to check") {
		t.Errorf("unexpected aggregate error: %v", err)
	}
	if len(results) != 1 || len(results[0].Results) != 1 {
		t.Fatalf("expected 1 feature with 1 component result, got %+v", results)
	}
	r := results[0].Results[0]
	if r.Component != "testext" {
		t.Errorf("expected component testext, got %q", r.Component)
	}
	if r.Error == "" {
		t.Fatal("expected CheckResult.Error to be set")
	}
	if !strings.Contains(r.Error, "failed to get available versions") {
		t.Errorf("unexpected CheckResult.Error: %q", r.Error)
	}
	if r.UpdateAvailable || r.NewestVersion != "" {
		t.Errorf("failed check must not claim an update: %+v", r)
	}

	// The JSON contract: the component is present with an error field.
	data, jsonErr := json.Marshal(results)
	if jsonErr != nil {
		t.Fatalf("marshal: %v", jsonErr)
	}
	if !strings.Contains(string(data), `"error":"`) {
		t.Errorf("expected error field in JSON, got %s", data)
	}
}

// TestCheckFeatures_SignatureFailure_ReportsError verifies that a transfer
// with Verify=true whose source has no detached signature is reported as a
// check failure rather than dropped.
func TestCheckFeatures_SignatureFailure_ReportsError(t *testing.T) {
	configDir := t.TempDir()
	targetDir := t.TempDir()

	// Serves SHA256SUMS but no SHA256SUMS.gpg.
	server := testutil.NewTestServer(t, testutil.TestServerFiles{
		Files: map[string]string{
			"testext_1.0.0.raw": "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
		},
	})
	defer server.Close()

	createFeatureFile(t, configDir, "testfeature", true)
	writeCheckTransfer(t, configDir, "testext", "testfeature", server.URL, targetDir, true)

	client := NewClient(ClientConfig{Definitions: configDir, SysextRunner: &sysext.MockRunner{}})
	results, err := client.CheckFeatures(t.Context(), CheckFeaturesOptions{})

	if err == nil {
		t.Fatal("expected CheckFeatures to return an error when signature verification fails")
	}
	if len(results) != 1 || len(results[0].Results) != 1 {
		t.Fatalf("expected 1 feature with 1 component result, got %+v", results)
	}
	r := results[0].Results[0]
	if r.Error == "" || !strings.Contains(r.Error, "signature") {
		t.Errorf("expected a signature-related CheckResult.Error, got %q", r.Error)
	}
	if r.UpdateAvailable {
		t.Error("failed check must not claim an update")
	}
}

// TestCheckFeatures_PartialFailure_KeepsHealthyResults verifies that one
// failing transfer does not hide the check result of a healthy one in the
// same feature, and that the aggregate error is still returned.
func TestCheckFeatures_PartialFailure_KeepsHealthyResults(t *testing.T) {
	configDir := t.TempDir()
	targetDir := t.TempDir()

	good := testutil.NewTestServer(t, testutil.TestServerFiles{
		Files: map[string]string{
			"goodext_1.0.0.raw": "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
			"goodext_2.0.0.raw": "b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3",
		},
	})
	defer good.Close()
	bad := testutil.NewErrorServer(t, 404)
	defer bad.Close()

	createFeatureFile(t, configDir, "testfeature", true)
	writeCheckTransfer(t, configDir, "badext", "testfeature", bad.URL, targetDir, false)
	writeCheckTransfer(t, configDir, "goodext", "testfeature", good.URL, targetDir, false)
	if err := os.WriteFile(filepath.Join(targetDir, "goodext_1.0.0.raw"), []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	client := NewClient(ClientConfig{Definitions: configDir, SysextRunner: &sysext.MockRunner{}})
	results, err := client.CheckFeatures(t.Context(), CheckFeaturesOptions{})

	if err == nil {
		t.Fatal("expected an aggregate error when one component fails")
	}
	if len(results) != 1 || len(results[0].Results) != 2 {
		t.Fatalf("expected 1 feature with 2 component results, got %+v", results)
	}
	byComponent := map[string]CheckResult{}
	for _, r := range results[0].Results {
		byComponent[r.Component] = r
	}
	if r := byComponent["badext"]; r.Error == "" {
		t.Errorf("expected badext to carry an error, got %+v", r)
	}
	r, ok := byComponent["goodext"]
	if !ok {
		t.Fatal("healthy component missing from results")
	}
	if r.Error != "" {
		t.Errorf("healthy component must not carry an error, got %q", r.Error)
	}
	if !r.UpdateAvailable || r.CurrentVersion != "1.0.0" || r.NewestVersion != "2.0.0" {
		t.Errorf("unexpected healthy result: %+v", r)
	}
}

// refreshFailureFixture stages one enabled feature with one downloadable
// transfer and returns a client whose sysext runner fails on Refresh, with
// every writable path redirected into temp dirs. enabled controls the
// feature's Enabled= so the same fixture serves enable and update/disable.
func refreshFailureFixture(t *testing.T, enabled bool) (*Client, *sysext.MockRunner, string) {
	t.Helper()
	configDir := t.TempDir()
	definitionRoot := t.TempDir()
	targetDir := t.TempDir()
	sysextLinkDir := t.TempDir()
	runExtensionsDir := t.TempDir()

	extContent := []byte("fake extension content")
	server := testutil.NewTestServer(t, testutil.TestServerFiles{
		Files:   map[string]string{"testext_1.0.0.raw": hashContent(extContent)},
		Content: map[string][]byte{"testext_1.0.0.raw": extContent},
	})
	t.Cleanup(server.Close)

	createFeatureFile(t, configDir, "testfeature", enabled)
	createFeatureTransferFileWithoutCurrentSymlink(t, configDir, "testext", "testfeature", server.URL, targetDir)

	mockRunner := &sysext.MockRunner{RefreshErr: errors.New("systemd-sysext refresh: exit status 1")}
	client := NewClient(ClientConfig{
		Definitions:  configDir,
		SysextRunner: mockRunner,
		Paths: RuntimePaths{
			DefinitionRoots:  []string{definitionRoot},
			SysextLinkDir:    sysextLinkDir,
			RunExtensionsDir: runExtensionsDir,
		},
	})
	return client, mockRunner, targetDir
}

// TestUpdateFeatures_RefreshFailure_ReturnsError verifies that a successful
// install followed by a failed `systemd-sysext refresh` is reported as an
// error (activation did not happen) while the per-component results still
// record the install that did happen.
func TestUpdateFeatures_RefreshFailure_ReturnsError(t *testing.T) {
	client, mockRunner, targetDir := refreshFailureFixture(t, true)

	results, err := client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{NoVacuum: true})

	if err == nil {
		t.Fatal("expected UpdateFeatures to return an error when refresh fails")
	}
	if !strings.Contains(err.Error(), "sysext refresh failed") {
		t.Errorf("unexpected error: %v", err)
	}
	if !mockRunner.RefreshCalled {
		t.Error("expected Refresh to be called")
	}
	if len(results) != 1 || len(results[0].Results) != 1 {
		t.Fatalf("expected populated results despite refresh failure, got %+v", results)
	}
	r := results[0].Results[0]
	if !r.Installed || !r.Downloaded || r.Error != "" {
		t.Errorf("install must still be reported: %+v", r)
	}
	if _, statErr := os.Stat(filepath.Join(targetDir, "testext_1.0.0.raw")); statErr != nil {
		t.Errorf("installed image missing: %v", statErr)
	}
}

// TestUpdateFeatures_NoRefresh_IgnoresRefreshFailure pins that NoRefresh
// never calls Refresh, so a broken runner cannot fail a --no-refresh run
// (the daemon path).
func TestUpdateFeatures_NoRefresh_IgnoresRefreshFailure(t *testing.T) {
	client, mockRunner, _ := refreshFailureFixture(t, true)

	if _, err := client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{NoRefresh: true, NoVacuum: true}); err != nil {
		t.Fatalf("UpdateFeatures with NoRefresh: %v", err)
	}
	if mockRunner.RefreshCalled {
		t.Error("Refresh must not be called with NoRefresh")
	}
}

// TestEnableFeature_Now_RefreshFailure_ReportsError verifies that
// EnableFeature{Now} whose downloads succeed but whose refresh fails
// returns an error with Success=false and RefreshError set, while still
// reporting the drop-in and downloaded files.
func TestEnableFeature_Now_RefreshFailure_ReportsError(t *testing.T) {
	client, mockRunner, _ := refreshFailureFixture(t, false)

	result, err := client.EnableFeature(t.Context(), "testfeature", EnableFeatureOptions{Now: true})

	if err == nil {
		t.Fatal("expected EnableFeature to return an error when refresh fails")
	}
	if !mockRunner.RefreshCalled {
		t.Error("expected Refresh to be called")
	}
	if result.Success {
		t.Error("Success must be false when refresh failed")
	}
	if result.RefreshError == "" || !strings.Contains(result.RefreshError, "sysext refresh failed") {
		t.Errorf("RefreshError = %q", result.RefreshError)
	}
	if result.Error != result.RefreshError {
		t.Errorf("Error %q should mirror RefreshError %q", result.Error, result.RefreshError)
	}
	if result.DropIn == "" {
		t.Error("drop-in was written and must be reported")
	}
	if len(result.DownloadedFiles) != 1 {
		t.Errorf("expected 1 downloaded file, got %v", result.DownloadedFiles)
	}
	if !strings.Contains(result.NextActionMessage, "systemd-sysext refresh") {
		t.Errorf("NextActionMessage should tell the operator how to activate, got %q", result.NextActionMessage)
	}
}

// TestDisableFeature_Now_RefreshFailure_ReportsError verifies that
// DisableFeature{Now, Force} whose unmerge and removal succeed but whose
// re-merge refresh fails returns an error and says the host is left with
// its extensions unmerged.
func TestDisableFeature_Now_RefreshFailure_ReportsError(t *testing.T) {
	client, mockRunner, targetDir := refreshFailureFixture(t, true)
	extPath := filepath.Join(targetDir, "testext_1.0.0.raw")
	if err := os.WriteFile(extPath, []byte("installed"), 0644); err != nil {
		t.Fatalf("write installed image: %v", err)
	}

	result, err := client.DisableFeature(t.Context(), "testfeature", DisableFeatureOptions{Now: true, Force: true})

	if err == nil {
		t.Fatal("expected DisableFeature to return an error when refresh fails")
	}
	if !mockRunner.UnmergeCalled || !mockRunner.RefreshCalled {
		t.Errorf("expected unmerge then refresh; unmerge=%v refresh=%v", mockRunner.UnmergeCalled, mockRunner.RefreshCalled)
	}
	if result.Success {
		t.Error("Success must be false when refresh failed")
	}
	if !result.Unmerged {
		t.Error("Unmerged must still be reported: the unmerge did happen")
	}
	if len(result.RemovedFiles) != 1 {
		t.Errorf("expected 1 removed file, got %v", result.RemovedFiles)
	}
	if _, statErr := os.Stat(extPath); !os.IsNotExist(statErr) {
		t.Errorf("image should have been removed: %v", statErr)
	}
	if result.RefreshError == "" || result.Error != result.RefreshError {
		t.Errorf("RefreshError=%q Error=%q", result.RefreshError, result.Error)
	}
	if !strings.Contains(result.NextActionMessage, "unmerged") || !strings.Contains(result.NextActionMessage, "systemd-sysext refresh") {
		t.Errorf("NextActionMessage should say extensions are unmerged and how to re-merge, got %q", result.NextActionMessage)
	}
}

// TestUpdateFeatures_MinVersion_FiltersVersions verifies MinVersion is applied during UpdateFeatures
func TestUpdateFeatures_MinVersion_FiltersVersions(t *testing.T) {
	configDir := t.TempDir()
	targetDir := t.TempDir()

	mockRunner := &sysext.MockRunner{}

	// Server has v0.9.0, v1.0.0, and v2.0.0 available
	ext1Content := []byte("ext v0.9.0")
	ext2Content := []byte("ext v1.0.0")
	ext3Content := []byte("ext v2.0.0")

	server := testutil.NewTestServer(t, testutil.TestServerFiles{
		Files: map[string]string{
			"testext_0.9.0.raw": hashContent(ext1Content),
			"testext_1.0.0.raw": hashContent(ext2Content),
			"testext_2.0.0.raw": hashContent(ext3Content),
		},
		Content: map[string][]byte{
			"testext_0.9.0.raw": ext1Content,
			"testext_1.0.0.raw": ext2Content,
			"testext_2.0.0.raw": ext3Content,
		},
	})
	defer server.Close()

	// Create enabled feature with MinVersion=1.0.0 (should skip 0.9.0)
	createFeatureFile(t, configDir, "testfeature", true)
	createFeatureTransferFileWithMinVersion(t, configDir, "testext", "testfeature", server.URL, "1.0.0")
	updateTransferTargetPath(t, configDir, targetDir)

	client := NewClient(ClientConfig{Definitions: configDir, SysextRunner: mockRunner})
	results, err := client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{
		NoRefresh: true,
	})

	if err != nil {
		t.Fatalf("UpdateFeatures failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 feature result, got %d", len(results))
	}
	if len(results[0].Results) != 1 {
		t.Fatalf("expected 1 component result, got %d", len(results[0].Results))
	}

	r := results[0].Results[0]
	if !r.Downloaded {
		t.Error("expected Downloaded=true")
	}
	// Should install 2.0.0 (newest above MinVersion), not 0.9.0
	if r.Version != "2.0.0" {
		t.Errorf("expected Version=2.0.0 (MinVersion filters out 0.9.0), got %q", r.Version)
	}

	// Verify only v2.0.0 was downloaded, not v0.9.0
	if _, err := os.Stat(filepath.Join(targetDir, "testext_2.0.0.raw")); os.IsNotExist(err) {
		t.Error("expected testext_2.0.0.raw to exist")
	}
	if _, err := os.Stat(filepath.Join(targetDir, "testext_0.9.0.raw")); !os.IsNotExist(err) {
		t.Error("expected testext_0.9.0.raw to NOT exist (filtered by MinVersion)")
	}
}

// TestEnableFeature_Now_MinVersion_FiltersVersions verifies MinVersion is applied during EnableFeature --now
func TestEnableFeature_Now_MinVersion_FiltersVersions(t *testing.T) {
	configDir := t.TempDir()
	targetDir := t.TempDir()

	mockRunner := &sysext.MockRunner{}

	ext1Content := []byte("ext v0.5.0")
	ext2Content := []byte("ext v1.5.0")

	server := testutil.NewTestServer(t, testutil.TestServerFiles{
		Files: map[string]string{
			"testext_0.5.0.raw": hashContent(ext1Content),
			"testext_1.5.0.raw": hashContent(ext2Content),
		},
		Content: map[string][]byte{
			"testext_0.5.0.raw": ext1Content,
			"testext_1.5.0.raw": ext2Content,
		},
	})
	defer server.Close()

	// Create feature (disabled) with MinVersion=1.0.0
	createFeatureFile(t, configDir, "testfeature", false)
	createFeatureTransferFileWithMinVersion(t, configDir, "testext", "testfeature", server.URL, "1.0.0")
	updateTransferTargetPath(t, configDir, targetDir)

	client := NewClient(ClientConfig{Definitions: configDir, SysextRunner: mockRunner})
	result, err := client.EnableFeature(t.Context(), "testfeature", EnableFeatureOptions{
		Now:       true,
		DryRun:    true, // dry-run to avoid /etc writes
		NoRefresh: true,
	})

	if err != nil {
		t.Fatalf("EnableFeature failed: %v", err)
	}
	if !result.Success {
		t.Errorf("expected Success=true, got false. Error: %s", result.Error)
	}

	// DryRun lists what would be downloaded; with MinVersion=1.0.0, only v1.5.0 qualifies
	if len(result.DownloadedFiles) != 1 {
		t.Fatalf("expected 1 DownloadedFiles entry, got %d: %v", len(result.DownloadedFiles), result.DownloadedFiles)
	}
	if !strings.Contains(result.DownloadedFiles[0], "testext") {
		t.Errorf("expected download entry for testext, got %q", result.DownloadedFiles[0])
	}
}

// TestEnableFeature_Now_AlreadyInstalled verifies that EnableFeature --now does not misreport
// already-installed versions as newly downloaded
func TestEnableFeature_Now_AlreadyInstalled(t *testing.T) {
	configDir := t.TempDir()
	targetDir := t.TempDir()

	mockRunner := &sysext.MockRunner{}

	extContent := []byte("fake extension content for already installed test")
	extHash := hashContent(extContent)

	server := testutil.NewTestServer(t, testutil.TestServerFiles{
		Files: map[string]string{
			"testext_1.0.0.raw": extHash,
		},
		Content: map[string][]byte{
			"testext_1.0.0.raw": extContent,
		},
	})
	defer server.Close()

	createFeatureFile(t, configDir, "testfeature", false)
	createFeatureTransferFile(t, configDir, "testext", "testfeature", server.URL)
	updateTransferTargetPath(t, configDir, targetDir)

	// Pre-install v1.0.0 with current symlink so installTransfer sees it as already current
	if err := os.WriteFile(filepath.Join(targetDir, "testext_1.0.0.raw"), extContent, 0644); err != nil {
		t.Fatalf("failed to write pre-installed file: %v", err)
	}
	if err := os.Symlink("testext_1.0.0.raw", filepath.Join(targetDir, "testext.raw")); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	client := NewClient(ClientConfig{Definitions: configDir, SysextRunner: mockRunner})
	result, err := client.EnableFeature(t.Context(), "testfeature", EnableFeatureOptions{
		Now:       true,
		NoRefresh: true,
		DryRun:    true, // dry-run to avoid /etc writes
	})

	if err != nil {
		t.Fatalf("EnableFeature failed: %v", err)
	}
	if !result.Success {
		t.Errorf("expected Success=true, got false. Error: %s", result.Error)
	}

	// DryRun with --now always lists "would download" (it doesn't call installTransfer),
	// so the downloaded boolean fix only applies to non-dry-run mode.
	// This test verifies the DryRun path still works correctly.
	if len(result.DownloadedFiles) != 1 {
		t.Fatalf("expected 1 DownloadedFiles entry in dry-run mode, got %d", len(result.DownloadedFiles))
	}
	if !strings.Contains(result.DownloadedFiles[0], "would download") {
		t.Errorf("expected dry-run entry to contain 'would download', got %q", result.DownloadedFiles[0])
	}
}

// TestUpdateFeatures_AlreadyInstalled_NotReportedAsDownloaded verifies that UpdateFeatures
// does not report already-current versions as downloaded
func TestUpdateFeatures_AlreadyInstalled_NotReportedAsDownloaded(t *testing.T) {
	configDir := t.TempDir()
	targetDir := t.TempDir()

	mockRunner := &sysext.MockRunner{}

	extContent := []byte("fake extension content")
	extHash := hashContent(extContent)

	server := testutil.NewTestServer(t, testutil.TestServerFiles{
		Files: map[string]string{
			"testext_1.0.0.raw": extHash,
		},
		Content: map[string][]byte{
			"testext_1.0.0.raw": extContent,
		},
	})
	defer server.Close()

	createFeatureFile(t, configDir, "testfeature", true)
	createFeatureTransferFile(t, configDir, "testext", "testfeature", server.URL)
	updateTransferTargetPath(t, configDir, targetDir)

	// Pre-install v1.0.0 with current symlink
	if err := os.WriteFile(filepath.Join(targetDir, "testext_1.0.0.raw"), extContent, 0644); err != nil {
		t.Fatalf("failed to write pre-installed file: %v", err)
	}
	if err := os.Symlink("testext_1.0.0.raw", filepath.Join(targetDir, "testext.raw")); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	client := NewClient(ClientConfig{Definitions: configDir, SysextRunner: mockRunner})
	results, err := client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{
		NoRefresh: true,
	})

	if err != nil {
		t.Fatalf("UpdateFeatures failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 feature result, got %d", len(results))
	}
	if len(results[0].Results) != 1 {
		t.Fatalf("expected 1 component result, got %d", len(results[0].Results))
	}

	r := results[0].Results[0]
	if r.Downloaded {
		t.Error("expected Downloaded=false for already-current version")
	}
	if !r.Installed {
		t.Error("expected Installed=true even when already current")
	}
	if r.Version != "1.0.0" {
		t.Errorf("expected Version=1.0.0, got %q", r.Version)
	}
}

// TestUpdateFeatures_NoVacuum_Respected verifies NoVacuum option is passed through to installTransfer
func TestUpdateFeatures_NoVacuum_Respected(t *testing.T) {
	configDir := t.TempDir()
	targetDir := t.TempDir()

	mockRunner := &sysext.MockRunner{}

	extContent := []byte("fake extension for vacuum test")
	extHash := hashContent(extContent)

	server := testutil.NewTestServer(t, testutil.TestServerFiles{
		Files: map[string]string{
			"testext_1.0.0.raw": extHash,
		},
		Content: map[string][]byte{
			"testext_1.0.0.raw": extContent,
		},
	})
	defer server.Close()

	createFeatureFile(t, configDir, "testfeature", true)
	createFeatureTransferFile(t, configDir, "testext", "testfeature", server.URL)
	updateTransferTargetPath(t, configDir, targetDir)

	client := NewClient(ClientConfig{Definitions: configDir, SysextRunner: mockRunner})
	results, err := client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{
		NoRefresh: true,
		NoVacuum:  true,
	})

	if err != nil {
		t.Fatalf("UpdateFeatures failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 feature result, got %d", len(results))
	}

	r := results[0].Results[0]
	if !r.Downloaded {
		t.Error("expected Downloaded=true")
	}

	// Verify file was downloaded despite NoVacuum
	if _, err := os.Stat(filepath.Join(targetDir, "testext_1.0.0.raw")); os.IsNotExist(err) {
		t.Error("expected extension file to exist")
	}
}

// TestCheckFeatures_UpToDate verifies CheckFeatures reports up to date
func TestCheckFeatures_UpToDate(t *testing.T) {
	configDir := t.TempDir()
	targetDir := t.TempDir()

	// Set up mock runner
	mockRunner := &sysext.MockRunner{}

	server := testutil.NewTestServer(t, testutil.TestServerFiles{
		Files: map[string]string{
			"testext_1.0.0.raw": "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
		},
	})
	defer server.Close()

	createFeatureFile(t, configDir, "testfeature", true)
	createFeatureTransferFile(t, configDir, "testext", "testfeature", server.URL)
	updateTransferTargetPath(t, configDir, targetDir)

	// Install v1 locally (same as newest available)
	if err := os.WriteFile(filepath.Join(targetDir, "testext_1.0.0.raw"), []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	client := NewClient(ClientConfig{Definitions: configDir, SysextRunner: mockRunner})
	results, err := client.CheckFeatures(t.Context(), CheckFeaturesOptions{})

	if err != nil {
		t.Fatalf("CheckFeatures failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 feature result, got %d", len(results))
	}
	if len(results[0].Results) != 1 {
		t.Fatalf("expected 1 component result, got %d", len(results[0].Results))
	}
	if results[0].Results[0].UpdateAvailable {
		t.Error("expected UpdateAvailable=false when up to date")
	}
}

// TestCheckFeatures_EmptyResultsSerializeAsArray verifies that when there are
// no enabled features (or no qualifying results), CheckFeatures returns a
// non-nil slice that JSON-marshals to `[]` rather than `null`. Consumers such
// as pilothouse and snosi scripts parse the CLI `--json` output and previously
// broke on a top-level `null`. See docs in features.go.
func TestCheckFeatures_EmptyResultsSerializeAsArray(t *testing.T) {
	configDir := t.TempDir() // empty: no .feature/.transfer files

	client := NewClient(ClientConfig{Definitions: configDir, SysextRunner: &sysext.MockRunner{}})
	results, err := client.CheckFeatures(t.Context(), CheckFeaturesOptions{})
	if err != nil {
		t.Fatalf("CheckFeatures failed: %v", err)
	}
	if results == nil {
		t.Fatal("expected non-nil slice, got nil (would serialize as JSON null)")
	}
	b, err := json.Marshal(results)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	if string(b) != "[]" {
		t.Errorf("expected JSON [], got %s", b)
	}
}

// TestUpdateFeatures_EmptyResultsSerializeAsArray is the UpdateFeatures analog
// of TestCheckFeatures_EmptyResultsSerializeAsArray.
func TestUpdateFeatures_EmptyResultsSerializeAsArray(t *testing.T) {
	configDir := t.TempDir() // empty: no .feature/.transfer files

	client := NewClient(ClientConfig{Definitions: configDir, SysextRunner: &sysext.MockRunner{}})
	results, err := client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{NoRefresh: true})
	if err != nil {
		t.Fatalf("UpdateFeatures failed: %v", err)
	}
	if results == nil {
		t.Fatal("expected non-nil slice, got nil (would serialize as JSON null)")
	}
	b, err := json.Marshal(results)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	if string(b) != "[]" {
		t.Errorf("expected JSON [], got %s", b)
	}
}

// TestCheckFeatures_NestedResultsSerializeAsArray covers the reachable case
// where an enabled feature has a transfer but the manifest yields no matching
// versions: the inner loop `continue`s without appending, so the feature's
// nested `Results` stays empty. It must serialize as `"results":[]`, not
// `"results":null`. This protects the load-bearing make([]CheckResult, 0) in
// CheckFeatures.
func TestCheckFeatures_NestedResultsSerializeAsArray(t *testing.T) {
	configDir := t.TempDir()
	targetDir := t.TempDir()

	// Server offers no files at all -> manifest has zero available versions.
	server := testutil.NewTestServer(t, testutil.TestServerFiles{
		Files: map[string]string{},
	})
	defer server.Close()

	createFeatureFile(t, configDir, "testfeature", true)
	createFeatureTransferFile(t, configDir, "testext", "testfeature", server.URL)
	updateTransferTargetPath(t, configDir, targetDir)

	client := NewClient(ClientConfig{Definitions: configDir, SysextRunner: &sysext.MockRunner{}})
	results, err := client.CheckFeatures(t.Context(), CheckFeaturesOptions{})
	if err != nil {
		t.Fatalf("CheckFeatures failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 feature result, got %d", len(results))
	}
	if len(results[0].Results) != 0 {
		t.Fatalf("expected 0 component results, got %d", len(results[0].Results))
	}
	b, err := json.Marshal(results)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	if !strings.Contains(string(b), `"results":[]`) {
		t.Errorf("expected nested \"results\":[] in output, got %s", b)
	}
}

// dropInGuardFixture prepares a temp DefinitionRoots root holding one
// legacy-scope feature and returns the client, the loaded feature, and the
// drop-in paths EnableFeature/DisableFeature resolve for it
// (<root>/sysupdate.d/<name>.feature.d/00-updex.conf). The feature is
// loaded *before* the caller plants anything, so a direct
// writeFeatureDropIn call models the window between load and write.
func dropInGuardFixture(t *testing.T, enabled bool) (client *Client, feature *config.Feature, dropInDir, dropInFile string) {
	t.Helper()
	root := t.TempDir()
	writeComponentFeature(t, filepath.Join(root, "sysupdate.d"), "testfeature", enabled)
	client = NewClient(ClientConfig{
		Paths:        RuntimePaths{DefinitionRoots: []string{root}},
		SysextRunner: &sysext.MockRunner{},
	})
	features, _, err := client.loadDomain("")
	if err != nil {
		t.Fatalf("loadDomain: %v", err)
	}
	feature, err = lookupFeature(features, "testfeature", "tested")
	if err != nil {
		t.Fatal(err)
	}
	dropInDir = filepath.Join(root, "sysupdate.d", "testfeature.feature.d")
	dropInFile = filepath.Join(dropInDir, updexDropInName)
	return client, feature, dropInDir, dropInFile
}

// TestEnableFeature_DropInDanglingSymlinkRefused: a dangling symlink at the
// drop-in path must be refused, not followed (ADR-0005). os.Stat would call
// it absent and os.WriteFile would create the link's target as root. The
// guard fires in writeFeatureDropIn itself (checked directly, modelling a
// link planted after the feature was loaded); the full EnableFeature path
// errors as well and never creates the target.
func TestEnableFeature_DropInDanglingSymlinkRefused(t *testing.T) {
	client, feature, dropInDir, dropInFile := dropInGuardFixture(t, false)
	if err := os.MkdirAll(dropInDir, 0755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "planted-target")
	if err := os.Symlink(target, dropInFile); err != nil {
		t.Fatal(err)
	}

	if _, err := client.writeFeatureDropIn(feature, true, false); err == nil {
		t.Fatal("writeFeatureDropIn succeeded through a dangling symlink; want error")
	} else if !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("writeFeatureDropIn error = %q, want it to mention 'not a regular file'", err)
	}
	if _, statErr := os.Lstat(target); !os.IsNotExist(statErr) {
		t.Errorf("symlink target %s: Lstat err = %v, want not-exist (write followed the link)", target, statErr)
	}

	result, err := client.EnableFeature(t.Context(), "testfeature", EnableFeatureOptions{})
	if err == nil {
		t.Fatal("EnableFeature succeeded through a dangling symlink; want error")
	}
	if result == nil || result.Success {
		t.Errorf("result = %+v, want Success=false", result)
	}
	if _, statErr := os.Lstat(target); !os.IsNotExist(statErr) {
		t.Errorf("symlink target %s: Lstat err = %v, want not-exist (write followed the link)", target, statErr)
	}
	// The link itself is left for the operator, and no temp debris remains.
	assertOnlyEntries(t, dropInDir, updexDropInName)
}

// TestEnableFeature_DropInLiveSymlinkRefused: a live symlink at the drop-in
// path is refused too, and whatever it points at is left untouched. The
// loader follows the link happily, so this case reaches the guard through
// the public EnableFeature path.
func TestEnableFeature_DropInLiveSymlinkRefused(t *testing.T) {
	client, _, dropInDir, dropInFile := dropInGuardFixture(t, false)
	if err := os.MkdirAll(dropInDir, 0755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "victim.conf")
	const original = "[Feature]\nEnabled=false\n# operator-owned\n"
	if err := os.WriteFile(target, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, dropInFile); err != nil {
		t.Fatal(err)
	}

	if _, err := client.EnableFeature(t.Context(), "testfeature", EnableFeatureOptions{}); err == nil {
		t.Fatal("EnableFeature succeeded through a live symlink; want error")
	} else if !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("error = %q, want it to mention 'not a regular file'", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Errorf("symlink target was rewritten:\n%s", got)
	}
	if info, err := os.Lstat(dropInFile); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("drop-in path should still be the operator's symlink (info=%v, err=%v)", info, err)
	}
	assertOnlyEntries(t, dropInDir, updexDropInName)
}

// TestEnableFeature_DropInDirSymlinkRefused: the <feature>.feature.d
// directory itself may not be a symlink — MkdirAll would happily descend
// through it and the drop-in would land in the link's target.
func TestEnableFeature_DropInDirSymlinkRefused(t *testing.T) {
	client, _, dropInDir, _ := dropInGuardFixture(t, false)
	elsewhere := t.TempDir()
	if err := os.Symlink(elsewhere, dropInDir); err != nil {
		t.Fatal(err)
	}

	if _, err := client.EnableFeature(t.Context(), "testfeature", EnableFeatureOptions{}); err == nil {
		t.Fatal("EnableFeature succeeded through a symlinked drop-in directory; want error")
	} else if !strings.Contains(err.Error(), "is not a directory") {
		t.Errorf("error = %q, want it to mention 'is not a directory'", err)
	}
	assertOnlyEntries(t, elsewhere)
	if info, err := os.Lstat(dropInDir); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("drop-in dir should still be the planted symlink (info=%v, err=%v)", info, err)
	}
}

// TestEnableFeature_DropInDirIsFileRefused: a regular file where the
// drop-in directory should be is an operator problem, not something to
// clobber. (The loader refuses it too; the guard is what protects the
// write once a file appears after loading.)
func TestEnableFeature_DropInDirIsFileRefused(t *testing.T) {
	client, feature, dropInDir, _ := dropInGuardFixture(t, false)
	if err := os.WriteFile(dropInDir, []byte("not a dir\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := client.writeFeatureDropIn(feature, true, false); err == nil {
		t.Fatal("writeFeatureDropIn succeeded over a file at the drop-in dir path; want error")
	} else if !strings.Contains(err.Error(), "is not a directory") {
		t.Errorf("error = %q, want it to mention 'is not a directory'", err)
	}
	if _, err := client.EnableFeature(t.Context(), "testfeature", EnableFeatureOptions{}); err == nil {
		t.Fatal("EnableFeature succeeded over a file at the drop-in dir path; want error")
	}
	got, err := os.ReadFile(dropInDir)
	if err != nil || string(got) != "not a dir\n" {
		t.Errorf("file at drop-in dir path changed: %q, %v", got, err)
	}
}

// TestEnableFeature_DropInWrittenAtomically: the happy path leaves exactly
// one 0644 regular file with the expected contents and no temp debris,
// whether the directory pre-existed or not; DisableFeature then replaces
// it in place through the same guard and write.
func TestEnableFeature_DropInWrittenAtomically(t *testing.T) {
	client, _, dropInDir, dropInFile := dropInGuardFixture(t, false)

	result, err := client.EnableFeature(t.Context(), "testfeature", EnableFeatureOptions{})
	if err != nil {
		t.Fatalf("EnableFeature: %v", err)
	}
	if !result.Success || result.DropIn != dropInFile {
		t.Errorf("result = %+v, want Success=true DropIn=%s", result, dropInFile)
	}
	assertOnlyEntries(t, dropInDir, updexDropInName)
	info, err := os.Lstat(dropInFile)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0644 {
		t.Errorf("drop-in mode = %v, want regular 0644", info.Mode())
	}
	got, _ := os.ReadFile(dropInFile)
	if string(got) != "[Feature]\nEnabled=true\n" {
		t.Errorf("drop-in contents = %q", got)
	}

	if _, err := client.DisableFeature(t.Context(), "testfeature", DisableFeatureOptions{}); err != nil {
		t.Fatalf("DisableFeature: %v", err)
	}
	assertOnlyEntries(t, dropInDir, updexDropInName)
	got, _ = os.ReadFile(dropInFile)
	if string(got) != "[Feature]\nEnabled=false\n" {
		t.Errorf("drop-in contents after disable = %q", got)
	}
}

// TestDisableFeature_DropInDanglingSymlinkRefused: DisableFeature writes
// through the same helper and inherits the same guard.
func TestDisableFeature_DropInDanglingSymlinkRefused(t *testing.T) {
	client, feature, dropInDir, dropInFile := dropInGuardFixture(t, true)
	if err := os.MkdirAll(dropInDir, 0755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "planted-target")
	if err := os.Symlink(target, dropInFile); err != nil {
		t.Fatal(err)
	}

	if _, err := client.writeFeatureDropIn(feature, false, false); err == nil {
		t.Fatal("writeFeatureDropIn succeeded through a dangling symlink; want error")
	} else if !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("writeFeatureDropIn error = %q, want it to mention 'not a regular file'", err)
	}
	result, err := client.DisableFeature(t.Context(), "testfeature", DisableFeatureOptions{})
	if err == nil {
		t.Fatal("DisableFeature succeeded through a dangling symlink; want error")
	}
	if result == nil || result.Success {
		t.Errorf("result = %+v, want Success=false", result)
	}
	if _, statErr := os.Lstat(target); !os.IsNotExist(statErr) {
		t.Errorf("symlink target %s: Lstat err = %v, want not-exist", target, statErr)
	}
	assertOnlyEntries(t, dropInDir, updexDropInName)
}

// assertOnlyEntries fails unless dir contains exactly the named entries —
// in particular no leftover ".00-updex.conf.tmp-*" from writeManagedFile.
func assertOnlyEntries(t *testing.T, dir string, want ...string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", dir, err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("%s entries = %v, want %v", dir, got, want)
	}
}

// sharedSourceFixture stages one enabled feature with two transfers that
// share a single Source.Path against a server that serves SHA256SUMS (and,
// when content is given, the files) but no SHA256SUMS.gpg. Components are
// named so that "aaa" sorts (and therefore loads) before "zzz": the caller
// picks which of the two carries Verify=true so both load orders can be
// exercised. It returns the config dir and the per-component target dir.
func sharedSourceFixture(t *testing.T, server *httptest.Server, verifyAAA, verifyZZZ bool) (configDir, targetDir string) {
	t.Helper()
	configDir = t.TempDir()
	targetDir = t.TempDir()
	createFeatureFile(t, configDir, "testfeature", true)
	writeCheckTransfer(t, configDir, "aaa", "testfeature", server.URL, targetDir, verifyAAA)
	writeCheckTransfer(t, configDir, "zzz", "testfeature", server.URL, targetDir, verifyZZZ)
	return configDir, targetDir
}

// unsignedSharedServer serves a manifest listing aaa/zzz 1.0.0 (plus their
// content when withContent is true) and counts SHA256SUMS requests; it never
// serves SHA256SUMS.gpg, so any Verify=true transfer must fail verification.
func unsignedSharedServer(t *testing.T, withContent bool) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	aaaContent := []byte("aaa extension content")
	zzzContent := []byte("zzz extension content")
	files := map[string]string{
		"aaa_1.0.0.raw": hashContent(aaaContent),
		"zzz_1.0.0.raw": hashContent(zzzContent),
	}
	content := map[string][]byte{}
	if withContent {
		content["aaa_1.0.0.raw"] = aaaContent
		content["zzz_1.0.0.raw"] = zzzContent
	}
	var manifestRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		switch {
		case path == "SHA256SUMS":
			manifestRequests.Add(1)
			for name, hash := range files {
				_, _ = fmt.Fprintf(w, "%s  %s\n", hash, name)
			}
		case content[path] != nil:
			_, _ = w.Write(content[path])
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server, &manifestRequests
}

// TestCheckFeatures_SharedSource_VerifyTrueNeverUsesUnverifiedCache pins the
// invariant that manifest verification is a property of the transfer, not of
// load order: with two transfers sharing one Source.Path, the Verify=true
// transfer must fail signature verification against a source without a
// detached signature no matter whether its Verify=false sibling fetched (and
// cached) the manifest first, and the Verify=false sibling still succeeds.
func TestCheckFeatures_SharedSource_VerifyTrueNeverUsesUnverifiedCache(t *testing.T) {
	cases := []struct {
		name                 string
		verifyAAA            bool // aaa loads first
		verifyZZZ            bool // zzz loads second
		verified, unverified string
	}{
		{name: "unverified first, verified second", verifyAAA: false, verifyZZZ: true, verified: "zzz", unverified: "aaa"},
		{name: "verified first, unverified second", verifyAAA: true, verifyZZZ: false, verified: "aaa", unverified: "zzz"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server, _ := unsignedSharedServer(t, false)
			configDir, _ := sharedSourceFixture(t, server, tc.verifyAAA, tc.verifyZZZ)

			client := NewClient(ClientConfig{Definitions: configDir, SysextRunner: &sysext.MockRunner{}})
			results, err := client.CheckFeatures(t.Context(), CheckFeaturesOptions{})

			if err == nil {
				t.Fatal("expected CheckFeatures to return an error: the Verify=true transfer cannot be verified")
			}
			if len(results) != 1 || len(results[0].Results) != 2 {
				t.Fatalf("expected 1 feature with 2 component results, got %+v", results)
			}
			byComponent := map[string]CheckResult{}
			for _, r := range results[0].Results {
				byComponent[r.Component] = r
			}
			v := byComponent[tc.verified]
			if v.Error == "" || !strings.Contains(v.Error, "signature") {
				t.Errorf("%s (Verify=true) must fail with a signature error, got %+v", tc.verified, v)
			}
			if v.UpdateAvailable || v.NewestVersion != "" {
				t.Errorf("%s (Verify=true) must not report an update from an unverified manifest: %+v", tc.verified, v)
			}
			u := byComponent[tc.unverified]
			if u.Error != "" {
				t.Errorf("%s (Verify=false) must still succeed, got error %q", tc.unverified, u.Error)
			}
			if !u.UpdateAvailable || u.NewestVersion != "1.0.0" {
				t.Errorf("%s (Verify=false) must still report its version, got %+v", tc.unverified, u)
			}
		})
	}
}

// TestUpdateFeatures_SharedSource_VerifyTrueNeverUsesUnverifiedCache is the
// UpdateFeatures counterpart: the Verify=false transfer downloads its file,
// the Verify=true sibling sharing the same Source.Path fails verification and
// writes nothing to the target directory, in either load order.
func TestUpdateFeatures_SharedSource_VerifyTrueNeverUsesUnverifiedCache(t *testing.T) {
	cases := []struct {
		name                 string
		verifyAAA, verifyZZZ bool
		verified, unverified string
	}{
		{name: "unverified first, verified second", verifyAAA: false, verifyZZZ: true, verified: "zzz", unverified: "aaa"},
		{name: "verified first, unverified second", verifyAAA: true, verifyZZZ: false, verified: "aaa", unverified: "zzz"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server, _ := unsignedSharedServer(t, true)
			configDir, targetDir := sharedSourceFixture(t, server, tc.verifyAAA, tc.verifyZZZ)

			client := NewClient(ClientConfig{
				Definitions:  configDir,
				SysextRunner: &sysext.MockRunner{},
				Paths: RuntimePaths{
					DefinitionRoots:  []string{t.TempDir()},
					SysextLinkDir:    t.TempDir(),
					RunExtensionsDir: t.TempDir(),
				},
			})
			results, err := client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{NoRefresh: true})

			if err == nil {
				t.Fatal("expected UpdateFeatures to return an error: the Verify=true transfer cannot be verified")
			}
			if len(results) != 1 || len(results[0].Results) != 2 {
				t.Fatalf("expected 1 feature with 2 component results, got %+v", results)
			}
			byComponent := map[string]UpdateResult{}
			for _, r := range results[0].Results {
				byComponent[r.Component] = r
			}
			v := byComponent[tc.verified]
			if v.Error == "" || !strings.Contains(v.Error, "signature") {
				t.Errorf("%s (Verify=true) must fail with a signature error, got %+v", tc.verified, v)
			}
			if v.Downloaded || v.Installed {
				t.Errorf("%s (Verify=true) must not download or install from an unverified manifest: %+v", tc.verified, v)
			}
			if _, statErr := os.Stat(filepath.Join(targetDir, tc.verified+"_1.0.0.raw")); !os.IsNotExist(statErr) {
				t.Errorf("%s (Verify=true) must leave no file in the target dir, stat err = %v", tc.verified, statErr)
			}
			u := byComponent[tc.unverified]
			if u.Error != "" || !u.Downloaded {
				t.Errorf("%s (Verify=false) must still download, got %+v", tc.unverified, u)
			}
			if _, statErr := os.Stat(filepath.Join(targetDir, tc.unverified+"_1.0.0.raw")); statErr != nil {
				t.Errorf("%s (Verify=false) file missing from target dir: %v", tc.unverified, statErr)
			}
		})
	}
}

// TestCheckFeatures_SharedSource_FetchesManifestOnce preserves the PR #69
// optimisation: two transfers sharing one Source.Path with the same
// verification requirement still trigger exactly one SHA256SUMS request per
// CheckFeatures call — the verification guard only bypasses the cache when a
// Verify=true transfer meets an unverified entry.
func TestCheckFeatures_SharedSource_FetchesManifestOnce(t *testing.T) {
	server, manifestRequests := unsignedSharedServer(t, false)
	configDir, _ := sharedSourceFixture(t, server, false, false)

	client := NewClient(ClientConfig{Definitions: configDir, SysextRunner: &sysext.MockRunner{}})
	results, err := client.CheckFeatures(t.Context(), CheckFeaturesOptions{})
	if err != nil {
		t.Fatalf("CheckFeatures failed: %v", err)
	}
	if len(results) != 1 || len(results[0].Results) != 2 {
		t.Fatalf("expected 1 feature with 2 component results, got %+v", results)
	}
	for _, r := range results[0].Results {
		if r.Error != "" || !r.UpdateAvailable || r.NewestVersion != "1.0.0" {
			t.Errorf("unexpected result for %s: %+v", r.Component, r)
		}
	}
	if got := manifestRequests.Load(); got != 1 {
		t.Fatalf("SHA256SUMS requested %d time(s) for two transfers sharing one source, want exactly 1", got)
	}
}

// sharedTransferFixture stages one image for a transfer two features share
// (`Features=alpha beta`, both enabled), links it under an instance sysext
// link dir, and returns a client whose definition roots, link dir, and merged
// state dir all live under temp directories.
func sharedTransferFixture(t *testing.T) (client *Client, extPath, linkPath string) {
	t.Helper()
	root := t.TempDir()
	defDir := filepath.Join(root, "sysupdate.d")
	if err := os.MkdirAll(defDir, 0755); err != nil {
		t.Fatal(err)
	}
	targetDir := t.TempDir()
	linkDir := t.TempDir()
	createFeatureFile(t, defDir, "alpha", true)
	createFeatureFile(t, defDir, "beta", true)
	transfer := "[Transfer]\nFeatures=alpha beta\nVerify=false\n\n[Source]\nType=url-file\nPath=http://localhost\nMatchPattern=shared_@v.raw\n\n[Target]\nPath=" + targetDir + "\nMatchPattern=shared_@v.raw\n"
	if err := os.WriteFile(filepath.Join(defDir, "shared.transfer"), []byte(transfer), 0644); err != nil {
		t.Fatal(err)
	}
	extPath = filepath.Join(targetDir, "shared_1.0.0.raw")
	if err := os.WriteFile(extPath, []byte("shared extension content"), 0644); err != nil {
		t.Fatal(err)
	}
	linkPath = filepath.Join(linkDir, "shared.raw")
	if err := os.Symlink(extPath, linkPath); err != nil {
		t.Fatal(err)
	}
	client = NewClient(ClientConfig{
		Paths:        RuntimePaths{DefinitionRoots: []string{root}, SysextLinkDir: linkDir, RunExtensionsDir: t.TempDir()},
		SysextRunner: &sysext.MockRunner{},
	})
	return client, extPath, linkPath
}

// TestDisableFeature_Now_KeepsTransferStillActivatedByAnotherEnabledFeature:
// disabling alpha must not remove an image and link that beta, still enabled,
// activates through the same transfer.
func TestDisableFeature_Now_KeepsTransferStillActivatedByAnotherEnabledFeature(t *testing.T) {
	client, extPath, linkPath := sharedTransferFixture(t)

	result, err := client.DisableFeature(t.Context(), "alpha", DisableFeatureOptions{Now: true, NoRefresh: true})
	if err != nil {
		t.Fatalf("DisableFeature failed: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected Success=true, got error %q", result.Error)
	}
	if _, err := os.Stat(extPath); err != nil {
		t.Errorf("shared image removed although beta still activates it: %v", err)
	}
	if _, err := os.Lstat(linkPath); err != nil {
		t.Errorf("shared sysext link removed although beta still activates it: %v", err)
	}
	if len(result.RemovedFiles) != 0 {
		t.Errorf("RemovedFiles = %v, want none", result.RemovedFiles)
	}
	if !strings.Contains(result.NextActionMessage, "Kept shared") {
		t.Errorf("NextActionMessage = %q, want it to say the shared transfer was kept", result.NextActionMessage)
	}
}

// TestDisableFeature_Now_RemovesTransferWhenLastActivatorDisabled: once beta
// is disabled too, the transfer has no activator left and its image and link
// go — so the shared-transfer test above cannot pass by never removing.
func TestDisableFeature_Now_RemovesTransferWhenLastActivatorDisabled(t *testing.T) {
	client, extPath, linkPath := sharedTransferFixture(t)

	if _, err := client.DisableFeature(t.Context(), "alpha", DisableFeatureOptions{Now: true, NoRefresh: true}); err != nil {
		t.Fatalf("disable alpha: %v", err)
	}
	if _, err := os.Stat(extPath); err != nil {
		t.Fatalf("image must survive the first disable: %v", err)
	}

	result, err := client.DisableFeature(t.Context(), "beta", DisableFeatureOptions{Now: true, NoRefresh: true})
	if err != nil {
		t.Fatalf("disable beta: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected Success=true, got error %q", result.Error)
	}
	if _, err := os.Stat(extPath); !os.IsNotExist(err) {
		t.Errorf("image still present after the last activator was disabled (stat err %v)", err)
	}
	if _, err := os.Lstat(linkPath); !os.IsNotExist(err) {
		t.Errorf("sysext link still present after the last activator was disabled (lstat err %v)", err)
	}
	if len(result.RemovedFiles) == 0 {
		t.Error("RemovedFiles is empty, want the removed image")
	}
	if strings.Contains(result.NextActionMessage, "Kept") {
		t.Errorf("NextActionMessage = %q, want no retained-transfer note", result.NextActionMessage)
	}
}
