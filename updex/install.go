package updex

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/frostyard/updex/config"
	"github.com/frostyard/updex/download"
	"github.com/frostyard/updex/manifest"
	"github.com/frostyard/updex/sysext"
	"github.com/frostyard/updex/version"
)

// installTransfer performs the update/install logic for a single transfer.
// It returns the version selected, the resolved manifest, whether a download occurred, and any error.
// If opts.CachedManifest is non-nil, it is used instead of fetching the manifest over HTTP.
func (c *Client) installTransfer(ctx context.Context, transfer *config.Transfer, opts installTransferOptions) (string, *manifest.Manifest, bool, error) {
	// Get available versions (applies MinVersion filter)
	available, m, patterns, err := c.getAvailableVersions(ctx, transfer, opts.CachedManifest)
	if err != nil {
		return "", nil, false, fmt.Errorf("failed to get available versions: %w", err)
	}

	if len(available) == 0 {
		return "", nil, false, fmt.Errorf("no versions available")
	}

	// Sort and get newest
	version.Sort(available)
	versionToInstall := available[0]
	c.debug("selected version %s (from %d available)", versionToInstall, len(available))

	// Check if already installed and current
	installed, current, err := sysext.GetInstalledVersionsAt(transfer, c.paths.sysextLinkDir)
	if err != nil {
		return "", nil, false, fmt.Errorf("failed to inspect installed versions: %w", err)
	}
	if !opts.DryRun && transfer.Target.CurrentSymlink != "" {
		if err := sysext.RemoveLegacyCurrentSymlinkAt(transfer, c.paths.sysextLinkDir); err != nil {
			c.warn("failed to remove legacy symlink for %s: %v", transfer.Component, err)
		}
	}
	for _, v := range installed {
		if v == versionToInstall && v == current {
			// The image is staged and current, but the systemd-sysext link
			// may be missing, dangling, or pointing at another image (a
			// crashed earlier run, a hand-edited link dir). Restore it
			// without re-downloading; a correct link is left untouched.
			if !opts.DryRun {
				linked, err := sysext.LinkIsCurrentAt(transfer, c.paths.sysextLinkDir)
				if err != nil {
					return "", nil, false, fmt.Errorf("failed to inspect sysext link: %w", err)
				}
				if !linked {
					if err := c.linkToSysext(transfer); err != nil {
						return "", nil, false, err
					}
					c.msg("restored sysext link for %s", transfer.Component)
				}
			}
			return versionToInstall, m, false, nil
		}
	}

	// Find the file for this version using patterns already parsed by getAvailableVersions
	var sourceFile string
	var expectedHash string
	for filename, hash := range m.Files {
		if v, _, ok := version.ExtractVersionParsed(filename, patterns); ok && v == versionToInstall {
			sourceFile = filename
			expectedHash = hash
			break
		}
	}

	if sourceFile == "" {
		return "", nil, false, fmt.Errorf("no file found for version %s", versionToInstall)
	}

	targetFile, err := buildTargetFilename(transfer.Target.Patterns(), versionToInstall)
	if err != nil {
		return "", nil, false, err
	}
	targetPath := filepath.Join(transfer.Target.Path, targetFile)

	// Download
	downloadURL := transfer.Source.Path + "/" + sourceFile
	if opts.DryRun {
		c.debug("would download %s → %s", downloadURL, targetPath)
		return versionToInstall, m, true, nil
	}

	c.debug("downloading %s → %s", downloadURL, targetPath)
	err = download.Download(ctx, c.httpClient, downloadURL, targetPath, expectedHash, transfer.Target.Mode, c.config.OnDownloadProgress, download.WithRetryNotify(c.retryNotify("download")))
	if err != nil {
		return "", nil, false, fmt.Errorf("download failed: %w", err)
	}

	if err := c.linkToSysext(transfer); err != nil {
		return "", nil, false, err
	}

	// Refresh systemd-sysext. Both SDK callers batch this with NoRefresh:
	// true; when a caller does ask for it, a failure is returned (the image
	// is installed and linked, so versionToInstall is still reported) rather
	// than swallowed, matching the batched refresh in UpdateFeatures.
	var refreshErr error
	if !opts.NoRefresh {
		if err := c.runner.Refresh(); err != nil {
			refreshErr = fmt.Errorf("sysext refresh failed: %w", err)
			c.warn("%s", refreshErr)
		}
	}

	// Run vacuum
	if !opts.NoVacuum {
		if err := sysext.VacuumAt(transfer, c.paths.sysextLinkDir); err != nil {
			c.warn("vacuum failed: %v", err)
		}
	}

	return versionToInstall, m, true, refreshErr
}

// linkToSysext points the systemd-sysext link for transfer at its newest
// staged image through the client's runner, in the client's link directory
// when the runner supports one.
func (c *Client) linkToSysext(transfer *config.Transfer) error {
	var linkErr error
	if runner, ok := c.runner.(sysext.PathSysextRunner); ok {
		linkErr = runner.LinkToSysextAt(transfer, c.paths.sysextLinkDir)
	} else {
		// Preserve compatibility with injected runners that implement the
		// original SysextRunner interface.
		linkErr = c.runner.LinkToSysext(transfer)
	}
	if linkErr != nil {
		return fmt.Errorf("failed to link to sysext: %w", linkErr)
	}
	return nil
}

// buildTargetFilename derives the installed filename for a version from the
// target match patterns. Downloads are always stored decompressed, so it
// prefers the first pattern whose name carries no compression suffix; if every
// pattern is a compressed variant, it strips the suffix from the first one so
// the on-disk name matches the actual content.
func buildTargetFilename(targetPatterns []string, ver string) (string, error) {
	var fallback string
	var firstErr error
	for _, patternStr := range targetPatterns {
		p, err := version.ParsePattern(patternStr)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		name := p.BuildFilename(ver)
		if err := validateTargetFilename(name); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if stripped := download.StripCompressionSuffix(name); stripped == name {
			return name, nil
		} else if fallback == "" {
			fallback = stripped
		}
	}
	if fallback != "" {
		return fallback, nil
	}
	if firstErr != nil {
		return "", fmt.Errorf("invalid target pattern: %w", firstErr)
	}
	return "", fmt.Errorf("no target pattern configured")
}

// validateTargetFilename rejects a filename built from a runtime-substituted
// version (config.Transfer.Target.Patterns' @v placeholder) that would escape
// Target.Path when joined with filepath.Join in installTransfer: "." or "..",
// any path separator, or a name whose Base differs from itself (e.g. an
// absolute path). This mirrors catalog/catalog.go's validateCatalogPatterns,
// which validates pattern *literals* configured by trusted operators; this
// check instead covers the version string substituted in at runtime from an
// untrusted manifest.
func validateTargetFilename(name string) error {
	if name == "." || name == ".." || strings.ContainsAny(name, "/\\") || filepath.Base(name) != name {
		return fmt.Errorf("invalid target filename %q: must not contain path separators or traverse directories", name)
	}
	return nil
}
