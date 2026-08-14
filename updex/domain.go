package updex

import (
	"context"
	"fmt"

	"github.com/frostyard/updex/config"
)

// FeaturesOptions configures the Features listing operation.
type FeaturesOptions struct {
	// Component scopes the listing to a single named systemd-sysupdate
	// component (see sysupdate.d(5) "Components"). Empty lists the default
	// domain: the union of the legacy default sysupdate.d directory and
	// every discovered component.
	Component string
}

// loadDomain resolves the feature/transfer domain a client operation should
// run over:
//
//   - If the client has a Definitions override (--definitions/-C), that
//     single directory is used verbatim, exactly as before component
//     support existed. component must be empty in this case.
//   - Else if component is non-empty, only that named component's own
//     search paths are used (see config.ComponentSearchPathsIn).
//   - Else (the default), the domain is the union of the legacy default
//     sysupdate.d directory and every discovered component (see
//     config.LoadAllFeaturesIn / config.LoadAllTransfersIn). Any name
//     collisions encountered while building the union are logged as
//     warnings through the client's reporter.
//
// The client's immutable paths (captured at NewClient) are used throughout;
// mutable package variables are never consulted after construction.
func (c *Client) loadDomain(component string) ([]*config.Feature, []*config.Transfer, error) {
	if c.config.Definitions != "" {
		if component != "" {
			return nil, nil, fmt.Errorf("cannot combine --definitions with --component")
		}

		features, err := config.LoadFeaturesIn(c.config.Definitions, c.paths.definitionRoots)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to load features: %w", err)
		}
		transfers, err := config.LoadTransfersIn(c.config.Definitions, c.paths.definitionRoots, c.paths.osReleasePaths)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to load transfers: %w", err)
		}
		return features, config.FilterSysextTransfers(transfers), nil
	}

	if component != "" {
		features, err := config.LoadComponentFeaturesIn(component, c.paths.definitionRoots)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to load features for component %q: %w", component, err)
		}
		transfers, err := config.LoadComponentTransfersIn(component, c.paths.definitionRoots, c.paths.osReleasePaths)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to load transfers for component %q: %w", component, err)
		}
		return features, config.FilterSysextTransfers(transfers), nil
	}

	features, featureWarnings, err := config.LoadAllFeaturesIn("", c.paths.definitionRoots)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load features: %w", err)
	}
	transfers, transferWarnings, err := config.LoadAllTransfersIn("", c.paths.definitionRoots, c.paths.osReleasePaths)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load transfers: %w", err)
	}
	for _, w := range featureWarnings {
		c.warn("%s", w)
	}
	for _, w := range transferWarnings {
		c.warn("%s", w)
	}

	return features, transfers, nil
}

// ComponentInfo describes a discovered systemd-sysupdate component.
type ComponentInfo struct {
	// Name is the component name, e.g. "docker" for sysupdate.docker.d.
	Name string `json:"name"`
	// SourceDir is the highest-priority existing search-path directory for
	// this component (see config.Component.SearchPaths).
	SourceDir string `json:"source_dir"`
	// FeatureCount is the number of .feature files defined by this
	// component alone (not counting union collisions with other sources).
	FeatureCount int `json:"feature_count"`
}

// Components lists the systemd-sysupdate components discovered on the
// system (see sysupdate.d(5) "Components"): every sysupdate.<name>.d
// directory found under the standard search roots. It does not include the
// legacy default sysupdate.d directory itself; use Features with the
// default (empty) Component to see the full union domain, including
// anything still defined there.
func (c *Client) Components(ctx context.Context) ([]ComponentInfo, error) {
	c.msg("Discovering components")

	components, err := config.DiscoverComponentsIn(c.paths.definitionRoots)
	if err != nil {
		return nil, fmt.Errorf("failed to discover components: %w", err)
	}

	infos := make([]ComponentInfo, 0, len(components))
	for _, comp := range components {
		features, err := config.LoadComponentFeaturesIn(comp.Name, c.paths.definitionRoots)
		if err != nil {
			return nil, fmt.Errorf("failed to load features for component %q: %w", comp.Name, err)
		}

		var sourceDir string
		if len(comp.SearchPaths) > 0 {
			sourceDir = comp.SearchPaths[0]
		}

		infos = append(infos, ComponentInfo{
			Name:         comp.Name,
			SourceDir:    sourceDir,
			FeatureCount: len(features),
		})
	}

	c.msg("Found %d component(s)", len(infos))

	return infos, nil
}
