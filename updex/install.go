package updex

import (
	"context"
	"fmt"
	"path/filepath"

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
	available, m, err := c.getAvailableVersions(ctx, transfer, opts.CachedManifest)
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
	installed, current, _ := sysext.GetInstalledVersions(transfer)
	for _, v := range installed {
		if v == versionToInstall && v == current {
			return versionToInstall, m, false, nil
		}
	}

	// Find the file for this version
	patternStrs := transfer.Source.Patterns()
	patterns, firstErr := version.ParsePatterns(patternStrs)
	if len(patterns) == 0 && firstErr != nil {
		return "", nil, false, fmt.Errorf("invalid source pattern: %w", firstErr)
	}

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

	// Build target path using first target pattern
	targetPatterns := transfer.Target.Patterns()

	targetPattern, err := version.ParsePattern(targetPatterns[0])
	if err != nil {
		return "", nil, false, fmt.Errorf("invalid target pattern: %w", err)
	}

	targetFile := targetPattern.BuildFilename(versionToInstall)
	targetPath := filepath.Join(transfer.Target.Path, targetFile)

	// Download
	downloadURL := transfer.Source.Path + "/" + sourceFile
	c.debug("downloading %s → %s", downloadURL, targetPath)
	err = download.Download(ctx, c.httpClient, downloadURL, targetPath, expectedHash, transfer.Target.Mode, c.config.OnDownloadProgress)
	if err != nil {
		return "", nil, false, fmt.Errorf("download failed: %w", err)
	}

	// Update symlink if configured
	if transfer.Target.CurrentSymlink != "" {
		err = sysext.UpdateSymlink(transfer.Target.Path, transfer.Target.CurrentSymlink, targetFile)
		if err != nil {
			c.warn("failed to update symlink: %v", err)
		}
	}

	// Link to /var/lib/extensions for systemd-sysext
	if err := sysext.LinkToSysext(transfer); err != nil {
		c.warn("failed to link to sysext: %v", err)
	}

	// Refresh systemd-sysext
	if !opts.NoRefresh {
		if err := c.runner.Refresh(); err != nil {
			c.warn("sysext refresh failed: %v", err)
		}
	}

	// Run vacuum
	if !opts.NoVacuum {
		if err := sysext.Vacuum(transfer); err != nil {
			c.warn("vacuum failed: %v", err)
		}
	}

	return versionToInstall, m, true, nil
}
