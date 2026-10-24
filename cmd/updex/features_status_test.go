package updex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostyard/clix"
	"github.com/frostyard/updex/v2/internal/testutil"
	"github.com/frostyard/updex/v2/sysext"
	"github.com/frostyard/updex/v2/updex"
	"github.com/spf13/cobra"
)

// statusHost is a rootless host for the RUNNING/LINKED handler tests. It points
// the CLI's runtime-path seam at a temporary link directory and hierarchy, so
// no test reads the host's /var/lib/extensions or /usr/.systemd-sysext.
type statusHost struct {
	configDir string
	targetDir string
	linkDir   string
	hierarchy string
	serverURL string

	rollbackDir string
}

func newStatusHost(t *testing.T) *statusHost {
	t.Helper()
	files := map[string]string{}
	content := map[string][]byte{}
	for _, name := range []string{"alpha", "beta"} {
		for _, v := range []string{"1.0.0", "2.0.0"} {
			file := name + "_" + v + ".raw"
			data := []byte("image " + file)
			files[file] = sha256Hex(data)
			content[file] = data
		}
	}
	server := testutil.NewTestServer(t, testutil.TestServerFiles{Files: files, Content: content})
	t.Cleanup(server.Close)

	h := &statusHost{
		configDir: t.TempDir(),
		targetDir: t.TempDir(),
		linkDir:   t.TempDir(),
		hierarchy: t.TempDir(),
		serverURL: server.URL,

		rollbackDir: filepath.Join(t.TempDir(), "rollback"),
	}
	setRuntimePaths(t, updex.RuntimePaths{
		RollbackDir:       h.rollbackDir,
		SysextLinkDir:     h.linkDir,
		SysextHierarchies: []string{h.hierarchy},
		RunExtensionsDir:  filepath.Join(t.TempDir(), "run-extensions"),
	})
	return h
}

