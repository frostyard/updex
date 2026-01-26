// Package updex provides a programmatic API for managing systemd-sysext images.
//
// This package allows you to download, verify, and manage sysext images from remote sources.
// It replicates the functionality of systemd-sysupdate for url-file transfers.
//
// Basic usage:
//
//	client := updex.NewClient(updex.ClientConfig{
//	    Verify: true,
//	})
//
//	results, err := client.List(ctx, updex.ListOptions{})
//	if err != nil {
//	    log.Fatal(err)
//	}
package updex

import (
	"github.com/frostyard/pm/progress"
	"github.com/frostyard/updex/internal/sysext"
)

// Client provides programmatic access to updex operations.
type Client struct {
	config ClientConfig
	helper *progress.ProgressHelper
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

	// Progress is an optional progress reporter for receiving progress updates.
	// If nil, no progress is reported.
	Progress progress.ProgressReporter

	// SysextRunner is an optional runner for systemd-sysext commands.
	// If nil, uses the default runner that executes real commands.
	// Set this in tests to inject a mock.
	SysextRunner sysext.SysextRunner
}

// NewClient creates a new updex API client with the given configuration.
func NewClient(cfg ClientConfig) *Client {
	if cfg.SysextRunner != nil {
		sysext.SetRunner(cfg.SysextRunner)
	}
	return &Client{
		config: cfg,
		helper: progress.NewProgressHelper(nil, cfg.Progress),
	}
}
