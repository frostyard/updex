package updex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostyard/updex/v2/internal/testutil"
	"github.com/frostyard/updex/v2/sysext"
)

// statusFixture is a rootless host for the RUNNING/LINKED status tests: a
// definitions directory, a staging directory, a sysext link directory, and one
// hierarchy that holds the merged-state metadata. A fake HTTP source publishes
// 1.0.0 and 2.0.0 of every component the tests use.
type statusFixture struct {
	t         *testing.T
	configDir string
	targetDir string
	linkDir   string
	hierarchy string
	serverURL string
	runner    *sysext.MockRunner
	reporter  *recordingReporter
}

func newStatusFixture(t *testing.T) *statusFixture {
	t.Helper()

	files := map[string]string{}
	content := map[string][]byte{}
	for _, name := range []string{"alpha", "beta"} {
		for _, v := range []string{"1.0.0", "2.0.0"} {
			file := name + "_" + v + ".raw"
			data := []byte("image " + file)
			files[file] = hashContent(data)
			content[file] = data
		}
	}
	server := testutil.NewTestServer(t, testutil.TestServerFiles{Files: files, Content: content})
	t.Cleanup(server.Close)

	return &statusFixture{
		t:         t,
		configDir: t.TempDir(),
		targetDir: t.TempDir(),
		linkDir:   t.TempDir(),
		hierarchy: t.TempDir(),
		serverURL: server.URL,
		runner:    &sysext.MockRunner{},
		reporter:  &recordingReporter{},
	}
}

// addFeature writes a feature and its single transfer (component == feature).
func (f *statusFixture) addFeature(name string, enabled bool) {
	f.t.Helper()
	createFeatureFile(f.t, f.configDir, name, enabled)
	writeCheckTransfer(f.t, f.configDir, name, name, f.serverURL, f.targetDir, false)
}

// stage writes the staged image <component>_<version>.raw and returns its path.
func (f *statusFixture) stage(component, version string) string {
	f.t.Helper()
	path := filepath.Join(f.targetDir, component+"_"+version+".raw")
	if err := os.WriteFile(path, []byte("image "+component+"_"+version+".raw"), 0o644); err != nil {
		f.t.Fatalf("stage image: %v", err)
	}
	return path
}

// link points <linkDir>/<component>.raw at the staged image of version. The
// image need not exist: that makes a dangling link.
func (f *statusFixture) link(component, version string) {
	f.t.Helper()
	target := filepath.Join(f.targetDir, component+"_"+version+".raw")
	if err := os.Symlink(target, filepath.Join(f.linkDir, component+".raw")); err != nil {
		f.t.Fatalf("link image: %v", err)
	}
}

// merge records, in the hierarchy's origin file, that each component's image
// of the given version is merged.
func (f *statusFixture) merge(versions map[string]string) {
	f.t.Helper()
	entries := map[string]map[string]string{}
	for component, version := range versions {
		entries[component] = map[string]string{"path": filepath.Join(f.targetDir, component+"_"+version+".raw")}
	}
	data, err := json.Marshal(map[string]any{"extensions": entries})
	if err != nil {
		f.t.Fatal(err)
	}
	f.writeState("origin", string(data))
}

