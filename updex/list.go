package updex

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/frostyard/updex/v2/catalog"
	"github.com/frostyard/updex/v2/config"
	"github.com/frostyard/updex/v2/manifest"
	"github.com/frostyard/updex/v2/version"
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

	policy := c.verifyPolicy(transfer)
	needVerify := policy.required
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
			if errors.Is(err, manifest.ErrSignatureNotFound) {
				return nil, nil, nil, fmt.Errorf("%s: %w", policy.explain(transfer), err)
			}
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

// verifyReason records which rule made a transfer require (or skip) GPG
// verification, so a missing signature can be reported with its policy.
type verifyReason int

const (
	verifyByTransfer        verifyReason = iota // non-catalog transfer: its own Verify= decides
	verifyForced                                // ClientConfig.Verify (--verify)
	verifyCatalogNotAllowed                     // catalog transfer, repo lacks AllowUnsigned=yes (or is not configured)
	verifyByCatalog                             // catalog transfer, AllowUnsigned=yes: catalog's Verify= decides
)

type verifyPolicy struct {
	required bool
	reason   verifyReason
	repo     string // owning catalog repo for catalog-generated transfers
}

// verifyPolicy resolves whether transfer's SHA256SUMS must carry a valid GPG
// signature. Client-wide Verify always forces it. A transfer written by
// catalog add trusts its own Verify= only while the owning repo's local
// .catalog sets AllowUnsigned=yes; otherwise, including when that repo is no
// longer configured, verification is required regardless of the file. The
// decision is made from local configuration before any fetch, so a missing
// signature never downgrades verification.
func (c *Client) verifyPolicy(transfer *config.Transfer) verifyPolicy {
	if c.config.Verify {
		return verifyPolicy{required: true, reason: verifyForced}
	}
	owner, ok := catalog.GeneratedFileRepo(transfer.FilePath)
	if !ok {
		return verifyPolicy{required: transfer.Transfer.Verify, reason: verifyByTransfer}
	}
	if !c.catalogAllowsUnsigned(owner) {
		return verifyPolicy{required: true, reason: verifyCatalogNotAllowed, repo: owner}
	}
	return verifyPolicy{required: transfer.Transfer.Verify, reason: verifyByCatalog, repo: owner}
}

// catalogAllowsUnsigned reports whether the configured repo named name sets
// AllowUnsigned=yes. Any failure to load the catalog configuration denies.
func (c *Client) catalogAllowsUnsigned(name string) bool {
	repos, err := catalog.LoadReposFrom(c.paths.catalogConfigRoots)
	if err != nil {
		c.debug("cannot load catalog configuration (%v); requiring signature verification", err)
		return false
	}
	repo, ok := catalog.RepoByName(repos, name)
	return ok && repo.AllowUnsigned
}

// explain describes why verification was required, for wrapping a missing
// signature error.
func (p verifyPolicy) explain(transfer *config.Transfer) string {
	switch p.reason {
	case verifyForced:
		return fmt.Sprintf("signature verification for %s is forced by client configuration (--verify)", transfer.Component)
	case verifyCatalogNotAllowed:
		return fmt.Sprintf("catalog %q must publish a signed SHA256SUMS for %s: set AllowUnsigned=yes in %s.catalog to honor the catalog's Verify= setting, accepting HTTPS and SHA256 integrity checks without publisher authentication",
			p.repo, transfer.Component, p.repo)
	case verifyByCatalog:
		return fmt.Sprintf("catalog %q declares Verify=yes for %s, so a signed SHA256SUMS is required", p.repo, transfer.Component)
	default:
		return fmt.Sprintf("transfer %s requires a signed SHA256SUMS (Verify=yes is the default; set Verify=no to opt out)", transfer.Component)
	}
}
