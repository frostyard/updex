package commands

import (
	"github.com/frostyard/updex/cmd/common"
	"github.com/frostyard/updex/updex"
)

// newClient creates a new updex client with the appropriate progress reporter.
func newClient() *updex.Client {
	var reporter interface{}
	if !common.JSONOutput {
		reporter = common.NewTextReporter()
	}

	cfg := updex.ClientConfig{
		Definitions: common.Definitions,
		Verify:      common.Verify,
	}

	if reporter != nil {
		cfg.Progress = reporter.(*common.TextReporter)
	}

	return updex.NewClient(cfg)
}