func (f *statusFixture) writeState(name, content string) {
	f.t.Helper()
	dir := filepath.Join(f.hierarchy, sysext.MergedStateDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *statusFixture) client() *Client {
	return NewClient(ClientConfig{
		Definitions:  f.configDir,
		SysextRunner: f.runner,
		Progress:     f.reporter,
		Paths: RuntimePaths{
			DefinitionRoots:   []string{f.t.TempDir()},
			SysextLinkDir:     f.linkDir,
			RunExtensionsDir:  filepath.Join(f.t.TempDir(), "run-extensions"),
			SysextHierarchies: []string{f.hierarchy},
		},
	})
}

// statusCases are the states one transfer ("alpha", enabled) can be in. Every
// case stages 1.0.0 and 2.0.0 except where it says otherwise.
var statusCases = []struct {
	name        string
	setup       func(f *statusFixture)
	wantRunning string
	wantLinked  string
	wantPending bool
	wantCurrent string
}{
	{
		name: "linked ahead of running",
		setup: func(f *statusFixture) {
			f.stage("alpha", "1.0.0")
			f.stage("alpha", "2.0.0")
			f.link("alpha", "2.0.0")
			f.merge(map[string]string{"alpha": "1.0.0"})
		},
		wantRunning: "1.0.0", wantLinked: "2.0.0", wantPending: true, wantCurrent: "2.0.0",
	},
	{
		name: "linked and not merged",
		setup: func(f *statusFixture) {
			f.stage("alpha", "1.0.0")
			f.stage("alpha", "2.0.0")
			f.link("alpha", "2.0.0")
		},
		wantRunning: "", wantLinked: "2.0.0", wantPending: true, wantCurrent: "2.0.0",
	},
	{
		name: "linked equals running",
		setup: func(f *statusFixture) {
			f.stage("alpha", "2.0.0")
			f.link("alpha", "2.0.0")
			f.merge(map[string]string{"alpha": "2.0.0"})
		},
		wantRunning: "2.0.0", wantLinked: "2.0.0", wantPending: false, wantCurrent: "2.0.0",
	},
	{
		name: "dangling link",
		setup: func(f *statusFixture) {
			f.stage("alpha", "1.0.0")
			f.link("alpha", "2.0.0") // 2.0.0 is not staged
			f.merge(map[string]string{"alpha": "1.0.0"})
		},
		wantRunning: "1.0.0", wantLinked: "", wantPending: false, wantCurrent: "1.0.0",
	},
	{
		name: "no link",
		setup: func(f *statusFixture) {
			f.stage("alpha", "1.0.0")
			f.merge(map[string]string{"alpha": "1.0.0"})
		},
		wantRunning: "1.0.0", wantLinked: "", wantPending: false, wantCurrent: "1.0.0",
	},
	{
		name: "merged without a recorded version",
		setup: func(f *statusFixture) {
			f.stage("alpha", "1.0.0")
			f.stage("alpha", "2.0.0")
			f.link("alpha", "2.0.0")
			f.writeState("extensions", "alpha\n")
		},
		wantRunning: sysext.UnknownVersion, wantLinked: "2.0.0", wantPending: false, wantCurrent: "2.0.0",
	},
	{
		name: "metadata of another image only",
		setup: func(f *statusFixture) {
			f.stage("alpha", "1.0.0")
			f.link("alpha", "1.0.0")
			f.merge(map[string]string{"beta": "2.0.0"})
		},
		wantRunning: "", wantLinked: "1.0.0", wantPending: true, wantCurrent: "1.0.0",
	},
}

func TestFeatures_ReportsRunningAndLinkedVersions(t *testing.T) {
	for _, tt := range statusCases {
		t.Run(tt.name, func(t *testing.T) {
			f := newStatusFixture(t)
			f.addFeature("alpha", true)
			tt.setup(f)

			features, err := f.client().Features(t.Context())
			if err != nil {
				t.Fatalf("Features: %v", err)
			}
			if len(features) != 1 || len(features[0].TransferStatus) != 1 {
				t.Fatalf("want 1 feature with 1 transfer status, got %+v", features)
			}
			got := features[0].TransferStatus[0]
			if got.Component != "alpha" || got.RunningVersion != tt.wantRunning || got.LinkedVersion != tt.wantLinked {
				t.Errorf("status = %+v, want running %q linked %q", got, tt.wantRunning, tt.wantLinked)
			}
			if got.PendingReboot != tt.wantPending {
				t.Errorf("PendingReboot = %v, want %v", got.PendingReboot, tt.wantPending)
			}
			if got.Error != "" || len(f.reporter.warnings) != 0 {
				t.Errorf("unexpected error %q or warnings %v", got.Error, f.reporter.warnings)
			}
		})
	}
}

// TestFeatures_StatusCoversDisabledAndSeveralTransfers pins that a disabled
// feature is reported too, and that a feature with several transfers carries
// one status per transfer, in Transfers order.
func TestFeatures_StatusCoversDisabledAndSeveralTransfers(t *testing.T) {
	f := newStatusFixture(t)
	f.addFeature("alpha", false)
	// beta's transfer joins the alpha feature as a second transfer.
	writeCheckTransfer(t, f.configDir, "beta", "alpha", f.serverURL, f.targetDir, false)
	f.stage("alpha", "1.0.0")
	f.stage("beta", "1.0.0")
	f.stage("beta", "2.0.0")
	f.link("alpha", "1.0.0")
	f.link("beta", "2.0.0")
	f.merge(map[string]string{"alpha": "1.0.0", "beta": "1.0.0"})

	features, err := f.client().Features(t.Context())
	if err != nil {
		t.Fatalf("Features: %v", err)
	}
	var feature FeatureInfo
	for _, info := range features {
		if info.Name == "alpha" {
			feature = info
		}
	}
	if feature.Enabled {
		t.Fatal("alpha must be disabled")
	}
	if len(feature.Transfers) != 2 || len(feature.TransferStatus) != 2 {
		t.Fatalf("want 2 transfers with 2 statuses, got %+v", feature)
	}
	for i, status := range feature.TransferStatus {
		if status.Component != feature.Transfers[i] {
			t.Errorf("status %d is for %q, want %q (Transfers order)", i, status.Component, feature.Transfers[i])
		}
		want := map[string][3]any{"alpha": {"1.0.0", "1.0.0", false}, "beta": {"1.0.0", "2.0.0", true}}[status.Component]
		if status.RunningVersion != want[0] || status.LinkedVersion != want[1] || status.PendingReboot != want[2] {
			t.Errorf("%s status = %+v, want %v", status.Component, status, want)
		}
	}
}

// TestFeatures_UnreadableMergedStateIsAWarning pins that a transfer whose
// merged state cannot be read does not break the listing: the link is still
// reported, the failure is a warning and the transfer's Error.
func TestFeatures_UnreadableMergedStateIsAWarning(t *testing.T) {
	f := newStatusFixture(t)
	f.addFeature("alpha", true)
	f.addFeature("beta", true)
	f.stage("alpha", "2.0.0")
	f.link("alpha", "2.0.0")
	f.stage("beta", "1.0.0")
	f.link("beta", "1.0.0")
	// A directory where the origin file belongs makes the read fail.
	if err := os.MkdirAll(filepath.Join(f.hierarchy, sysext.MergedStateDirName, "origin"), 0o755); err != nil {
		t.Fatal(err)
	}

	features, err := f.client().Features(t.Context())
	if err != nil {
		t.Fatalf("Features must not fail: %v", err)
	}
	if len(features) != 2 {
		t.Fatalf("want 2 features, got %d", len(features))
	}
	for _, info := range features {
		s := info.TransferStatus[0]
		if !strings.Contains(s.Error, "failed to read merged state") {
			t.Errorf("%s: Error = %q, want a merged-state failure", info.Name, s.Error)
		}
		if s.LinkedVersion == "" {
			t.Errorf("%s: the link must still be reported", info.Name)
		}
		if s.PendingReboot {
			t.Errorf("%s: no pending state without a readable running version", info.Name)
		}
	}
	if !f.reporter.hasWarningContaining("failed to read merged state") {
		t.Errorf("want a merged-state warning, got %v", f.reporter.warnings)
	}
}

func TestCheckFeatures_ReportsRunningAndLinkedVersions(t *testing.T) {
	for _, tt := range statusCases {
		t.Run(tt.name, func(t *testing.T) {
			f := newStatusFixture(t)
			f.addFeature("alpha", true)
			tt.setup(f)

			results, err := f.client().CheckFeatures(t.Context(), CheckFeaturesOptions{})
			if err != nil {
				t.Fatalf("CheckFeatures: %v", err)
			}
			if len(results) != 1 || len(results[0].Results) != 1 {
				t.Fatalf("want 1 result, got %+v", results)
			}
			got := results[0].Results[0]
			if got.RunningVersion != tt.wantRunning || got.LinkedVersion != tt.wantLinked || got.PendingReboot != tt.wantPending {
				t.Errorf("result = %+v, want running %q linked %q pending %v", got, tt.wantRunning, tt.wantLinked, tt.wantPending)
			}
			if got.CurrentVersion != tt.wantCurrent {
				t.Errorf("CurrentVersion = %q, want %q (its meaning is unchanged)", got.CurrentVersion, tt.wantCurrent)
			}
			if got.NewestVersion != "2.0.0" {
				t.Errorf("NewestVersion = %q, want 2.0.0", got.NewestVersion)
			}
			if got.Error != "" {
				t.Errorf("unexpected Error %q", got.Error)
			}
		})
	}
}

// TestCheckFeatures_UnreadableMergedStateIsTheComponentsError pins that a
// merged-state read failure is reported in that component's Error, with the
// link and newest version kept, and does not stop the other components.
func TestCheckFeatures_UnreadableMergedStateIsTheComponentsError(t *testing.T) {
	f := newStatusFixture(t)
	f.addFeature("alpha", true)
	f.stage("alpha", "1.0.0")
	f.link("alpha", "1.0.0")
	if err := os.MkdirAll(filepath.Join(f.hierarchy, sysext.MergedStateDirName, "origin"), 0o755); err != nil {
		t.Fatal(err)
	}

	results, err := f.client().CheckFeatures(t.Context(), CheckFeaturesOptions{})
	if err == nil {
		t.Fatal("want the aggregate error for a component that could not be checked")
	}
	got := results[0].Results[0]
	if !strings.Contains(got.Error, "failed to read merged state") {
		t.Errorf("Error = %q", got.Error)
	}
	if got.UpdateAvailable || got.NewestVersion != "2.0.0" || got.LinkedVersion != "1.0.0" || got.RunningVersion != "" {
		t.Errorf("partial result wrong: %+v", got)
	}
}

// TestCheckFeatures_ManifestFailureKeepsStatus pins that a component whose
// manifest cannot be fetched still reports what it runs and has linked.
func TestCheckFeatures_ManifestFailureKeepsStatus(t *testing.T) {
	f := newStatusFixture(t)
	server := testutil.NewErrorServer(t, 404)
	defer server.Close()
	createFeatureFile(t, f.configDir, "alpha", true)
	writeCheckTransfer(t, f.configDir, "alpha", "alpha", server.URL, f.targetDir, false)
	f.stage("alpha", "1.0.0")
	f.link("alpha", "1.0.0")
	f.merge(map[string]string{"alpha": "1.0.0"})

	results, err := f.client().CheckFeatures(t.Context(), CheckFeaturesOptions{})
	if err == nil {
		t.Fatal("want an error")
	}
	got := results[0].Results[0]
	if got.Error == "" || got.RunningVersion != "1.0.0" || got.LinkedVersion != "1.0.0" {
		t.Errorf("result = %+v", got)
	}
}

// TestCheckResultJSONShape pins the JSON keys: running_version and
// linked_version are always present ("" means none), and current_version is
// still emitted.
func TestCheckResultJSONShape(t *testing.T) {
	f := newStatusFixture(t)
	f.addFeature("alpha", true)
	f.stage("alpha", "1.0.0")
	f.stage("alpha", "2.0.0")
	f.link("alpha", "2.0.0")

	results, err := f.client().CheckFeatures(t.Context(), CheckFeaturesOptions{})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(results[0].Results[0])
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]any{
		"current_version": "2.0.0", "newest_version": "2.0.0",
		"running_version": "", "linked_version": "2.0.0", "pending_reboot": true,
	} {
		if m[key] != want {
			t.Errorf("%s = %v, want %v (json %s)", key, m[key], want, data)
		}
	}
}

