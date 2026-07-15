package updex

import (
	"fmt"
	"os"
	"text/tabwriter"

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

	if len(components) == 0 {
		fmt.Println("No components discovered.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "COMPONENT\tSOURCE\tFEATURES")
	for _, c := range components {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%d\n", c.Name, c.SourceDir, c.FeatureCount)
	}
	_ = w.Flush()

	return nil
}
