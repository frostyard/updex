package updex

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostyard/updex/v2/sysext"
)

// TestMain points every default-constructed client at temporary lock,
// rollback, definition, os-release, catalog, and sysext paths, so no test
// touches /run/updex/lock, /var/lib/updex, /etc, /run, /usr/lib,
// /var/lib/extensions, /run/extensions, or the host's other sysext images
// (the production defaults), and at empty temporary merged-state hierarchies,
// so the vacuum that follows an install never reads the host's
// /usr/.systemd-sysext and /opt/.systemd-sysext. Tests run one after another
// in a shared process, so one set of defaults is enough; a test that runs
// clients concurrently, that tests the lock itself, or that needs other paths
// gives each client its own RuntimePaths. A test that replaces a
// compatibility variable (config.SearchRoots, sysext.SysextDir, ...) keeps
// that value: resolveRuntimePaths uses it rather than the default below.
// There is no *testing.T here, so this uses os.MkdirTemp.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "updex-sdk-paths-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create temporary path directory: %v\n", err)
		os.Exit(1)
	}
	lockPathDefault = filepath.Join(dir, "lock")
	rollbackDirDefault = filepath.Join(dir, "rollback")
	definitionRootsDefault = []string{filepath.Join(dir, "definitions")}
	osReleasePathsDefault = []string{filepath.Join(dir, "os-release")}
	catalogConfigRootsDefault = []string{filepath.Join(dir, "catalogs")}
	catalogTargetPathDefault = filepath.Join(dir, "extensions.d")
	sysextLinkDirDefault = filepath.Join(dir, "extensions")
	runExtensionsDirDefault = filepath.Join(dir, "run-extensions")
	sysext.MergedHierarchies = []string{filepath.Join(dir, "usr-hierarchy"), filepath.Join(dir, "opt-hierarchy")}

	for _, d := range []string{
		definitionRootsDefault[0],
		catalogConfigRootsDefault[0],
		catalogTargetPathDefault,
		sysextLinkDirDefault,
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "create temporary path %s: %v\n", d, err)
			_ = os.RemoveAll(dir)
			os.Exit(1)
		}
	}

	code := m.Run()

	if err := os.RemoveAll(dir); err != nil {
		fmt.Fprintf(os.Stderr, "remove temporary path directory: %v\n", err)
	}
	os.Exit(code)
}

// TestDefaultClientPathsAreTemporary guards the TestMain defaults: a client
// constructed without explicit RuntimePaths must resolve every path it reads
// or writes into a temporary directory, never the host's definitions,
// os-release files, sysext images, merged-state hierarchies, lock, rollback
// records, or catalog roots.
func TestDefaultClientPathsAreTemporary(t *testing.T) {
	client := NewClient(ClientConfig{})
	tempDir := os.TempDir()

	hostPaths := []string{
		"/etc", "/run", "/usr", "/var/lib/extensions",
		"/var/lib/extensions.d", "/var/lib/updex",
	}
	check := func(name, path string) {
		t.Helper()
		if path == "" {
			t.Errorf("%s is empty", name)
			return
		}
		for _, host := range hostPaths {
			if path == host || strings.HasPrefix(path, host+string(filepath.Separator)) {
				t.Errorf("%s = %q is a production host path", name, path)
			}
		}
		rel, err := filepath.Rel(tempDir, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			t.Errorf("%s = %q is not inside %q", name, path, tempDir)
		}
	}

	for _, root := range client.paths.definitionRoots {
		check("definition root", root)
	}
	for _, path := range client.paths.osReleasePaths {
		check("os-release path", path)
	}
	for _, root := range client.paths.catalogConfigRoots {
		check("catalog config root", root)
	}
	for _, hierarchy := range client.paths.sysextHierarchies {
		check("sysext hierarchy", hierarchy)
	}
	check("catalog target path", client.paths.catalogTargetPath)
	check("sysext link dir", client.paths.sysextLinkDir)
	check("run extensions dir", client.paths.runExtensionsDir)
	check("lock path", client.paths.lockPath)
	check("rollback dir", client.paths.rollbackDir)
}
