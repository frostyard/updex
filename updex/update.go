package updex

import (
	"context"
	"fmt"

	"github.com/frostyard/updex/internal/download"
	"github.com/frostyard/updex/internal/manifest"
	"github.com/frostyard/updex/internal/sysext"
	"github.com/frostyard/updex/internal/version"
)

// Update downloads and installs new versions.
// Returns results for all components that were processed.
func (c *Client) Update(ctx context.Context, opts UpdateOptions) ([]UpdateResult, error) {
	c.helper.BeginAction("Update extensions")
	defer c.helper.EndAction()

	transfers, err := c.loadTransfers(opts.Component)
	if err != nil {
		return nil, err
	}

	if len(transfers) == 0 {
		return nil, fmt.Errorf("no transfer configurations found")
	}

	var results []UpdateResult
	var hasErrors bool

	for _, transfer := range transfers {
		c.helper.BeginTask(fmt.Sprintf("Processing %s", transfer.Component))

		result := UpdateResult{
			Component: transfer.Component,
		}

		// Get available versions
		available, err := c.getAvailableVersions(transfer)
		if err != nil {
			result.Error = fmt.Sprintf("failed to get available versions: %v", err)
			c.helper.Warning(result.Error)
			results = append(results, result)
			hasErrors = true
			c.helper.EndTask()
			continue
		}

		if len(available) == 0 {
			result.Error = "no versions available"
			c.helper.Warning(result.Error)
			results = append(results, result)
			hasErrors = true
			c.helper.EndTask()
			continue
		}

		// Determine which version to install
		version.Sort(available)
		versionToInstall := available[0] // newest

		if opts.Version != "" {
			found := false
			for _, v := range available {
				if v == opts.Version {
					versionToInstall = v
					found = true
					break
				}
			}
			if !found {
				result.Error = fmt.Sprintf("version %s not found", opts.Version)
				c.helper.Warning(result.Error)
				results = append(results, result)
				hasErrors = true
				c.helper.EndTask()
				continue
			}
		}

		result.Version = versionToInstall

		// Check if already installed
		installed, current, _ := sysext.GetInstalledVersions(transfer)
		alreadyInstalled := false
		for _, v := range installed {
			if v == versionToInstall {
				alreadyInstalled = true
				break
			}
		}

		if alreadyInstalled && versionToInstall == current {
			result.Installed = true
			c.helper.Info(fmt.Sprintf("Version %s already installed and current", versionToInstall))
			results = append(results, result)
			c.helper.EndTask()
			continue
		}

		// Fetch manifest for download
		m, err := manifest.Fetch(transfer.Source.Path, c.config.Verify || transfer.Transfer.Verify)
		if err != nil {
			result.Error = fmt.Sprintf("failed to fetch manifest: %v", err)
			c.helper.Warning(result.Error)
			results = append(results, result)
			hasErrors = true
			c.helper.EndTask()
			continue
		}

		// Get all patterns
		patterns := transfer.Source.MatchPatterns
		if len(patterns) == 0 && transfer.Source.MatchPattern != "" {
			patterns = []string{transfer.Source.MatchPattern}
		}

		// Find the file for this version using any pattern
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
			result.Error = fmt.Sprintf("no file found for version %s", versionToInstall)
			c.helper.Warning(result.Error)
			results = append(results, result)
			hasErrors = true
			c.helper.EndTask()
			continue
		}

		// Build target path using first target pattern
		targetPatterns := transfer.Target.MatchPatterns
		if len(targetPatterns) == 0 && transfer.Target.MatchPattern != "" {
			targetPatterns = []string{transfer.Target.MatchPattern}
		}

		targetPattern, err := version.ParsePattern(targetPatterns[0])
		if err != nil {
			result.Error = fmt.Sprintf("invalid target pattern: %v", err)
			c.helper.Warning(result.Error)
			results = append(results, result)
			hasErrors = true
			c.helper.EndTask()
			continue
		}

		targetFile := targetPattern.BuildFilename(versionToInstall)
		targetPath := fmt.Sprintf("%s/%s", transfer.Target.Path, targetFile)

		// Download
		c.helper.Info(fmt.Sprintf("Downloading version %s", versionToInstall))
		downloadURL := transfer.Source.Path + "/" + sourceFile
		err = download.Download(downloadURL, targetPath, expectedHash, transfer.Target.Mode)
		if err != nil {
			result.Error = fmt.Sprintf("download failed: %v", err)
			c.helper.Warning(result.Error)
			results = append(results, result)
			hasErrors = true
			c.helper.EndTask()
			continue
		}

		result.Downloaded = true
		result.Installed = true
		result.NextActionMessage = "Reboot required to activate changes"

		// Update symlink if configured
		if transfer.Target.CurrentSymlink != "" {
			err = sysext.UpdateSymlink(transfer.Target.Path, transfer.Target.CurrentSymlink, targetFile)
			if err != nil {
				c.helper.Warning(fmt.Sprintf("failed to update symlink: %v", err))
			}
		}

		// Link to /var/lib/extensions for systemd-sysext
		if err := sysext.LinkToSysext(transfer); err != nil {
			c.helper.Warning(fmt.Sprintf("failed to link to sysext: %v", err))
		}

		c.helper.Info(fmt.Sprintf("Installed version %s", versionToInstall))
		results = append(results, result)

		// Run vacuum unless disabled
		if !opts.NoVacuum {
			if err := sysext.Vacuum(transfer); err != nil {
				c.helper.Warning(fmt.Sprintf("vacuum failed: %v", err))
			}
		}

		c.helper.EndTask()
	}

	// Refresh systemd-sysext to pick up all changes (unless --no-refresh)
	if !opts.NoRefresh {
		if err := sysext.Refresh(); err != nil {
			c.helper.Warning(fmt.Sprintf("sysext refresh failed: %v", err))
		}
	} else {
		c.helper.Info("Skipping sysext refresh (--no-refresh)")
	}

	if hasErrors {
		return results, fmt.Errorf("one or more components failed to update")
	}
	return results, nil
}
