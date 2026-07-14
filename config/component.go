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
// win). Overridable in tests.
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

// ComponentSearchPaths returns the systemd-style search-path directories for
// a component, in priority order (earlier entries win). Pass "" for the
// legacy default component (/etc/sysupdate.d, /run/sysupdate.d, ...).
func ComponentSearchPaths(name string) []string {
	dirName := componentDirName(name)
	paths := make([]string, len(SearchRoots))
	for i, root := range SearchRoots {
		paths[i] = filepath.Join(root, dirName)
	}
	return paths
}

// defaultSearchPaths are the search paths for the legacy default component.
func defaultSearchPaths() []string {
	return ComponentSearchPaths("")
}

// EtcComponentDir returns the /etc override directory for a component's
// definitions (e.g. "/etc/sysupdate.docker.d"), used when writing drop-in
// configuration overrides. Pass "" for the legacy default component
// (/etc/sysupdate.d).
func EtcComponentDir(name string) string {
	return filepath.Join("/etc", componentDirName(name))
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

// DiscoverComponents scans SearchRoots for sysupdate.<name>.d directories
// and returns the named components found, sorted by name. It does not
// include the legacy default component (plain sysupdate.d); use
// ComponentSearchPaths("") for that. Directory names that don't match the
// component charset (see parseComponentDirName) are ignored.
func DiscoverComponents() ([]Component, error) {
	found := make(map[string]bool)

	for _, root := range SearchRoots {
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
			SearchPaths: existingDirs(ComponentSearchPaths(name)),
		})
	}

	slices.SortFunc(components, func(a, b Component) int {
		return cmp.Compare(a.Name, b.Name)
	})

	return components, nil
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
