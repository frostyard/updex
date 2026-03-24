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
	"net/http"
	"time"

	"github.com/frostyard/std/reporter"
	"github.com/frostyard/updex/download"
	"github.com/frostyard/updex/sysext"
)

// Client provides programmatic access to updex operations.
type Client struct {
	config     ClientConfig
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
	// downloaded bytes. Return nil from the callback to disable progress for
	// a given download.
	OnDownloadProgress download.ProgressFunc

	// HTTPClient is an optional HTTP client used for all downloads and manifest
	// fetches. If nil, a default client with a 10-minute timeout is created.
	// Providing a shared client enables HTTP keep-alive connection reuse across
	// multiple downloads from the same host.
	HTTPClient *http.Client
}

// NewClient creates a new updex API client with the given configuration.
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
			Timeout: 10 * time.Minute,
		}
	}
	return &Client{
		config:     cfg,
		httpClient: hc,
		reporter:   r,
		runner:     sr,
	}
}

func (c *Client) msg(format string, a ...any) {
	c.reporter.Message(format, a...)
}

func (c *Client) warn(format string, a ...any) {
	c.reporter.Warning(format, a...)
}

func (c *Client) debug(format string, a ...any) {
	if c.config.Verbose {
		c.reporter.Message("debug: "+format, a...)
	}
}
