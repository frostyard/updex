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

// TestMain gives every handler test an isolated host state before any test
// runs. newClient hands runtimePaths to the SDK, so a test that runs
// `features disable --now` or `catalog remove` cannot read the host's
// /usr and /opt metadata (where a real merged image such as docker could make
// a refusal test pass or fail for the wrong reason), its definitions under
// /etc, /run, and /usr, its os-release files, its sysext images under
// /var/lib/extensions, or its catalog roots. The runtime image search
// directory stays absent, as on snosi. The directory names avoid a plain usr,
// opt, or run/extensions component so an strace of the test binary cannot be
// mistaken for a read of the host paths. Tests that need other paths call
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

	testRuntimePaths = updex.RuntimePaths{
		// No definitions live here unless a test writes them through
		// setRuntimePaths, so no handler test sees the host's features.
		DefinitionRoots: []string{filepath.Join(root, "definitions")},
		OSReleasePaths:  []string{filepath.Join(root, "os-release")},
		// No catalog is configured here: the host's /etc/updex/catalogs.d
		// files are never read.
		CatalogConfigRoots: []string{filepath.Join(root, "catalogs")},
		CatalogTargetPath:  filepath.Join(root, "catalog-extensions"),
		// No image is linked here unless a test writes it: the host's
		// /var/lib/extensions is never read.
		SysextLinkDir:     filepath.Join(root, "var-extensions"),
		SysextHierarchies: hierarchies,
		// Deliberately not created: no image is staged for a future refresh.
		RunExtensionsDir: filepath.Join(root, "run-search", "extensions"),
		// Every mutating handler takes this lock; the production default
		// /run/updex/lock is never touched by a test.
		LockPath: filepath.Join(root, "run-updex", "lock"),
		// Rollback records live here; the production default
		// /var/lib/updex/rollback is never touched by a test.
		RollbackDir: filepath.Join(root, "var-updex", "rollback"),
	}
	for _, d := range []string{
		testRuntimePaths.DefinitionRoots[0],
		testRuntimePaths.CatalogConfigRoots[0],
		testRuntimePaths.CatalogTargetPath,
		testRuntimePaths.SysextLinkDir,
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "create temporary path %s: %v\n", d, err)
			_ = os.RemoveAll(root)
			os.Exit(1)
		}
	}
	runtimePaths = testRuntimePaths

	code := m.Run()

	if err := os.RemoveAll(root); err != nil {
		fmt.Fprintf(os.Stderr, "remove temporary runtime paths: %v\n", err)
	}
	os.Exit(code)
}

// testRuntimePaths is the temporary path set TestMain gives every handler
// test. setRuntimePaths keeps these values for the fields a test leaves zero.
var testRuntimePaths updex.RuntimePaths

// setRuntimePaths replaces the CLI runtime-path seam for one test and restores
// the previous value on cleanup, so a test can point a handler at a fake
// merged hierarchy without leaking paths into the next test. A field the test
// leaves zero keeps the temporary TestMain value, so no handler test can fall
// back to a host path.
func setRuntimePaths(t *testing.T, rp updex.RuntimePaths) {
	t.Helper()
	old := runtimePaths
	t.Cleanup(func() { runtimePaths = old })

	if rp.LockPath == "" {
		rp.LockPath = testRuntimePaths.LockPath
	}
	if rp.RollbackDir == "" {
		rp.RollbackDir = testRuntimePaths.RollbackDir
	}
	if rp.CatalogTargetPath == "" {
		rp.CatalogTargetPath = testRuntimePaths.CatalogTargetPath
	}
	if rp.SysextLinkDir == "" {
		rp.SysextLinkDir = testRuntimePaths.SysextLinkDir
	}
	if rp.RunExtensionsDir == "" {
		rp.RunExtensionsDir = testRuntimePaths.RunExtensionsDir
	}
	if len(rp.DefinitionRoots) == 0 {
		rp.DefinitionRoots = testRuntimePaths.DefinitionRoots
	}
	if len(rp.OSReleasePaths) == 0 {
		rp.OSReleasePaths = testRuntimePaths.OSReleasePaths
	}
	if len(rp.CatalogConfigRoots) == 0 {
		rp.CatalogConfigRoots = testRuntimePaths.CatalogConfigRoots
	}
	if len(rp.SysextHierarchies) == 0 {
		rp.SysextHierarchies = testRuntimePaths.SysextHierarchies
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

	if runtimePaths.RollbackDir == "" || runtimePaths.RollbackDir == updex.DefaultRollbackDir {
		t.Errorf("runtimePaths.RollbackDir = %q: handler tests would write the production rollback records", runtimePaths.RollbackDir)
	}

	if runtimePaths.RunExtensionsDir == "" {
		t.Error("runtimePaths.RunExtensionsDir is empty: handler tests would skip the runtime search check")
	}
	if runtimePaths.RunExtensionsDir == sysext.RunExtensionsDir {
		t.Errorf("runtimePaths.RunExtensionsDir is the production path %q", sysext.RunExtensionsDir)
	}
}

// TestHandlerTestsDoNotReadHostPaths guards the rest of the TestMain seam: a
// handler test that uses the default runtimePaths must not read the host's
// definitions, os-release files, sysext images, or catalog roots.
func TestHandlerTestsDoNotReadHostPaths(t *testing.T) {
	tempDir := os.TempDir()
	check := func(name, path string) {
		t.Helper()
		if path == "" {
			t.Errorf("%s is empty", name)
			return
		}
		for _, host := range []string{"/etc", "/run", "/usr", "/var/lib/extensions", "/var/lib/extensions.d", "/var/lib/updex"} {
			if path == host || strings.HasPrefix(path, host+string(filepath.Separator)) {
				t.Errorf("%s = %q is a production host path", name, path)
			}
		}
		rel, err := filepath.Rel(tempDir, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			t.Errorf("%s = %q is not inside %q", name, path, tempDir)
		}
	}

	for _, root := range runtimePaths.DefinitionRoots {
		check("definition root", root)
	}
	for _, path := range runtimePaths.OSReleasePaths {
		check("os-release path", path)
	}
	for _, root := range runtimePaths.CatalogConfigRoots {
		check("catalog config root", root)
	}
	check("catalog target path", runtimePaths.CatalogTargetPath)
	check("sysext link dir", runtimePaths.SysextLinkDir)
}
