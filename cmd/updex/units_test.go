package updex

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostyard/updex/v2/systemd"
	sdk "github.com/frostyard/updex/v2/updex"
)

// TestUnitsCommandWritesGeneratorOutput runs the hidden units command that
// scripts/units.sh runs at release time.
func TestUnitsCommandWritesGeneratorOutput(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "units")
	root := NewRootCmd()
	root.SetArgs([]string{"units", "--output-dir", dir})
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	if err := root.Execute(); err != nil {
		t.Fatalf("units failed: %v", err)
	}

	timer, service := sdk.DaemonUnits()
	for name, want := range map[string]string{
		"updex-update.timer":   systemd.GenerateTimer(timer),
		"updex-update.service": systemd.GenerateService(service),
	} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if string(got) != want {
			t.Errorf("%s differs from the generator:\n%s", name, got)
		}
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
