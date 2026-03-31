package updex

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/frostyard/updex/config"
	"github.com/frostyard/updex/manifest"
	"github.com/frostyard/updex/sysext"
	"github.com/frostyard/updex/version"
)

// Features returns all configured features with their status.
func (c *Client) Features(ctx context.Context) ([]FeatureInfo, error) {
	c.msg("Loading configurations")

	features, err := config.LoadFeatures(c.config.Definitions)
	if err != nil {
		return nil, fmt.Errorf("failed to load features: %w", err)
	}

	if len(features) == 0 {
		c.msg("No features configured")
		return []FeatureInfo{}, nil
	}

	// Load transfers to show which belong to each feature
	transfers, err := config.LoadTransfers(c.config.Definitions)
	if err != nil {
		return nil, fmt.Errorf("failed to load transfers: %w", err)
	}

	var featureInfos []FeatureInfo

	for _, f := range features {
		// Get transfers associated with this feature
		featureTransfers := config.GetTransfersForFeature(transfers, f.Name)
		var transferNames []string
		for _, t := range featureTransfers {
			transferNames = append(transferNames, t.Component)
		}

		info := FeatureInfo{
			Name:          f.Name,
			Description:   f.Description,
			Documentation: f.Documentation,
			Enabled:       f.Enabled,
			Masked:        f.Masked,
			Source:        f.FilePath,
			Transfers:     transferNames,
		}
		featureInfos = append(featureInfos, info)
	}

	c.msg("Found %d feature(s)", len(featureInfos))

	return featureInfos, nil
}

// findFeature loads all features and returns the one matching name. It returns
// an error if the feature is not found or is masked. The action parameter
// (e.g. "enabled", "disabled") is used in the masked error message.
func (c *Client) findFeature(name, action string) (*config.Feature, error) {
	features, err := config.LoadFeatures(c.config.Definitions)
	if err != nil {
		return nil, fmt.Errorf("failed to load features: %w", err)
	}

	for _, f := range features {
		if f.Name == name {
			if f.Masked {
				return nil, fmt.Errorf("feature '%s' is masked and cannot be %s", name, action)
			}
			return f, nil
		}
	}

	return nil, fmt.Errorf("feature '%s' not found", name)
}

// loadFeatureTransfers loads all transfers and returns those associated with
// the given feature name.
func (c *Client) loadFeatureTransfers(name string) ([]*config.Transfer, error) {
	transfers, err := config.LoadTransfers(c.config.Definitions)
	if err != nil {
		return nil, fmt.Errorf("failed to load transfers: %w", err)
	}

	return config.GetTransfersForFeature(transfers, name), nil
}

// writeFeatureDropIn creates a drop-in configuration file that sets a
// feature's enabled state. In dry-run mode it only logs what would happen
// and returns the path without writing anything.
func (c *Client) writeFeatureDropIn(name string, enabled bool, dryRun bool) (string, error) {
	dropInDir := filepath.Join("/etc/sysupdate.d", name+".feature.d")
	dropInFile := filepath.Join(dropInDir, "00-updex.conf")

	if dryRun {
		c.msg("Would create drop-in: %s", dropInFile)
		return dropInFile, nil
	}

	if err := os.MkdirAll(dropInDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create drop-in directory: %w", err)
	}

	content := fmt.Sprintf("[Feature]\nEnabled=%v\n", enabled)
	if err := os.WriteFile(dropInFile, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("failed to write drop-in file: %w", err)
	}

	c.msg("Created drop-in: %s", dropInFile)
	return dropInFile, nil
}

