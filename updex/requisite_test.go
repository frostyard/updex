package updex

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/frostyard/std/reporter"
	"github.com/frostyard/updex/v2/sysext"
)

// reqRunner links through the real runner, and counts and swallows the
// unmerge and the refresh, which would run systemd-sysext.
type reqRunner struct {
	*sysext.DefaultRunner
	unmerges  int
	refreshes int
}

func (r *reqRunner) Unmerge() error { r.unmerges++; return nil }
func (r *reqRunner) Refresh() error { r.refreshes++; return nil }

// reqFixture is a host with a few features and transfers for the
// Features= / RequisiteFeatures= activation rule (ADR-0017). Every component
// has its own revision server that publishes 1.0.0 and 2.0.0.
type reqFixture struct {
	t           *testing.T
	client      *Client
	out         *bytes.Buffer
	runner      *reqRunner
	servers     map[string]*revisionServer
	defDir      string
	targetDir   string
	linkDir     string
	hierarchy   string
	rollbackDir string
}

func newReqFixture(t *testing.T) *reqFixture {
	t.Helper()
	root := t.TempDir()
	f := &reqFixture{
		t:           t,
		out:         &bytes.Buffer{},
		runner:      &reqRunner{DefaultRunner: &sysext.DefaultRunner{}},
		servers:     map[string]*revisionServer{},
		defDir:      filepath.Join(root, "sysupdate.d"),
		targetDir:   t.TempDir(),
		linkDir:     t.TempDir(),
		hierarchy:   t.TempDir(),
		rollbackDir: filepath.Join(t.TempDir(), "rollback"),
	}
	if err := os.MkdirAll(f.defDir, 0o755); err != nil {
		t.Fatal(err)
	}
	f.client = NewClient(ClientConfig{
		SysextRunner: f.runner,
		Progress:     reporter.NewTextReporter(f.out),
		Paths: RuntimePaths{
			DefinitionRoots:   []string{root},
			SysextLinkDir:     f.linkDir,
			SysextHierarchies: []string{f.hierarchy},
			RunExtensionsDir:  t.TempDir(),
			LockPath:          filepath.Join(t.TempDir(), "lock"),
			RollbackDir:       f.rollbackDir,
		},
	})
	return f
}

// feature writes a feature file.
func (f *reqFixture) feature(name string, enabled bool) {
	f.t.Helper()
	createFeatureFile(f.t, f.defDir, name, enabled)
}

// transfer writes a transfer with the given Features= and RequisiteFeatures=
// and a revision server that publishes 1.0.0 and 2.0.0.
func (f *reqFixture) transfer(component string, features, requisites []string) {
	f.t.Helper()
	server := newRevisionServer(f.t, component)
	server.publish("1.0.0")
	server.publish("2.0.0")
	f.servers[component] = server
	content := "[Transfer]\n"
	if len(features) > 0 {
		content += "Features=" + strings.Join(features, " ") + "\n"
	}
	if len(requisites) > 0 {
		content += "RequisiteFeatures=" + strings.Join(requisites, " ") + "\n"
	}
	content += "Verify=false\n\n[Source]\nType=url-file\nPath=" + server.URL + "\nMatchPattern=" + component + "_@v.raw\n\n" +
		"[Target]\nPath=" + f.targetDir + "\nMatchPattern=" + component + "_@v.raw\n"
	writeTestFile(f.t, filepath.Join(f.defDir, component+".transfer"), content)
}

// install makes the given versions staged for component and links linked.
func (f *reqFixture) install(component, linked string, staged ...string) {
	f.t.Helper()
	for _, v := range staged {
		writeTestFile(f.t, filepath.Join(f.targetDir, component+"_"+v+".raw"), component+" image "+v)
	}
	if err := os.Symlink(filepath.Join(f.targetDir, component+"_"+linked+".raw"), filepath.Join(f.linkDir, component+".raw")); err != nil {
		f.t.Fatal(err)
	}
}

func (f *reqFixture) gets(component string) int32 { return f.servers[component].fileGets.Load() }

func (f *reqFixture) linkExists(component string) bool {
	_, err := os.Lstat(filepath.Join(f.linkDir, component+".raw"))
	return err == nil
}

