package updex

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/frostyard/updex/catalog"
	"github.com/frostyard/updex/config"
)

// catalogRepos loads the configured catalog repos, translating the empty
// configuration into actionable guidance. Catalog operations are
// component-scoped, so they cannot be combined with a Definitions override.
func (c *Client) catalogRepos() ([]catalog.Repo, error) {
	if c.config.Definitions != "" {
		return nil, fmt.Errorf("catalog operations cannot be combined with a definitions override (--definitions)")
	}
	repos, err := catalog.LoadReposFrom(c.paths.catalogConfigRoots)
	if errors.Is(err, catalog.ErrNoCatalogs) {
		firstRoot := ""
		if len(c.paths.catalogConfigRoots) > 0 {
			firstRoot = c.paths.catalogConfigRoots[0]
		}
		return nil, fmt.Errorf("no catalogs configured: create a <name>.catalog file under %s (see the Catalog section of the README)", firstRoot)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load catalogs: %w", err)
	}
	return repos, nil
}

// catalogRepo resolves a single configured repo by name.
func (c *Client) catalogRepo(repos []catalog.Repo, name string) (catalog.Repo, error) {
	repo, ok := catalog.RepoByName(repos, name)
	if !ok {
		var names []string
		for _, r := range repos {
			names = append(names, r.Name)
		}
		return catalog.Repo{}, fmt.Errorf("unknown catalog %q (configured: %s)", name, strings.Join(names, ", "))
	}
	return repo, nil
}

// CatalogList enumerates the sysexts available from the configured catalog
// repos, marking those already installed (added) and enabled. Repos without
// a ListURL are skipped with a warning unless explicitly selected via
// opts.Repo, in which case the missing ListURL is an error.
func (c *Client) CatalogList(ctx context.Context, opts CatalogListOptions) ([]CatalogEntry, error) {
	repos, err := c.catalogRepos()
	if err != nil {
		return nil, err
	}

	if opts.Repo != "" {
		repo, err := c.catalogRepo(repos, opts.Repo)
		if err != nil {
			return nil, err
		}
		repos = []catalog.Repo{repo}
	}

	// Non-nil so an empty listing serializes as JSON [] rather than null.
	entries := make([]CatalogEntry, 0)

	for _, repo := range repos {
		if repo.ListURL == "" {
			if opts.Repo != "" {
				return nil, fmt.Errorf("catalog %q has no ListURL configured; listing is unavailable", repo.Name)
			}
			c.warn("catalog %q has no ListURL configured; skipping", repo.Name)
			continue
		}

		c.msg("Listing catalog %q", repo.Name)
		names, cacheRes, err := catalog.CachedListIn(ctx, c.httpClient, repo, catalog.CachedListOptions{
			NoCache: opts.NoCache,
		}, c.paths.catalogCacheDir)
		if err != nil {
			return nil, err
		}
		switch {
		case cacheRes.Stale:
			c.warn("live fetch for catalog %q failed; using stale cached listing (age %s)",
				repo.Name, cacheRes.Age.Round(time.Minute))
		case cacheRes.FromCache && cacheRes.Age > 0:
			c.msg("Using cached listing for %q (age %s); use --no-cache to refresh",
				repo.Name, cacheRes.Age.Round(time.Minute))
		}

		features, err := config.LoadComponentFeaturesIn(repo.Component, c.paths.definitionRoots)
		if err != nil {
			return nil, fmt.Errorf("failed to load features for component %q: %w", repo.Component, err)
		}

		for _, name := range names {
			if opts.Search != "" && !strings.Contains(name, opts.Search) {
				continue
			}
			entry := CatalogEntry{Name: name, Repo: repo.Name}
			for _, f := range features {
				if f.Name != name {
					continue
				}
				// Installed means "this repo's catalog add wrote this
				// definition". A same-named feature that is hand-written,
				// package-shipped, or added from another catalog sharing
				// this Component is not this repo's install. f.FilePath is
				// the highest-priority (i.e. /etc) file, which is where
				// catalog add writes.
				if owner, ok := catalog.GeneratedFileRepo(f.FilePath); !ok || owner != repo.Name {
					break
				}
				entry.Installed = true
				entry.Enabled = f.Enabled && !f.Masked
				break
			}
			entries = append(entries, entry)
		}
	}

	return entries, nil
}