// EnableFeature enables a feature by creating a drop-in configuration file.
func (c *Client) EnableFeature(ctx context.Context, name string, opts EnableFeatureOptions) (*FeatureActionResult, error) {
	c.msg("Enabling %s", name)

	result := &FeatureActionResult{
		Feature: name,
		Action:  "enable",
		DryRun:  opts.DryRun,
	}

	// Verify the feature exists and is not masked
	if _, err := c.findFeature(name, "enabled"); err != nil {
		result.Error = err.Error()
		c.warn("%s", result.Error)
		return result, err
	}

	// Create drop-in directory and file
	dropInFile, err := c.writeFeatureDropIn(name, true, opts.DryRun)
	if err != nil {
		result.Error = err.Error()
		c.warn("%s", result.Error)
		return result, err
	}
	if !opts.DryRun {
		result.DropIn = dropInFile
	}

	// Handle --now flag: download extensions immediately
	if opts.Now {
		c.msg("Downloading extensions")

		featureTransfers, err := c.loadFeatureTransfers(name)
		if err != nil {
			result.Error = err.Error()
			c.warn("%s", result.Error)
			return result, err
		}

		if len(featureTransfers) == 0 {
			c.msg("No transfers associated with this feature")
		} else {
			for _, transfer := range featureTransfers {
				c.msg("Processing %s", transfer.Component)

				if opts.DryRun {
					c.msg("Would download: %s", transfer.Component)
					result.DownloadedFiles = append(result.DownloadedFiles, transfer.Component+" (would download)")
				} else {
					// Use installTransfer which handles all the download logic
					version, _, downloaded, err := c.installTransfer(ctx, transfer, installTransferOptions{
						NoRefresh: true, // refresh is batched at the end
					})
					if err != nil {
						err = fmt.Errorf("failed to download %s: %w", transfer.Component, err)
						result.Error = err.Error()
						c.warn("%s", result.Error)
						return result, err
					}
					if downloaded {
						result.DownloadedFiles = append(result.DownloadedFiles, fmt.Sprintf("%s@%s", transfer.Component, version))
						c.msg("Downloaded %s version %s", transfer.Component, version)
					} else {
						c.msg("Version %s already installed and current for %s", version, transfer.Component)
					}
				}
			}

			// Refresh if we downloaded (unless --no-refresh or --dry-run)
			if !opts.NoRefresh && !opts.DryRun {
				c.msg("Refreshing sysext")
				if err := c.runner.Refresh(); err != nil {
					c.warn("sysext refresh failed: %v", err)
				}
			}
		}
	}

	result.Success = true

	// Set appropriate NextActionMessage
	if opts.DryRun {
		result.NextActionMessage = fmt.Sprintf("Dry run complete. Would enable feature '%s'", name)
		if opts.Now {
			result.NextActionMessage += " and download extensions"
		}
	} else if opts.Now && len(result.DownloadedFiles) > 0 {
		result.NextActionMessage = fmt.Sprintf("Feature '%s' enabled and %d extension(s) downloaded", name, len(result.DownloadedFiles))
	} else {
		result.NextActionMessage = fmt.Sprintf("Feature '%s' enabled. Run 'updex features update' to download extensions.", name)
	}

	return result, nil
}

