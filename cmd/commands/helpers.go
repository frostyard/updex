package commands

import (
	"github.com/frostyard/updex/cmd/common"
	"github.com/frostyard/updex/updex"
)

// newClient creates a new updex client with the appropriate progress reporter.
func newClient() *updex.Client {
	var reporter *common.TextReporter
	if !common.JSONOutput {
		reporter = common.NewTextReporter()
	}

	cfg := updex.ClientConfig{
		Definitions: common.Definitions,
	}

	if reporter != nil {
		cfg.Progress = reporter
	}

	return updex.NewClient(cfg)
}
