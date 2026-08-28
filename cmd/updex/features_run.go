package updex

import (
	"errors"
	"fmt"

	"github.com/frostyard/clix"
	"github.com/frostyard/updex/updex"
	"github.com/spf13/cobra"
)

func runFeaturesList(cmd *cobra.Command, args []string) error {
	client := newClient()

	features, err := client.Features(cmd.Context(), updex.FeaturesOptions{
		Component: featureComponent,
	})
	if err != nil {
		return err
	}

	if clix.JSONOutput {
		_, err = clix.OutputJSON(features)
		return err
	}

	out := cmd.OutOrStdout()

	if len(features) == 0 {
		return writeLine(out, "No features configured.")
	}

	table := newTextTable(out)
	table.Rowf("FEATURE\tDESCRIPTION\tENABLED\tCATALOG\tTRANSFERS\n")
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

		table.Rowf("%s\t%s\t%s\t%s\t%s\n", f.Name, f.Description, status, formatOrigin(f), transfersStr)
	}

	return table.Flush()
}

// formatOrigin renders a feature's origin for the CATALOG column: a bare
// catalog name for catalog-added features (the column header already says
// what it is), and a kind:detail form otherwise so nothing else can be
// misread as a catalog name.
func formatOrigin(f updex.FeatureInfo) string {
	switch {
	case f.Origin == updex.FeatureOriginCatalog:
		return f.OriginName
	case f.Origin == "" || f.Origin == updex.FeatureOriginUnknown:
		return updex.FeatureOriginUnknown
	case f.OriginName == "":
		// e.g. an image whose os-release names no identifier.
		return f.Origin
	default:
		return f.Origin + ":" + f.OriginName
	}
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
		Component: featureComponent,
	}

	result, err := client.EnableFeature(cmd.Context(), args[0], opts)

	if clix.JSONOutput {
		_, jsonErr := clix.OutputJSON(result)
		return errors.Join(err, jsonErr)
	} else if result != nil {
		switch {
		case result.RefreshError != "":
			// Everything but activation succeeded: show what was done, then
			// the failure and how to finish. The command still exits non-zero.
			fmt.Printf("Feature '%s' enabled.\n", result.Feature)
			printDownloadedFiles(result.DownloadedFiles)
			fmt.Printf("Error: %s\n%s\n", result.RefreshError, result.NextActionMessage)
		case result.Error != "":
			fmt.Printf("Error: %s\n", result.Error)
		case result.Success:
			if result.DryRun {
				fmt.Printf("[DRY RUN] %s\n", result.NextActionMessage)
			} else {
				fmt.Printf("Feature '%s' enabled.\n", result.Feature)
				if len(result.DownloadedFiles) > 0 {
					printDownloadedFiles(result.DownloadedFiles)
				} else if !featureEnableNow {
					fmt.Printf("Run 'updex features update' to download extensions.\n")
				}
			}
		}
	}

	return err
}

func printDownloadedFiles(files []string) {
	if len(files) == 0 {
		return
	}
	fmt.Printf("Downloaded %d extension(s):\n", len(files))
	for _, f := range files {
		fmt.Printf("  - %s\n", f)
	}
}

