package updex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostyard/clix"
	"github.com/frostyard/updex/v2/sysext"
	"github.com/frostyard/updex/v2/updex"
)

// writeGatedTransfer writes a transfer for component with Features= and
// RequisiteFeatures= (ADR-0017).
func (h *statusHost) writeGatedTransfer(t *testing.T, component string, features, requisites string) {
	t.Helper()
	content := "[Transfer]\nFeatures=" + features + "\n"
	if requisites != "" {
		content += "RequisiteFeatures=" + requisites + "\n"
	}
	content += "Verify=false\n\n[Source]\nType=url-file\nPath=" + h.serverURL + "\nMatchPattern=" + component + "_@v.raw\n\n" +
		"[Target]\nPath=" + h.targetDir + "\nMatchPattern=" + component + "_@v.raw\n"
	if err := os.WriteFile(filepath.Join(h.configDir, component+".transfer"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (h *statusHost) stage(t *testing.T, component, linked string, versions ...string) {
	t.Helper()
	for _, v := range versions {
		if err := os.WriteFile(filepath.Join(h.targetDir, component+"_"+v+".raw"), []byte("image "+component+"_"+v+".raw"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(h.targetDir, component+"_"+linked+".raw"), filepath.Join(h.linkDir, component+".raw")); err != nil {
		t.Fatal(err)
	}
}

// gatedHost: maild is enabled, lodge is disabled, alpha needs both.
func gatedHost(t *testing.T) *statusHost {
	t.Helper()
	h := newStatusHost(t)
	writeFeatureFile(t, h.configDir, "maild", true)
	writeFeatureFile(t, h.configDir, "lodge", false)
	h.writeGatedTransfer(t, "alpha", "maild", "lodge")
	return h
}

func TestRunFeaturesCheck_TextAndJSONReportInactiveTransfer(t *testing.T) {
	h := gatedHost(t)

	output, err := h.run(t, false, runFeaturesCheck)
	if err != nil {
		t.Fatalf("runFeaturesCheck: %v", err)
	}
	assertContains(t, output, "maild/alpha: inactive: waits for feature(s) lodge")
	assertNotContains(t, output, "No enabled features")

	output, err = h.run(t, true, runFeaturesCheck)
	if err != nil {
		t.Fatalf("runFeaturesCheck --json: %v", err)
	}
	var results []updex.CheckFeaturesResult
	if err := json.Unmarshal([]byte(output), &results); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, output)
	}
	if len(results) != 1 || len(results[0].Results) != 0 || len(results[0].Inactive) != 1 ||
		results[0].Inactive[0].Component != "alpha" || strings.Join(results[0].Inactive[0].MissingRequisites, ",") != "lodge" {
		t.Errorf("results = %+v", results)
	}
}

func TestRunFeaturesUpdate_TextAndJSONReportInactiveTransfer(t *testing.T) {
	h := gatedHost(t)
	oldNoRefresh, oldNoVac, oldDryRun, oldEUID, oldRunner := noRefresh, featureUpdateNoVac, clix.DryRun, getEUID, sysextRunner
	t.Cleanup(func() {
		noRefresh, featureUpdateNoVac, clix.DryRun, getEUID, sysextRunner = oldNoRefresh, oldNoVac, oldDryRun, oldEUID, oldRunner
	})
	noRefresh, featureUpdateNoVac, clix.DryRun = true, true, false
	getEUID = func() int { return 0 }
	sysextRunner = &sysext.MockRunner{}

	output, err := h.run(t, false, runFeaturesUpdate)
	if err != nil {
		t.Fatalf("runFeaturesUpdate: %v", err)
	}
	assertContains(t, output, "maild/alpha: inactive: waits for feature(s) lodge")

	output, err = h.run(t, true, runFeaturesUpdate)
	if err != nil {
		t.Fatalf("runFeaturesUpdate --json: %v", err)
	}
	var results []updex.UpdateFeaturesResult
	if err := json.Unmarshal([]byte(output), &results); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, output)
	}
	if len(results) != 1 || len(results[0].Results) != 0 || len(results[0].Inactive) != 1 || results[0].Inactive[0].Component != "alpha" {
		t.Errorf("results = %+v", results)
	}
	if _, err := os.Lstat(filepath.Join(h.linkDir, "alpha.raw")); !os.IsNotExist(err) {
		t.Errorf("an inactive transfer must not be linked (err = %v)", err)
	}
}

func TestRunFeaturesEnable_NowTextReportsInactiveTransfer(t *testing.T) {
	h := gatedHost(t)
	writeFeatureFile(t, h.configDir, "maild", false)
	setFeatureCLIFlags(t, featureCLIFlags{definitions: h.configDir, now: true, noRefresh: true, euid: 0, runner: &sysext.MockRunner{}})

	output, err := runFeatureHandler(t, runFeaturesEnable, "maild")
	if err != nil {
		t.Fatalf("runFeaturesEnable: %v\n%s", err, output)
	}
	assertContains(t, output, "Feature 'maild' enabled.", "maild/alpha: inactive: waits for feature(s) lodge")
	if _, err := os.Lstat(filepath.Join(h.linkDir, "alpha.raw")); !os.IsNotExist(err) {
		t.Errorf("an inactive transfer must not be linked (err = %v)", err)
	}
}

func TestRunFeaturesDisable_NowTextAndJSONReportKeptTransfer(t *testing.T) {
	for _, jsonOut := range []bool{false, true} {
		name := "text"
		if jsonOut {
			name = "json"
		}
		t.Run(name, func(t *testing.T) {
			h := newStatusHost(t)
			writeFeatureFile(t, h.configDir, "a", true)
			writeFeatureFile(t, h.configDir, "b", true)
			h.writeGatedTransfer(t, "alpha", "a b", "")
			h.stage(t, "alpha", "2.0.0", "1.0.0", "2.0.0")
			runner := &sysext.MockRunner{}
			setFeatureCLIFlags(t, featureCLIFlags{definitions: h.configDir, now: true, euid: 0, runner: runner, jsonOutput: jsonOut})

			output, err := runFeatureHandler(t, runFeaturesDisable, "a")
			if err != nil {
				t.Fatalf("runFeaturesDisable: %v\n%s", err, output)
			}
			if runner.UnmergeCalled || runner.RefreshCalled {
				t.Error("nothing was removed: no unmerge and no refresh")
			}
			if _, err := os.Lstat(filepath.Join(h.targetDir, "alpha_2.0.0.raw")); err != nil {
				t.Errorf("the kept image must stay: %v", err)
			}
			if jsonOut {
				result := decodeActionResult(t, output)
				if len(result.Kept) != 1 || result.Kept[0].Component != "alpha" || strings.Join(result.Kept[0].ActiveThrough, ",") != "b" {
					t.Errorf("Kept = %+v", result.Kept)
				}
				return
			}
			assertContains(t, output, "alpha: kept: still active through feature(s) b")
		})
	}
}
