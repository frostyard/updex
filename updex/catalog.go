package updex

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	repos, err := catalog.LoadRepos()
	if errors.Is(err, catalog.ErrNoCatalogs) {
		return nil, fmt.Errorf("no catalogs configured: create a <name>.catalog file under %s (see the Catalog section of the README)", catalog.ConfigRoots[0])
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
		names, err := catalog.List(ctx, c.httpClient, repo)
		if err != nil {
			return nil, err
		}

		features, err := config.LoadComponentFeatures(repo.Component)
		if err != nil {
			return nil, fmt.Errorf("failed to load features for component %q: %w", repo.Component, err)
		}

		for _, name := range names {
			if opts.Search != "" && !strings.Contains(name, opts.Search) {
				continue
			}
			entry := CatalogEntry{Name: name, Repo: repo.Name}
			for _, f := range features {
				if f.Name == name {
					entry.Installed = true
					entry.Enabled = f.Enabled && !f.Masked
					break
				}
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

	transferData, err := catalog.RenderTransfer(conf, name)
	if err != nil {
		return nil, err
	}
	featureData := catalog.RenderFeature(repo, name)

	dir := config.EtcComponentDir(repo.Component)
	result := &CatalogAddResult{
		Name:         name,
		Repo:         repo.Name,
		Component:    repo.Component,
		TransferFile: filepath.Join(dir, name+".transfer"),
		FeatureFile:  filepath.Join(dir, name+".feature"),
		DryRun:       opts.DryRun,
	}

	if opts.DryRun {
		c.msg("Would write %s", result.TransferFile)
		c.msg("Would write %s", result.FeatureFile)
		c.msg("Would enable feature %q and download extensions", name)
		return result, nil
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create component directory: %w", err)
	}
	if err := os.WriteFile(result.TransferFile, transferData, 0644); err != nil {
		return nil, fmt.Errorf("failed to write transfer file: %w", err)
	}
	if err := os.WriteFile(result.FeatureFile, featureData, 0644); err != nil {
		return nil, fmt.Errorf("failed to write feature file: %w", err)
	}
	c.msg("Added %s/%s to %s", repo.Name, name, dir)

	enableResult, err := c.EnableFeature(ctx, name, EnableFeatureOptions{
		Now:       true,
		NoRefresh: opts.NoRefresh,
		Component: repo.Component,
	})
	result.Enable = enableResult
	if err != nil {
		return result, err
	}

	return result, nil
}

// CatalogRemove undoes CatalogAdd for a sysext: it disables the feature
// with full cleanup (DisableFeature with Now: unmerge, remove downloaded
// images and the /var/lib/extensions link — requiring opts.Force when the
// extension is currently merged) and deletes the generated
// .transfer/.feature files and drop-ins from the repo's /etc component
// directory.
func (c *Client) CatalogRemove(ctx context.Context, name string, opts CatalogRemoveOptions) (*CatalogRemoveResult, error) {
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

	// A sysext is catalog-managed when its feature is defined in a
	// configured repo's component.
	var owners []catalog.Repo
	for _, repo := range repos {
		features, err := config.LoadComponentFeatures(repo.Component)
		if err != nil {
			return nil, fmt.Errorf("failed to load features for component %q: %w", repo.Component, err)
		}
		for _, f := range features {
			if f.Name == name {
				owners = append(owners, repo)
				break
			}
		}
	}

	if len(owners) == 0 {
		return nil, fmt.Errorf("%q is not a catalog-managed sysext", name)
	}
	if len(owners) > 1 {
		var candidates []string
		for _, r := range owners {
			candidates = append(candidates, r.Name+"/"+name)
		}
		return nil, fmt.Errorf("%q is managed by multiple catalogs; specify one of: %s", name, strings.Join(candidates, ", "))
	}
	repo := owners[0]

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

	dir := config.EtcComponentDir(repo.Component)
	paths := []string{
		filepath.Join(dir, name+".transfer"),
		filepath.Join(dir, name+".feature"),
		filepath.Join(dir, name+".feature.d"),
	}
	for _, path := range paths {
		if _, err := os.Stat(path); err != nil {
			continue
		}
		if opts.DryRun {
			c.msg("Would remove %s", path)
			result.RemovedFiles = append(result.RemovedFiles, path+" (would remove)")
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			return result, fmt.Errorf("failed to remove %s: %w", path, err)
		}
		result.RemovedFiles = append(result.RemovedFiles, path)
	}

	if !opts.DryRun {
		// Drop the component directory when nothing else lives there;
		// os.Remove refuses to delete non-empty directories.
		_ = os.Remove(dir)
		c.msg("Removed %s/%s", repo.Name, name)
	}

	return result, nil
}
