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
// unit files the frostyard-updex package installs under
// /usr/lib/systemd/system: the auto-update timer and service and
// updex-suite-switch.service (ADR-0021). scripts/units.sh runs it as a
// GoReleaser before-hook (next to completions.sh and manpages.sh), and
// updex/units_hook_contract_test.go runs the same script and compares the
// files with systemd.GenerateTimer/GenerateService of updex.DaemonUnits() and
// updex.SuiteSwitchUnit(), the text that `updex daemon enable` writes. The command is hidden: it is a
// build tool, not part of the administrator interface.
func newUnitsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "units",
		Short:  "Write the auto-update unit files (release tooling)",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE:   runUnits,
	}
	cmd.Flags().StringVar(&unitsOutputDir, "output-dir", "", "Directory to write updex-update.timer, updex-update.service and updex-suite-switch.service to")
	_ = cmd.MarkFlagRequired("output-dir")
	return cmd
}

func runUnits(cmd *cobra.Command, args []string) error {
	return writeUnits(unitsOutputDir)
}

// writeUnits writes updex-update.timer, updex-update.service and
// updex-suite-switch.service to dir,
// creating it when needed. The files carry no ownership marker: the package
// owns them, and `updex daemon disable` never removes files under
// /usr/lib/systemd/system. Each file is chmodded to 0644 after the write, so
// the mode the package installs (a package input, not a private file) does
// not depend on the process umask and a file that already existed with
// another mode is corrected.
func writeUnits(dir string) error {
	if dir == "" {
		return fmt.Errorf("output directory is empty")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	timer, service := updex.DaemonUnits()
	suite := updex.SuiteSwitchUnit()
	files := []struct{ name, content string }{
		{timer.Name + ".timer", systemd.GenerateTimer(timer)},
		{service.Name + ".service", systemd.GenerateService(service)},
		{suite.Name + ".service", systemd.GenerateService(suite)},
	}
	for _, file := range files {
		path := filepath.Join(dir, file.name)
		if err := os.WriteFile(path, []byte(file.content), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", file.name, err)
		}
		if err := os.Chmod(path, 0o644); err != nil {
			return fmt.Errorf("set mode of %s: %w", file.name, err)
		}
	}
	return nil
}
