package commands

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/frostyard/updex/cmd/common"
	"github.com/frostyard/updex/internal/config"
	"github.com/frostyard/updex/internal/manifest"
	"github.com/frostyard/updex/internal/sysext"
	"github.com/frostyard/updex/internal/version"
	"github.com/spf13/cobra"
)

// NewListCmd creates the list command
func NewListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list [VERSION]",
		Short: "List available and installed versions",
		Long: `List all available versions from remote sources and installed versions.

If VERSION is specified, show detailed information about that specific version.`,
		Args: cobra.MaximumNArgs(1),
		RunE: runList,
	}
}

// VersionInfo represents version information for output
type VersionInfo struct {
	Version   string `json:"version"`
	Installed bool   `json:"installed"`
	Available bool   `json:"available"`
	Current   bool   `json:"current"`
	Protected bool   `json:"protected,omitempty"`
	Component string `json:"component,omitempty"`
}

func runList(cmd *cobra.Command, args []string) error {
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
		if len(transfers) == 0 {
			return fmt.Errorf("no transfer configuration found for component: %s", common.Component)
		}
	}

	var allVersions []VersionInfo

	for _, transfer := range transfers {
		// Get available versions from remote
		available, err := GetAvailableVersions(transfer)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to get available versions for %s: %v\n", transfer.Component, err)
			available = []string{}
		}

		// Get installed versions
		installed, current, err := sysext.GetInstalledVersions(transfer)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to get installed versions for %s: %v\n", transfer.Component, err)
			installed = []string{}
		}

		// Merge available and installed
		versionSet := make(map[string]*VersionInfo)

		for _, v := range available {
			versionSet[v] = &VersionInfo{
				Version:   v,
				Available: true,
				Component: transfer.Component,
			}
		}

		for _, v := range installed {
			if info, exists := versionSet[v]; exists {
				info.Installed = true
				info.Current = (v == current)
			} else {
				versionSet[v] = &VersionInfo{
					Version:   v,
					Installed: true,
					Current:   v == current,
					Component: transfer.Component,
				}
			}
		}

		// Check protected versions
		for v, info := range versionSet {
			if transfer.Transfer.ProtectVersion != "" && v == transfer.Transfer.ProtectVersion {
				info.Protected = true
			}
		}

		// Collect and sort versions
		versions := make([]string, 0, len(versionSet))
		for v := range versionSet {
			versions = append(versions, v)
		}
		version.Sort(versions)

		for _, v := range versions {
			allVersions = append(allVersions, *versionSet[v])
		}
	}

	// If specific version requested, filter
	if len(args) == 1 {
		targetVersion := args[0]
		filtered := make([]VersionInfo, 0)
		for _, v := range allVersions {
			if v.Version == targetVersion {
				filtered = append(filtered, v)
			}
		}
		allVersions = filtered
	}

	// Output
	if common.JSONOutput {
		items := make([]interface{}, len(allVersions))
		for i, v := range allVersions {
			items[i] = v
		}
		common.OutputJSONLines(items)
		return nil
	}

	// Table output
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "VERSION\tINSTALLED\tAVAILABLE\tCURRENT\tCOMPONENT")
	for _, v := range allVersions {
		installed := "-"
		if v.Installed {
			installed = "yes"
		}
		available := "-"
		if v.Available {
			available = "yes"
		}
		current := ""
		if v.Current {
			current = "→"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", v.Version, installed, available, current, v.Component)
	}
	w.Flush()

	return nil
}

// GetAvailableVersions retrieves available versions for a transfer from remote manifest
func GetAvailableVersions(transfer *config.Transfer) ([]string, error) {
	if transfer.Source.Type != "url-file" {
		return nil, fmt.Errorf("unsupported source type: %s", transfer.Source.Type)
	}

	// Fetch manifest
	m, err := manifest.Fetch(transfer.Source.Path, common.Verify)
	if err != nil {
		return nil, err
	}

	// Extract versions from filenames using all patterns
	patterns := transfer.Source.MatchPatterns
	if len(patterns) == 0 && transfer.Source.MatchPattern != "" {
		// Fallback to single pattern for backward compatibility
		patterns = []string{transfer.Source.MatchPattern}
	}

	versionSet := make(map[string]bool)
	for filename := range m.Files {
		if v, _, ok := version.ExtractVersionMulti(filename, patterns); ok {
			// Apply MinVersion filter
			if transfer.Transfer.MinVersion != "" {
				if version.Compare(v, transfer.Transfer.MinVersion) < 0 {
					continue
				}
			}
			versionSet[v] = true
		}
	}

	versions := make([]string, 0, len(versionSet))
	for v := range versionSet {
		versions = append(versions, v)
	}

	return versions, nil
}
