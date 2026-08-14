package config

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// SearchRoots are the systemd-style root directories scanned for the legacy
// default component (SearchRoots[i]+"/sysupdate.d") and for named components
// (SearchRoots[i]+"/sysupdate.<name>.d"), in priority order (earlier roots
// win). SDK callers should inject roots via updex.RuntimePaths rather than
// mutating this variable; it remains for compatibility wrappers and tests that
// have not yet migrated.
var SearchRoots = []string{
	"/etc",
	"/run",
	"/usr/local/lib",
	"/usr/lib",
}

// componentDirName returns the sysupdate.d subdirectory name for a
// component: "sysupdate.d" for the legacy default (empty name), or
// "sysupdate.<name>.d" for a named component (see sysupdate.d(5)
// "Components").
func componentDirName(name string) string {
	if name == "" {
		return "sysupdate.d"
	}
	return "sysupdate." + name + ".d"
}

// ComponentSearchPathsIn returns the systemd-style search-path directories for
// a component from the given roots, in priority order. Pass "" for the legacy
// default component. This is the explicit-roots variant of ComponentSearchPaths.
func ComponentSearchPathsIn(name string, roots []string) []string {
	dirName := componentDirName(name)
	paths := make([]string, len(roots))
	for i, root := range roots {
		paths[i] = filepath.Join(root, dirName)
	}
	return paths
}

// ComponentSearchPaths returns the systemd-style search-path directories for
// a component, in priority order (earlier entries win). Pass "" for the
// legacy default component (/etc/sysupdate.d, /run/sysupdate.d, ...).
func ComponentSearchPaths(name string) []string {
	return ComponentSearchPathsIn(name, SearchRoots)
}

// EtcComponentDirIn returns the /etc override directory for a component's
// definitions using the given roots (the highest-priority root is used).
// This is the explicit-roots variant of EtcComponentDir.
func EtcComponentDirIn(name string, roots []string) string {
	return filepath.Join(roots[0], componentDirName(name))
}

// EtcComponentDir returns the /etc override directory for a component's
// definitions (e.g. "/etc/sysupdate.docker.d"), used when writing drop-in
// configuration overrides and catalog-generated definitions. Pass "" for
// the legacy default component (/etc/sysupdate.d). The highest-priority
// search root is used so tests that override SearchRoots exercise real
// writes; in production that root is /etc.
func EtcComponentDir(name string) string {
	return EtcComponentDirIn(name, SearchRoots)
}

// componentNamePattern matches systemd-sysupdate component names (see
// sysupdate.d(5)): non-empty strings drawn from [a-zA-Z0-9_-]+. Dotted or
// empty names are rejected.
var componentNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// parseComponentDirName returns the component name encoded in a
// "sysupdate.<name>.d" directory name, or ("", false) if dirName doesn't
// have that shape, encodes an invalid/empty name, or is the legacy default
// "sysupdate.d" directory itself.
func parseComponentDirName(dirName string) (string, bool) {
	const prefix, suffix = "sysupdate.", ".d"
	if len(dirName) < len(prefix)+len(suffix) ||
		!strings.HasPrefix(dirName, prefix) || !strings.HasSuffix(dirName, suffix) {
		return "", false
	}
	name := dirName[len(prefix) : len(dirName)-len(suffix)]
	if name == "" || !componentNamePattern.MatchString(name) {
		return "", false
	}
	return name, true
}

// ComponentOfPath returns the component name encoded in path's parent
// directory (a sysupdate.<name>.d directory), or ("", false) if the parent
// is the legacy default sysupdate.d directory, or doesn't match the
// component shape at all (e.g. a --definitions override directory).
func ComponentOfPath(path string) (string, bool) {
	return parseComponentDirName(filepath.Base(filepath.Dir(path)))
}

// SearchRootIndexIn returns the index into roots of the root that contains
// path, or (-1, false) when path lies outside every root. This is the
// explicit-roots variant of SearchRootIndex.
func SearchRootIndexIn(path string, roots []string) (int, bool) {
	path = filepath.Clean(path)

	best, bestLen := -1, 0
	for i, root := range roots {
		root = filepath.Clean(root)
		if path != root && !strings.HasPrefix(path, root+string(filepath.Separator)) {
			continue
		}
		if best == -1 || len(root) > bestLen {
			best, bestLen = i, len(root)
		}
	}
	if best == -1 {
		return -1, false
	}
	return best, true
}

// SearchRootIndex returns the index into SearchRoots of the root that
// contains path, or (-1, false) when path lies outside every root (e.g. a
// --definitions override directory). The most specific root wins, so a
// nested root is preferred over one that merely shares a prefix. Callers
// use the index rather than the directory string because SearchRoots is
// overridden with temporary directories in tests.
func SearchRootIndex(path string) (int, bool) {
	return SearchRootIndexIn(path, SearchRoots)
}

// Component describes a discovered systemd-sysupdate component: a named
// grouping of .transfer/.feature files under sysupdate.<name>.d directories
// (see sysupdate.d(5) "Components"). Components let a sysext's transfer and
// feature files move out of the shared default sysupdate.d directory into
// their own versioning scope without updex losing track of them.
type Component struct {
	// Name is the component name, e.g. "docker" for sysupdate.docker.d.
	Name string
	// SearchPaths lists the component's search-path directories that
	// actually exist on disk, in priority order (highest priority first).
	SearchPaths []string
}

// DiscoverComponentsIn scans the given roots for sysupdate.<name>.d directories
// and returns the named components found, sorted by name. It does not include
// the legacy default component. This is the explicit-roots variant of
// DiscoverComponents.
func DiscoverComponentsIn(roots []string) ([]Component, error) {
	found := make(map[string]bool)

	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("failed to read directory %s: %w", root, err)
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			name, ok := parseComponentDirName(entry.Name())
			if !ok {
				continue
			}
			found[name] = true
		}
	}

	components := make([]Component, 0, len(found))
	for name := range found {
		components = append(components, Component{
			Name:        name,
			SearchPaths: existingDirs(ComponentSearchPathsIn(name, roots)),
		})
	}

	slices.SortFunc(components, func(a, b Component) int {
		return cmp.Compare(a.Name, b.Name)
	})

	return components, nil
}

// DiscoverComponents scans SearchRoots for sysupdate.<name>.d directories
// and returns the named components found, sorted by name. It does not
// include the legacy default component (plain sysupdate.d); use
// ComponentSearchPaths("") for that. Directory names that don't match the
// component charset (see parseComponentDirName) are ignored.
func DiscoverComponents() ([]Component, error) {
	return DiscoverComponentsIn(SearchRoots)
}

// existingDirs filters paths to those that exist as directories on disk,
// preserving order.
func existingDirs(paths []string) []string {
	var result []string
	for _, p := range paths {
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			result = append(result, p)
		}
	}
	return result
}
