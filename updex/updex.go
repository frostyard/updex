// Package updex provides a programmatic API for managing systemd-sysext images
// through a feature-based interface.
//
// Features are the primary unit of management. Each feature groups related
// sysext transfers that can be enabled, disabled, updated, and checked together.
//
// Basic usage:
//
//	client := updex.NewClient(updex.ClientConfig{
//	    Verify: true,
//	})
//
//	results, err := client.UpdateFeatures(ctx, updex.UpdateFeaturesOptions{})
//	if err != nil {
//	    log.Fatal(err)
//	}
package updex

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/frostyard/std/reporter"
	"github.com/frostyard/updex/catalog"
	"github.com/frostyard/updex/config"
	"github.com/frostyard/updex/download"
	"github.com/frostyard/updex/sysext"
)

// RuntimePaths holds the filesystem paths an updex.Client consults at
// runtime. Zero values resolve to the current production defaults so the
// CLI and existing SDK callers require no change. Values are captured
// immutably at NewClient: subsequent mutations to the compatibility package
// variables (config.SearchRoots, catalog.ConfigRoots, etc.) cannot redirect
// an already-constructed client.
//
// Set non-zero fields only to override specific paths; mixing zero and
// non-zero fields within one RuntimePaths is valid.
type RuntimePaths struct {
	// DefinitionRoots are the systemd-style root directories searched for
	// sysupdate.d and sysupdate.<name>.d directories (see
	// config.ComponentSearchPaths). Zero value uses config.SearchRoots
	// (/etc, /run, /usr/local/lib, /usr/lib).
	DefinitionRoots []string

	// OSReleasePaths are the os-release files consulted for
	// %-specifier expansion and image-name identification. Zero value uses
	// config.OSReleasePaths (/etc/os-release, /usr/lib/os-release).
	OSReleasePaths []string

	// CatalogConfigRoots are the directories scanned for *.catalog repo
	// definition files (see catalog.LoadRepos). Zero value uses
	// catalog.ConfigRoots.
	CatalogConfigRoots []string

	// CatalogCacheDir is the directory for catalog listing caches.
	// Zero value captures catalog.CacheDir at construction.
	//
	// Use the sentinel value DisableCatalogCache to explicitly disable
	// caching without affecting other clients.
	CatalogCacheDir string

	// CatalogTargetPath is the trusted staging directory for catalog
	// transfers. Zero value uses catalog.TargetPath
	// (/var/lib/extensions.d).
	CatalogTargetPath string

	// SysextLinkDir is the directory where systemd-sysext looks for
	// extension images. Zero value uses sysext.SysextDir
	// (/var/lib/extensions).
	SysextLinkDir string
}

// DisableCatalogCache is a sentinel value for RuntimePaths.CatalogCacheDir
// that explicitly disables catalog listing caching for a client without
// affecting other clients or the package-level CacheDir variable.
const DisableCatalogCache = "\x00"

// runtimePaths holds the resolved (never-zero) path values captured at
// client construction. The zero-value resolution happens once in NewClient.
type runtimePaths struct {
	definitionRoots    []string
	osReleasePaths     []string
	catalogConfigRoots []string
	catalogCacheDir    string // "" means disabled
	catalogTargetPath  string
	sysextLinkDir      string
}

// resolveRuntimePaths converts a RuntimePaths (zero = default) to a fully
// resolved runtimePaths, reading the package-level globals exactly once and
// taking defensive copies of all slices so later global mutations cannot
// affect the client.
func resolveRuntimePaths(rp RuntimePaths) runtimePaths {
	p := runtimePaths{}

	if len(rp.DefinitionRoots) > 0 {
		p.definitionRoots = slices.Clone(rp.DefinitionRoots)
	} else {
		p.definitionRoots = slices.Clone(config.SearchRoots)
	}

	if len(rp.OSReleasePaths) > 0 {
		p.osReleasePaths = slices.Clone(rp.OSReleasePaths)
	} else {
		p.osReleasePaths = slices.Clone(config.OSReleasePaths)
	}

	if len(rp.CatalogConfigRoots) > 0 {
		p.catalogConfigRoots = slices.Clone(rp.CatalogConfigRoots)
	} else {
		p.catalogConfigRoots = slices.Clone(catalog.ConfigRoots)
	}

	switch rp.CatalogCacheDir {
	case DisableCatalogCache:
		p.catalogCacheDir = ""
	case "":
		p.catalogCacheDir = catalog.CacheDir
	default:
		p.catalogCacheDir = rp.CatalogCacheDir
	}

	if rp.CatalogTargetPath != "" {
		p.catalogTargetPath = rp.CatalogTargetPath
	} else {
		p.catalogTargetPath = catalog.TargetPath
	}

	if rp.SysextLinkDir != "" {
		p.sysextLinkDir = rp.SysextLinkDir
	} else {
		p.sysextLinkDir = sysext.SysextDir
	}

	return p
}