// pendingFixture is a host with an enabled feature "alpha" that is current and
// merged, and a disabled feature "beta" whose link names 2.0.0 while 1.0.0 is
// merged: the refresh activates beta too.
func pendingFixture(t *testing.T) *statusFixture {
	t.Helper()
	f := newStatusFixture(t)
	f.addFeature("alpha", true)
	f.addFeature("beta", false)
	f.stage("alpha", "2.0.0")
	f.link("alpha", "2.0.0")
	f.stage("beta", "1.0.0")
	f.stage("beta", "2.0.0")
	f.link("beta", "2.0.0")
	f.merge(map[string]string{"alpha": "2.0.0", "beta": "1.0.0"})
	return f
}

func wantBetaPending(t *testing.T, got []PendingActivation) {
	t.Helper()
	want := PendingActivation{Component: "beta", RunningVersion: "1.0.0", LinkedVersion: "2.0.0"}
	if len(got) != 1 || got[0] != want {
		t.Errorf("PendingActivations = %+v, want [%+v]", got, want)
	}
}

func TestUpdateFeatures_RefreshWarnsAboutOtherPendingLinks(t *testing.T) {
	f := pendingFixture(t)

	results, err := f.client().UpdateFeatures(t.Context(), UpdateFeaturesOptions{NoVacuum: true})
	if err != nil {
		t.Fatalf("UpdateFeatures: %v", err)
	}
	if !f.runner.RefreshCalled {
		t.Error("the refresh must still run")
	}
	if len(f.reporter.warnings) != 1 ||
		!strings.Contains(f.reporter.warnings[0], "will also activate 1 sysext(s)") ||
		!strings.Contains(f.reporter.warnings[0], "beta: 1.0.0 -> 2.0.0") {
		t.Errorf("warnings = %v", f.reporter.warnings)
	}
	if strings.Contains(f.reporter.warnings[0], "alpha") {
		t.Errorf("alpha is this run's own feature and must not be listed: %v", f.reporter.warnings)
	}
	if len(results) != 1 {
		t.Fatalf("want 1 feature result, got %d", len(results))
	}
	wantBetaPending(t, results[0].PendingActivations)
}