func printRemovedFiles(files []string) {
	if len(files) == 0 {
		return
	}
	fmt.Printf("Removed %d file(s):\n", len(files))
	for _, f := range files {
		fmt.Printf("  - %s\n", f)
	}
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
		Component: featureComponent,
	}

	result, err := client.DisableFeature(cmd.Context(), args[0], opts)

	if clix.JSONOutput {
		_, jsonErr := clix.OutputJSON(result)
		return errors.Join(err, jsonErr)
	} else if result != nil {
		switch {
		case result.RefreshError != "":
			// Unmerge and removal happened; only the re-merge refresh failed.
			// Show the state the host is in, then the failure and next step.
			fmt.Printf("Feature '%s' disabled.\n", result.Feature)
			if result.Unmerged {
				fmt.Printf("Extensions unmerged.\n")
			}
			printRemovedFiles(result.RemovedFiles)
			fmt.Printf("Error: %s\n%s\n", result.RefreshError, result.NextActionMessage)
		case result.Error != "":
			fmt.Printf("Error: %s\n", result.Error)
		case result.Success:
			if result.DryRun {
				fmt.Printf("[DRY RUN] %s\n", result.NextActionMessage)
			} else {
				fmt.Printf("Feature '%s' disabled.\n", result.Feature)
				if result.Unmerged {
					fmt.Printf("Extensions unmerged.\n")
				}
				printRemovedFiles(result.RemovedFiles)
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
		DryRun:    clix.DryRun,
		NoRefresh: noRefresh,
		NoVacuum:  featureUpdateNoVac,
		Component: featureComponent,
	}

	results, err := client.UpdateFeatures(cmd.Context(), opts)

	if clix.JSONOutput {
		// Never emit JSON `null` on stdout, even on the error path where the
		// client returns a nil slice (e.g. domain load failure). Consumers
		// parse this stream and expect an array. The error is still returned
		// (non-zero exit) via errors.Join below.
		if results == nil {
			results = []updex.UpdateFeaturesResult{}
		}
		_, jsonErr := clix.OutputJSON(results)
		return errors.Join(err, jsonErr)
	}

	out := cmd.OutOrStdout()

	if len(results) == 0 {
		return errors.Join(err, writeLine(out, "No enabled features with transfers found."))
	}

	if clix.DryRun {
		if lineErr := writeLine(out, "[DRY RUN] Previewing feature updates."); lineErr != nil {
			return errors.Join(err, lineErr)
		}
	}

	table := newTextTable(out)
	table.Rowf("FEATURE\tCOMPONENT\tVERSION\tSTATUS\n")
	for _, fr := range results {
		for _, r := range fr.Results {
			status := "error"
			if r.Error != "" {
				status = r.Error
			} else if r.DryRun && r.Downloaded {
				status = "would download"
			} else if r.Downloaded {
				status = "downloaded"
			} else if r.Installed {
				status = "up to date"
			}
			table.Rowf("%s\t%s\t%s\t%s\n", fr.Feature, r.Component, r.Version, status)
		}
	}

	// Both failures matter: the SDK error decides the exit status, and the
	// output error says the table the operator is reading is incomplete.
	return errors.Join(err, table.Flush())
}

func runFeaturesCheck(cmd *cobra.Command, args []string) error {
	client := newClient()

	results, err := client.CheckFeatures(cmd.Context(), updex.CheckFeaturesOptions{
		Component: featureComponent,
	})

	if clix.JSONOutput {
		// Never emit JSON `null` on stdout, even on the error path where the
		// client returns a nil slice (e.g. domain load failure). See
		// runFeaturesUpdate for the same rationale.
		if results == nil {
			results = []updex.CheckFeaturesResult{}
		}
		_, jsonErr := clix.OutputJSON(results)
		return errors.Join(err, jsonErr)
	}

	out := cmd.OutOrStdout()

	if len(results) == 0 {
		return errors.Join(err, writeLine(out, "No enabled features with transfers found."))
	}

	table := newTextTable(out)
	table.Rowf("FEATURE\tCOMPONENT\tCURRENT\tNEWEST\tUPDATE\n")
	for _, fr := range results {
		for _, r := range fr.Results {
			current := r.CurrentVersion
			if current == "" {
				current = "-"
			}
			newest := r.NewestVersion
			if newest == "" {
				newest = "-"
			}
			// A component that could not be checked is marked "error" so the table
			// never reads as "no update" for it. Details are available in r.Error and
			// may be reported on stderr by the SDK reporter (suppressed under --silent).
			update := "no"
			switch {
			case r.Error != "":
				update = "error"
			case r.UpdateAvailable:
				update = "yes"
			}
			table.Rowf("%s\t%s\t%s\t%s\t%s\n", fr.Feature, r.Component, current, newest, update)
		}
	}

	// See runFeaturesUpdate: the SDK error and the output error are reported
	// together so neither is lost.
	return errors.Join(err, table.Flush())
}