// feature writes a feature and a transfer for component (the same name),
// stages the listed versions, and links linked ("" for no link).
func (h *statusHost) feature(t *testing.T, component string, enabled bool, linked string, staged ...string) {
	t.Helper()
	writeFeatureFile(t, h.configDir, component, enabled)
	writeTransferFile(t, h.configDir, h.targetDir, component, component, h.serverURL, "")
	for _, v := range staged {
		if err := os.WriteFile(filepath.Join(h.targetDir, component+"_"+v+".raw"), []byte("image "+component+"_"+v+".raw"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if linked != "" {
		if err := os.Symlink(filepath.Join(h.targetDir, component+"_"+linked+".raw"), filepath.Join(h.linkDir, component+".raw")); err != nil {
			t.Fatal(err)
		}
	}
}

// merge records the merged versions in the hierarchy's origin file.
func (h *statusHost) merge(t *testing.T, versions map[string]string) {
	t.Helper()
	entries := map[string]map[string]string{}
	for component, v := range versions {
		entries[component] = map[string]string{"path": filepath.Join(h.targetDir, component+"_"+v+".raw")}
	}
	data, err := json.Marshal(map[string]any{"extensions": entries})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(h.hierarchy, sysext.MergedStateDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "origin"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// run runs a read-only features handler against the host.
func (h *statusHost) run(t *testing.T, jsonOut bool, handler func(*cobra.Command, []string) error) (string, error) {
	t.Helper()
	oldDefinitions, oldComponent, oldJSON, oldSilent := definitions, featureComponent, clix.JSONOutput, clix.Silent
	t.Cleanup(func() {
		definitions, featureComponent, clix.JSONOutput, clix.Silent = oldDefinitions, oldComponent, oldJSON, oldSilent
	})
	definitions, featureComponent, clix.JSONOutput, clix.Silent = h.configDir, "", jsonOut, true

	return captureStdout(t, func() error {
		cmd := &cobra.Command{}
		cmd.SetContext(t.Context())
		return handler(cmd, nil)
	})
}

// rowFields returns the whitespace-separated fields of the first output line
// that starts with prefix.
func rowFields(t *testing.T, output, prefix string) []string {
	t.Helper()
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.Fields(line)
		}
	}
	t.Fatalf("no line starting with %q in:\n%s", prefix, output)
	return nil
}

func TestRunFeaturesCheck_TextShowsRunningLinkedAndPendingReboot(t *testing.T) {
	h := newStatusHost(t)
	h.feature(t, "alpha", true, "2.0.0", "1.0.0", "2.0.0")
	h.merge(t, map[string]string{"alpha": "1.0.0"})

	output, err := h.run(t, false, runFeaturesCheck)
	if err != nil {
		t.Fatalf("runFeaturesCheck: %v", err)
	}

	header := rowFields(t, output, "FEATURE")
	if strings.Join(header, " ") != "FEATURE COMPONENT CURRENT RUNNING LINKED NEWEST UPDATE" {
		t.Errorf("header = %v", header)
	}
	// FEATURE COMPONENT CURRENT RUNNING LINKED NEWEST UPDATE
	if got := strings.Join(rowFields(t, output, "alpha"), " "); got != "alpha alpha 2.0.0 1.0.0 2.0.0 2.0.0 no" {
		t.Errorf("row = %q", got)
	}
	assertContains(t, output, "PENDING REBOOT: 1 sysext(s) linked to a version that is not running: alpha: 1.0.0 -> 2.0.0")
}

func TestRunFeaturesCheck_TextWithoutPendingHasNoMarker(t *testing.T) {
	h := newStatusHost(t)
	h.feature(t, "alpha", true, "2.0.0", "2.0.0")
	h.merge(t, map[string]string{"alpha": "2.0.0"})

	output, err := h.run(t, false, runFeaturesCheck)
	if err != nil {
		t.Fatalf("runFeaturesCheck: %v", err)
	}
	if got := strings.Join(rowFields(t, output, "alpha"), " "); got != "alpha alpha 2.0.0 2.0.0 2.0.0 2.0.0 no" {
		t.Errorf("row = %q", got)
	}
	assertNotContains(t, output, "PENDING")
}

func TestRunFeaturesCheck_TextShowsDashForNotMergedAndUnknownForUnrecorded(t *testing.T) {
	h := newStatusHost(t)
	h.feature(t, "alpha", true, "2.0.0", "2.0.0")
	h.feature(t, "beta", true, "", "1.0.0")
	// alpha is merged without a recorded version; beta is not merged and has no link.
	dir := filepath.Join(h.hierarchy, sysext.MergedStateDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "extensions"), []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	output, err := h.run(t, false, runFeaturesCheck)
	if err != nil {
		t.Fatalf("runFeaturesCheck: %v", err)
	}
	if got := strings.Join(rowFields(t, output, "alpha"), " "); got != "alpha alpha 2.0.0 unknown 2.0.0 2.0.0 no" {
		t.Errorf("alpha row = %q", got)
	}
	if got := strings.Join(rowFields(t, output, "beta"), " "); got != "beta beta 1.0.0 - - 2.0.0 yes" {
		t.Errorf("beta row = %q", got)
	}
	assertNotContains(t, output, "PENDING")
}

func TestRunFeaturesCheck_JSONCarriesRunningAndLinked(t *testing.T) {
	h := newStatusHost(t)
	h.feature(t, "alpha", true, "2.0.0", "1.0.0", "2.0.0")
	h.merge(t, map[string]string{"alpha": "1.0.0"})

	output, err := h.run(t, true, runFeaturesCheck)
	if err != nil {
		t.Fatalf("runFeaturesCheck: %v", err)
	}
	var raw []struct {
		Feature string                   `json:"feature"`
		Results []map[string]interface{} `json:"results"`
	}
	if err := json.Unmarshal([]byte(output), &raw); err != nil {
		t.Fatalf("decode: %v\n%s", err, output)
	}
	if len(raw) != 1 || len(raw[0].Results) != 1 {
		t.Fatalf("unexpected shape: %s", output)
	}
	r := raw[0].Results[0]
	for key, want := range map[string]any{
		"component": "alpha", "current_version": "2.0.0", "newest_version": "2.0.0",
		"running_version": "1.0.0", "linked_version": "2.0.0", "pending_reboot": true, "update_available": false,
	} {
		if r[key] != want {
			t.Errorf("%s = %v, want %v", key, r[key], want)
		}
	}
}

func TestRunFeaturesList_TextShowsRunningAndLinked(t *testing.T) {
	h := newStatusHost(t)
	h.feature(t, "alpha", true, "2.0.0", "1.0.0", "2.0.0")
	h.merge(t, map[string]string{"alpha": "1.0.0"})
	// A disabled feature with a link and no merged image is listed too.
	h.feature(t, "beta", false, "1.0.0", "1.0.0")

	output, err := h.run(t, false, runFeaturesList)
	if err != nil {
		t.Fatalf("runFeaturesList: %v", err)
	}
	if got := rowFields(t, output, "FEATURE"); strings.Join(got[len(got)-3:], " ") != "TRANSFERS RUNNING LINKED" {
		t.Errorf("header = %v", got)
	}
	// FEATURE DESCRIPTION(2 words) ENABLED CATALOG TRANSFERS RUNNING LINKED
	alpha := rowFields(t, output, "alpha")
	if got := strings.Join(alpha[len(alpha)-3:], " "); got != "alpha 1.0.0 2.0.0" {
		t.Errorf("alpha row = %v", alpha)
	}
	beta := rowFields(t, output, "beta")
	if got := strings.Join(beta[len(beta)-3:], " "); got != "beta - 1.0.0" {
		t.Errorf("beta row = %v", beta)
	}
	assertContains(t, output,
		"PENDING REBOOT: 2 sysext(s) linked to a version that is not running: alpha: 1.0.0 -> 2.0.0, beta: not merged -> 1.0.0")
}

func TestRunFeaturesList_TextShowsOneValuePerTransfer(t *testing.T) {
	h := newStatusHost(t)
	h.feature(t, "alpha", true, "2.0.0", "1.0.0", "2.0.0")
	// beta's transfer also belongs to the alpha feature.
	writeTransferFile(t, h.configDir, h.targetDir, "beta", "alpha", h.serverURL, "")
	h.merge(t, map[string]string{"alpha": "2.0.0"})

	output, err := h.run(t, false, runFeaturesList)
	if err != nil {
		t.Fatalf("runFeaturesList: %v", err)
	}
	// Both transfers: alpha is merged and linked, beta is neither.
	assertContains(t, output, "alpha, beta", "2.0.0, -")
}

func TestRunFeaturesList_JSONCarriesTransferStatus(t *testing.T) {
	h := newStatusHost(t)
	h.feature(t, "alpha", true, "2.0.0", "1.0.0", "2.0.0")
	h.merge(t, map[string]string{"alpha": "1.0.0"})

	output, err := h.run(t, true, runFeaturesList)
	if err != nil {
		t.Fatalf("runFeaturesList: %v", err)
	}
	var features []map[string]any
	if err := json.Unmarshal([]byte(output), &features); err != nil {
		t.Fatalf("decode: %v\n%s", err, output)
	}
	if len(features) != 1 {
		t.Fatalf("want 1 feature: %s", output)
	}
	f := features[0]
	for _, key := range []string{"name", "enabled", "source", "origin", "transfers"} {
		if _, ok := f[key]; !ok {
			t.Errorf("existing field %q is missing: %s", key, output)
		}
	}
	statuses, ok := f["transfer_status"].([]any)
	if !ok || len(statuses) != 1 {
		t.Fatalf("transfer_status = %v", f["transfer_status"])
	}
	s := statuses[0].(map[string]any)
	for key, want := range map[string]any{
		"component": "alpha", "running_version": "1.0.0", "linked_version": "2.0.0", "pending_reboot": true,
	} {
		if s[key] != want {
			t.Errorf("transfer_status.%s = %v, want %v", key, s[key], want)
		}
	}
}

// TestRunFeaturesList_UnreadableMergedStateDoesNotBreakTheListing pins that a
// transfer whose merged state cannot be read shows "error" and the listing
// still succeeds.
func TestRunFeaturesList_UnreadableMergedStateDoesNotBreakTheListing(t *testing.T) {
	h := newStatusHost(t)
	h.feature(t, "alpha", true, "2.0.0", "2.0.0")
	if err := os.MkdirAll(filepath.Join(h.hierarchy, sysext.MergedStateDirName, "origin"), 0o755); err != nil {
		t.Fatal(err)
	}

	output, err := h.run(t, false, runFeaturesList)
	if err != nil {
		t.Fatalf("runFeaturesList must not fail: %v", err)
	}
	alpha := rowFields(t, output, "alpha")
	if got := strings.Join(alpha[len(alpha)-3:], " "); got != "alpha error error" {
		t.Errorf("alpha row = %v", alpha)
	}
	assertNotContains(t, output, "PENDING")
}

// TestRunFeaturesUpdate_JSONListsOtherPendingActivations runs the update
// handler (refresh on, mock runner) on a host where a disabled feature's link
// is ahead of what runs: the refresh runs, and the JSON result lists it.
func TestRunFeaturesUpdate_JSONListsOtherPendingActivations(t *testing.T) {
	h := newStatusHost(t)
	h.feature(t, "alpha", true, "2.0.0", "2.0.0")
	h.feature(t, "beta", false, "2.0.0", "1.0.0", "2.0.0")
	h.merge(t, map[string]string{"alpha": "2.0.0", "beta": "1.0.0"})

	runner := &sysext.MockRunner{}
	oldDefinitions, oldNoRefresh, oldNoVac := definitions, noRefresh, featureUpdateNoVac
	oldJSON, oldSilent, oldDryRun := clix.JSONOutput, clix.Silent, clix.DryRun
	oldEUID, oldRunner := getEUID, sysextRunner
	t.Cleanup(func() {
		definitions, noRefresh, featureUpdateNoVac = oldDefinitions, oldNoRefresh, oldNoVac
		clix.JSONOutput, clix.Silent, clix.DryRun = oldJSON, oldSilent, oldDryRun
		getEUID, sysextRunner = oldEUID, oldRunner
	})
	definitions, noRefresh, featureUpdateNoVac = h.configDir, false, true
	clix.JSONOutput, clix.Silent, clix.DryRun = true, true, false
	getEUID = func() int { return 0 }
	sysextRunner = runner

	output, err := captureStdout(t, func() error {
		cmd := &cobra.Command{}
		cmd.SetContext(t.Context())
		return runFeaturesUpdate(cmd, nil)
	})
	if err != nil {
		t.Fatalf("runFeaturesUpdate: %v", err)
	}
	if !runner.RefreshCalled {
		t.Error("the refresh must still run")
	}
	var results []updex.UpdateFeaturesResult
	if err := json.Unmarshal([]byte(output), &results); err != nil {
		t.Fatalf("decode: %v\n%s", err, output)
	}
	if len(results) != 1 {
		t.Fatalf("want 1 result: %s", output)
	}
	want := updex.PendingActivation{Component: "beta", RunningVersion: "1.0.0", LinkedVersion: "2.0.0"}
	if got := results[0].PendingActivations; len(got) != 1 || got[0] != want {
		t.Errorf("PendingActivations = %+v, want [%+v]", got, want)
	}
}

// TestRunFeaturesEnable_NowJSONListsOtherPendingActivations covers the enable
// handler: refresh still runs and the result lists the other pending link.
func TestRunFeaturesEnable_NowJSONListsOtherPendingActivations(t *testing.T) {
	h := newStatusHost(t)
	h.feature(t, "alpha", false, "2.0.0", "2.0.0")
	h.feature(t, "beta", false, "2.0.0", "1.0.0", "2.0.0")
	h.merge(t, map[string]string{"alpha": "2.0.0", "beta": "1.0.0"})

	runner := &sysext.MockRunner{}
	roots := []string{t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()}
	setRuntimePaths(t, updex.RuntimePaths{
		DefinitionRoots:   roots,
		SysextLinkDir:     h.linkDir,
		SysextHierarchies: []string{h.hierarchy},
		RunExtensionsDir:  filepath.Join(t.TempDir(), "run-extensions"),
	})
	setFeatureCLIFlags(t, featureCLIFlags{definitions: h.configDir, now: true, jsonOutput: true, runner: runner})

	output, err := captureStdout(t, func() error {
		cmd := &cobra.Command{}
		cmd.SetContext(t.Context())
		return runFeaturesEnable(cmd, []string{"alpha"})
	})
	if err != nil {
		t.Fatalf("runFeaturesEnable: %v", err)
	}
	if !runner.RefreshCalled {
		t.Error("the refresh must still run")
	}
	result := decodeActionResult(t, output)
	want := updex.PendingActivation{Component: "beta", RunningVersion: "1.0.0", LinkedVersion: "2.0.0"}
	if len(result.PendingActivations) != 1 || result.PendingActivations[0] != want {
		t.Errorf("PendingActivations = %+v, want [%+v]", result.PendingActivations, want)
	}
}
