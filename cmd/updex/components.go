package updex

import (
	"github.com/frostyard/clix"
	"github.com/spf13/cobra"
)

func newComponentsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "components",
		Short: "List discovered systemd-sysupdate components",
		Long: `List the systemd-sysupdate components discovered on the system.

A component is a named grouping of .transfer/.feature files under a
sysupdate.<name>.d directory (see sysupdate.d(5) "Components"), searched
across /etc, /run, /usr/local/lib, and /usr/lib in that priority order. This
does not list the legacy default sysupdate.d directory itself; use
'updex features list' (which reads the union of the default directory and
every component below) to see everything.

OUTPUT COLUMNS:
  COMPONENT  - Component name
  SOURCE     - Highest-priority directory providing this component
  FEATURES   - Number of .feature files defined by this component`,
		Example: `  # List discovered components
  updex components

  # List in JSON format
  updex components --json`,
		Args: cobra.NoArgs,
		RunE: runComponents,
	}
}

func runComponents(cmd *cobra.Command, args []string) error {
	client := newClient()

	components, err := client.Components(cmd.Context())
	if err != nil {
		return err
	}

	if clix.JSONOutput {
		_, err = clix.OutputJSON(components)
		return err
	}

	out := cmd.OutOrStdout()

	if len(components) == 0 {
		return writeLine(out, "No components discovered.")
	}

	table := newTextTable(out)
	table.Rowf("COMPONENT\tSOURCE\tFEATURES\n")
	for _, c := range components {
		table.Rowf("%s\t%s\t%d\n", c.Name, c.SourceDir, c.FeatureCount)
	}

	return table.Flush()
}
