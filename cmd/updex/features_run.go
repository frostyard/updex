package updex

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/frostyard/clix"
	"github.com/frostyard/updex/v2/updex"
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
	table.Rowf("FEATURE\tDESCRIPTION\tENABLED\tCATALOG\tTRANSFERS\tRUNNING\tLINKED\n")
	var pending []updex.PendingActivation
	var held []string
	var needsImage []string
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

		running, linked := formatTransferVersions(f.TransferStatus)
		table.Rowf("%s\t%s\t%s\t%s\t%s\t%s\t%s\n", f.Name, f.Description, status, formatOrigin(f), transfersStr, running, linked)

		for _, ts := range f.TransferStatus {
			if ts.RollbackRejected != "" {
				held = append(held, fmt.Sprintf("%s (rejected %s)", ts.Component, ts.RollbackRejected))
			}
			if ts.NeedsImage != "" {
				needsImage = append(needsImage, fmt.Sprintf("%s (%s)", ts.Component, ts.NeedsImage))
			}
			if ts.PendingReboot {
				pending = append(pending, updex.PendingActivation{
					Component:      ts.Component,
					RunningVersion: ts.RunningVersion,
					LinkedVersion:  ts.LinkedVersion,
				})
			}
		}
	}

	// Both failures matter: see runFeaturesUpdate.
	return errors.Join(table.Flush(), writePendingReboot(out, pending), writeHeldByRollback(out, held), writeNeedsImage(out, needsImage, ""))
}

// writeHeldByRollback prints the footer that marks transfers a rollback
// record holds: updates link nothing at or below the rejected version. It
// prints nothing when held is empty.
func writeHeldByRollback(out io.Writer, held []string) error {
	if len(held) == 0 {
		return nil
	}
	return writeLine(out, fmt.Sprintf("HELD BY ROLLBACK: %d sysext(s) that 'updex features update' leaves alone until a newer version is published: %s", len(held), strings.Join(held, ", ")))
}

// formatTransferVersions renders a feature's per-transfer versions for the
// RUNNING and LINKED columns. One transfer shows its value. Several show one
// value per transfer, in the order of the TRANSFERS column, joined by ", ".
// "-" stands for no version (not merged, or not linked), and "error" for a
// transfer whose merged state or link could not be read (the SDK warns).
// A feature with no transfers shows "-".
func formatTransferVersions(statuses []updex.TransferStatus) (running, linked string) {
	if len(statuses) == 0 {
		return "-", "-"
	}
	runs := make([]string, len(statuses))
	links := make([]string, len(statuses))
	for i, s := range statuses {
		runs[i], links[i] = dashIfEmpty(s.RunningVersion), dashIfEmpty(s.LinkedVersion)
		if s.Error != "" {
			runs[i], links[i] = "error", "error"
		}
	}
	return strings.Join(runs, ", "), strings.Join(links, ", ")
}

func dashIfEmpty(v string) string {
	if v == "" {
		return "-"
	}
	return v
}

