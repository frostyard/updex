package updex

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/frostyard/clix"
	"github.com/frostyard/updex/updex"
	"github.com/spf13/cobra"
)

func runFeaturesList(cmd *cobra.Command, args []string) error {
	client := newClient()

	features, err := client.Features(cmd.Context())
	if err != nil {
		return err
	}

	if clix.JSONOutput {
		clix.OutputJSON(features)
		return nil
	}

	if len(features) == 0 {
		fmt.Println("No features configured.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "FEATURE\tDESCRIPTION\tENABLED\tTRANSFERS")
	for _, f := range features {
		status := "no"
		if f.Masked {
			status = "masked"
		} else if f.Enabled {
			status = "yes"
		}

		transfersStr := "-"
		if len(f.Transfers) > 0 {
			transfersStr = ""
			for i, t := range f.Transfers {
				if i > 0 {
					transfersStr += ", "
				}
				transfersStr += t
			}
		}

		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", f.Name, f.Description, status, transfersStr)
	}
	_ = w.Flush()

	return nil
}

func runFeaturesEnable(cmd *cobra.Command, args []string) error {
	if err := requireRoot(); err != nil {
		return err
	}

	client := newClient()

	opts := updex.EnableFeatureOptions{
		Now:       featureEnableNow,
		DryRun:    clix.DryRun,
		NoRefresh: noRefresh,
	}

	result, err := client.EnableFeature(cmd.Context(), args[0], opts)

	if clix.JSONOutput {
		clix.OutputJSON(result)
	} else if result != nil {
		if result.Error != "" {
			fmt.Printf("Error: %s\n", result.Error)
		} else if result.Success {
			if result.DryRun {
				fmt.Printf("[DRY RUN] %s\n", result.NextActionMessage)
			} else {
				fmt.Printf("Feature '%s' enabled.\n", result.Feature)
				if len(result.DownloadedFiles) > 0 {
					fmt.Printf("Downloaded %d extension(s):\n", len(result.DownloadedFiles))
					for _, f := range result.DownloadedFiles {
						fmt.Printf("  - %s\n", f)
					}
				} else if !featureEnableNow {
					fmt.Printf("Run 'updex features update' to download extensions.\n")
				}
			}
		}
	}

	return err
}

func runFeaturesDisable(cmd *cobra.Command, args []string) error {
	if err := requireRoot(); err != nil {
		return err
	}

	client := newClient()

	opts := updex.DisableFeatureOptions{
		Now:       featureDisableNow,
		Force:     featureDisableForce,
		DryRun:    clix.DryRun,
		NoRefresh: noRefresh,
	}

	result, err := client.DisableFeature(cmd.Context(), args[0], opts)

	if clix.JSONOutput {
		clix.OutputJSON(result)
	} else if result != nil {
		if result.Error != "" {
			fmt.Printf("Error: %s\n", result.Error)
		} else if result.Success {
			if result.DryRun {
				fmt.Printf("[DRY RUN] %s\n", result.NextActionMessage)
			} else {
				fmt.Printf("Feature '%s' disabled.\n", result.Feature)
				if result.Unmerged {
					fmt.Printf("Extensions unmerged.\n")
				}
				if len(result.RemovedFiles) > 0 {
					fmt.Printf("Removed %d file(s):\n", len(result.RemovedFiles))
					for _, f := range result.RemovedFiles {
						fmt.Printf("  - %s\n", f)
					}
				}
				if featureDisableForce {
					fmt.Printf("Warning: Reboot required for changes to take effect.\n")
				} else if !featureDisableNow {
					fmt.Printf("Run 'updex features update' to apply changes.\n")
				}
			}
		}
	}

	return err
}

func runFeaturesUpdate(cmd *cobra.Command, args []string) error {
	if err := requireRoot(); err != nil {
		return err
	}

	client := newClient()

	opts := updex.UpdateFeaturesOptions{
		NoRefresh: noRefresh,
		NoVacuum:  featureUpdateNoVac,
	}

	results, err := client.UpdateFeatures(cmd.Context(), opts)

	if clix.JSONOutput {
		clix.OutputJSON(results)
		return err
	}

	if len(results) == 0 {
		fmt.Println("No enabled features with transfers found.")
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "FEATURE\tCOMPONENT\tVERSION\tSTATUS")
	for _, fr := range results {
		for _, r := range fr.Results {
			status := "error"
			if r.Error != "" {
				status = r.Error
			} else if r.Downloaded {
				status = "downloaded"
			} else if r.Installed {
				status = "up to date"
			}
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", fr.Feature, r.Component, r.Version, status)
		}
	}
	_ = w.Flush()

	return err
}

func runFeaturesCheck(cmd *cobra.Command, args []string) error {
	client := newClient()

	results, err := client.CheckFeatures(cmd.Context(), updex.CheckFeaturesOptions{})

	if clix.JSONOutput {
		clix.OutputJSON(results)
		return err
	}

	if len(results) == 0 {
		fmt.Println("No enabled features with transfers found.")
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "FEATURE\tCOMPONENT\tCURRENT\tNEWEST\tUPDATE")
	for _, fr := range results {
		for _, r := range fr.Results {
			update := "no"
			if r.UpdateAvailable {
				update = "yes"
			}
			current := r.CurrentVersion
			if current == "" {
				current = "-"
			}
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", fr.Feature, r.Component, current, r.NewestVersion, update)
		}
	}
	_ = w.Flush()

	return err
}
