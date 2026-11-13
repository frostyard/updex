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

// addBar adds feature "bar" with transfer "bar" (bar_@v.raw, no %w), its
// staged images, its link target and its merged version ("" for not merged).
func (f *suiteFixture) addBar(t *testing.T, staged []string, linkTarget, merged string) {
	t.Helper()
	createFeatureFile(t, f.defDir, "bar", true)
	transfer := "[Transfer]\nFeatures=bar\nVerify=false\n\n[Source]\nType=url-file\nPath=" + f.server.URL +
		"\nMatchPattern=bar_@v.raw\n\n[Target]\nPath=" + f.targetDir + "\nMatchPattern=bar_@v.raw\n"
	if err := os.WriteFile(filepath.Join(f.defDir, "bar.transfer"), []byte(transfer), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range staged {
		if err := os.WriteFile(filepath.Join(f.targetDir, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if linkTarget != "" {
		if err := os.Symlink(filepath.Join(f.targetDir, linkTarget), filepath.Join(f.linkDir, "bar.raw")); err != nil {
			t.Fatal(err)
		}
	}
	if merged != "" {
		data, err := json.Marshal(map[string]any{"extensions": map[string]any{
			"bar": map[string]string{"path": filepath.Join(f.targetDir, merged)},
		}})
		if err != nil {
			t.Fatal(err)
		}
		writeMergedState(t, f.hierarchy, string(data))
	}
}

func (f *suiteFixture) barLink(t *testing.T) string {
	t.Helper()
	target, err := os.Readlink(filepath.Join(f.linkDir, "bar.raw"))
	if err != nil {
		return ""
	}
	return filepath.Base(target)
}

// The timer linked a newer bar image with --no-refresh earlier on this boot:
// it waits for the next boot (ADR-0016). The suite-switch run keeps its new
// link, skips the refresh and says why; it is not an error. So does a bar
// that is merged but not linked: the refresh would unmerge it. With nothing
// else pending (bar runs what it links), it refreshes.
func TestUpdateFeatures_SuiteSwitchKeepsOtherActivationsForNextBoot(t *testing.T) {
	tests := []struct {
		name        string
		barLink     string
		barMerged   string
		wantRefresh int
		wantSkip    string
	}{
		{name: "other update linked by the timer", barLink: "bar_2.0.raw", barMerged: "bar_1.0.raw",
			wantSkip: "refresh skipped: bar has an update that waits for the next boot; the suite switch takes effect at the next boot"},
		{name: "other link already merged", barLink: "bar_2.0.raw", barMerged: "bar_2.0.raw", wantRefresh: 1},
		{name: "other image merged but not linked", barMerged: "bar_1.0.raw",
			wantSkip: "refresh skipped: bar is merged but not linked; the suite switch takes effect at the next boot"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newSuiteFixture(t, forky, []string{"foo_1.0_13.raw"}, "foo_1.0_13.raw")
			f.server.publish("2.0_14")
			f.addBar(t, []string{"bar_1.0.raw", "bar_2.0.raw"}, tt.barLink, tt.barMerged)

			results, err := f.client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{SuiteSwitchOnly: true})
			if err != nil {
				t.Fatalf("UpdateFeatures(SuiteSwitchOnly) error = %v\n%s", err, f.out)
			}
			if len(results) != 1 || results[0].Feature != "grp" {
				t.Fatalf("results = %+v, want only feature grp", results)
			}
			if got := f.link(t); got != "foo_2.0_14.raw" {
				t.Errorf("link = %q, want foo_2.0_14.raw", got)
			}
			if got := f.barLink(t); got != tt.barLink {
				t.Errorf("bar link = %q, want %q", got, tt.barLink)
			}
			if f.runner.refreshes != tt.wantRefresh {
				t.Errorf("refreshes = %d, want %d\n%s", f.runner.refreshes, tt.wantRefresh, f.out)
			}
			rec, _ := readLastRun(t, f.lastRun)
			if tt.wantRefresh == 0 {
				if results[0].RefreshSkipped != tt.wantSkip {
					t.Errorf("RefreshSkipped = %q, want %q", results[0].RefreshSkipped, tt.wantSkip)
				}
				if rec.Refresh != "skipped" || rec.Outcome != "completed" {
					t.Errorf("last-run record = %+v, want completed with refresh skipped", rec)
				}
				return
			}
			if results[0].RefreshSkipped != "" || rec.Refresh != "done" {
				t.Errorf("RefreshSkipped = %q, record refresh = %q; want the refresh done", results[0].RefreshSkipped, rec.Refresh)
			}
		})
	}
}

// The scan before the lock is a fast path only: the run selects again under
// the lock. When another operation linked the running suite's image between
// both scans, the run downloads, links and refreshes nothing and writes no
// last-run record.
func TestUpdateFeatures_SuiteSwitchSelectsAgainUnderLock(t *testing.T) {
	f := newSuiteFixture(t, forky, []string{"foo_1.0_13.raw", "foo_1.0_14.raw"}, "foo_1.0_13.raw")
	f.server.publish("2.0_14")
	scans := 0
	f.client.afterSuiteSwitchScan = func() {
		scans++
		link := filepath.Join(f.linkDir, "foo.raw")
		if err := os.Remove(link); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(f.targetDir, "foo_1.0_14.raw"), link); err != nil {
			t.Fatal(err)
		}
	}

	results, err := f.client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{SuiteSwitchOnly: true})
	if err != nil {
		t.Fatalf("UpdateFeatures(SuiteSwitchOnly) error = %v\n%s", err, f.out)
	}
	if scans != 1 {
		t.Fatalf("hook ran %d times, want 1", scans)
	}
	if results == nil || len(results) != 0 {
		t.Errorf("results = %#v, want an empty, non-nil slice", results)
	}
	if got := f.link(t); got != "foo_1.0_14.raw" {
		t.Errorf("link = %q, want foo_1.0_14.raw", got)
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
}

// A domain that cannot be loaded is a failed suite-switch run of class
// config: it replaces the previous record, as the normal path does
// (ADR-0020). Refused options still write nothing.
func TestUpdateFeatures_SuiteSwitchRecordsDomainFailure(t *testing.T) {
	f := newSuiteFixture(t, forky, []string{"foo_1.0_13.raw"}, "foo_1.0_13.raw")
	previous := `{"format":1,"started":"2026-11-13T00:00:00Z","finished":"2026-11-13T00:01:00Z","outcome":"completed","refresh":"not_run","selected":false,"features":[]}`
	if err := os.WriteFile(f.lastRun, []byte(previous), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := f.client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{SuiteSwitchOnly: true, NoRefresh: true}); err == nil {
		t.Fatal("UpdateFeatures(SuiteSwitchOnly, NoRefresh) succeeded, want an error")
	}
	if data, _ := os.ReadFile(f.lastRun); string(data) != previous {
		t.Fatalf("a refused run changed the record:\n%s", data)
	}

	if err := os.WriteFile(filepath.Join(f.defDir, "broken.transfer"), []byte("[Transfer\nFeatures=grp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := f.client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{SuiteSwitchOnly: true})
	if err == nil {
		t.Fatal("UpdateFeatures(SuiteSwitchOnly) error = nil, want the domain error")
	}
	rec, data := readLastRun(t, f.lastRun)
	if rec.Outcome != "failed" || rec.ErrorClass != "config" || rec.Refresh != "not_run" || !rec.Selected || len(rec.Features) != 0 {
		t.Errorf("record = %s, want a failed selected run of class config, refresh not_run, no features", data)
	}
	if got := f.link(t); got != "foo_1.0_13.raw" {
		t.Errorf("link = %q, want it unchanged", got)
	}
	if f.runner.refreshes != 0 {
		t.Errorf("refreshes = %d, want 0", f.runner.refreshes)
	}
}

// A run scoped to 1 component still refreshes the whole host, so it must
// know every pending change of the host. When the host's definitions do not
// load (a malformed transfer in another component), an unrelated pending
// update would go unseen: the run keeps its links and skips the refresh.
func TestUpdateFeatures_SuiteSwitchHostDefinitionsUnreadable(t *testing.T) {
	f := newSuiteFixture(t, forky, []string{"foo_1.0_13.raw"}, "foo_1.0_13.raw")
	f.server.publish("2.0_14")
	root := filepath.Dir(f.defDir)
	// The suite-switch feature lives in component sw.
	swDir := filepath.Join(root, "sysupdate.sw.d")
	if err := os.Mkdir(swDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"grp.feature", "foo.transfer"} {
		if err := os.Rename(filepath.Join(f.defDir, name), filepath.Join(swDir, name)); err != nil {
			t.Fatal(err)
		}
	}
	// An unrelated pending update in the default directory.
	f.addBar(t, []string{"bar_1.0.raw", "bar_2.0.raw"}, "bar_2.0.raw", "bar_1.0.raw")
	// A malformed definition in another component.
	brokenDir := filepath.Join(root, "sysupdate.broken.d")
	if err := os.Mkdir(brokenDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(brokenDir, "x.transfer"), []byte("[Transfer\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	results, err := f.client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{SuiteSwitchOnly: true, Component: "sw"})
	if err != nil {
		t.Fatalf("UpdateFeatures(SuiteSwitchOnly, Component sw) error = %v\n%s", err, f.out)
	}
	if len(results) != 1 || results[0].Feature != "grp" {
		t.Fatalf("results = %+v, want only feature grp", results)
	}
	if got := f.link(t); got != "foo_2.0_14.raw" {
		t.Errorf("link = %q, want foo_2.0_14.raw", got)
	}
	if f.runner.refreshes != 0 {
		t.Errorf("refreshes = %d, want 0\n%s", f.runner.refreshes, f.out)
	}
	want := "refresh skipped: the host's sysext definitions cannot be read, so what a refresh changes is unknown; the suite switch takes effect at the next boot"
	if results[0].RefreshSkipped != want {
		t.Errorf("RefreshSkipped = %q, want %q", results[0].RefreshSkipped, want)
	}
	if rec, data := readLastRun(t, f.lastRun); rec.Refresh != "skipped" || rec.Outcome != "completed" {
		t.Errorf("last-run record = %s, want completed with refresh skipped", data)
	}
}