// DisableFeature disables a feature by creating a drop-in configuration file.
func (c *Client) DisableFeature(ctx context.Context, name string, opts DisableFeatureOptions) (*FeatureActionResult, error) {
	c.msg("Disabling %s", name)

	result := &FeatureActionResult{
		Feature: name,
		Action:  "disable",
		DryRun:  opts.DryRun,
	}

	// Verify the feature exists and is not masked
	if _, err := c.findFeature(name, "disabled"); err != nil {
		result.Error = err.Error()
		c.warn("%s", result.Error)
		return result, err
	}

	// Load transfers for this feature (needed for merge state check and file removal)
	featureTransfers, err := c.loadFeatureTransfers(name)
	if err != nil {
		result.Error = err.Error()
		c.warn("%s", result.Error)
		return result, err
	}

	willRemoveFiles := opts.Now

	// Check merge state BEFORE any destructive operations
	if willRemoveFiles && len(featureTransfers) > 0 {
		var mergedExtensions []string
		for _, t := range featureTransfers {
			activeVersion, err := sysext.GetActiveVersion(t)
			if err != nil {
				c.warn("could not check merge state for %s: %v", t.Component, err)
				continue
			}
			if activeVersion != "" {
				mergedExtensions = append(mergedExtensions, fmt.Sprintf("%s (version %s)", t.Component, activeVersion))
			}
		}

		if len(mergedExtensions) > 0 && !opts.Force {
			var errMsg string
			if len(mergedExtensions) == 1 {
				errMsg = fmt.Sprintf("Extension %s is active. Removing requires --force and a reboot to take effect.", mergedExtensions[0])
			} else {
				errMsg = fmt.Sprintf("Extensions are active: %v. Removing requires --force and a reboot to take effect.", mergedExtensions)
			}
			result.Error = errMsg
			c.warn("%s", errMsg)
			return result, errors.New(errMsg)
		}

		if len(mergedExtensions) > 0 && opts.Force {
			c.warn("Extensions are currently active. Changes will take effect after reboot.")
		}
	}

	// Create drop-in directory and file
	dropInFile, err := c.writeFeatureDropIn(name, false, opts.DryRun)
	if err != nil {
		result.Error = err.Error()
		c.warn("%s", result.Error)
		return result, err
	}
	if !opts.DryRun {
		result.DropIn = dropInFile
	}

	// Handle --now (or --remove for backward compat): remove files and unmerge
	if willRemoveFiles && len(featureTransfers) > 0 {
		// If --now is specified, unmerge first (unless dry-run)
		if opts.Now && !opts.DryRun {
			c.msg("Unmerging extensions")
			if err := c.runner.Unmerge(); err != nil {
				err = fmt.Errorf("failed to unmerge: %w", err)
				result.Error = err.Error()
				c.warn("%s", result.Error)
				return result, err
			}
			result.Unmerged = true
		} else if opts.Now && opts.DryRun {
			c.msg("Would unmerge extensions")
		}

		// Remove files for each transfer
		c.msg("Removing files")
		var allRemoved []string
		for _, t := range featureTransfers {
			if opts.DryRun {
				c.msg("Would remove files for: %s", t.Component)
				allRemoved = append(allRemoved, t.Component+" (would remove)")
			} else {
				// Remove the symlink from /var/lib/extensions
				if err := sysext.UnlinkFromSysext(t); err != nil {
					c.warn("failed to unlink %s: %v", t.Component, err)
				}

				// Remove all versions
				removed, err := sysext.RemoveAllVersions(t)
				if err != nil {
					err = fmt.Errorf("failed to remove files for %s: %w", t.Component, err)
					result.Error = err.Error()
					c.warn("%s", result.Error)
					return result, err
				}
				allRemoved = append(allRemoved, removed...)
			}
		}
		result.RemovedFiles = allRemoved
		if !opts.DryRun {
			c.msg("Removed %d file(s)", len(allRemoved))
		}

		// Refresh if we unmerged (unless --no-refresh or --dry-run)
		if opts.Now && !opts.NoRefresh && !opts.DryRun {
			c.msg("Refreshing sysext")
			if err := c.runner.Refresh(); err != nil {
				c.warn("sysext refresh failed: %v", err)
			}
		}
	}

	result.Success = true

	// Set the next action message based on what was done
	if opts.DryRun {
		result.NextActionMessage = fmt.Sprintf("Dry run complete. Would disable feature '%s'", name)
		if willRemoveFiles {
			result.NextActionMessage += " and remove extension files"
		}
	} else if willRemoveFiles && opts.Force {
		result.NextActionMessage = fmt.Sprintf("Feature '%s' disabled and files removed. Reboot required for changes to take effect.", name)
	} else if willRemoveFiles {
		result.NextActionMessage = fmt.Sprintf("Feature '%s' disabled and %d extension file(s) removed.", name, len(result.RemovedFiles))
	} else {
		result.NextActionMessage = fmt.Sprintf("Feature '%s' disabled. Run 'updex features update' to apply changes.", name)
	}

	return result, nil
}

