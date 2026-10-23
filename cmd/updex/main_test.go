package updex

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/frostyard/updex/v2/sysext"
	"github.com/frostyard/updex/v2/updex"
)

// TestMain gives every handler test an isolated merged-sysext state before
// any test runs. newClient hands runtimePaths to the SDK, so a test that runs
// `features disable --now` or `catalog remove` cannot read the host's
// /usr and /opt metadata (where a real merged image such as docker could make
// a refusal test pass or fail for the wrong reason). The runtime image search
// directory is left absent, as on snosi. The directory names avoid a plain
// usr, opt, or run/extensions component so an strace of the test binary cannot
// be mistaken for a read of the host paths. Tests that need other paths call
// setRuntimePaths. There is no *testing.T here, so this uses os.MkdirTemp
// instead of t.TempDir.
func TestMain(m *testing.M) {
	root, err := os.MkdirTemp("", "updex-cmd-runtime-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create temporary runtime paths: %v\n", err)
		os.Exit(1)
	}

	hierarchies := []string{filepath.Join(root, "usr-hierarchy"), filepath.Join(root, "opt-hierarchy")}
	for _, hierarchy := range hierarchies {
		if err := os.MkdirAll(hierarchy, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "create temporary hierarchy %s: %v\n", hierarchy, err)
			_ = os.RemoveAll(root)
			os.Exit(1)
		}
	}

	runtimePaths = updex.RuntimePaths{
		SysextHierarchies: hierarchies,
		// Deliberately not created: no image is staged for a future refresh.
		RunExtensionsDir: filepath.Join(root, "run-search", "extensions"),
		// Every mutating handler takes this lock; the production default
		// /run/updex/lock is never touched by a test.
		LockPath: filepath.Join(root, "run-updex", "lock"),
	}
	testLockPath = runtimePaths.LockPath

	code := m.Run()

	if err := os.RemoveAll(root); err != nil {
		fmt.Fprintf(os.Stderr, "remove temporary runtime paths: %v\n", err)
	}
	os.Exit(code)
}

// testLockPath is the temporary lock file TestMain gives every handler test.
var testLockPath string

// setRuntimePaths replaces the CLI runtime-path seam for one test and restores
// the previous value on cleanup, so a test can point a handler at a fake
// merged hierarchy without leaking paths into the next test.
func setRuntimePaths(t *testing.T, rp updex.RuntimePaths) {
	t.Helper()
	old := runtimePaths
	t.Cleanup(func() { runtimePaths = old })

	if rp.LockPath == "" {
		// A test that sets its own paths still never reaches /run/updex.
		rp.LockPath = testLockPath
	}
	rp.DefinitionRoots = slices.Clone(rp.DefinitionRoots)
	rp.OSReleasePaths = slices.Clone(rp.OSReleasePaths)
	rp.CatalogConfigRoots = slices.Clone(rp.CatalogConfigRoots)
	rp.SysextHierarchies = slices.Clone(rp.SysextHierarchies)
	runtimePaths = rp
}

// TestHandlerTestsDoNotReadHostMergedState guards the TestMain seam: if it
// ever points at the production paths again, every handler test that disables
// a feature or removes a catalog could read the host's merged image.
func TestHandlerTestsDoNotReadHostMergedState(t *testing.T) {
	if len(runtimePaths.SysextHierarchies) == 0 {
		t.Fatal("runtimePaths.SysextHierarchies is empty: handler tests would skip the merged-state check")
	}

	tempDir := os.TempDir()
	for _, hierarchy := range runtimePaths.SysextHierarchies {
		if slices.Contains(sysext.MergedHierarchies, hierarchy) {
			t.Errorf("runtimePaths.SysextHierarchies contains production hierarchy %q", hierarchy)
		}
		if hierarchy == "/usr" || hierarchy == "/opt" {
			t.Errorf("runtimePaths.SysextHierarchies contains production hierarchy %q", hierarchy)
		}
		rel, err := filepath.Rel(tempDir, hierarchy)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			t.Errorf("runtimePaths.SysextHierarchies entry %q is not inside %q", hierarchy, tempDir)
		}
	}

	if runtimePaths.LockPath == "" || runtimePaths.LockPath == updex.DefaultLockPath {
		t.Errorf("runtimePaths.LockPath = %q: handler tests would take the production lock", runtimePaths.LockPath)
	}

	if runtimePaths.RunExtensionsDir == "" {
		t.Error("runtimePaths.RunExtensionsDir is empty: handler tests would skip the runtime search check")
	}
	if runtimePaths.RunExtensionsDir == sysext.RunExtensionsDir {
		t.Errorf("runtimePaths.RunExtensionsDir is the production path %q", sysext.RunExtensionsDir)
	}
}