// CatalogAdd installs a sysext from a configured catalog: it fetches the
// catalog's published transfer definition, writes the generated
// .transfer/.feature files into the repo's component directory, and enables
// the feature with an immediate download (EnableFeature with Now). From
// then on the sysext is managed by the standard feature operations; only
// CatalogRemove knows it came from a catalog.
func (c *Client) CatalogAdd(ctx context.Context, name string, opts CatalogAddOptions) (*CatalogAddResult, error) {
	if err := catalog.ValidateSysextName(name); err != nil {
		return nil, err
	}
	repos, err := c.catalogRepos()
	if err != nil {
		return nil, err
	}

	type hit struct {
		repo catalog.Repo
		conf []byte
	}
	var hits []hit

	if opts.Repo != "" {
		repo, err := c.catalogRepo(repos, opts.Repo)
		if err != nil {
			return nil, err
		}
		conf, err := catalog.FetchConf(ctx, c.httpClient, repo, name)
		if err != nil {
			return nil, err
		}
		hits = append(hits, hit{repo, conf})
	} else {
		for _, repo := range repos {
			conf, err := catalog.FetchConf(ctx, c.httpClient, repo, name)
			if errors.Is(err, catalog.ErrNotFound) {
				continue
			}
			if err != nil {
				return nil, err
			}
			hits = append(hits, hit{repo, conf})
		}
		if len(hits) == 0 {
			return nil, fmt.Errorf("%q not found in any configured catalog", name)
		}
		if len(hits) > 1 {
			var candidates []string
			for _, h := range hits {
				candidates = append(candidates, h.repo.Name+"/"+name)
			}
			return nil, fmt.Errorf("%q exists in multiple catalogs; specify one of: %s", name, strings.Join(candidates, ", "))
		}
	}

	repo, conf := hits[0].repo, hits[0].conf

	transferData, err := catalog.RenderTransferTo(conf, repo, name, c.paths.catalogTargetPath)
	if err != nil {
		return nil, err
	}
	featureData := catalog.RenderFeature(repo, name)

	dir := config.EtcComponentDirIn(repo.Component, c.paths.definitionRoots)
	result := &CatalogAddResult{
		Name:         name,
		Repo:         repo.Name,
		Component:    repo.Component,
		TransferFile: filepath.Join(dir, name+".transfer"),
		FeatureFile:  filepath.Join(dir, name+".feature"),
		DryRun:       opts.DryRun,
	}

	// Refuse to overwrite definitions this repo did not generate. The
	// marker header names its generating repo, so a hand-written file, a
	// package-shipped one, or another catalog's file sharing this
	// Component all stop the add rather than being clobbered.
	existedBefore := false
	for _, path := range []string{result.TransferFile, result.FeatureFile} {
		exists, err := managedFileExists(path)
		if err != nil {
			return nil, fmt.Errorf("cannot determine whether %s exists: %w", path, err)
		}
		if !exists {
			continue
		}
		owner, ok := catalog.GeneratedFileRepo(path)
		if !ok {
			return nil, fmt.Errorf("refusing to overwrite %s: not generated by updex catalog", path)
		}
		if owner != repo.Name {
			return nil, fmt.Errorf("refusing to overwrite %s: generated by catalog %q, not %q", path, owner, repo.Name)
		}
		existedBefore = true
	}

	if opts.DryRun {
		c.msg("Would write %s", result.TransferFile)
		c.msg("Would write %s", result.FeatureFile)
		c.msg("Would enable feature %q and download extensions", name)
		return result, nil
	}

	// Snapshot everything the add is about to touch so a failure restores
	// the previous state exactly: a fresh add rolls back to nothing, a
	// re-add rolls back to its previously working definitions. Every
	// failure past this point must go through rollback — a half-written
	// definition is as damaging as a half-installed one, and os.WriteFile
	// truncates on open, so even the write calls can destroy a working
	// file before failing.
	dropInDir := filepath.Join(dir, name+".feature.d")
	snapshots := []fileSnapshot{
		snapshotFile(result.TransferFile),
		snapshotFile(result.FeatureFile),
		snapshotFile(filepath.Join(dropInDir, updexDropInName)),
	}
	rollback := func() {
		if existedBefore {
			c.warn("add failed; restoring previous definitions")
		} else {
			c.warn("add failed; rolling back generated files")
		}
		for _, s := range snapshots {
			if s.existed && !s.captured {
				c.warn("leaving %s as-is: its contents could not be read when the add started", s.path)
			}
			s.restore()
		}
		// Only removes directories left empty, so administrator drop-ins
		// and a pre-existing component keep their contents.
		_ = os.Remove(dropInDir)
		if !existedBefore {
			_ = os.Remove(dir)
		}
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		rollback()
		return result, fmt.Errorf("failed to create component directory: %w", err)
	}
	if err := os.WriteFile(result.TransferFile, transferData, 0644); err != nil {
		rollback()
		return result, fmt.Errorf("failed to write transfer file: %w", err)
	}
	if err := os.WriteFile(result.FeatureFile, featureData, 0644); err != nil {
		rollback()
		return result, fmt.Errorf("failed to write feature file: %w", err)
	}
	c.msg("Added %s/%s to %s", repo.Name, name, dir)

	enableResult, err := c.EnableFeature(ctx, name, EnableFeatureOptions{
		Now:       true,
		NoRefresh: opts.NoRefresh,
		Component: repo.Component,
	})
	result.Enable = enableResult
	if err != nil {
		// Never leave a persistent half-installed state behind: definitions
		// present plus an enabled drop-in would make every later update
		// retry an operation this call reported as failed.
		rollback()
		return result, err
	}

	return result, nil
}

