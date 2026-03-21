package updex

import (
	"context"
	"fmt"

	"github.com/frostyard/updex/config"
	"github.com/frostyard/updex/manifest"
	"github.com/frostyard/updex/version"
)

// getAvailableVersions retrieves available versions for a transfer from remote manifest.
// It returns the fetched manifest alongside the versions so callers can reuse it
// without a redundant HTTP request.
func (c *Client) getAvailableVersions(ctx context.Context, transfer *config.Transfer) ([]string, *manifest.Manifest, error) {
	if transfer.Source.Type != "url-file" {
		return nil, nil, fmt.Errorf("unsupported source type: %s", transfer.Source.Type)
	}

	// Fetch manifest
	c.debug("fetching manifest from %s", transfer.Source.Path)
	m, err := manifest.Fetch(ctx, transfer.Source.Path, c.config.Verify || transfer.Transfer.Verify)
	if err != nil {
		return nil, nil, err
	}
	c.debug("manifest has %d file(s)", len(m.Files))

	// Extract versions from filenames using all patterns
	patterns := transfer.Source.Patterns()
	c.debug("matching against pattern(s): %v", patterns)

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
	c.debug("found %d matching version(s): %v", len(versions), versions)

	return versions, m, nil
}
