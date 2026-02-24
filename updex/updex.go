// Package updex provides a programmatic API for managing systemd-sysext features.
//
// This package allows you to enable, disable, and update sysext features
// using systemd-sysupdate and systemd-sysext under the hood.
//
// Basic usage:
//
//	client := updex.NewClient(updex.ClientConfig{})
//
//	features, err := client.Features(ctx)
//	if err != nil {
//	    log.Fatal(err)
//	}
package updex

import (
	"github.com/frostyard/pm/progress"
	"github.com/frostyard/updex/internal/sysext"
	"github.com/frostyard/updex/internal/sysupdate"
)

// Client provides programmatic access to updex operations.
type Client struct {
	config ClientConfig
	helper *progress.ProgressHelper
}

// ClientConfig holds configuration for the Client.
type ClientConfig struct {
	// Definitions is the custom path to directory containing .transfer and .feature files.
	// If empty, standard paths are used:
	//   - /etc/sysupdate.d/*.transfer
	//   - /run/sysupdate.d/*.transfer
	//   - /usr/local/lib/sysupdate.d/*.transfer
	//   - /usr/lib/sysupdate.d/*.transfer
	Definitions string

	// Progress is an optional progress reporter for receiving progress updates.
	// If nil, no progress is reported.
	Progress progress.ProgressReporter

	// SysextRunner is an optional runner for systemd-sysext commands.
	// If nil, uses the default runner that executes real commands.
	// Set this in tests to inject a mock.
	SysextRunner sysext.SysextRunner

	// SysupdateRunner is an optional runner for systemd-sysupdate commands.
	// If nil, uses the default runner that executes real commands.
	// Set this in tests to inject a mock.
	SysupdateRunner sysupdate.SysupdateRunner
}

// NewClient creates a new updex API client with the given configuration.
func NewClient(cfg ClientConfig) *Client {
	if cfg.SysextRunner != nil {
		sysext.SetRunner(cfg.SysextRunner)
	}
	if cfg.SysupdateRunner != nil {
		sysupdate.SetRunner(cfg.SysupdateRunner)
	}
	return &Client{
		config: cfg,
		helper: progress.NewProgressHelper(nil, cfg.Progress),
	}
}