// TestUpdateFeatures_OwnPendingLinkIsNotListed pins the rule: an enabled
// feature's pending link is what the operator asked to apply, so only others
// are listed. With nothing else pending, nothing is listed.
func TestUpdateFeatures_OwnPendingLinkIsNotListed(t *testing.T) {
	f := newStatusFixture(t)
	f.addFeature("alpha", true)
	f.stage("alpha", "2.0.0")
	f.link("alpha", "2.0.0")
	f.merge(map[string]string{"alpha": "1.0.0"}) // alpha itself is pending

	results, err := f.client().UpdateFeatures(t.Context(), UpdateFeaturesOptions{NoVacuum: true})
	if err != nil {
		t.Fatalf("UpdateFeatures: %v", err)
	}
	if !f.runner.RefreshCalled {
		t.Error("the refresh must run")
	}
	if len(f.reporter.warnings) != 0 || len(results[0].PendingActivations) != 0 {
		t.Errorf("nothing else is pending: warnings %v, list %v", f.reporter.warnings, results[0].PendingActivations)
	}
}

func TestUpdateFeatures_NoRefreshDoesNotListPendingLinks(t *testing.T) {
	f := pendingFixture(t)

	results, err := f.client().UpdateFeatures(t.Context(), UpdateFeaturesOptions{NoRefresh: true, NoVacuum: true})
	if err != nil {
		t.Fatalf("UpdateFeatures: %v", err)
	}
	if f.runner.RefreshCalled {
		t.Error("NoRefresh must not refresh")
	}
	if len(f.reporter.warnings) != 0 || len(results[0].PendingActivations) != 0 {
		t.Errorf("no refresh, no list: warnings %v, list %v", f.reporter.warnings, results[0].PendingActivations)
	}
}

