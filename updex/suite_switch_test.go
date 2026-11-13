package updex

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/frostyard/updex/v2/internal/fault"
)

// forky is the os-release of the host after `bootc switch` from trixie (13)
// to forky (14).
const forky = "ID=debian\nVERSION_ID=14\n"

// First boot after a suite switch: the link still names the trixie image.
// A suite-switch run downloads the forky image, links it and refreshes once,
// so the feature is merged on this boot (#511).
func TestUpdateFeatures_SuiteSwitchLinksRunningSuite(t *testing.T) {
	f := newSuiteFixture(t, forky, []string{"foo_1.0_13.raw"}, "foo_1.0_13.raw")
	f.server.publish("2.0_14")

	results, err := f.client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{SuiteSwitchOnly: true})
	if err != nil {
		t.Fatalf("UpdateFeatures(SuiteSwitchOnly) error = %v\n%s", err, f.out)
	}
	if len(results) != 1 || results[0].Feature != "grp" {
		t.Fatalf("results = %+v, want feature grp", results)
	}
	r := resultsByComponent(t, results)["foo"]
	if !r.Downloaded || !r.Linked || r.Version != "2.0" {
		t.Errorf("result = %+v, want 2.0 downloaded and linked", r)
	}
	if got := f.link(t); got != "foo_2.0_14.raw" {
		t.Errorf("link = %q, want foo_2.0_14.raw", got)
	}
	if f.runner.refreshes != 1 {
		t.Errorf("refreshes = %d, want 1", f.runner.refreshes)
	}
	if !strings.Contains(f.out.String(), "grp/foo: the sysext link points to foo_1.0_13.raw, an image of suite 13") {
		t.Errorf("output does not name the other-suite link:\n%s", f.out)
	}

	// The run is recorded as a run that names its features.
	data, err := os.ReadFile(f.lastRun)
	if err != nil {
		t.Fatalf("read last-run record: %v", err)
	}
	var rec LastRunRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatalf("parse last-run record: %v\n%s", err, data)
	}
	if !rec.Selected || rec.Refresh != "done" || len(rec.Features) != 1 || rec.Features[0].Outcome != "updated" {
		t.Errorf("last-run record = %s, want a selected run with refresh done and grp updated", data)
	}
}

// Switching back: the image of the running suite is still staged, so the run
// relinks it without a download, then refreshes.
func TestUpdateFeatures_SuiteSwitchRelinksStagedImage(t *testing.T) {
	f := newSuiteFixture(t, forky, []string{"foo_1.0_13.raw", "foo_1.0_14.raw"}, "foo_1.0_13.raw")
	f.server.publish("1.0_14")

	if _, err := f.client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{SuiteSwitchOnly: true}); err != nil {
		t.Fatalf("UpdateFeatures(SuiteSwitchOnly) error = %v\n%s", err, f.out)
	}
	if got := f.link(t); got != "foo_1.0_14.raw" {
		t.Errorf("link = %q, want foo_1.0_14.raw", got)
	}
	if got := f.server.fileGets.Load(); got != 0 {
		t.Errorf("downloads = %d, want 0", got)
	}
	if f.runner.refreshes != 1 {
		t.Errorf("refreshes = %d, want 1", f.runner.refreshes)
	}
}

// Every other boot: the link names the running suite's image. A suite-switch
// run takes no lock, downloads, links and refreshes nothing, and writes no
// last-run record, also when a newer image is published (the timer handles
// that).
func TestUpdateFeatures_SuiteSwitchNothingToDo(t *testing.T) {
	tests := []struct {
		name       string
		staged     []string
		linkTarget string
	}{
		{name: "own suite linked", staged: []string{"foo_1.0_14.raw"}, linkTarget: "foo_1.0_14.raw"},
		{name: "no link", staged: []string{"foo_1.0_14.raw"}},
		{name: "link to an image of no suite", staged: []string{"bar.raw"}, linkTarget: "bar.raw"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newSuiteFixture(t, forky, tt.staged, tt.linkTarget)
			f.server.publish("2.0_14")
			// A held lock proves the run takes none.
			release, err := f.client.lockMutation(false)
			if err != nil {
				t.Fatal(err)
			}
			defer release()

			results, err := f.client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{SuiteSwitchOnly: true})
			if err != nil {
				t.Fatalf("UpdateFeatures(SuiteSwitchOnly) error = %v", err)
			}
			if results == nil || len(results) != 0 {
				t.Errorf("results = %#v, want an empty, non-nil slice", results)
			}
			if got := f.link(t); got != filepath.Base(tt.linkTarget) && tt.linkTarget != "" {
				t.Errorf("link = %q, want it unchanged", got)
			}
			if got := f.server.fileGets.Load(); got != 0 {
				t.Errorf("downloads = %d, want 0", got)
			}
			if f.runner.refreshes != 0 {
				t.Errorf("refreshes = %d, want 0", f.runner.refreshes)
			}
			if _, err := os.Stat(f.lastRun); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("last-run record written: %v", err)
			}
		})
	}
}

// A disabled feature is not selected, even when its link names another suite.
func TestUpdateFeatures_SuiteSwitchSkipsDisabledFeature(t *testing.T) {
	f := newSuiteFixture(t, forky, []string{"foo_1.0_13.raw"}, "foo_1.0_13.raw")
	f.server.publish("2.0_14")
	if _, err := f.client.DisableFeature(t.Context(), "grp", DisableFeatureOptions{}); err != nil {
		t.Fatal(err)
	}

	results, err := f.client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{SuiteSwitchOnly: true})
	if err != nil || len(results) != 0 {
		t.Fatalf("UpdateFeatures(SuiteSwitchOnly) = %+v, %v; want nothing", results, err)
	}
	if got := f.link(t); got != "foo_1.0_13.raw" {
		t.Errorf("link = %q, want it unchanged", got)
	}
	if f.runner.refreshes != 0 {
		t.Errorf("refreshes = %d, want 0", f.runner.refreshes)
	}
}

// The run selects its own features and exists to refresh.
func TestUpdateFeatures_SuiteSwitchRefusesNamesAndNoRefresh(t *testing.T) {
	f := newSuiteFixture(t, forky, []string{"foo_1.0_13.raw"}, "foo_1.0_13.raw")
	for _, opts := range []UpdateFeaturesOptions{
		{SuiteSwitchOnly: true, Features: []string{"grp"}},
		{SuiteSwitchOnly: true, NoRefresh: true},
	} {
		_, err := f.client.UpdateFeatures(t.Context(), opts)
		if err == nil {
			t.Fatalf("UpdateFeatures(%+v) succeeded, want an error", opts)
		}
		if class, ok := fault.ClassOf(err); !ok || class != fault.Config {
			t.Errorf("UpdateFeatures(%+v) error class = %v, %v; want config", opts, class, ok)
		}
	}
	if got := f.link(t); got != "foo_1.0_13.raw" {
		t.Errorf("link = %q, want it unchanged", got)
	}
	if entries, _ := os.ReadDir(f.targetDir); !slices.EqualFunc(entries, []string{"foo_1.0_13.raw"}, func(e os.DirEntry, n string) bool { return e.Name() == n }) {
		t.Errorf("staging changed: %v", entries)
	}
}