func (f *reqFixture) staged(component, version string) bool {
	_, err := os.Stat(filepath.Join(f.targetDir, component+"_"+version+".raw"))
	return err == nil
}

func (f *reqFixture) linked(component string) string {
	target, err := os.Readlink(filepath.Join(f.linkDir, component+".raw"))
	if err != nil {
		return ""
	}
	return filepath.Base(target)
}

// merge records the component's image as merged, as systemd-sysext does.
func (f *reqFixture) merge(component, version string) {
	f.t.Helper()
	data, err := json.Marshal(map[string]any{
		"extensions": map[string]map[string]string{
			component: {"path": filepath.Join(f.targetDir, component+"_"+version+".raw")},
		},
	})
	if err != nil {
		f.t.Fatal(err)
	}
	writeMergedState(f.t, f.hierarchy, string(data))
}

// enabled reports whether the feature is enabled now, as the client loads it.
func (f *reqFixture) enabled(name string) bool {
	f.t.Helper()
	infos, err := f.client.Features(f.t.Context())
	if err != nil {
		f.t.Fatal(err)
	}
	for _, info := range infos {
		if info.Name == name {
			return info.Enabled
		}
	}
	f.t.Fatalf("feature %s is not loaded", name)
	return false
}

// writeRecord writes a rollback record of feature that rejects the versions.
func (f *reqFixture) writeRecord(feature string, rejected map[string]string) {
	f.t.Helper()
	data, err := json.Marshal(rollbackRecord{Format: rollbackRecordFormat, Feature: feature, Created: "2026-10-26T00:00:00Z", Rejected: rejected})
	if err != nil {
		f.t.Fatal(err)
	}
	if err := os.MkdirAll(f.rollbackDir, 0o755); err != nil {
		f.t.Fatal(err)
	}
	writeTestFile(f.t, filepath.Join(f.rollbackDir, feature), string(data))
}

func (f *reqFixture) update(opts UpdateFeaturesOptions) []UpdateFeaturesResult {
	f.t.Helper()
	opts.NoRefresh = true
	results, err := f.client.UpdateFeatures(f.t.Context(), opts)
	if err != nil {
		f.t.Fatalf("UpdateFeatures: %v\n%s", err, f.out.String())
	}
	return results
}

func updateFeatureResult(t *testing.T, results []UpdateFeaturesResult, feature string) UpdateFeaturesResult {
	t.Helper()
	for _, r := range results {
		if r.Feature == feature {
			return r
		}
	}
	t.Fatalf("no result for feature %s in %+v", feature, results)
	return UpdateFeaturesResult{}
}

func checkFeatureResult(t *testing.T, results []CheckFeaturesResult, feature string) CheckFeaturesResult {
	t.Helper()
	for _, r := range results {
		if r.Feature == feature {
			return r
		}
	}
	t.Fatalf("no check result for feature %s in %+v", feature, results)
	return CheckFeaturesResult{}
}

// maildLodge is the fixture of the issue: T has Features=maild and
// RequisiteFeatures=lodge.
func maildLodge(t *testing.T, maild, lodge bool) *reqFixture {
	t.Helper()
	f := newReqFixture(t)
	f.feature("maild", maild)
	f.feature("lodge", lodge)
	f.transfer("T", []string{"maild"}, []string{"lodge"})
	return f
}

func wantInactive(t *testing.T, got []InactiveTransfer, want InactiveTransfer) {
	t.Helper()
	if len(got) != 1 || got[0].Component != want.Component ||
		!slices.Equal(got[0].MissingRequisites, want.MissingRequisites) ||
		!slices.Equal(got[0].NeedsOneOf, want.NeedsOneOf) {
		t.Errorf("Inactive = %+v, want [%+v]", got, want)
	}
}

// --- update ---

