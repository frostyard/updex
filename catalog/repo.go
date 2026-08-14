// Package catalog provides primitives for consuming sysext catalogs such as
// fedora-sysexts (https://fedora-sysexts.github.io/): repo configuration
// loading, sysext enumeration, and rendering of the catalog's published
// sysupdate .conf files into updex-managed .transfer/.feature files.
//
// The package deliberately has no built-in repos: catalogs like
// fedora-sysexts only apply to specific systems (ucore, Fedora
// atomic/CoreOS), so every repo comes from a *.catalog configuration file.
package catalog

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/ini.v1"
)

const catalogSuffix = ".catalog"

// ConfigRoots are the directories scanned for *.catalog repo definitions,
// in priority order (earlier roots win per filename). SDK callers should
// inject roots via updex.RuntimePaths rather than mutating this variable;
// it remains for compatibility wrappers and tests that have not yet migrated.
var ConfigRoots = []string{
	"/etc/updex/catalogs.d",
	"/run/updex/catalogs.d",
	"/usr/local/lib/updex/catalogs.d",
	"/usr/lib/updex/catalogs.d",
}

// ErrNoCatalogs is returned by LoadRepos when no *.catalog files exist in
// any ConfigRoots directory, so callers can print setup guidance.
var ErrNoCatalogs = errors.New("no catalogs configured")

// LoadReposFrom loads all configured catalog repos from the given configRoots,
// earlier roots winning per filename, sorted by name. It returns ErrNoCatalogs
// when no .catalog file exists anywhere. This is the explicit-roots variant of
// LoadRepos.
func LoadReposFrom(configRoots []string) ([]Repo, error) {
	files := make(map[string]string) // name -> path, first root wins

	for _, dir := range configRoots {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("failed to read directory %s: %w", dir, err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), catalogSuffix) {
				continue
			}
			name := strings.TrimSuffix(entry.Name(), catalogSuffix)
			if _, exists := files[name]; !exists {
				files[name] = filepath.Join(dir, entry.Name())
			}
		}
	}

	if len(files) == 0 {
		return nil, ErrNoCatalogs
	}

	repos := make([]Repo, 0, len(files))
	for name, path := range files {
		repo, err := parseRepoFile(name, path)
		if err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", path, err)
		}
		repos = append(repos, repo)
	}

	slices.SortFunc(repos, func(a, b Repo) int {
		return cmp.Compare(a.Name, b.Name)
	})

	return repos, nil
}

// LoadRepos loads all configured catalog repos from ConfigRoots, earlier
// roots winning per filename, sorted by name. It returns ErrNoCatalogs when
// no .catalog file exists anywhere.
func LoadRepos() ([]Repo, error) {
	return LoadReposFrom(ConfigRoots)
}

// Repo describes one configured catalog repo, loaded from a
// <name>.catalog INI file:
//
//	[Catalog]
//	SiteURL=https://extensions.fcos.fr/fedora
//	ListURL=https://api.github.com/repos/fedora-sysexts/fedora/contents/
//	# Component=catalog-fedora   (optional; default catalog-<name>)
type Repo struct {
	// Name is the repo name, derived from the .catalog filename stem.
	Name string
	// SiteURL is the base URL sysext artifacts are served from; the
	// catalog's <name>.conf, SHA256SUMS, and .raw files all resolve
	// beneath <SiteURL>/<sysext>/. Required, stored without trailing slash.
	SiteURL string
	// ListURL is a GitHub contents API endpoint (or compatible) used to
	// enumerate available sysexts. Optional: without it list/search skip
	// this repo; add/remove only need SiteURL.
	ListURL string
	// Component is the systemd-sysupdate component that added sysexts'
	// .transfer/.feature files are written under (sysupdate.<Component>.d).
	// Defaults to "catalog-<name>".
	Component string
}

// repoNamePattern matches valid repo and component names, mirroring the
// systemd-sysupdate component charset (see sysupdate.d(5)) since both repo
// names and Component values become component directory names.
var repoNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// RepoByName returns the repo with the given name from repos.
func RepoByName(repos []Repo, name string) (Repo, bool) {
	for _, r := range repos {
		if r.Name == name {
			return r, true
		}
	}
	return Repo{}, false
}

func parseRepoFile(name, path string) (Repo, error) {
	if !repoNamePattern.MatchString(name) {
		return Repo{}, fmt.Errorf("invalid catalog name %q (allowed: [a-zA-Z0-9_-]+)", name)
	}

	cfg, err := ini.Load(path)
	if err != nil {
		return Repo{}, fmt.Errorf("failed to load INI file: %w", err)
	}

	sec, err := cfg.GetSection("Catalog")
	if err != nil {
		return Repo{}, fmt.Errorf("missing [Catalog] section")
	}

	repo := Repo{
		Name:      name,
		Component: "catalog-" + name,
	}
	if key, err := sec.GetKey("SiteURL"); err == nil {
		repo.SiteURL = strings.TrimRight(key.String(), "/")
	}
	if key, err := sec.GetKey("ListURL"); err == nil {
		repo.ListURL = key.String()
	}
	if key, err := sec.GetKey("Component"); err == nil && key.String() != "" {
		repo.Component = key.String()
	}

	if repo.SiteURL == "" {
		return Repo{}, fmt.Errorf("SiteURL is required")
	}
	if !repoNamePattern.MatchString(repo.Component) {
		return Repo{}, fmt.Errorf("invalid Component %q (allowed: [a-zA-Z0-9_-]+)", repo.Component)
	}

	return repo, nil
}
