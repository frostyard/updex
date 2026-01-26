package updex

import (
	"context"
	"fmt"

	"github.com/frostyard/updex/internal/config"
	"github.com/frostyard/updex/internal/manifest"
	"github.com/frostyard/updex/internal/sysext"
	"github.com/frostyard/updex/internal/version"
)

// List returns available and installed versions for all configured components.
func (c *Client) List(ctx context.Context, opts ListOptions) ([]VersionInfo, error) {
	c.helper.BeginAction("List versions")
	defer c.helper.EndAction()

	transfers, err := c.loadTransfers(opts.Component)
	if err != nil {
		return nil, err
	}

	if len(transfers) == 0 {
		return nil, fmt.Errorf("no transfer configurations found")
	}

	var allVersions []VersionInfo

	for _, transfer := range transfers {
		c.helper.BeginTask(fmt.Sprintf("Processing %s", transfer.Component))

		// Get available versions from remote
		available, err := c.getAvailableVersions(transfer)
		if err != nil {
			c.helper.Warning(fmt.Sprintf("failed to get available versions: %v", err))
			available = []string{}
		}

		// Get installed versions
		installed, current, err := sysext.GetInstalledVersions(transfer)
		if err != nil {
			c.helper.Warning(fmt.Sprintf("failed to get installed versions: %v", err))
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

		c.helper.EndTask()
	}

	// Filter by specific version if requested
	if opts.Version != "" {
		filtered := make([]VersionInfo, 0)
		for _, v := range allVersions {
			if v.Version == opts.Version {
				filtered = append(filtered, v)
			}
		}
		allVersions = filtered
	}

	c.helper.Info(fmt.Sprintf("Found %d version(s)", len(allVersions)))
	return allVersions, nil
}

// loadTransfers loads and filters transfer configurations.
func (c *Client) loadTransfers(component string) ([]*config.Transfer, error) {
	transfers, err := config.LoadTransfers(c.config.Definitions)
	if err != nil {
		return nil, fmt.Errorf("failed to load transfer configs: %w", err)
	}

	// Filter by component if specified
	if component != "" {
		filtered := make([]*config.Transfer, 0)
		for _, t := range transfers {
			if t.Component == component {
				filtered = append(filtered, t)
			}
		}
		transfers = filtered
		if len(transfers) == 0 {
			return nil, fmt.Errorf("no transfer configuration found for component: %s", component)
		}
	}

	// Filter by enabled features
	features, err := config.LoadFeatures(c.config.Definitions)
	if err != nil {
		return nil, fmt.Errorf("failed to load features: %w", err)
	}
	transfers = config.FilterTransfersByFeatures(transfers, features)

	return transfers, nil
}

// getAvailableVersions retrieves available versions for a transfer from remote manifest.
func (c *Client) getAvailableVersions(transfer *config.Transfer) ([]string, error) {
	if transfer.Source.Type != "url-file" {
		return nil, fmt.Errorf("unsupported source type: %s", transfer.Source.Type)
	}

	// Fetch manifest
	m, err := manifest.Fetch(transfer.Source.Path, c.config.Verify || transfer.Transfer.Verify)
	if err != nil {
		return nil, err
	}

	// Extract versions from filenames using all patterns
	patterns := transfer.Source.MatchPatterns
	if len(patterns) == 0 && transfer.Source.MatchPattern != "" {
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