func TestUpdateFeatures_RequisiteFeatureGate(t *testing.T) {
	tests := []struct {
		name         string
		maild, lodge bool
		feature      string // the feature whose result is read
		wantActive   bool
		wantInactive InactiveTransfer
	}{
		{name: "maild enabled lodge disabled", maild: true, feature: "maild",
			wantInactive: InactiveTransfer{Component: "T", MissingRequisites: []string{"lodge"}}},
		{name: "only lodge enabled", lodge: true, feature: "lodge",
			wantInactive: InactiveTransfer{Component: "T", NeedsOneOf: []string{"maild"}}},
		{name: "both enabled", maild: true, lodge: true, feature: "maild", wantActive: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := maildLodge(t, tt.maild, tt.lodge)
			results := f.update(UpdateFeaturesOptions{})
			r := updateFeatureResult(t, results, tt.feature)
			if tt.wantActive {
				if len(r.Inactive) != 0 {
					t.Errorf("Inactive = %+v, want none", r.Inactive)
				}
				if got := f.gets("T"); got != 1 {
					t.Errorf("T downloaded %d times, want once", got)
				}
				if got := f.linked("T"); got != "T_2.0.0.raw" {
					t.Errorf("T link = %q, want T_2.0.0.raw", got)
				}
				return
			}
			if len(r.Results) != 0 {
				t.Errorf("Results = %+v, want none: T is not acted on", r.Results)
			}
			wantInactive(t, r.Inactive, tt.wantInactive)
			if got := f.gets("T"); got != 0 {
				t.Errorf("T downloaded %d times, want 0", got)
			}
			if f.linkExists("T") || f.staged("T", "2.0.0") {
				t.Error("an inactive transfer must not be staged or linked")
			}
			if !strings.Contains(f.out.String(), "inactive") {
				t.Errorf("no inactive line in the output:\n%s", f.out.String())
			}
		})
	}
}

// An inactive transfer is also left alone by the vacuum and the refresh gate:
// its old images and link stay as they are.
func TestUpdateFeatures_InactiveTransferKeepsItsImagesAndLink(t *testing.T) {
	f := maildLodge(t, true, false)
	f.install("T", "1.0.0", "0.5.0", "1.0.0")
	f.update(UpdateFeaturesOptions{})
	if got := f.linked("T"); got != "T_1.0.0.raw" {
		t.Errorf("T link = %q, want T_1.0.0.raw", got)
	}
	if !f.staged("T", "0.5.0") || !f.staged("T", "1.0.0") || f.staged("T", "2.0.0") {
		t.Error("an inactive transfer must not be downloaded or vacuumed")
	}
}

// The feature still gets its other transfers.
func TestUpdateFeatures_InactiveTransferDoesNotStopTheFeaturesOthers(t *testing.T) {
	f := maildLodge(t, true, false)
	f.transfer("A", []string{"maild"}, nil)
	results := f.update(UpdateFeaturesOptions{})
	r := updateFeatureResult(t, results, "maild")
	if len(r.Results) != 1 || r.Results[0].Component != "A" || !r.Results[0].Downloaded {
		t.Errorf("Results = %+v, want A downloaded", r.Results)
	}
	wantInactive(t, r.Inactive, InactiveTransfer{Component: "T", MissingRequisites: []string{"lodge"}})
	if f.gets("T") != 0 || f.gets("A") != 1 {
		t.Errorf("downloads: T %d, A %d; want 0 and 1", f.gets("T"), f.gets("A"))
	}
}

func TestUpdateFeatures_DryRunReportsInactive(t *testing.T) {
	f := maildLodge(t, true, false)
	results := f.update(UpdateFeaturesOptions{DryRun: true})
	wantInactive(t, updateFeatureResult(t, results, "maild").Inactive, InactiveTransfer{Component: "T", MissingRequisites: []string{"lodge"}})
}

// A requisite feature that does not exist is never enabled.
func TestUpdateFeatures_UnknownRequisiteFeatureIsInactive(t *testing.T) {
	f := newReqFixture(t)
	f.feature("maild", true)
	f.transfer("T", []string{"maild"}, []string{"nonexistent"})
	results := f.update(UpdateFeaturesOptions{})
	wantInactive(t, updateFeatureResult(t, results, "maild").Inactive, InactiveTransfer{Component: "T", MissingRequisites: []string{"nonexistent"}})
	if f.gets("T") != 0 {
		t.Error("T must not be downloaded")
	}
}

