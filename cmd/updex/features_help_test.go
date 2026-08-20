package updex

import (
	"bytes"
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

func TestRootRequiredDryRunExamplesUseSudo(t *testing.T) {
	tests := []struct {
		name string
		cmd  *cobra.Command
		want string
	}{
		{name: "features enable", cmd: newFeaturesEnableCmd(), want: "sudo updex features enable --dry-run docker"},
		{name: "features disable", cmd: newFeaturesDisableCmd(), want: "sudo updex features disable --dry-run docker --now"},
		{name: "catalog add", cmd: newCatalogAddCmd(), want: "sudo updex catalog add zoxide --dry-run"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var output bytes.Buffer
			tt.cmd.SetOut(&output)
			if err := tt.cmd.Help(); err != nil {
				t.Fatalf("render help: %v", err)
			}
			if !strings.Contains(output.String(), tt.want) {
				t.Errorf("%s help does not contain %q:\n%s", tt.name, tt.want, output.String())
			}
		})
	}
}
