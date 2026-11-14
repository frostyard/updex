package updex

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostyard/updex/v2/internal/testutil"
	"github.com/frostyard/updex/v2/systemd"
	sdk "github.com/frostyard/updex/v2/updex"
)

// TestUnitsCommandWritesGeneratorOutput runs the hidden units command that
// scripts/units.sh runs at release time, under a permissive and a strict
// umask: the 3 packaged files are 0644 either way (issue #524).
func TestUnitsCommandWritesGeneratorOutput(t *testing.T) {
	for _, tc := range []struct {
		name      string
		mask      int
		preCreate string
	}{
		{name: "umask 0022", mask: 0o022},
		// A file that already exists with another mode is corrected.
		{name: "umask 0077", mask: 0o077, preCreate: "updex-update.service"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testutil.Umask(t, tc.mask)
			dir := filepath.Join(t.TempDir(), "nested", "units")
			if tc.preCreate != "" {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, tc.preCreate), []byte("stale\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			root := NewRootCmd()
			root.SetArgs([]string{"units", "--output-dir", dir})
			root.SetOut(&bytes.Buffer{})
			root.SetErr(&bytes.Buffer{})
			if err := root.Execute(); err != nil {
				t.Fatalf("units failed: %v", err)
			}

			timer, service := sdk.DaemonUnits()
			for name, want := range map[string]string{
				"updex-update.timer":         systemd.GenerateTimer(timer),
				"updex-update.service":       systemd.GenerateService(service),
				"updex-suite-switch.service": systemd.GenerateService(sdk.SuiteSwitchUnit()),
			} {
				path := filepath.Join(dir, name)
				got, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("read %s: %v", name, err)
				}
				if string(got) != want {
					t.Errorf("%s differs from the generator:\n%s", name, got)
				}
				info, err := os.Lstat(path)
				if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o644 {
					t.Errorf("%s = %v, %v; want a regular 0644 file", name, info, err)
				}
			}
		})
	}
}

func TestUnitsCommandIsHiddenAndNeedsOutputDir(t *testing.T) {
	root := NewRootCmd()
	units, _, err := root.Find([]string{"units"})
	if err != nil || units.Name() != "units" {
		t.Fatalf("units command not registered: %v", err)
	}
	if !units.Hidden {
		t.Error("units command must be hidden: it is release tooling")
	}

	var help bytes.Buffer
	root.SetOut(&help)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(help.String(), "units") {
		t.Errorf("root help lists the hidden units command:\n%s", help.String())
	}

	root = NewRootCmd()
	root.SetArgs([]string{"units"})
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "output-dir") {
		t.Errorf("units without --output-dir error = %v, want a required-flag error", err)
	}

	if err := writeUnits(""); err == nil {
		t.Error("writeUnits(\"\") succeeded")
	}
}
