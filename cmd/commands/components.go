package commands

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/frostyard/updex/cmd/common"
	"github.com/frostyard/updex/internal/config"
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

// ComponentInfo represents component information for output
type ComponentInfo struct {
	Name         string `json:"name"`
	Source       string `json:"source"`
	SourceType   string `json:"source_type"`
	TargetPath   string `json:"target_path"`
	InstancesMax int    `json:"instances_max"`
}

func runComponents(cmd *cobra.Command, args []string) error {
	transfers, err := config.LoadTransfers(common.Definitions)
	if err != nil {
		return fmt.Errorf("failed to load transfer configs: %w", err)
	}

	if len(transfers) == 0 {
		fmt.Println("No components configured.")
		return nil
	}

	var components []ComponentInfo

	for _, t := range transfers {
		info := ComponentInfo{
			Name:         t.Component,
			Source:       t.Source.Path,
			SourceType:   t.Source.Type,
			TargetPath:   t.Target.Path,
			InstancesMax: t.Transfer.InstancesMax,
		}
		components = append(components, info)
	}

	if common.JSONOutput {
		items := make([]interface{}, len(components))
		for i, c := range components {
			items[i] = c
		}
		common.OutputJSONLines(items)
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
