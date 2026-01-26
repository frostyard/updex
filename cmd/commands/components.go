package commands

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/frostyard/updex/cmd/common"
	"github.com/frostyard/updex/updex"
	"github.com/spf13/cobra"
)

// NewComponentsCmd creates the components command
func NewComponentsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "components",
		Short: "List available components",
		Long:  `List all components defined in transfer configuration files.`,
		RunE:  runComponents,
	}
}

func runComponents(cmd *cobra.Command, args []string) error {
	client := newClient()

	components, err := client.Components(context.Background())
	if err != nil {
		return err
	}

	if common.JSONOutput {
		common.OutputJSON(components)
		return nil
	}

	if len(components) == 0 {
		fmt.Println("No components configured.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "COMPONENT\tSOURCE TYPE\tTARGET PATH\tINSTANCES MAX")
	for _, c := range components {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%d\n", c.Name, c.SourceType, c.TargetPath, c.InstancesMax)
	}
	_ = w.Flush()

	return nil
}

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
