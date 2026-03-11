package updex

import (
	"github.com/frostyard/clix"
	"github.com/frostyard/updex/updex"
)

// newClient creates a new updex client with the appropriate progress reporter.
func newClient() *updex.Client {
	return updex.NewClient(updex.ClientConfig{
		Definitions: definitions,
		Verify:      verify,
		Progress:    clix.NewReporter(),
	})
}
