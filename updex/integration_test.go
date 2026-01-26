package updex

import (
	"net/http/httptest"
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
