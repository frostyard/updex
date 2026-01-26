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
func (c *Client) EnableFeature(ctx context.Context, name string) (*FeatureActionResult, error) {
	c.helper.BeginAction("Enable feature")
	defer c.helper.EndAction()

	c.helper.BeginTask(fmt.Sprintf("Enabling %s", name))

	result := &FeatureActionResult{
		Feature: name,
		Action:  "enable",
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

	result.Success = true
	result.DropIn = dropInFile
	result.NextActionMessage = "Run 'updex update' to apply changes"

	c.helper.Info(fmt.Sprintf("Created drop-in: %s", dropInFile))
	c.helper.EndTask()

	return result, nil
}

// DisableFeature disables a feature by creating a drop-in configuration file.
func (c *Client) DisableFeature(ctx context.Context, name string, opts DisableFeatureOptions) (*FeatureActionResult, error) {
	c.helper.BeginAction("Disable feature")
	defer c.helper.EndAction()

	c.helper.BeginTask(fmt.Sprintf("Disabling %s", name))

	result := &FeatureActionResult{
		Feature: name,
		Action:  "disable",
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

	// Create drop-in directory and file
	dropInDir := filepath.Join("/etc/sysupdate.d", name+".feature.d")
	dropInFile := filepath.Join(dropInDir, "00-updex.conf")

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

	result.Success = true
	result.DropIn = dropInFile

	c.helper.Info(fmt.Sprintf("Created drop-in: %s", dropInFile))
	c.helper.EndTask()

	// Handle --now and --remove flags
	if opts.Now || opts.Remove {
		// Load transfers to find which ones belong to this feature
		transfers, err := config.LoadTransfers(c.config.Definitions)
		if err != nil {
			result.Error = fmt.Sprintf("failed to load transfers: %v", err)
			c.helper.Warning(result.Error)
			return result, fmt.Errorf("%s", result.Error)
		}

		featureTransfers := config.GetTransfersForFeature(transfers, name)

		// If --now is specified, unmerge first
		if opts.Now {
			c.helper.BeginTask("Unmerging extensions")
			if err := sysext.Unmerge(); err != nil {
				result.Error = fmt.Sprintf("failed to unmerge: %v", err)
				c.helper.Warning(result.Error)
				c.helper.EndTask()
				return result, fmt.Errorf("%s", result.Error)
			}
			result.Unmerged = true
			c.helper.EndTask()
		}

		// If --remove is specified, remove files for each transfer
		if opts.Remove {
			c.helper.BeginTask("Removing files")
			var allRemoved []string
			for _, t := range featureTransfers {
				// Remove the symlink
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
			result.RemovedFiles = allRemoved
			c.helper.Info(fmt.Sprintf("Removed %d file(s)", len(allRemoved)))
			c.helper.EndTask()
		}

		// Refresh if we unmerged (unless --no-refresh)
		if opts.Now && !opts.NoRefresh {
			c.helper.BeginTask("Refreshing sysext")
			if err := sysext.Refresh(); err != nil {
				c.helper.Warning(fmt.Sprintf("sysext refresh failed: %v", err))
			}
			c.helper.EndTask()
		}
	}

	// Set the next action message based on what was done
	if opts.Remove && opts.Now {
		result.NextActionMessage = "Feature disabled, files removed, and extensions unmerged"
	} else if opts.Remove {
		result.NextActionMessage = "Feature disabled and files removed. Changes will take effect after reboot."
	} else if opts.Now {
		result.NextActionMessage = "Feature disabled and extensions unmerged"
	} else {
		result.NextActionMessage = "Run 'updex update' to apply changes"
	}

	return result, nil
}