// A masked requisite feature is not enabled.
func TestUpdateFeatures_MaskedRequisiteFeatureIsInactive(t *testing.T) {
	f := maildLodge(t, true, true)
	// A masked feature is a symlink to /dev/null.
	lodge := filepath.Join(f.defDir, "lodge.feature")
	if err := os.Remove(lodge); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/dev/null", lodge); err != nil {
		t.Fatal(err)
	}
	results := f.update(UpdateFeaturesOptions{})
	wantInactive(t, updateFeatureResult(t, results, "maild").Inactive, InactiveTransfer{Component: "T", MissingRequisites: []string{"lodge"}})
}

// --- check ---

func TestCheckFeatures_RequisiteFeatureGate(t *testing.T) {
	tests := []struct {
		name         string
		maild, lodge bool
		feature      string
		wantActive   bool
		wantInactive InactiveTransfer
	}{
		{name: "maild enabled lodge disabled", maild: true, feature: "maild",
			wantInactive: InactiveTransfer{Component: "T", MissingRequisites: []string{"lodge"}}},
		{name: "only lodge enabled", lodge: true, feature: "lodge",
			wantInactive: InactiveTransfer{Component: "T", NeedsOneOf: []string{"maild"}}},
		{name: "both enabled", maild: true, lodge: true, feature: "maild", wantActive: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := maildLodge(t, tt.maild, tt.lodge)
			results, err := f.client.CheckFeatures(t.Context(), CheckFeaturesOptions{})
			if err != nil {
				t.Fatal(err)
			}
			r := checkFeatureResult(t, results, tt.feature)
			if tt.wantActive {
				if len(r.Inactive) != 0 || len(r.Results) != 1 || r.Results[0].Component != "T" || !r.Results[0].UpdateAvailable {
					t.Errorf("result = %+v, want T checked with an update", r)
				}
				return
			}
			if len(r.Results) != 0 {
				t.Errorf("Results = %+v, want none: T is not checked", r.Results)
			}
			wantInactive(t, r.Inactive, tt.wantInactive)
			if f.servers["T"].fileGets.Load() != 0 {
				t.Error("check must not download")
			}
		})
	}
}

// --- enable ---

func TestEnableFeatureNow_RequisiteFeatureWaits(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		name := "real"
		if dryRun {
			name = "dry run"
		}
		t.Run(name, func(t *testing.T) {
			f := maildLodge(t, false, false)
			f.transfer("A", []string{"maild"}, nil)
			result, err := f.client.EnableFeature(t.Context(), "maild", EnableFeatureOptions{Now: true, DryRun: dryRun, NoRefresh: true})
			if err != nil {
				t.Fatalf("EnableFeature: %v", err)
			}
			wantInactive(t, result.Inactive, InactiveTransfer{Component: "T", MissingRequisites: []string{"lodge"}})
			if !strings.Contains(result.NextActionMessage, "not downloaded") || !strings.Contains(result.NextActionMessage, "T waits for feature(s) lodge") {
				t.Errorf("NextActionMessage = %q", result.NextActionMessage)
			}
			if f.gets("T") != 0 || f.linkExists("T") || f.staged("T", "2.0.0") {
				t.Error("T must not be downloaded or linked")
			}
			if dryRun {
				if len(result.DownloadedFiles) != 1 || !strings.HasPrefix(result.DownloadedFiles[0], "A ") {
					t.Errorf("DownloadedFiles = %v, want only A", result.DownloadedFiles)
				}
				if f.enabled("maild") {
					t.Error("a dry run must not enable the feature")
				}
				return
			}
			if len(result.DownloadedFiles) != 1 || !strings.HasPrefix(result.DownloadedFiles[0], "A@") {
				t.Errorf("DownloadedFiles = %v, want only A", result.DownloadedFiles)
			}
			if f.linked("A") != "A_2.0.0.raw" {
				t.Errorf("A link = %q", f.linked("A"))
			}
			if !f.enabled("maild") {
				t.Error("maild must be enabled")
			}
		})
	}
}