func TestUpdateFeatures_DryRunReportsWhatWouldBeActivated(t *testing.T) {
	f := pendingFixture(t)

	results, err := f.client().UpdateFeatures(t.Context(), UpdateFeaturesOptions{DryRun: true, NoVacuum: true})
	if err != nil {
		t.Fatalf("UpdateFeatures: %v", err)
	}
	if f.runner.RefreshCalled {
		t.Error("a dry run must not refresh")
	}
	if len(f.reporter.warnings) != 1 || !strings.Contains(f.reporter.warnings[0], "would also activate") {
		t.Errorf("warnings = %v", f.reporter.warnings)
	}
	wantBetaPending(t, results[0].PendingActivations)
}

func TestEnableFeature_NowWarnsAboutOtherPendingLinks(t *testing.T) {
	f := newStatusFixture(t)
	f.addFeature("alpha", false)
	f.addFeature("beta", false)
	f.stage("alpha", "2.0.0") // alpha's newest image is staged and linked: enable --now finds it current
	f.link("alpha", "2.0.0")
	f.stage("beta", "1.0.0")
	f.stage("beta", "2.0.0")
	f.link("beta", "2.0.0")
	f.merge(map[string]string{"alpha": "1.0.0", "beta": "1.0.0"})

	result, err := f.client().EnableFeature(t.Context(), "alpha", EnableFeatureOptions{Now: true})
	if err != nil {
		t.Fatalf("EnableFeature: %v", err)
	}
	if !f.runner.RefreshCalled {
		t.Error("the refresh must still run")
	}
	wantBetaPending(t, result.PendingActivations)
	if !f.reporter.hasWarningContaining("beta: 1.0.0 -> 2.0.0") {
		t.Errorf("warnings = %v", f.reporter.warnings)
	}
}