// managedFileExists reports whether a regular file exists at path, which
// is the only thing updex generates or deletes at a managed definition
// path.
//
// It uses Lstat, never Stat: a symlink must be seen as itself rather than
// followed. A dangling link would otherwise stat as absent, skipping the
// ownership check, and the subsequent os.WriteFile would follow it and
// create the target wherever it points — a privileged write outside the
// component directory. Anything present that is not a regular file is an
// error so the operator resolves it deliberately.
//
// A stat failure for any reason other than absence is likewise an error
// rather than "not there": reading an unreadable path as absent would
// skip a safety check or under-report what was left behind.
func managedFileExists(path string) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("%s is not a regular file (mode %s); updex manages definitions in place and will not write through it", path, info.Mode().Type())
	}
	return true, nil
}

// fileSnapshot captures a file's contents (or its absence) so a failed
// operation can restore exactly what was there before.
type fileSnapshot struct {
	path string
	data []byte
	mode os.FileMode
	// existed records that something was at path when the snapshot was
	// taken, even if its contents could not be read.
	existed bool
	// captured records that data holds those contents. A path that
	// existed but could not be read (a directory, an unreadable file) is
	// existed && !captured, and restore leaves it strictly alone —
	// removing it would destroy state this snapshot cannot rebuild.
	captured bool
}

