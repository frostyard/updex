package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/frostyard/updex/cmd/common"
	"github.com/frostyard/updex/internal/config"
	"github.com/spf13/cobra"
)

// NewFeaturesCmd creates the features command with subcommands
func NewFeaturesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "features",
		Short: "Manage optional features",
		Long: `List, enable, and disable optional features defined in .feature files.

Features are optional sets of transfers that can be enabled or disabled by the
system administrator. When a feature is enabled, its associated transfers will
be considered during updates. When disabled, they are skipped.

Configuration files are read from:
  - /etc/sysupdate.d/*.feature
  - /run/sysupdate.d/*.feature
  - /usr/local/lib/sysupdate.d/*.feature
  - /usr/lib/sysupdate.d/*.feature`,
	}

	cmd.AddCommand(newFeaturesListCmd())
	cmd.AddCommand(newFeaturesEnableCmd())
	cmd.AddCommand(newFeaturesDisableCmd())

	return cmd
}

// FeatureInfo represents feature information for JSON output
type FeatureInfo struct {
	Name          string   `json:"name"`
	Description   string   `json:"description,omitempty"`
	Documentation string   `json:"documentation,omitempty"`
	Enabled       bool     `json:"enabled"`
	Masked        bool     `json:"masked,omitempty"`
	Source        string   `json:"source"`
	Transfers     []string `json:"transfers,omitempty"`
}

func newFeaturesListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available features",
		Long:  `List all features defined in .feature configuration files with their status and associated transfers.`,
		RunE:  runFeaturesList,
	}
}

func newFeaturesEnableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "enable FEATURE",
		Short: "Enable a feature",
		Long: `Enable a feature by creating a drop-in configuration file.

This creates a file at /etc/sysupdate.d/<feature>.feature.d/00-updex.conf
that sets Enabled=true for the specified feature.

Requires root privileges.`,
		Args: cobra.ExactArgs(1),
		RunE: runFeaturesEnable,
	}
}

func newFeaturesDisableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "disable FEATURE",
		Short: "Disable a feature",
		Long: `Disable a feature by creating a drop-in configuration file.

This creates a file at /etc/sysupdate.d/<feature>.feature.d/00-updex.conf
that sets Enabled=false for the specified feature.

Requires root privileges.`,
		Args: cobra.ExactArgs(1),
		RunE: runFeaturesDisable,
	}
}

func runFeaturesList(cmd *cobra.Command, args []string) error {
	features, err := config.LoadFeatures(common.Definitions)
	if err != nil {
		return fmt.Errorf("failed to load features: %w", err)
	}

	if len(features) == 0 {
		fmt.Println("No features configured.")
		return nil
	}

	// Load transfers to show which belong to each feature
	transfers, err := config.LoadTransfers(common.Definitions)
	if err != nil {
		return fmt.Errorf("failed to load transfers: %w", err)
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

	if common.JSONOutput {
		items := make([]interface{}, len(featureInfos))
		for i, f := range featureInfos {
			items[i] = f
		}
		common.OutputJSONLines(items)
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "FEATURE\tDESCRIPTION\tENABLED\tTRANSFERS")
	for _, f := range featureInfos {
		status := "no"
		if f.Masked {
			status = "masked"
		} else if f.Enabled {
			status = "yes"
		}

		transfersStr := "-"
		if len(f.Transfers) > 0 {
			transfersStr = ""
			for i, t := range f.Transfers {
				if i > 0 {
					transfersStr += ", "
				}
				transfersStr += t
			}
		}

		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", f.Name, f.Description, status, transfersStr)
	}
	_ = w.Flush()

	return nil
}

func runFeaturesEnable(cmd *cobra.Command, args []string) error {
	featureName := args[0]

	// Check for root privileges
	if os.Geteuid() != 0 {
		return fmt.Errorf("enabling features requires root privileges")
	}

	// Verify the feature exists
	features, err := config.LoadFeatures(common.Definitions)
	if err != nil {
		return fmt.Errorf("failed to load features: %w", err)
	}

	found := false
	for _, f := range features {
		if f.Name == featureName {
			found = true
			if f.Masked {
				return fmt.Errorf("feature '%s' is masked and cannot be enabled", featureName)
			}
			break
		}
	}

	if !found {
		return fmt.Errorf("feature '%s' not found", featureName)
	}

	// Create drop-in directory and file
	dropInDir := filepath.Join("/etc/sysupdate.d", featureName+".feature.d")
	dropInFile := filepath.Join(dropInDir, "00-updex.conf")

	if err := os.MkdirAll(dropInDir, 0755); err != nil {
		return fmt.Errorf("failed to create drop-in directory: %w", err)
	}

	content := "[Feature]\nEnabled=true\n"
	if err := os.WriteFile(dropInFile, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write drop-in file: %w", err)
	}

	fmt.Printf("Feature '%s' enabled.\n", featureName)
	return nil
}

func runFeaturesDisable(cmd *cobra.Command, args []string) error {
	featureName := args[0]

	// Check for root privileges
	if os.Geteuid() != 0 {
		return fmt.Errorf("disabling features requires root privileges")
	}

	// Verify the feature exists
	features, err := config.LoadFeatures(common.Definitions)
	if err != nil {
		return fmt.Errorf("failed to load features: %w", err)
	}

	found := false
	for _, f := range features {
		if f.Name == featureName {
			found = true
			if f.Masked {
				return fmt.Errorf("feature '%s' is masked and cannot be disabled", featureName)
			}
			break
		}
	}

	if !found {
		return fmt.Errorf("feature '%s' not found", featureName)
	}

	// Create drop-in directory and file
	dropInDir := filepath.Join("/etc/sysupdate.d", featureName+".feature.d")
	dropInFile := filepath.Join(dropInDir, "00-updex.conf")

	if err := os.MkdirAll(dropInDir, 0755); err != nil {
		return fmt.Errorf("failed to create drop-in directory: %w", err)
	}

	content := "[Feature]\nEnabled=false\n"
	if err := os.WriteFile(dropInFile, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write drop-in file: %w", err)
	}

	fmt.Printf("Feature '%s' disabled.\n", featureName)
	return nil
}
