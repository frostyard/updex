package updex

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/frostyard/updex/config"
	"github.com/frostyard/updex/manifest"
	"github.com/frostyard/updex/version"
)

// getAvailableVersions retrieves available versions for a transfer from remote manifest.
// It returns the fetched manifest and the parsed source patterns alongside the versions
// so callers can reuse both without redundant HTTP requests or pattern parsing.
// If cachedManifest is non-nil and satisfies the transfer's verification
// requirement, it is used instead of fetching the manifest over HTTP. A
// transfer that requires GPG verification (client-wide Verify or Verify=true
// on the transfer) never consumes a cached manifest that was fetched without
// a successful signature check; it fetches (with verification) as if no cache
// entry existed. A verified manifest may serve unverified transfers, never the
// reverse, so verification is a property of the transfer rather than of which
// transfer sharing a Source.Path happened to load first.
func (c *Client) getAvailableVersions(ctx context.Context, transfer *config.Transfer, cachedManifest *manifest.Manifest) ([]string, *manifest.Manifest, []*version.Pattern, error) {
	if transfer.Source.Type != "url-file" {
		return nil, nil, nil, fmt.Errorf("unsupported source type: %s", transfer.Source.Type)
	}

	needVerify := c.config.Verify || transfer.Transfer.Verify
	m := cachedManifest
	if m != nil && needVerify && !m.Verified {
		c.debug("cached manifest for %s was not signature-verified; refetching with verification", transfer.Source.Path)
		m = nil
	}
	if m == nil {
		// Fetch manifest
		c.debug("fetching manifest from %s", transfer.Source.Path)
		var err error
		m, err = manifest.Fetch(ctx, c.httpClient, transfer.Source.Path, needVerify, manifest.WithRetryNotify(c.retryNotify("manifest fetch")))
		if err != nil {
			return nil, nil, nil, err
		}
	} else {
		c.debug("using cached manifest for %s", transfer.Source.Path)
	}
	c.debug("manifest has %d file(s)", len(m.Files))

	// Extract versions from filenames using all patterns
	patternStrs := transfer.Source.Patterns()
	c.debug("matching against pattern(s): %v", patternStrs)
	patterns, firstErr := version.ParsePatterns(patternStrs)
	if len(patterns) == 0 && firstErr != nil {
		return nil, nil, nil, fmt.Errorf("invalid source pattern: %w", firstErr)
	}

	versionSet := make(map[string]bool)
	for filename := range m.Files {
		if v, _, ok := version.ExtractVersionParsed(filename, patterns); ok {
			// Apply MinVersion filter
			if transfer.Transfer.MinVersion != "" {
				if version.Compare(v, transfer.Transfer.MinVersion) < 0 {
					continue
				}
			}
			versionSet[v] = true
		}
	}

	// Sort lexically so the order never depends on map iteration: version.Sort
	// is stable, so if a comparator gap ever makes two distinct versions compare
	// equal, selection stays reproducible instead of random.
	versions := slices.Sorted(maps.Keys(versionSet))
	c.debug("found %d matching version(s): %v", len(versions), versions)

	return versions, m, patterns, nil
}
