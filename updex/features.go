package updex

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/frostyard/updex/internal/config"
	"github.com/frostyard/updex/internal/sysext"
)

// Features returns all configured features with their status.
func (c *Client) Features(ctx context.Context) ([]FeatureInfo, error) {
	c.helper.BeginAction("List features")
	defer c.helper.EndAction()

	c.helper.BeginTask("Loading configurations")

	features, err := config.LoadFeatures(c.config.Definitions)
	if err != nil {
		c.helper.EndTask()
		return nil, fmt.Errorf("failed to load features: %w", err)
	}

	if len(features) == 0 {
		c.helper.Info("No features configured")
		c.helper.EndTask()
		return []FeatureInfo{}, nil
	}

	// Load transfers to show which belong to each feature
	transfers, err := config.LoadTransfers(c.config.Definitions)
	if err != nil {
		c.helper.EndTask()
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

	c.helper.Info(fmt.Sprintf("Found %d feature(s)", len(featureInfos)))
	c.helper.EndTask()

	return featureInfos, nil
}

// EnableFeature enables a feature by creating a drop-in configuration file.
func (c *Client) EnableFeature(ctx context.Context, name string, opts EnableFeatureOptions) (*FeatureActionResult, error) {
	actionName := "Enable feature"
	if opts.DryRun {
		actionName = "Enable feature (dry run)"
	}
	c.helper.BeginAction(actionName)
	defer c.helper.EndAction()

	c.helper.BeginTask(fmt.Sprintf("Enabling %s", name))

	result := &FeatureActionResult{
		Feature: name,
		Action:  "enable",
		DryRun:  opts.DryRun,
	}

	// Verify the feature exists
	features, err := config.LoadFeatures(c.config.Definitions)
	if err != nil {
		result.Error = fmt.Sprintf("failed to load features: %v", err)
		c.helper.Warning(result.Error)
		c.helper.EndTask()
		return result, fmt.Errorf("%s", result.Error)
	}

	found := false
	for _, f := range features {
		if f.Name == name {
			found = true
			if f.Masked {
				result.Error = fmt.Sprintf("feature '%s' is masked and cannot be enabled", name)
				c.helper.Warning(result.Error)
				c.helper.EndTask()
				return result, fmt.Errorf("%s", result.Error)
			}
			break
		}
	}

	if !found {
		result.Error = fmt.Sprintf("feature '%s' not found", name)
		c.helper.Warning(result.Error)
		c.helper.EndTask()
		return result, fmt.Errorf("%s", result.Error)
	}

	// Create drop-in directory and file
	dropInDir := filepath.Join("/etc/sysupdate.d", name+".feature.d")
	dropInFile := filepath.Join(dropInDir, "00-updex.conf")

	if opts.DryRun {
		c.helper.Info(fmt.Sprintf("Would create drop-in: %s", dropInFile))
	} else {
		if err := os.MkdirAll(dropInDir, 0755); err != nil {
			result.Error = fmt.Sprintf("failed to create drop-in directory: %v", err)
			c.helper.Warning(result.Error)
			c.helper.EndTask()
			return result, fmt.Errorf("%s", result.Error)
		}

		content := "[Feature]\nEnabled=true\n"
		if err := os.WriteFile(dropInFile, []byte(content), 0644); err != nil {
			result.Error = fmt.Sprintf("failed to write drop-in file: %v", err)
			c.helper.Warning(result.Error)
			c.helper.EndTask()
			return result, fmt.Errorf("%s", result.Error)
		}

		result.DropIn = dropInFile
		c.helper.Info(fmt.Sprintf("Created drop-in: %s", dropInFile))
	}
	c.helper.EndTask()

	// Handle --now flag: download extensions immediately
	if opts.Now {
		c.helper.BeginTask("Downloading extensions")

		// Load transfers to find which ones belong to this feature
		transfers, err := config.LoadTransfers(c.config.Definitions)
		if err != nil {
			result.Error = fmt.Sprintf("failed to load transfers: %v", err)
			c.helper.Warning(result.Error)
			c.helper.EndTask()
			return result, fmt.Errorf("%s", result.Error)
		}

		featureTransfers := config.GetTransfersForFeature(transfers, name)

		if len(featureTransfers) == 0 {
			c.helper.Info("No transfers associated with this feature")
			c.helper.EndTask()
		} else {
			for _, transfer := range featureTransfers {
				c.helper.Info(fmt.Sprintf("Processing %s", transfer.Component))

				if opts.DryRun {
					c.helper.Info(fmt.Sprintf("Would download: %s", transfer.Component))
					result.DownloadedFiles = append(result.DownloadedFiles, transfer.Component+" (would download)")
				} else {
					// Use installTransfer which handles all the download logic
					version, err := c.installTransfer(transfer, true) // noRefresh=true, we'll refresh once at the end
					if err != nil {
						result.Error = fmt.Sprintf("failed to download %s: %v", transfer.Component, err)
						c.helper.Warning(result.Error)
						c.helper.EndTask()
						return result, fmt.Errorf("%s", result.Error)
					}
					result.DownloadedFiles = append(result.DownloadedFiles, fmt.Sprintf("%s@%s", transfer.Component, version))
					c.helper.Info(fmt.Sprintf("Downloaded %s version %s", transfer.Component, version))
				}
			}
			c.helper.EndTask()

			// Refresh if we downloaded (unless --no-refresh or --dry-run)
			if !opts.NoRefresh && !opts.DryRun {
				c.helper.BeginTask("Refreshing sysext")
				if err := sysext.Refresh(); err != nil {
					c.helper.Warning(fmt.Sprintf("sysext refresh failed: %v", err))
				}
				c.helper.EndTask()
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
		result.NextActionMessage = fmt.Sprintf("Feature '%s' enabled. Run 'updex update' to download extensions.", name)
	}

	return result, nil
}

// DisableFeature disables a feature by creating a drop-in configuration file.
func (c *Client) DisableFeature(ctx context.Context, name string, opts DisableFeatureOptions) (*FeatureActionResult, error) {
	actionName := "Disable feature"
	if opts.DryRun {
		actionName = "Disable feature (dry run)"
	}
	c.helper.BeginAction(actionName)
	defer c.helper.EndAction()

	c.helper.BeginTask(fmt.Sprintf("Disabling %s", name))

	result := &FeatureActionResult{
		Feature: name,
		Action:  "disable",
		DryRun:  opts.DryRun,
	}

	// Verify the feature exists
	features, err := config.LoadFeatures(c.config.Definitions)
	if err != nil {
		result.Error = fmt.Sprintf("failed to load features: %v", err)
		c.helper.Warning(result.Error)
		c.helper.EndTask()
		return result, fmt.Errorf("%s", result.Error)
	}

	found := false
	for _, f := range features {
		if f.Name == name {
			found = true
			if f.Masked {
				result.Error = fmt.Sprintf("feature '%s' is masked and cannot be disabled", name)
				c.helper.Warning(result.Error)
				c.helper.EndTask()
				return result, fmt.Errorf("%s", result.Error)
			}
			break
		}
	}

	if !found {
		result.Error = fmt.Sprintf("feature '%s' not found", name)
		c.helper.Warning(result.Error)
		c.helper.EndTask()
		return result, fmt.Errorf("%s", result.Error)
	}

	// Load transfers for this feature (needed for merge state check and file removal)
	transfers, err := config.LoadTransfers(c.config.Definitions)
	if err != nil {
		result.Error = fmt.Sprintf("failed to load transfers: %v", err)
		c.helper.Warning(result.Error)
		c.helper.EndTask()
		return result, fmt.Errorf("%s", result.Error)
	}

	featureTransfers := config.GetTransfersForFeature(transfers, name)

	// --now now implies file removal (same as --remove)
	// Keep --remove for backward compatibility
	willRemoveFiles := opts.Now || opts.Remove

	// Check merge state BEFORE any destructive operations
	if willRemoveFiles && len(featureTransfers) > 0 {
		var mergedExtensions []string
		for _, t := range featureTransfers {
			activeVersion, err := sysext.GetActiveVersion(t)
			if err != nil {
				c.helper.Warning(fmt.Sprintf("could not check merge state for %s: %v", t.Component, err))
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
			c.helper.Warning(errMsg)
			c.helper.EndTask()
			return result, fmt.Errorf("%s", errMsg)
		}

		if len(mergedExtensions) > 0 && opts.Force {
			c.helper.Warning("Extensions are currently active. Changes will take effect after reboot.")
		}
	}

	// Create drop-in directory and file
	dropInDir := filepath.Join("/etc/sysupdate.d", name+".feature.d")
	dropInFile := filepath.Join(dropInDir, "00-updex.conf")

	if opts.DryRun {
		c.helper.Info(fmt.Sprintf("Would create drop-in: %s", dropInFile))
	} else {
		if err := os.MkdirAll(dropInDir, 0755); err != nil {
			result.Error = fmt.Sprintf("failed to create drop-in directory: %v", err)
			c.helper.Warning(result.Error)
			c.helper.EndTask()
			return result, fmt.Errorf("%s", result.Error)
		}

		content := "[Feature]\nEnabled=false\n"
		if err := os.WriteFile(dropInFile, []byte(content), 0644); err != nil {
			result.Error = fmt.Sprintf("failed to write drop-in file: %v", err)
			c.helper.Warning(result.Error)
			c.helper.EndTask()
			return result, fmt.Errorf("%s", result.Error)
		}

		result.DropIn = dropInFile
		c.helper.Info(fmt.Sprintf("Created drop-in: %s", dropInFile))
	}
	c.helper.EndTask()

	// Handle --now (or --remove for backward compat): remove files and unmerge
	if willRemoveFiles && len(featureTransfers) > 0 {
		// If --now is specified, unmerge first (unless dry-run)
		if opts.Now && !opts.DryRun {
			c.helper.BeginTask("Unmerging extensions")
			if err := sysext.Unmerge(); err != nil {
				result.Error = fmt.Sprintf("failed to unmerge: %v", err)
				c.helper.Warning(result.Error)
				c.helper.EndTask()
				return result, fmt.Errorf("%s", result.Error)
			}
			result.Unmerged = true
			c.helper.EndTask()
		} else if opts.Now && opts.DryRun {
			c.helper.Info("Would unmerge extensions")
		}

		// Remove files for each transfer
		c.helper.BeginTask("Removing files")
		var allRemoved []string
		for _, t := range featureTransfers {
			if opts.DryRun {
				c.helper.Info(fmt.Sprintf("Would remove files for: %s", t.Component))
				allRemoved = append(allRemoved, t.Component+" (would remove)")
			} else {
				// Remove the symlink from /var/lib/extensions
				if err := sysext.UnlinkFromSysext(t); err != nil {
					c.helper.Warning(fmt.Sprintf("failed to unlink %s: %v", t.Component, err))
				}

				// Remove all versions
				removed, err := sysext.RemoveAllVersions(t)
				if err != nil {
					result.Error = fmt.Sprintf("failed to remove files for %s: %v", t.Component, err)
					c.helper.Warning(result.Error)
					c.helper.EndTask()
					return result, fmt.Errorf("%s", result.Error)
				}
				allRemoved = append(allRemoved, removed...)
			}
		}
		result.RemovedFiles = allRemoved
		if !opts.DryRun {
			c.helper.Info(fmt.Sprintf("Removed %d file(s)", len(allRemoved)))
		}
		c.helper.EndTask()

		// Refresh if we unmerged (unless --no-refresh or --dry-run)
		if opts.Now && !opts.NoRefresh && !opts.DryRun {
			c.helper.BeginTask("Refreshing sysext")
			if err := sysext.Refresh(); err != nil {
				c.helper.Warning(fmt.Sprintf("sysext refresh failed: %v", err))
			}
			c.helper.EndTask()
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
		result.NextActionMessage = fmt.Sprintf("Feature '%s' disabled. Run 'updex update' to apply changes.", name)
	}

	return result, nil
}
