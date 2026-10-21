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
	verifyByTransfer         verifyReason = iota // non-catalog transfer: its own Verify= decides
	verifyForced                                 // ClientConfig.Verify (--verify)
	verifyCatalogNotAllowed                      // catalog transfer, repo lacks AllowUnsigned=yes (or is not configured)
	verifyByCatalog                              // catalog transfer, AllowUnsigned=yes: the generated transfer's Verify= decides
	verifyUnknownOwner                           // catalog marker present but its repo cannot be parsed, or the header is unreadable
	verifyCatalogStaleSource                     // catalog transfer, AllowUnsigned=yes, but its source is not under the repo's current SiteURL
)

type verifyPolicy struct {
	required bool
	reason   verifyReason
	repo     string // owning catalog repo for catalog-generated transfers
	// loadErr is why the owning repo's .catalog could not be consulted
	// (catalog.ErrRepoNotFound when it is not configured).
	loadErr error
}

// verifyPolicy resolves whether transfer's SHA256SUMS must carry a valid GPG
// signature. Client-wide Verify always forces it. A transfer written by
// catalog add trusts its own Verify= only while the owning repo's local
// .catalog sets AllowUnsigned=yes; otherwise, including when that repo is no
// longer configured or its file fails to load, verification is required
// regardless of the file. The decision is made from local configuration
// before any fetch, so a missing signature never downgrades verification.
func (c *Client) verifyPolicy(transfer *config.Transfer) verifyPolicy {
	if c.config.Verify {
		return verifyPolicy{required: true, reason: verifyForced}
	}
	if transfer.FilePath == "" {
		return verifyPolicy{required: transfer.Transfer.Verify, reason: verifyByTransfer}
	}
	owner, marked, err := catalog.GeneratedFileHeader(transfer.FilePath)
	if err != nil {
		// Ownership is unknown, so the transfer's own Verify= cannot be
		// trusted.
		c.debug("cannot read header of %s (%v); requiring signature verification", transfer.FilePath, err)
		return verifyPolicy{required: true, reason: verifyUnknownOwner}
	}
	if owner == "" {
		// A marker that names no parseable repo cannot be tied to an
		// opt-in, so it must not be mistaken for a hand-written transfer
		// whose own Verify= is trusted.
		if marked {
			return verifyPolicy{required: true, reason: verifyUnknownOwner}
		}
		return verifyPolicy{required: transfer.Transfer.Verify, reason: verifyByTransfer}
	}
	repo, err := catalog.LoadRepoFrom(c.paths.catalogConfigRoots, owner)
	if err != nil {
		c.debug("cannot load catalog %q (%v); requiring signature verification", owner, err)
		return verifyPolicy{required: true, reason: verifyCatalogNotAllowed, repo: owner, loadErr: err}
	}
	if !repo.AllowUnsigned {
		return verifyPolicy{required: true, reason: verifyCatalogNotAllowed, repo: owner}
	}
	// The opt-in covers the catalog as configured now. catalog add always
	// writes Source.Path=<SiteURL>/<name>/, and SiteURL is HTTPS whenever
	// AllowUnsigned is set, so a transfer pointing elsewhere was rendered
	// under older configuration (e.g. a v2.0.0 render of an AllowInsecure
	// http:// catalog) and must not inherit the permission.
	if transfer.Source.Path != repo.SiteURL+"/"+transfer.Component {
		return verifyPolicy{required: true, reason: verifyCatalogStaleSource, repo: owner}
	}
	return verifyPolicy{required: transfer.Transfer.Verify, reason: verifyByCatalog, repo: owner}
}

// explain describes why verification was required, for wrapping a missing
// signature error.
func (p verifyPolicy) explain(transfer *config.Transfer) string {
	switch p.reason {
	case verifyForced:
		return fmt.Sprintf("signature verification for %s is forced by client configuration (--verify)", transfer.Component)
	case verifyCatalogNotAllowed:
		switch {
		case errors.Is(p.loadErr, catalog.ErrRepoNotFound):
			return fmt.Sprintf("catalog %q that generated %s is not configured, so a signed SHA256SUMS is required", p.repo, transfer.Component)
		case p.loadErr != nil:
			return fmt.Sprintf("catalog %q configuration could not be loaded (%v), so a signed SHA256SUMS is required for %s", p.repo, p.loadErr, transfer.Component)
		}
		return fmt.Sprintf("catalog %q must publish a signed SHA256SUMS for %s: set AllowUnsigned=yes in %s.catalog to honor the catalog's Verify= setting, accepting HTTPS and SHA256 integrity checks without publisher authentication",
			p.repo, transfer.Component, p.repo)
	case verifyByCatalog:
		// The generated file, not the live catalog, is what was consulted: a
		// transfer rendered before the opt-in (or by v2.0.1/v2.0.2, which
		// dropped Verify=) requires a signature even if the catalog now says no.
		return fmt.Sprintf("the generated transfer for %s requires a signed SHA256SUMS: if catalog %q declares Verify=no, re-run 'updex catalog add %s/%s' to regenerate it under AllowUnsigned=yes",
			transfer.Component, p.repo, p.repo, transfer.Component)
	case verifyCatalogStaleSource:
		return fmt.Sprintf("the generated transfer for %s no longer fetches from catalog %q's SiteURL, so a signed SHA256SUMS is required: re-run 'updex catalog add %s/%s' to regenerate it",
			transfer.Component, p.repo, p.repo, transfer.Component)
	case verifyUnknownOwner:
		return fmt.Sprintf("transfer %s carries an updex catalog marker without a recognizable catalog, so a signed SHA256SUMS is required", transfer.Component)
	default:
		return fmt.Sprintf("transfer %s requires a signed SHA256SUMS; the publisher must provide SHA256SUMS.gpg", transfer.Component)
	}
}
