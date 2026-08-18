package updex

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestFeaturesMutationHelpNamesBothDropInPaths pins the help text of
// `features enable` and `features disable` to the drop-in locations
// writeFeatureDropIn actually uses: the legacy default directory and the
// component-scoped directory (see README "Components" and
// docs/design/overview.md). A future edit that drops either path from the
// Long text fails here.
func TestFeaturesMutationHelpNamesBothDropInPaths(t *testing.T) {
	t.Parallel()

	const (
		legacyPath    = "/etc/sysupdate.d/<feature>.feature.d/00-updex.conf"
		componentPath = "/etc/sysupdate.<component>.d/<feature>.feature.d/00-updex.conf"
	)

	tests := []struct {
		name string
		cmd  *cobra.Command
	}{
		{name: "enable", cmd: newFeaturesEnableCmd()},
		{name: "disable", cmd: newFeaturesDisableCmd()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			for _, want := range []string{legacyPath, componentPath} {
				if !strings.Contains(tt.cmd.Long, want) {
					t.Errorf("features %s help does not mention %q:\n%s", tt.name, want, tt.cmd.Long)
				}
			}
		})
	}
}
