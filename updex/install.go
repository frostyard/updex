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
func (c *Client) installTransfer(ctx context.Context, transfer *config.Transfer, noRefresh bool) (string, error) {
	// Get available versions
	m, err := manifest.Fetch(ctx, transfer.Source.Path, c.config.Verify || transfer.Transfer.Verify)
	if err != nil {
		return "", fmt.Errorf("failed to fetch manifest: %w", err)
	}

	// Get all patterns
	patterns := transfer.Source.Patterns()
	c.debug("source patterns: %v", patterns)

	// Find available versions using all patterns
	versionSet := make(map[string]bool)
	for filename := range m.Files {
		if v, _, ok := version.ExtractVersionMulti(filename, patterns); ok {
			versionSet[v] = true
		}
	}

	if len(versionSet) == 0 {
		return "", fmt.Errorf("no versions available")
	}

	available := make([]string, 0, len(versionSet))
	for v := range versionSet {
		available = append(available, v)
	}

	// Sort and get newest
	version.Sort(available)
	versionToInstall := available[0]
	c.debug("selected version %s (from %d available)", versionToInstall, len(available))

	// Check if already installed
	installed, current, _ := sysext.GetInstalledVersions(transfer)
	for _, v := range installed {
		if v == versionToInstall && v == current {
			return versionToInstall, nil // Already installed and current
		}
	}

	// Find the file for this version
	var sourceFile string
	var expectedHash string
	for filename, hash := range m.Files {
		if v, _, ok := version.ExtractVersionMulti(filename, patterns); ok && v == versionToInstall {
			sourceFile = filename
			expectedHash = hash
			break
		}
	}

	if sourceFile == "" {
		return "", fmt.Errorf("no file found for version %s", versionToInstall)
	}

	// Build target path using first target pattern
	targetPatterns := transfer.Target.Patterns()

	targetPattern, err := version.ParsePattern(targetPatterns[0])
	if err != nil {
		return "", fmt.Errorf("invalid target pattern: %w", err)
	}

	targetFile := targetPattern.BuildFilename(versionToInstall)
	targetPath := filepath.Join(transfer.Target.Path, targetFile)

	// Download
	downloadURL := transfer.Source.Path + "/" + sourceFile
	c.debug("downloading %s → %s", downloadURL, targetPath)
	err = download.Download(ctx, downloadURL, targetPath, expectedHash, transfer.Target.Mode)
	if err != nil {
		return "", fmt.Errorf("download failed: %w", err)
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

	// Refresh systemd-sysext (unless --no-refresh)
	if !noRefresh {
		if err := sysext.Refresh(); err != nil {
			c.warn("sysext refresh failed: %v", err)
		}
	} else {
		c.msg("Skipping sysext refresh (--no-refresh)")
	}

	// Run vacuum
	if err := sysext.Vacuum(transfer); err != nil {
		c.warn("vacuum failed: %v", err)
	}

	return versionToInstall, nil
}
