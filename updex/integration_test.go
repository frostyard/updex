package updex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/frostyard/updex/internal/sysext"
	"github.com/frostyard/updex/internal/testutil"
)

// IntegrationTestEnv holds a complete test environment for integration tests.
// It encapsulates all the setup needed for end-to-end workflow tests.
type IntegrationTestEnv struct {
	ConfigDir  string
	TargetDir  string
	Server     *httptest.Server
	Client     *Client
	MockRunner *sysext.MockRunner
	cleanup    func()
}

// NewIntegrationTestEnv creates a complete test environment with:
// - Temp directories for config and target
// - Mock runner for systemd-sysext operations
// - HTTP test server with provided files
// - Client configured to use temp directories
// Cleanup is registered automatically with t.Cleanup.
func NewIntegrationTestEnv(t *testing.T, files testutil.TestServerFiles) *IntegrationTestEnv {
	t.Helper()

	configDir := t.TempDir()
	targetDir := t.TempDir()

	mockRunner := &sysext.MockRunner{}
	runnerCleanup := sysext.SetRunner(mockRunner)

	server := testutil.NewTestServer(t, files)

	env := &IntegrationTestEnv{
		ConfigDir:  configDir,
		TargetDir:  targetDir,
		Server:     server,
		MockRunner: mockRunner,
		cleanup: func() {
			server.Close()
			runnerCleanup()
		},
	}

	// Create client configured to use temp directories
	env.Client = NewClient(ClientConfig{Definitions: configDir})

	t.Cleanup(env.cleanup)
	return env
}

// AddComponent sets up a component with transfer config pointing to test server.
// It creates the .transfer file and updates the target path.
func (e *IntegrationTestEnv) AddComponent(t *testing.T, name string) {
	t.Helper()
	createTransferFile(t, e.ConfigDir, name, e.Server.URL)
	updateTransferTargetPath(t, e.ConfigDir, e.TargetDir)
}

// computeContentHash returns the SHA256 hash of the given content as a hex string.
func computeContentHash(content []byte) string {
	h := sha256.Sum256(content)
	return hex.EncodeToString(h[:])
}