// Enabling the requisite feature installs the transfer that waited, when the
// feature in its Features= is already enabled.
func TestEnableFeatureNow_RequisiteFeatureInstallsTransferThatWaited(t *testing.T) {
	f := maildLodge(t, true, false)
	result, err := f.client.EnableFeature(t.Context(), "lodge", EnableFeatureOptions{Now: true, NoRefresh: true})
	if err != nil {
		t.Fatalf("EnableFeature: %v", err)
	}
	if len(result.Inactive) != 0 {
		t.Errorf("Inactive = %+v, want none", result.Inactive)
	}
	if len(result.DownloadedFiles) != 1 || !strings.HasPrefix(result.DownloadedFiles[0], "T@") {
		t.Errorf("DownloadedFiles = %v, want T", result.DownloadedFiles)
	}
	if got := f.linked("T"); got != "T_2.0.0.raw" {
		t.Errorf("T link = %q, want T_2.0.0.raw", got)
	}
}

// Enabling the requisite feature does not install a transfer whose Features=
// has no enabled feature.
func TestEnableFeatureNow_RequisiteFeatureWithoutAnEnabledFeatureWaits(t *testing.T) {
	f := maildLodge(t, false, false)
	result, err := f.client.EnableFeature(t.Context(), "lodge", EnableFeatureOptions{Now: true, NoRefresh: true})
	if err != nil {
		t.Fatalf("EnableFeature: %v", err)
	}
	wantInactive(t, result.Inactive, InactiveTransfer{Component: "T", NeedsOneOf: []string{"maild"}})
	if f.gets("T") != 0 || f.linkExists("T") {
		t.Error("T must not be downloaded or linked")
	}
	if len(result.DownloadedFiles) != 0 {
		t.Errorf("DownloadedFiles = %v", result.DownloadedFiles)
	}
	if f.runner.refreshes != 0 {
		t.Error("nothing was installed: no refresh")
	}
}

// --- disable ---

func TestDisableFeatureNow_KeepsTransferThatAnotherEnabledFeatureActivates(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		name := "real"
		if dryRun {
			name = "dry run"
		}
		t.Run(name, func(t *testing.T) {
			f := newReqFixture(t)
			f.feature("a", true)
			f.feature("b", true)
			f.transfer("S", []string{"a", "b"}, nil)
			f.install("S", "2.0.0", "1.0.0", "2.0.0")
			f.merge("S", "2.0.0") // merged: removing it would need --force, but it stays

			result, err := f.client.DisableFeature(t.Context(), "a", DisableFeatureOptions{Now: true, DryRun: dryRun})
			if err != nil {
				t.Fatalf("DisableFeature: %v", err)
			}
			if len(result.Kept) != 1 || result.Kept[0].Component != "S" || !slices.Equal(result.Kept[0].ActiveThrough, []string{"b"}) {
				t.Errorf("Kept = %+v, want S through b", result.Kept)
			}
			if len(result.RemovedFiles) != 0 || result.Unmerged {
				t.Errorf("RemovedFiles = %v Unmerged = %v, want neither", result.RemovedFiles, result.Unmerged)
			}
			if f.linked("S") != "S_2.0.0.raw" || !f.staged("S", "1.0.0") || !f.staged("S", "2.0.0") {
				t.Error("S link and images must stay")
			}
			if f.runner.unmerges != 0 || f.runner.refreshes != 0 {
				t.Errorf("unmerge %d, refresh %d; nothing was removed, so neither may run", f.runner.unmerges, f.runner.refreshes)
			}
			if !strings.Contains(result.NextActionMessage, "S (still active through feature(s) b)") {
				t.Errorf("NextActionMessage = %q", result.NextActionMessage)
			}
			if !dryRun {
				if result.DropIn == "" || f.enabled("a") {
					t.Errorf("the drop-in must still disable a (drop-in %q)", result.DropIn)
				}
			}
		})
	}
}

