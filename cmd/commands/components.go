package commands

import (
	"os"

	"github.com/frostyard/std/reporter"
	"github.com/frostyard/updex/cmd/common"
	"github.com/frostyard/updex/updex"
)

// newClient creates a new updex client with the appropriate progress reporter.
func newClient() *updex.Client {
	var r reporter.Reporter
	if !common.JSONOutput {
		r = reporter.NewTextReporter(os.Stderr)
	}

	return updex.NewClient(updex.ClientConfig{
		Definitions: common.Definitions,
		Verify:      common.Verify,
		Progress:    r,
	})
}