func TestEnableFeature_NowWithNothingElsePendingListsNothing(t *testing.T) {
	f := newStatusFixture(t)
	f.addFeature("alpha", false)
	f.stage("alpha", "2.0.0")
	f.link("alpha", "2.0.0")

	result, err := f.client().EnableFeature(t.Context(), "alpha", EnableFeatureOptions{Now: true})
	if err != nil {
		t.Fatalf("EnableFeature: %v", err)
	}
	if !f.runner.RefreshCalled {
		t.Error("the refresh must run")
	}
	if len(result.PendingActivations) != 0 || len(f.reporter.warnings) != 0 {
		t.Errorf("want nothing listed, got %v / %v", result.PendingActivations, f.reporter.warnings)
	}
}

func TestEnableFeature_NowDryRunReportsWhatWouldBeActivated(t *testing.T) {
	f := pendingFixture(t)

	result, err := f.client().EnableFeature(t.Context(), "alpha", EnableFeatureOptions{Now: true, DryRun: true})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if f.runner.RefreshCalled {
		t.Error("a dry run must not refresh")
	}
	wantBetaPending(t, result.PendingActivations)
	if !f.reporter.hasWarningContaining("would also activate") {
		t.Errorf("warnings = %v", f.reporter.warnings)
	}
}

func TestEnableFeature_NowNoRefreshDoesNotList(t *testing.T) {
	f := pendingFixture(t)

	result, err := f.client().EnableFeature(t.Context(), "alpha", EnableFeatureOptions{Now: true, NoRefresh: true})
	if err != nil {
		t.Fatalf("EnableFeature: %v", err)
	}
	if f.runner.RefreshCalled {
		t.Error("NoRefresh must not refresh")
	}
	if len(result.PendingActivations) != 0 || len(f.reporter.warnings) != 0 {
		t.Errorf("want nothing listed, got %v / %v", result.PendingActivations, f.reporter.warnings)
	}
}

