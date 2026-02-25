package updex

import (
	"fmt"

	"github.com/frostyard/updex/internal/config"
	"github.com/frostyard/updex/internal/manifest"
	"github.com/frostyard/updex/internal/version"
)

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
