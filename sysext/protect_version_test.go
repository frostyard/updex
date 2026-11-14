package sysext

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/frostyard/updex/v2/config"
	"github.com/frostyard/updex/v2/internal/fault"
)

// A transfer whose ProtectVersion= has an empty specifier (%w with no
// VERSION_ID in os-release) must not be vacuumed: every vacuum and vacuum
// plan entry point returns an error that keeps the config class and says
// that nothing was removed, and no staged image is deleted, also beyond
// InstancesMax.
func TestVacuumRefusesProtectVersionWithEmptySpecifier(t *testing.T) {
	for _, protect := range []string{"%w", "1%w"} {
		t.Run(protect, func(t *testing.T) {
			dir := t.TempDir()
			osRelease := filepath.Join(dir, "os-release")
			if err := os.WriteFile(osRelease, []byte("ID=debian\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			staging := filepath.Join(dir, "staging")
			defs := filepath.Join(dir, "defs")
			for _, d := range []string{staging, defs} {
				if err := os.Mkdir(d, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			staged := []string{"myext_0.1.raw", "myext_0.2.raw", "myext_0.3.raw", "myext_1.raw", "myext_2.0.raw"}
			for _, name := range staged {
				if err := os.WriteFile(filepath.Join(staging, name), []byte(name), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			content := "[Transfer]\nInstancesMax=2\nProtectVersion=" + protect +
				"\n\n[Source]\nType=url-file\nPath=https://example.com\nMatchPattern=myext_@v.raw\n" +
				"\n[Target]\nPath=" + staging + "\nMatchPattern=myext_@v.raw\n"
			if err := os.WriteFile(filepath.Join(defs, "myext.transfer"), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			transfers, err := config.LoadTransfersIn(defs, nil, []string{osRelease})
			if err != nil || len(transfers) != 1 {
				t.Fatalf("LoadTransfersIn() = %d transfers, %v", len(transfers), err)
			}
			tr := transfers[0]
			if tr.ProtectVersionError == nil {
				t.Fatal("ProtectVersionError = nil, want the empty specifier")
			}

			paths := VacuumPaths{DefaultDir: staging, LinkDir: t.TempDir(), Hierarchies: []string{t.TempDir()}}
			wantMsg := `ProtectVersion "` + protect + `": specifier %w is empty: os-release has no VERSION_ID; nothing was removed`
			calls := map[string]func() (removed []string, err error){
				"VacuumWith": func() ([]string, error) {
					removed, _, err := VacuumWith(tr, paths)
					return removed, err
				},
				"VacuumWithReport": func() ([]string, error) {
					r, err := VacuumWithReport(tr, paths)
					return append(r.Removed, r.OtherSuiteRemoved...), err
				},
				"PlanVacuumWith": func() ([]string, error) {
					removed, _, err := PlanVacuumWith(tr, "3.0", paths)
					return removed, err
				},
				"PlanVacuumWithReport": func() ([]string, error) {
					r, err := PlanVacuumWithReport(tr, "3.0", paths)
					return append(r.Removed, r.OtherSuiteRemoved...), err
				},
				"PlanVacuumAfterInstallAt": func() ([]string, error) {
					removed, _, err := PlanVacuumAfterInstallAt(tr, "3.0", staging)
					return removed, err
				},
				"PlanVacuumAfterInstall": func() ([]string, error) {
					removed, _, err := PlanVacuumAfterInstall(tr, "3.0")
					return removed, err
				},
				"VacuumAt": func() ([]string, error) {
					return nil, VacuumAt(tr, staging)
				},
				"Vacuum": func() ([]string, error) {
					return nil, Vacuum(tr)
				},
				"VacuumWithDetails": func() ([]string, error) {
					removed, _, err := VacuumWithDetails(tr)
					return removed, err
				},
			}
			for name, call := range calls {
				removed, err := call()
				if err == nil || err.Error() != wantMsg {
					t.Errorf("%s() error = %v, want %q", name, err, wantMsg)
				}
				if !errors.Is(err, ErrProtectVersionEmptySpecifier) || !errors.Is(err, tr.ProtectVersionError) {
					t.Errorf("%s() error = %v, want it to wrap the sentinel and the ProtectVersion error", name, err)
				}
				if class, ok := fault.ClassOf(err); !ok || class != fault.Config {
					t.Errorf("%s() class = %v, %v; want config", name, class, ok)
				}
				if len(removed) != 0 {
					t.Errorf("%s() removed = %v, want none", name, removed)
				}
				entries, err := os.ReadDir(staging)
				if err != nil {
					t.Fatal(err)
				}
				var got []string
				for _, e := range entries {
					got = append(got, e.Name())
				}
				if !slices.Equal(got, staged) {
					t.Fatalf("%s() changed the staging dir: %v, want %v", name, got, staged)
				}
			}
		})
	}
}

// A transfer whose ProtectVersion= expanded fully is vacuumed as before.
func TestVacuumProtectVersionWithoutErrorStillVacuums(t *testing.T) {
	staging := t.TempDir()
	for _, name := range []string{"myext_1.0.raw", "myext_2.0.raw", "myext_3.0.raw", "myext_4.0.raw"} {
		if err := os.WriteFile(filepath.Join(staging, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tr := &config.Transfer{
		Component: "myext",
		Transfer:  config.TransferSection{InstancesMax: 2, ProtectVersion: "1.0"},
		Target:    config.TargetSection{Path: staging, MatchPattern: "myext_@v.raw"},
	}
	removed, _, err := VacuumWith(tr, VacuumPaths{DefaultDir: staging})
	if err != nil {
		t.Fatalf("VacuumWith() error = %v", err)
	}
	if !slices.Equal(removed, []string{"2.0"}) {
		t.Errorf("removed = %v, want [2.0]", removed)
	}
	if _, err := os.Stat(filepath.Join(staging, "myext_1.0.raw")); err != nil {
		t.Errorf("protected image removed: %v", err)
	}
}