// With nothing to remove, the refresh-gate preflight does not run either: an
// unreadable pending link elsewhere cannot block a disable that changes nothing.
func TestDisableFeatureNow_NothingToRemoveRunsNoPreflight(t *testing.T) {
	f := newReqFixture(t)
	f.feature("a", true)
	f.feature("b", true)
	f.feature("c", true)
	f.transfer("S", []string{"a", "b"}, nil)
	f.install("S", "2.0.0", "2.0.0")
	// c's link is dangling-free but needs a state read: a directory where the
	// merged-state origin belongs makes every status read fail, and a refresh
	// gate would fail closed on it.
	f.transfer("O", []string{"c"}, nil)
	f.install("O", "2.0.0", "2.0.0")
	if err := os.MkdirAll(filepath.Join(f.hierarchy, sysext.MergedStateDirName, "origin"), 0o755); err != nil {
		t.Fatal(err)
	}

	result, err := f.client.DisableFeature(t.Context(), "a", DisableFeatureOptions{Now: true})
	if err != nil {
		t.Fatalf("DisableFeature: %v", err)
	}
	if !result.Success || len(result.Kept) != 1 {
		t.Errorf("result = %+v", result)
	}
}

func TestDisableFeatureNow_RemovesWhatItMakesInactive(t *testing.T) {
	f := maildLodge(t, true, true)
	f.install("T", "2.0.0", "1.0.0", "2.0.0")

	result, err := f.client.DisableFeature(t.Context(), "lodge", DisableFeatureOptions{Now: true})
	if err != nil {
		t.Fatalf("DisableFeature: %v", err)
	}
	if len(result.Kept) != 0 {
		t.Errorf("Kept = %+v, want none: T needs lodge", result.Kept)
	}
	if len(result.RemovedFiles) != 2 {
		t.Errorf("RemovedFiles = %v, want both images", result.RemovedFiles)
	}
	if f.linkExists("T") || f.staged("T", "1.0.0") || f.staged("T", "2.0.0") {
		t.Error("T must be unlinked and removed")
	}
	if f.runner.unmerges != 1 || f.runner.refreshes != 1 {
		t.Errorf("unmerge %d, refresh %d; want 1 and 1", f.runner.unmerges, f.runner.refreshes)
	}
}

// Disabling maild removes T too (maild is its only Features=).
func TestDisableFeatureNow_RemovesTransferWhoseOnlyFeatureIsDisabled(t *testing.T) {
	f := maildLodge(t, true, true)
	f.install("T", "2.0.0", "2.0.0")
	result, err := f.client.DisableFeature(t.Context(), "maild", DisableFeatureOptions{Now: true})
	if err != nil {
		t.Fatalf("DisableFeature: %v", err)
	}
	if len(result.Kept) != 0 || len(result.RemovedFiles) != 1 || f.linkExists("T") {
		t.Errorf("result = %+v, link exists = %v", result, f.linkExists("T"))
	}
}

// One kept and one removed transfer: only the removed one goes, and the
// merged-state check ignores the kept one.
func TestDisableFeatureNow_MixedKeepsOneAndRemovesOne(t *testing.T) {
	setup := func(t *testing.T) *reqFixture {
		f := newReqFixture(t)
		f.feature("a", true)
		f.feature("b", true)
		f.transfer("S", []string{"a", "b"}, nil) // kept: b activates it
		f.transfer("U", []string{"a"}, nil)      // removed: only a
		f.install("S", "2.0.0", "2.0.0")
		f.install("U", "2.0.0", "1.0.0", "2.0.0")
		return f
	}

	t.Run("merged kept transfer needs no force", func(t *testing.T) {
		f := setup(t)
		f.merge("S", "2.0.0")
		result, err := f.client.DisableFeature(t.Context(), "a", DisableFeatureOptions{Now: true})
		if err != nil {
			t.Fatalf("DisableFeature: %v", err)
		}
		if len(result.Kept) != 1 || result.Kept[0].Component != "S" {
			t.Errorf("Kept = %+v", result.Kept)
		}
		if len(result.RemovedFiles) != 2 {
			t.Errorf("RemovedFiles = %v, want U's 2 images", result.RemovedFiles)
		}
		if f.linkExists("U") || f.staged("U", "2.0.0") {
			t.Error("U must be removed")
		}
		if f.linked("S") != "S_2.0.0.raw" || !f.staged("S", "2.0.0") {
			t.Error("S must stay")
		}
		if f.runner.unmerges != 1 || f.runner.refreshes != 1 {
			t.Errorf("unmerge %d, refresh %d; want 1 and 1", f.runner.unmerges, f.runner.refreshes)
		}
	})

	t.Run("merged removed transfer still needs force", func(t *testing.T) {
		f := setup(t)
		f.merge("U", "2.0.0")
		_, err := f.client.DisableFeature(t.Context(), "a", DisableFeatureOptions{Now: true})
		if err == nil || !strings.Contains(err.Error(), "--force") {
			t.Fatalf("err = %v, want a --force refusal for U", err)
		}
		if !f.linkExists("U") || !f.staged("U", "2.0.0") || f.runner.unmerges != 0 {
			t.Error("nothing may change when the refusal fires")
		}
	})
}