func snapshotFile(path string) fileSnapshot {
	s := fileSnapshot{path: path, mode: 0644}
	// Lstat, not Stat: a symlink is state in its own right. Reading and
	// rewriting its target would both mis-capture it and write outside
	// the component directory on restore.
	info, err := os.Lstat(path)
	if err != nil {
		return s
	}
	s.existed = true
	if !info.Mode().IsRegular() {
		return s
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	s.data, s.mode, s.captured = data, info.Mode().Perm(), true
	return s
}

// restore rewrites the captured contents, or removes the file when it did
// not exist at snapshot time. It is a no-op for a path that existed but
// whose contents could not be captured. Best-effort otherwise: the caller
// is already returning the original failure.
func (s fileSnapshot) restore() {
	switch {
	case !s.existed:
		_ = os.Remove(s.path)
	case !s.captured:
		// Nothing to rewrite and nothing safe to delete.
	default:
		if err := os.MkdirAll(filepath.Dir(s.path), 0755); err == nil {
			_ = os.WriteFile(s.path, s.data, s.mode)
		}
	}
}

// CatalogRemove undoes CatalogAdd for a sysext: it disables the feature
// with full cleanup (DisableFeature with Now: unmerge, remove downloaded
// images and the /var/lib/extensions link — requiring opts.Force when the
// extension is currently merged) and deletes the generated
// .transfer/.feature files and drop-ins from the repo's /etc component
// directory.
func (c *Client) CatalogRemove(ctx context.Context, name string, opts CatalogRemoveOptions) (*CatalogRemoveResult, error) {
	if err := catalog.ValidateSysextName(name); err != nil {
		return nil, err
	}
	repos, err := c.catalogRepos()
	if err != nil {
		return nil, err
	}

	if opts.Repo != "" {
		repo, err := c.catalogRepo(repos, opts.Repo)
		if err != nil {
			return nil, err
		}
		repos = []catalog.Repo{repo}
	}

	// A sysext is catalog-managed only when its feature file in the repo's
	// /etc component directory carries a marker header naming *this* repo.
	// A same-named feature that merely lives in the same component
	// (hand-written, package-shipped, defined in a lower root, or added
	// from a different catalog sharing the Component) must never be
	// disabled or deleted here.
	var owners []catalog.Repo
	for _, repo := range repos {
		featureFile := filepath.Join(config.EtcComponentDirIn(repo.Component, c.paths.definitionRoots), name+".feature")
		if owner, ok := catalog.GeneratedFileRepo(featureFile); ok && owner == repo.Name {
			owners = append(owners, repo)
		}
	}

	if len(owners) == 0 {
		return nil, fmt.Errorf("%q is not a catalog-managed sysext (no generated definition found)", name)
	}
	if len(owners) > 1 {
		var candidates []string
		for _, r := range owners {
			candidates = append(candidates, r.Name+"/"+name)
		}
		return nil, fmt.Errorf("%q is managed by multiple catalogs; specify one of: %s", name, strings.Join(candidates, ", "))
	}
	repo := owners[0]

	dir := config.EtcComponentDirIn(repo.Component, c.paths.definitionRoots)
	transferFile := filepath.Join(dir, name+".transfer")
	dropInDir := filepath.Join(dir, name+".feature.d")

	// Validate both definitions *before* anything destructive runs.
	// DisableFeature{Now} removes images and links described by whatever
	// transfer currently claims this feature, so discovering a foreign
	// transfer — or a path we refuse to manage, such as a symlink —
	// afterwards would mean having already destroyed state belonging to a
	// definition we then decline to delete.
	featureFile := filepath.Join(dir, name+".feature")
	if _, err := managedFileExists(featureFile); err != nil {
		return nil, fmt.Errorf("cannot determine whether %s exists: %w", featureFile, err)
	}
	transferExists, err := managedFileExists(transferFile)
	if err != nil {
		return nil, fmt.Errorf("cannot determine whether %s exists: %w", transferFile, err)
	}
	if transferExists {
		if owner, ok := catalog.GeneratedFileRepo(transferFile); !ok || owner != repo.Name {
			return nil, fmt.Errorf(
				"refusing to remove %q: %s was not generated by catalog %q; disable it with 'updex features disable %s --now' and remove the files manually",
				name, transferFile, repo.Name, name)
		}
	}

	result := &CatalogRemoveResult{
		Name:      name,
		Repo:      repo.Name,
		Component: repo.Component,
		DryRun:    opts.DryRun,
	}

	disableResult, err := c.DisableFeature(ctx, name, DisableFeatureOptions{
		Now:       true,
		Force:     opts.Force,
		DryRun:    opts.DryRun,
		NoRefresh: opts.NoRefresh,
		Component: repo.Component,
	})
	result.Disable = disableResult
	if err != nil {
		return result, err
	}

	// Only updex's own drop-in is removed: anything else in the
	// <name>.feature.d directory (e.g. an administrator's 50-local.conf)
	// belongs to the operator and outlives the catalog sysext.
	paths := []string{
		transferFile,
		featureFile,
		filepath.Join(dropInDir, updexDropInName),
	}
	for _, path := range paths {
		exists, err := managedFileExists(path)
		if err != nil {
			return result, fmt.Errorf("cannot determine whether %s exists: %w", path, err)
		}
		if !exists {
			continue
		}
		if opts.DryRun {
			c.msg("Would remove %s", path)
			result.RemovedFiles = append(result.RemovedFiles, path+" (would remove)")
			continue
		}
		if err := os.Remove(path); err != nil {
			return result, fmt.Errorf("failed to remove %s: %w", path, err)
		}
		result.RemovedFiles = append(result.RemovedFiles, path)
	}

	if !opts.DryRun {
		// Drop the drop-in and component directories only when nothing else
		// lives in them; os.Remove refuses to delete non-empty directories.
		_ = os.Remove(dropInDir)
		_ = os.Remove(dir)
		c.msg("Removed %s/%s", repo.Name, name)
	}

	return result, nil
}