// TestWorkflow_UpdateWithPriorInstall tests that update correctly downloads a new version
// when an older version is already installed.
func TestWorkflow_UpdateWithPriorInstall(t *testing.T) {
	v1Content := []byte("extension v1 content")
	v2Content := []byte("extension v2 content - newer")
	v1Hash := computeContentHash(v1Content)
	v2Hash := computeContentHash(v2Content)

	// Set up server with v1 and v2 available
	files := testutil.TestServerFiles{
		Files: map[string]string{
			"myext_1.0.0.raw": v1Hash,
			"myext_2.0.0.raw": v2Hash,
		},
		Content: map[string][]byte{
			"myext_2.0.0.raw": v2Content, // Only v2 needs to be downloadable
		},
	}

	env := NewIntegrationTestEnv(t, files)
	env.AddComponent(t, "myext")

	// Simulate prior install: create v1 file in target directory
	v1Path := filepath.Join(env.TargetDir, "myext_1.0.0.raw")
	if err := os.WriteFile(v1Path, v1Content, 0644); err != nil {
		t.Fatalf("failed to create v1 file: %v", err)
	}

	// Run update with NoRefresh
	results, err := env.Client.Update(context.Background(), UpdateOptions{
		NoRefresh: true,
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Assert: result shows v2 downloaded
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	result := results[0]
	if !result.Downloaded {
		t.Error("expected Downloaded = true")
	}
	if result.Version != "2.0.0" {
		t.Errorf("expected Version = 2.0.0, got %s", result.Version)
	}

	// Assert: v2 file exists in target directory
	v2Path := filepath.Join(env.TargetDir, "myext_2.0.0.raw")
	if _, err := os.Stat(v2Path); err != nil {
		t.Errorf("v2 file should exist at %s: %v", v2Path, err)
	}

	// Assert: mock runner was NOT called (NoRefresh)
	if env.MockRunner.RefreshCalled {
		t.Error("expected RefreshCalled = false when NoRefresh is set")
	}
}

// TestWorkflow_UpdateThenRemove tests the complete workflow of updating an extension
// and then removing it.
func TestWorkflow_UpdateThenRemove(t *testing.T) {
	v1Content := []byte("extension v1 content for update-remove test")
	v1Hash := computeContentHash(v1Content)

	files := testutil.TestServerFiles{
		Files: map[string]string{
			"myext_1.0.0.raw": v1Hash,
		},
		Content: map[string][]byte{
			"myext_1.0.0.raw": v1Content,
		},
	}

	env := NewIntegrationTestEnv(t, files)
	env.AddComponent(t, "myext")

	// Step 1: Update - should install v1
	updateResults, err := env.Client.Update(context.Background(), UpdateOptions{
		NoRefresh: true,
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if len(updateResults) != 1 {
		t.Fatalf("expected 1 update result, got %d", len(updateResults))
	}
	if !updateResults[0].Installed {
		t.Error("expected Installed = true after update")
	}
	if updateResults[0].Version != "1.0.0" {
		t.Errorf("expected Version = 1.0.0, got %s", updateResults[0].Version)
	}

	// Verify file exists
	v1Path := filepath.Join(env.TargetDir, "myext_1.0.0.raw")
	if _, err := os.Stat(v1Path); err != nil {
		t.Errorf("v1 file should exist after update: %v", err)
	}

	// Step 2: Remove with NoRefresh
	removeResult, err := env.Client.Remove(context.Background(), "myext", RemoveOptions{
		NoRefresh: true,
	})
	if err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	// Assert: remove succeeded
	if !removeResult.Success {
		t.Error("expected Success = true after remove")
	}

	// Assert: file was removed
	if len(removeResult.RemovedFiles) == 0 {
		t.Error("expected at least one file to be removed")
	}

	// Verify file no longer exists
	if _, err := os.Stat(v1Path); !os.IsNotExist(err) {
		t.Errorf("v1 file should be removed: %v", err)
	}
}

// TestWorkflow_MultipleVersionsUpdate tests updating through multiple versions
// to ensure the update process handles version progression correctly.
func TestWorkflow_MultipleVersionsUpdate(t *testing.T) {
	v1Content := []byte("extension v1 content")
	v2Content := []byte("extension v2 content - update 1")
	v3Content := []byte("extension v3 content - update 2")
	v1Hash := computeContentHash(v1Content)
	v2Hash := computeContentHash(v2Content)
	v3Hash := computeContentHash(v3Content)

	// Initial setup: v1 and v2 available
	files := testutil.TestServerFiles{
		Files: map[string]string{
			"myext_1.0.0.raw": v1Hash,
			"myext_2.0.0.raw": v2Hash,
		},
		Content: map[string][]byte{
			"myext_1.0.0.raw": v1Content,
			"myext_2.0.0.raw": v2Content,
		},
	}

	env := NewIntegrationTestEnv(t, files)
	env.AddComponent(t, "myext")

	// Step 1: Update - should install v2 (newest)
	updateResults, err := env.Client.Update(context.Background(), UpdateOptions{
		NoRefresh: true,
	})
	if err != nil {
		t.Fatalf("First update failed: %v", err)
	}

	if len(updateResults) != 1 {
		t.Fatalf("expected 1 update result, got %d", len(updateResults))
	}
	if updateResults[0].Version != "2.0.0" {
		t.Errorf("expected first update to v2.0.0, got %s", updateResults[0].Version)
	}

	// Verify v2 exists
	v2Path := filepath.Join(env.TargetDir, "myext_2.0.0.raw")
	if _, err := os.Stat(v2Path); err != nil {
		t.Errorf("v2 file should exist: %v", err)
	}

	// Step 2: Add v3 to server (simulating new version release)
	// We need to close old server and create new one with v3
	env.cleanup() // Close old server

	// Create new server with v3 added
	filesV3 := testutil.TestServerFiles{
		Files: map[string]string{
			"myext_1.0.0.raw": v1Hash,
			"myext_2.0.0.raw": v2Hash,
			"myext_3.0.0.raw": v3Hash,
		},
		Content: map[string][]byte{
			"myext_3.0.0.raw": v3Content,
		},
	}

	// Re-setup environment with v3 available
	newMockRunner := &sysext.MockRunner{}
	runnerCleanup := sysext.SetRunner(newMockRunner)
	defer runnerCleanup()

	newServer := testutil.NewTestServer(t, filesV3)
	defer newServer.Close()

	// Update transfer file to point to new server
	createTransferFile(t, env.ConfigDir, "myext", newServer.URL)
	updateTransferTargetPath(t, env.ConfigDir, env.TargetDir)

	// Recreate client with updated config
	client := NewClient(ClientConfig{Definitions: env.ConfigDir})

	// Step 3: Update again - should install v3
	updateResults2, err := client.Update(context.Background(), UpdateOptions{
		NoRefresh: true,
	})
	if err != nil {
		t.Fatalf("Second update failed: %v", err)
	}

	if len(updateResults2) != 1 {
		t.Fatalf("expected 1 update result, got %d", len(updateResults2))
	}
	if updateResults2[0].Version != "3.0.0" {
		t.Errorf("expected second update to v3.0.0, got %s", updateResults2[0].Version)
	}

	// Verify v3 exists
	v3Path := filepath.Join(env.TargetDir, "myext_3.0.0.raw")
	if _, err := os.Stat(v3Path); err != nil {
		t.Errorf("v3 file should exist: %v", err)
	}
}