// --- rollback holds are computed over the acted-on transfers ---

// holdFixture: maild is enabled, lodge is disabled. maild has A, and T that
// needs lodge. A is linked at 1.0.0 and the source publishes 2.0.0, so
// `update` advances it. A record of lodge rejects T 2.0.0.
func holdFixture(t *testing.T, lodgeEnabled bool) *reqFixture {
	t.Helper()
	f := maildLodge(t, true, lodgeEnabled)
	f.transfer("A", []string{"maild"}, nil)
	f.install("A", "1.0.0", "1.0.0")
	f.install("T", "1.0.0", "1.0.0")
	f.writeRecord("lodge", map[string]string{"T": "2.0.0"})
	return f
}

func TestRollbackHoldOfATransferThatIsNotActedOnDoesNotHold(t *testing.T) {
	f := holdFixture(t, false)

	// features list
	infos, err := f.client.Features(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, info := range infos {
		if info.Name != "maild" {
			continue
		}
		for _, s := range info.TransferStatus {
			if s.RollbackHeldBy != "" || s.RollbackRejected != "" || s.Error != "" {
				t.Errorf("list: %s status = %+v, want no hold", s.Component, s)
			}
		}
		if len(info.TransferStatus) != 2 {
			t.Errorf("list: statuses = %+v, want A and T", info.TransferStatus)
		}
	}

	// features check
	check, err := f.client.CheckFeatures(t.Context(), CheckFeaturesOptions{})
	if err != nil {
		t.Fatal(err)
	}
	c := checkFeatureResult(t, check, "maild")
	if len(c.Results) != 1 || c.Results[0].Component != "A" || !c.Results[0].UpdateAvailable || c.Results[0].RollbackHeldBy != "" {
		t.Errorf("check = %+v, want A with an update and no hold", c)
	}

	// features update
	results := f.update(UpdateFeaturesOptions{})
	r := updateFeatureResult(t, results, "maild")
	if len(r.Results) != 1 || r.Results[0].Component != "A" || r.Results[0].Held || !r.Results[0].Downloaded || r.Results[0].Error != "" {
		t.Errorf("update = %+v, want A advanced and not held", r.Results)
	}
	if f.linked("A") != "A_2.0.0.raw" {
		t.Errorf("A link = %q, want A_2.0.0.raw", f.linked("A"))
	}
	if f.linked("T") != "T_1.0.0.raw" || f.gets("T") != 0 {
		t.Error("T is not acted on")
	}
	// The record of lodge is not maild's to delete.
	if _, err := os.Stat(filepath.Join(f.rollbackDir, "lodge")); err != nil {
		t.Errorf("the record of lodge must stay: %v", err)
	}
}

// When T is active (both features enabled) the record of lodge holds maild, as
// before.
func TestRollbackHoldOfAnActedOnTransferStillHoldsTheFeature(t *testing.T) {
	f := holdFixture(t, true)
	const owner = "lodge: it advances only when lodge updates (or run 'updex features rollback --clear lodge')"

	infos, err := f.client.Features(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, info := range infos {
		if info.Name != "maild" {
			continue
		}
		for _, s := range info.TransferStatus {
			if s.RollbackHeldBy != owner {
				t.Errorf("list: %s held by %q, want %q", s.Component, s.RollbackHeldBy, owner)
			}
		}
	}

	check, err := f.client.CheckFeatures(t.Context(), CheckFeaturesOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range checkFeatureResult(t, check, "maild").Results {
		if r.UpdateAvailable || r.RollbackHeldBy != owner {
			t.Errorf("check %s = %+v, want held by %q", r.Component, r, owner)
		}
	}

	results := f.update(UpdateFeaturesOptions{})
	for _, r := range updateFeatureResult(t, results, "maild").Results {
		if !r.Held || r.Downloaded {
			t.Errorf("update %s = %+v, want held", r.Component, r)
		}
	}
	if f.linked("A") != "A_1.0.0.raw" || f.gets("A") != 0 {
		t.Error("A must not advance while the record of lodge holds maild")
	}
}

func TestRollbackFeature_RollsBackOnlyActedOnTransfers(t *testing.T) {
	f := maildLodge(t, true, false)
	f.transfer("A", []string{"maild"}, nil)
	f.install("A", "2.0.0", "1.0.0", "2.0.0")
	f.install("T", "2.0.0", "1.0.0", "2.0.0")

	result, err := f.client.RollbackFeature(t.Context(), "maild", RollbackFeatureOptions{})
	if err != nil {
		t.Fatalf("RollbackFeature: %v", err)
	}
	if len(result.Transfers) != 1 || result.Transfers[0].Component != "A" {
		t.Errorf("Transfers = %+v, want only A", result.Transfers)
	}
	if f.linked("A") != "A_1.0.0.raw" || f.linked("T") != "T_2.0.0.raw" {
		t.Errorf("links: A %s, T %s; want A rolled back and T untouched", f.linked("A"), f.linked("T"))
	}
	data, err := os.ReadFile(filepath.Join(f.rollbackDir, "maild"))
	if err != nil {
		t.Fatal(err)
	}
	var rec rollbackRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatal(err)
	}
	if len(rec.Rejected) != 1 || rec.Rejected["A"] != "2.0.0" {
		t.Errorf("record rejects %v, want only A 2.0.0", rec.Rejected)
	}
}

func TestRollbackFeature_NoActiveTransfersFails(t *testing.T) {
	f := newReqFixture(t)
	f.feature("x", false)
	f.feature("y", false)
	f.transfer("T", []string{"x"}, []string{"y"})
	f.install("T", "2.0.0", "1.0.0", "2.0.0")

	_, err := f.client.RollbackFeature(t.Context(), "x", RollbackFeatureOptions{})
	if err == nil || !strings.Contains(err.Error(), "feature 'x' has no active transfers to roll back") || !strings.Contains(err.Error(), "T waits for feature(s) y") {
		t.Fatalf("err = %v", err)
	}
	if f.linked("T") != "T_2.0.0.raw" {
		t.Error("nothing may change")
	}
	if _, statErr := os.Lstat(filepath.Join(f.rollbackDir, "x")); !os.IsNotExist(statErr) {
		t.Errorf("no record may be written (err = %v)", statErr)
	}
}

// --- result helpers ---

func TestInactiveTransferReason(t *testing.T) {
	tests := []struct {
		in   InactiveTransfer
		want string
	}{
		{InactiveTransfer{MissingRequisites: []string{"lodge"}}, "waits for feature(s) lodge"},
		{InactiveTransfer{MissingRequisites: []string{"a", "b"}}, "waits for feature(s) a, b"},
		{InactiveTransfer{NeedsOneOf: []string{"maild", "x"}}, "needs one of features: maild, x"},
		{InactiveTransfer{MissingRequisites: []string{"lodge"}, NeedsOneOf: []string{"maild"}}, "waits for feature(s) lodge and needs one of features: maild"},
	}
	for _, tt := range tests {
		if got := tt.in.Reason(); got != tt.want {
			t.Errorf("Reason() = %q, want %q", got, tt.want)
		}
	}
}

// The new JSON fields are additive and absent when empty.
func TestInactiveAndKeptJSON(t *testing.T) {
	data, err := json.Marshal(UpdateFeaturesResult{Feature: "f", Results: []UpdateResult{}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "inactive") {
		t.Errorf("empty Inactive must be omitted: %s", data)
	}
	data, err = json.Marshal(FeatureActionResult{Inactive: []InactiveTransfer{{Component: "T", MissingRequisites: []string{"lodge"}}}, Kept: []KeptTransfer{{Component: "S", ActiveThrough: []string{"b"}}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"inactive":[{"component":"T","missing_requisites":["lodge"]}]`, `"kept":[{"component":"S","active_through":["b"]}]`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("JSON %s lacks %s", data, want)
		}
	}
}