// UpdateFeatures downloads and installs new versions for all enabled features.
func (c *Client) UpdateFeatures(ctx context.Context, opts UpdateFeaturesOptions) ([]UpdateFeaturesResult, error) {
	features, err := config.LoadFeatures(c.config.Definitions)
	if err != nil {
		return nil, fmt.Errorf("failed to load features: %w", err)
	}

	transfers, err := config.LoadTransfers(c.config.Definitions)
	if err != nil {
		return nil, fmt.Errorf("failed to load transfers: %w", err)
	}

	var allResults []UpdateFeaturesResult
	var hasErrors bool

	// Cache manifests by source URL to avoid redundant HTTP requests
	// when multiple transfers share the same source.
	manifestCache := make(map[string]*manifest.Manifest)

	for _, f := range features {
		if !f.Enabled || f.Masked {
			continue
		}

		featureTransfers := config.GetTransfersForFeature(transfers, f.Name)
		if len(featureTransfers) == 0 {
			continue
		}

		featureResult := UpdateFeaturesResult{
			Feature: f.Name,
		}

		for _, transfer := range featureTransfers {
			c.msg("Processing %s/%s", f.Name, transfer.Component)

			result := UpdateResult{
				Component: transfer.Component,
			}

			v, m, downloaded, err := c.installTransfer(ctx, transfer, installTransferOptions{
				NoVacuum:       opts.NoVacuum,
				NoRefresh:      true, // refresh is batched at the end
				CachedManifest: manifestCache[transfer.Source.Path],
			})
			if m != nil {
				manifestCache[transfer.Source.Path] = m
			}
			if err != nil {
				result.Error = err.Error()
				c.warn("%s", result.Error)
				featureResult.Results = append(featureResult.Results, result)
				hasErrors = true
				continue
			}

			result.Version = v
			if downloaded {
				result.Downloaded = true
				result.Installed = true
				result.NextActionMessage = "Reboot required to activate changes"
				c.msg("Installed version %s", v)
			} else {
				result.Installed = true
				c.msg("Version %s already installed and current", v)
			}

			featureResult.Results = append(featureResult.Results, result)
		}

		allResults = append(allResults, featureResult)
	}

	if !opts.NoRefresh {
		if err := c.runner.Refresh(); err != nil {
			c.warn("sysext refresh failed: %v", err)
		}
	} else {
		c.msg("Skipping sysext refresh (--no-refresh)")
	}

	if hasErrors {
		return allResults, fmt.Errorf("one or more components failed to update")
	}
	return allResults, nil
}

// CheckFeatures checks if newer versions are available for all enabled features.
func (c *Client) CheckFeatures(ctx context.Context, opts CheckFeaturesOptions) ([]CheckFeaturesResult, error) {
	features, err := config.LoadFeatures(c.config.Definitions)
	if err != nil {
		return nil, fmt.Errorf("failed to load features: %w", err)
	}

	transfers, err := config.LoadTransfers(c.config.Definitions)
	if err != nil {
		return nil, fmt.Errorf("failed to load transfers: %w", err)
	}

	var allResults []CheckFeaturesResult

	// Cache manifests by source URL to avoid redundant HTTP requests
	// when multiple transfers share the same source.
	manifestCache := make(map[string]*manifest.Manifest)

	for _, f := range features {
		if !f.Enabled || f.Masked {
			continue
		}

		featureTransfers := config.GetTransfersForFeature(transfers, f.Name)
		if len(featureTransfers) == 0 {
			continue
		}

		featureResult := CheckFeaturesResult{
			Feature: f.Name,
		}

		for _, transfer := range featureTransfers {
			c.msg("Checking %s/%s", f.Name, transfer.Component)

			available, m, _, err := c.getAvailableVersions(ctx, transfer, manifestCache[transfer.Source.Path])
			if m != nil {
				manifestCache[transfer.Source.Path] = m
			}
			if err != nil {
				c.warn("failed to get available versions: %v", err)
				continue
			}

			if len(available) == 0 {
				continue
			}

			version.Sort(available)
			newest := available[0]

			installed, current, err := sysext.GetInstalledVersions(transfer)
			if err != nil {
				c.warn("failed to get installed versions: %v", err)
			}

			result := CheckResult{
				Component:      transfer.Component,
				CurrentVersion: current,
				NewestVersion:  newest,
			}

			if len(installed) == 0 {
				result.UpdateAvailable = true
				c.msg("New version available: %s", newest)
			} else if version.Compare(newest, current) > 0 {
				result.UpdateAvailable = true
				c.msg("Update available: %s → %s", current, newest)
			} else {
				c.msg("Up to date: %s", current)
			}

			featureResult.Results = append(featureResult.Results, result)
		}

		allResults = append(allResults, featureResult)
	}

	return allResults, nil
}