// writePendingReboot prints the footer that marks sysexts whose linked
// version is not the running one: they take effect at the next boot (or the
// next systemd-sysext refresh). It prints nothing when pending is empty.
func writePendingReboot(out io.Writer, pending []updex.PendingActivation) error {
	if len(pending) == 0 {
		return nil
	}
	items := make([]string, len(pending))
	for i, p := range pending {
		items[i] = p.String()
	}
	return writeLine(out, fmt.Sprintf("PENDING REBOOT: %d sysext(s) linked to a version that is not running: %s", len(pending), strings.Join(items, ", ")))
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
				if result.RefreshSkipped != "" {
					fmt.Printf("%s. Reboot to activate it.\n", result.RefreshSkipped)
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
				if !featureDisableNow {
					fmt.Printf("Its extensions stay installed and merged. Run 'updex features disable %s --now' to remove them.\n", result.Feature)
				} else if featureDisableForce {
					fmt.Printf("Warning: Reboot required for changes to take effect.\n")
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

	// ADR-0016: a scheduled run that finds another updex operation running
	// is not a failure. systemd would otherwise mark updex-update.service
	// failed for a run that only had to wait for the next one. An operator
	// at a shell still gets the error.
	if errors.Is(err, updex.ErrBusy) && runningUnderSystemd() {
		if _, werr := fmt.Fprintf(cmd.ErrOrStderr(), "updex: %v; skipping this run\n", err); werr != nil {
			return werr
		}
		if clix.JSONOutput {
			// Keep stdout a JSON array for consumers that parse it.
			_, jsonErr := clix.OutputJSON([]updex.UpdateFeaturesResult{})
			return jsonErr
		}
		return nil
	}

	if errors.Is(err, updex.ErrBusy) && !clix.JSONOutput {
		// Nothing ran: do not print "no enabled features" next to the error.
		return err
	}

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
			} else if r.NeedsImage != "" {
				// ADR-0016: not an error; the old link stays.
				status = r.NeedsImage
				if r.DryRun {
					status = "would not link: " + status
				} else {
					status = "not linked: " + status
				}
			} else if r.NotLinked {
				status = "not linked: another transfer needs a newer image"
			} else if r.Held {
				status = "held by rollback"
				if r.RejectedVersion != "" {
					status += ": rejected " + r.RejectedVersion
				}
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
	return errors.Join(err, table.Flush(), writeNeedsImageUpdate(out, results), writeRefreshSkipped(out, results))
}

// writeRefreshSkipped prints the reason a run did not refresh systemd-sysext:
// the refresh merges into the running image, and a sysext it would activate
// needs a newer one (ADR-0016). The text is host-wide, so the first result
// that carries it is enough. It prints nothing when the refresh ran or was
// not requested.
func writeRefreshSkipped(out io.Writer, results []updex.UpdateFeaturesResult) error {
	for _, fr := range results {
		if fr.RefreshSkipped != "" {
			return writeLine(out, fr.RefreshSkipped)
		}
	}
	return nil
}

// writeNeedsImageUpdate prints, under the update table, why each transfer
// that needs a newer base image was not linked, with the next-boot image
// version that was compared. It prints nothing when no transfer needs one.
func writeNeedsImageUpdate(out io.Writer, results []updex.UpdateFeaturesResult) error {
	var errs []error
	for _, fr := range results {
		for _, r := range fr.Results {
			if r.NeedsImage == "" {
				continue
			}
			errs = append(errs, writeLine(out, fmt.Sprintf("%s/%s: %s", fr.Feature, r.Component, r.NextActionMessage)))
		}
	}
	return errors.Join(errs...)
}

// writeNeedsImage prints the footer that marks staged images that
// `features update` does not link because they need a newer base image
// (ADR-0016). items are "<component> (needs image >= X)". nextBoot is the
// next-boot image version that was compared, or empty. It prints nothing
// when items is empty.
func writeNeedsImage(out io.Writer, items []string, nextBoot string) error {
	if len(items) == 0 {
		return nil
	}
	line := fmt.Sprintf("NEEDS NEWER IMAGE: %d staged sysext(s) that 'updex features update' does not link until the host image is new enough: %s", len(items), strings.Join(items, ", "))
	if nextBoot != "" {
		line += fmt.Sprintf(" (next-boot image %s)", nextBoot)
	}
	return writeLine(out, line)
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
	table.Rowf("FEATURE\tCOMPONENT\tCURRENT\tRUNNING\tLINKED\tNEWEST\tUPDATE\n")
	var pending []updex.PendingActivation
	var needsImage []string
	nextBoot := ""
	for _, fr := range results {
		for _, r := range fr.Results {
			if r.NeedsImage != "" {
				needsImage = append(needsImage, fmt.Sprintf("%s (%s)", r.Component, r.NeedsImage))
			}
			if r.NextBootImageVersion != "" {
				nextBoot = r.NextBootImageVersion
			}
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
			case r.NeedsImage != "":
				// A staged image waits for a newer host image (ADR-0016).
				update = r.NeedsImage
			case r.UpdateAvailable:
				update = "yes"
			case r.RollbackRejected != "":
				update = "held (rejected " + r.RollbackRejected + ")"
			}
			table.Rowf("%s\t%s\t%s\t%s\t%s\t%s\t%s\n", fr.Feature, r.Component, current, dashIfEmpty(r.RunningVersion), dashIfEmpty(r.LinkedVersion), newest, update)
			if r.PendingReboot {
				pending = append(pending, updex.PendingActivation{
					Component:      r.Component,
					RunningVersion: r.RunningVersion,
					LinkedVersion:  r.LinkedVersion,
				})
			}
		}
	}

	// See runFeaturesUpdate: the SDK error and the output error are reported
	// together so neither is lost.
	return errors.Join(err, table.Flush(), writePendingReboot(out, pending), writeNeedsImage(out, needsImage, nextBoot))
}

func runFeaturesRollback(cmd *cobra.Command, args []string) error {
	// A dry run changes nothing and reads only world-readable state.
	if !clix.DryRun {
		if err := requireRoot(); err != nil {
			return err
		}
	}

	client := newClient()

	result, err := client.RollbackFeature(cmd.Context(), args[0], updex.RollbackFeatureOptions{
		To:        featureRollbackTo,
		Now:       featureRollbackNow,
		Clear:     featureRollbackClear,
		DryRun:    clix.DryRun,
		NoRefresh: noRefresh,
		Component: featureComponent,
	})

	if clix.JSONOutput {
		_, jsonErr := clix.OutputJSON(result)
		return errors.Join(err, jsonErr)
	}
	if result == nil {
		return err
	}
	// Nothing was changed: the returned error says why.
	if err != nil && result.RefreshError == "" {
		return err
	}

	out := cmd.OutOrStdout()
	prefix := ""
	if result.DryRun {
		prefix = "[DRY RUN] "
	}
	var outErr error
	if len(result.Transfers) > 0 {
		table := newTextTable(out)
		table.Rowf("FEATURE\tCOMPONENT\tFROM\tTO\tREJECTED\n")
		for _, t := range result.Transfers {
			table.Rowf("%s\t%s\t%s\t%s\t%s\n", result.Feature, t.Component, t.FromVersion, t.ToVersion, t.RejectedVersion)
		}
		outErr = table.Flush()
	}
	if result.RefreshError != "" {
		return errors.Join(err, outErr, writeLine(out, fmt.Sprintf("Error: %s", result.RefreshError)), writeLine(out, result.NextActionMessage))
	}
	return errors.Join(outErr, writeLine(out, prefix+result.NextActionMessage))
}