func TestDisableFeature_NowWarnsAboutOtherPendingLinks(t *testing.T) {
	f := pendingFixture(t)

	result, err := f.client().DisableFeature(t.Context(), "alpha", DisableFeatureOptions{Now: true, Force: true})
	if err != nil {
		t.Fatalf("DisableFeature: %v", err)
	}
	if !f.runner.UnmergeCalled || !f.runner.RefreshCalled {
		t.Errorf("unmerge and refresh must run: unmerge=%v refresh=%v", f.runner.UnmergeCalled, f.runner.RefreshCalled)
	}
	wantBetaPending(t, result.PendingActivations)
	if !f.reporter.hasWarningContaining("will also activate") {
		t.Errorf("warnings = %v", f.reporter.warnings)
	}
}

func TestDisableFeature_NowDryRunReportsWhatWouldBeActivated(t *testing.T) {
	f := pendingFixture(t)

	result, err := f.client().DisableFeature(t.Context(), "alpha", DisableFeatureOptions{Now: true, Force: true, DryRun: true})
	if err != nil {
		t.Fatalf("DisableFeature: %v", err)
	}
	if f.runner.UnmergeCalled || f.runner.RefreshCalled {
		t.Error("a dry run must not unmerge or refresh")
	}
	wantBetaPending(t, result.PendingActivations)
	if !f.reporter.hasWarningContaining("would also activate") {
		t.Errorf("warnings = %v", f.reporter.warnings)
	}
}

// TestDisableFeature_WithoutNowDoesNotList pins that a plain disable runs no
// refresh and so lists nothing.
func TestDisableFeature_WithoutNowDoesNotList(t *testing.T) {
	f := pendingFixture(t)

	result, err := f.client().DisableFeature(t.Context(), "alpha", DisableFeatureOptions{})
	if err != nil {
		t.Fatalf("DisableFeature: %v", err)
	}
	if len(result.PendingActivations) != 0 || len(f.reporter.warnings) != 0 {
		t.Errorf("want nothing listed, got %v / %v", result.PendingActivations, f.reporter.warnings)
	}
}

