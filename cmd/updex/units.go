package updex

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/frostyard/updex/v2/systemd"
	"github.com/frostyard/updex/v2/updex"
	"github.com/spf13/cobra"
)

// unitsOutputDir is the --output-dir flag of the hidden units command.
var unitsOutputDir string

// newUnitsCmd returns the hidden release-time command that writes the
// timer and service unit files the frostyard-updex package installs under
// /usr/lib/systemd/system. scripts/units.sh runs it as a GoReleaser
// before-hook (next to completions.sh and manpages.sh), and
// updex/units_hook_contract_test.go runs the same script and compares the
// files with systemd.GenerateTimer/GenerateService(updex.DaemonUnits()), the
// text that `updex daemon enable` writes. The command is hidden: it is a
// build tool, not part of the administrator interface.
func newUnitsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "units",
		Short:  "Write the auto-update unit files (release tooling)",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE:   runUnits,
	}
	cmd.Flags().StringVar(&unitsOutputDir, "output-dir", "", "Directory to write updex-update.timer and updex-update.service to")
	_ = cmd.MarkFlagRequired("output-dir")
	return cmd
}

func runUnits(cmd *cobra.Command, args []string) error {
	return writeUnits(unitsOutputDir)
}

// writeUnits writes updex-update.timer and updex-update.service to dir,
// creating it when needed. The files carry no ownership marker: the package
// owns them, and `updex daemon disable` never removes files under
// /usr/lib/systemd/system.
func writeUnits(dir string) error {
	if dir == "" {
		return fmt.Errorf("output directory is empty")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	timer, service := updex.DaemonUnits()
	files := []struct{ name, content string }{
		{timer.Name + ".timer", systemd.GenerateTimer(timer)},
		{service.Name + ".service", systemd.GenerateService(service)},
	}
	for _, file := range files {
		if err := os.WriteFile(filepath.Join(dir, file.name), []byte(file.content), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", file.name, err)
		}
	}
	return nil
}