// Client provides programmatic access to updex operations.
type Client struct {
	config     ClientConfig
	paths      runtimePaths
	httpClient *http.Client
	reporter   reporter.Reporter
	runner     sysext.SysextRunner
}

// ClientConfig holds configuration for the Client.
type ClientConfig struct {
	// Definitions is the custom path to directory containing .transfer files.
	// If empty, standard paths are used:
	//   - /etc/sysupdate.d/*.transfer
	//   - /run/sysupdate.d/*.transfer
	//   - /usr/local/lib/sysupdate.d/*.transfer
	//   - /usr/lib/sysupdate.d/*.transfer
	Definitions string

	// Verify enables GPG signature verification on SHA256SUMS files.
	Verify bool

	// Verbose enables debug-level output through the Progress reporter.
	Verbose bool

	// Progress is an optional progress reporter for receiving progress updates.
	// If nil, no progress is reported.
	Progress reporter.Reporter

	// SysextRunner is an optional runner for systemd-sysext commands.
	// If nil, uses the default runner that executes real commands.
	// Set this in tests to inject a mock.
	SysextRunner sysext.SysextRunner

	// OnDownloadProgress is an optional callback for download progress tracking.
	// If non-nil, it is passed to [download.Download] and called with the
	// response content length (-1 if unknown). The returned io.Writer receives
	// downloaded bytes. Retries call this once per attempt, so return a fresh
	// independent writer each time to avoid double-counting progress. Return
	// nil from the callback to disable progress for a given download.
	OnDownloadProgress download.ProgressFunc

	// HTTPClient is an optional HTTP client used for all downloads and manifest
	// fetches. If nil, a default client with a 10-minute timeout is created.
	// Providing a shared client enables HTTP keep-alive connection reuse across
	// multiple downloads from the same host.
	HTTPClient *http.Client

	// Paths holds the filesystem paths this client consults at runtime.
	// Zero values resolve to current production defaults at NewClient time.
	// See RuntimePaths for field-by-field documentation.
	Paths RuntimePaths
}

// NewClient creates a new updex API client with the given configuration.
// Filesystem paths in cfg.Paths are resolved once at construction: the
// client captures immutable copies of all path configuration, so subsequent
// mutations to package-level globals (config.SearchRoots, catalog.ConfigRoots,
// etc.) cannot redirect this client.
func NewClient(cfg ClientConfig) *Client {
	r := cfg.Progress
	if r == nil {
		r = reporter.NoopReporter{}
	}
	sr := cfg.SysextRunner
	if sr == nil {
		sr = &sysext.DefaultRunner{}
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{
			Timeout:       10 * time.Minute,
			CheckRedirect: checkSecureRedirect,
		}
	}
	return &Client{
		config:     cfg,
		paths:      resolveRuntimePaths(cfg.Paths),
		httpClient: hc,
		reporter:   r,
		runner:     sr,
	}
}

func checkSecureRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return fmt.Errorf("stopped after 10 redirects")
	}
	if len(via) > 0 &&
		strings.EqualFold(via[len(via)-1].URL.Scheme, "https") &&
		strings.EqualFold(req.URL.Scheme, "http") {
		return fmt.Errorf("refusing redirect downgrade from https to http")
	}
	return nil
}

func (c *Client) msg(format string, a ...any) {
	c.reporter.Message(format, a...)
}

func (c *Client) warn(format string, a ...any) {
	c.reporter.Warning(format, a...)
}

func (c *Client) retryNotify(what string) func(attempt, maxAttempts int, reason error) {
	return func(attempt, maxAttempts int, reason error) {
		c.warn("retrying %s (attempt %d/%d): %v", what, attempt, maxAttempts, reason)
	}
}

func (c *Client) debug(format string, a ...any) {
	if c.config.Verbose {
		c.reporter.Message("debug: "+format, a...)
	}
}
