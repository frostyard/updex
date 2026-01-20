package commands

import (
	"fmt"
	"os"

	"github.com/frostyard/updex/cmd/common"
	"github.com/frostyard/updex/internal/config"
	"github.com/frostyard/updex/internal/download"
	"github.com/frostyard/updex/internal/manifest"
	"github.com/frostyard/updex/internal/sysext"
	"github.com/frostyard/updex/internal/version"
	"github.com/spf13/cobra"
)

var noVacuum bool

// NewUpdateCmd creates the update command
func NewUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update [VERSION]",
		Short: "Download and install a new version",
		Long: `Download and install the newest available version, or a specific version if specified.

After installation, old versions are automatically removed according to InstancesMax
unless --no-vacuum is specified.`,
		Args: cobra.MaximumNArgs(1),
		RunE: runUpdate,
	}
	cmd.Flags().BoolVar(&noVacuum, "no-vacuum", false, "Do not remove old versions after update")
	return cmd
}

// UpdateResult represents the result of an update operation
type UpdateResult struct {
	Component         string `json:"component"`
	Version           string `json:"version"`
	Downloaded        bool   `json:"downloaded"`
	Installed         bool   `json:"installed"`
	Error             string `json:"error,omitempty"`
	NextActionMessage string `json:"next_action_message,omitempty"`
}

func runUpdate(cmd *cobra.Command, args []string) error {
	// Check for root privileges before attempting any operations
	if err := common.RequireRoot(); err != nil {
		return err
	}

	transfers, err := config.LoadTransfers(common.Definitions)
	if err != nil {
		return fmt.Errorf("failed to load transfer configs: %w", err)
	}

	if len(transfers) == 0 {
		return fmt.Errorf("no transfer configurations found")
	}

	// Filter by component if specified
	if common.Component != "" {
		filtered := make([]*config.Transfer, 0)
		for _, t := range transfers {
			if t.Component == common.Component {
				filtered = append(filtered, t)
			}
		}
		transfers = filtered
	}

	// Filter by enabled features
	features, err := config.LoadFeatures(common.Definitions)
	if err != nil {
		return fmt.Errorf("failed to load features: %w", err)
	}
	transfers = config.FilterTransfersByFeatures(transfers, features)

	var targetVersion string
	if len(args) == 1 {
		targetVersion = args[0]
	}

	var results []UpdateResult

	for _, transfer := range transfers {
		result := UpdateResult{
			Component: transfer.Component,
		}

		// Get available versions
		available, err := GetAvailableVersions(transfer)
		if err != nil {
			result.Error = fmt.Sprintf("failed to get available versions: %v", err)
			results = append(results, result)
			continue
		}

		if len(available) == 0 {
			result.Error = "no versions available"
			results = append(results, result)
			continue
		}

		// Determine which version to install
		version.Sort(available)
		versionToInstall := available[0] // newest

		if targetVersion != "" {
			found := false
			for _, v := range available {
				if v == targetVersion {
					versionToInstall = v
					found = true
					break
				}
			}
			if !found {
				result.Error = fmt.Sprintf("version %s not found", targetVersion)
				results = append(results, result)
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
			if !common.JSONOutput {
				fmt.Printf("%s: version %s already installed and current\n", transfer.Component, versionToInstall)
			}
			results = append(results, result)
			continue
		}

		// Fetch manifest for download
		m, err := manifest.Fetch(transfer.Source.Path, common.Verify || transfer.Transfer.Verify)
		if err != nil {
			result.Error = fmt.Sprintf("failed to fetch manifest: %v", err)
			results = append(results, result)
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
			results = append(results, result)
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
			results = append(results, result)
			continue
		}

		targetFile := targetPattern.BuildFilename(versionToInstall)
		targetPath := fmt.Sprintf("%s/%s", transfer.Target.Path, targetFile)

		// Download
		if !common.JSONOutput {
			fmt.Printf("%s: downloading version %s...\n", transfer.Component, versionToInstall)
		}

		downloadURL := transfer.Source.Path + "/" + sourceFile
		err = download.Download(downloadURL, targetPath, expectedHash, transfer.Target.Mode)
		if err != nil {
			result.Error = fmt.Sprintf("download failed: %v", err)
			results = append(results, result)
			continue
		}

		result.Downloaded = true
		result.Installed = true
		result.NextActionMessage = "Reboot required to activate changes"

		// Update symlink if configured
		if transfer.Target.CurrentSymlink != "" {
			err = sysext.UpdateSymlink(transfer.Target.Path, transfer.Target.CurrentSymlink, targetFile)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to update symlink: %v\n", err)
			}
		}

		// Link to /var/lib/extensions for systemd-sysext
		if err := sysext.LinkToSysext(transfer); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to link to sysext: %v\n", err)
		}

		if !common.JSONOutput {
			fmt.Printf("%s: installed version %s\n", transfer.Component, versionToInstall)
		}

		results = append(results, result)

		// Run vacuum unless disabled
		if !noVacuum {
			if err := sysext.Vacuum(transfer); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: vacuum failed for %s: %v\n", transfer.Component, err)
			}
		}
	}

	// Refresh systemd-sysext to pick up all changes (unless --no-refresh)
	if !common.NoRefresh {
		if err := sysext.Refresh(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: sysext refresh failed: %v\n", err)
		}
	} else if !common.JSONOutput {
		fmt.Fprintf(os.Stderr, "Note: skipping sysext refresh (--no-refresh). Run 'systemd-sysext refresh' manually.\n")
	}

	if common.JSONOutput {
		common.OutputJSON(results)
	}

	// Check if any errors occurred
	for _, r := range results {
		if r.Error != "" {
			if !common.JSONOutput {
				fmt.Fprintf(os.Stderr, "%s: %s\n", r.Component, r.Error)
			}
		}
	}

	// Check if any updates were installed and notify about reboot
	anyInstalled := false
	for _, r := range results {
		if r.Installed && r.Error == "" {
			anyInstalled = true
			break
		}
	}
	if anyInstalled && !common.JSONOutput {
		fmt.Printf("\nReboot required to activate changes.\n")
	}

	return nil
}