// TestPendingActivations_ComponentScopeStillChecksTheWholeHost pins that a
// run scoped to one component lists pending links of other components: the
// refresh does not honour the scope.
func TestPendingActivations_ComponentScopeStillChecksTheWholeHost(t *testing.T) {
	root := t.TempDir()
	targetDir := t.TempDir()
	linkDir := t.TempDir()
	hierarchy := t.TempDir()
	for _, name := range []string{"alpha", "beta"} {
		dir := filepath.Join(root, "sysupdate."+name+".d")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		createFeatureFile(t, dir, name, name == "beta") // alpha disabled; beta enabled
		writeCheckTransfer(t, dir, name, name, "http://127.0.0.1:1", targetDir, false)
	}
	// beta (other component, enabled) is linked ahead of what runs.
	for _, v := range []string{"1.0.0", "2.0.0"} {
		if err := os.WriteFile(filepath.Join(targetDir, "beta_"+v+".raw"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(targetDir, "beta_2.0.0.raw"), filepath.Join(linkDir, "beta.raw")); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(hierarchy, sysext.MergedStateDirName)
	if err := os.MkdirAll(state, 0o755); err != nil {
		t.Fatal(err)
	}
	origin := `{"extensions":{"beta":{"path":"` + filepath.Join(targetDir, "beta_1.0.0.raw") + `"}}}`
	if err := os.WriteFile(filepath.Join(state, "origin"), []byte(origin), 0o644); err != nil {
		t.Fatal(err)
	}

	runner := &sysext.MockRunner{}
	rep := &recordingReporter{}
	client := NewClient(ClientConfig{
		SysextRunner: runner,
		Progress:     rep,
		Paths: RuntimePaths{
			DefinitionRoots:   []string{root},
			SysextLinkDir:     linkDir,
			RunExtensionsDir:  filepath.Join(t.TempDir(), "run-extensions"),
			SysextHierarchies: []string{hierarchy},
		},
	})

	// Disable alpha's feature --now within its own component.
	result, err := client.DisableFeature(t.Context(), "alpha", DisableFeatureOptions{Now: true, Force: true, Component: "alpha"})
	if err != nil {
		t.Fatalf("DisableFeature: %v", err)
	}
	if len(result.PendingActivations) != 1 || result.PendingActivations[0].Component != "beta" {
		t.Errorf("PendingActivations = %+v, want beta (another component)", result.PendingActivations)
	}
}

func TestActivationPending(t *testing.T) {
	for _, tt := range []struct {
		running, linked string
		want            bool
	}{
		{"1", "2", true},
		{"", "2", true},
		{"2", "2", false},
		{"1", "", false},
		{"", "", false},
		{sysext.UnknownVersion, "2", false},
	} {
		if got := activationPending(tt.running, tt.linked); got != tt.want {
			t.Errorf("activationPending(%q, %q) = %v, want %v", tt.running, tt.linked, got, tt.want)
		}
	}
}

func TestPendingActivationString(t *testing.T) {
	if got := (PendingActivation{Component: "a", RunningVersion: "1", LinkedVersion: "2"}).String(); got != "a: 1 -> 2" {
		t.Errorf("String = %q", got)
	}
	if got := (PendingActivation{Component: "a", LinkedVersion: "2"}).String(); got != "a: not merged -> 2" {
		t.Errorf("String = %q", got)
	}
}

// TestEnableFeature_NowWarnsAboutDeactivations pins that a refresh lists a
// sysext that is merged but not linked (for example a feature disabled
// without --now): the refresh unmerges it under its workload.
func TestEnableFeature_NowWarnsAboutDeactivations(t *testing.T) {
	f := newStatusFixture(t)
	f.addFeature("alpha", false)
	f.addFeature("beta", false)
	f.stage("alpha", "2.0.0")
	f.link("alpha", "2.0.0")
	f.stage("beta", "1.0.0") // beta is merged, but its link is gone
	f.merge(map[string]string{"alpha": "1.0.0", "beta": "1.0.0"})

	result, err := f.client().EnableFeature(t.Context(), "alpha", EnableFeatureOptions{Now: true})
	if err != nil {
		t.Fatalf("EnableFeature: %v", err)
	}
	if !f.runner.RefreshCalled {
		t.Error("the refresh must still run")
	}
	want := PendingActivation{Component: "beta", RunningVersion: "1.0.0", Deactivate: true}
	if len(result.PendingActivations) != 1 || result.PendingActivations[0] != want {
		t.Errorf("PendingActivations = %+v, want [%+v]", result.PendingActivations, want)
	}
	if !f.reporter.hasWarningContaining("also deactivate 1 sysext(s) that are merged but not linked: beta: 1.0.0 -> not linked") {
		t.Errorf("warnings = %v", f.reporter.warnings)
	}
	if f.reporter.hasWarningContaining("also activate") {
		t.Errorf("nothing is activated, but warnings = %v", f.reporter.warnings)
	}
}

func TestDeactivationPending(t *testing.T) {
	for _, tt := range []struct {
		running, linked string
		want            bool
	}{
		{"1", "", true},
		{sysext.UnknownVersion, "", true},
		{"", "", false},
		{"1", "1", false},
		{"1", "2", false},
	} {
		if got := deactivationPending(tt.running, tt.linked); got != tt.want {
			t.Errorf("deactivationPending(%q, %q) = %v, want %v", tt.running, tt.linked, got, tt.want)
		}
	}
	if got := (PendingActivation{Component: "a", RunningVersion: "1", Deactivate: true}).String(); got != "a: 1 -> not linked" {
		t.Errorf("String = %q", got)
	}
}
