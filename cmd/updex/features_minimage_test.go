package updex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostyard/clix"
	"github.com/frostyard/updex/v2/internal/testutil"
	"github.com/frostyard/updex/v2/updex"
)

// Handler tests for the minimum image version check (ADR-0016): update and
// check print why a staged image is not linked, and the JSON carries the new
// fields. The staged image is a real fixture (GPT + EROFS); bootc is a fake.

type cliFakeBootc struct {
	out   string
	calls int
}

func (f *cliFakeBootc) Status(context.Context) ([]byte, error) {
	f.calls++
	return []byte(f.out), nil
}

// minImageHost stages alpha 1.0.0 (linked) and alpha 2.0.0, whose image sets
// UPDEX_MIN_IMAGE_VERSION=min, and points the client at an os-release with
// IMAGE_VERSION=host and a fake bootc with no staged deployment.
func minImageHost(t *testing.T, min, host string) (*statusHost, *cliFakeBootc) {
	t.Helper()
	h := newStatusHost(t)
	h.feature(t, "alpha", true, "1.0.0", "1.0.0", "2.0.0")

	release := "ID=_any\nSYSEXT_ID=alpha\n"
	if min != "" {
		release += "UPDEX_MIN_IMAGE_VERSION=" + min + "\n"
	}
	image := testutil.SysextImage(t, "alpha", release)
	if err := os.WriteFile(filepath.Join(h.targetDir, "alpha_2.0.0.raw"), image, 0o644); err != nil {
		t.Fatal(err)
	}

	osRelease := filepath.Join(t.TempDir(), "os-release")
	if err := os.WriteFile(osRelease, []byte("ID=snow\nIMAGE_VERSION="+host+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runtimePaths.OSReleasePaths = []string{osRelease}

	fake := &cliFakeBootc{out: `{"status":{"staged":null}}`}
	oldBootc, oldNoRefresh, oldDry, oldEUID := bootcRunner, noRefresh, clix.DryRun, getEUID
	t.Cleanup(func() { bootcRunner, noRefresh, clix.DryRun, getEUID = oldBootc, oldNoRefresh, oldDry, oldEUID })
	bootcRunner, noRefresh, clix.DryRun, getEUID = fake, true, false, func() int { return 0 }
	return h, fake
}

func TestRunFeaturesUpdate_MinImageVersion_TextShowsReason(t *testing.T) {
	h, fake := minImageHost(t, "20270101000000", "20261001000000")

	out, err := h.run(t, false, runFeaturesUpdate)
	if err != nil {
		t.Fatalf("a blocked image must exit 0: %v\n%s", err, out)
	}
	for _, want := range []string{"not linked: needs image >= 20270101000000", "alpha/alpha: not linked: needs image >= 20270101000000 (next-boot image 20261001000000)"} {
		if !strings.Contains(out, want) {
			t.Errorf("update output lacks %q:\n%s", want, out)
		}
	}
	if got := h.linkVersion(t, "alpha"); got != "1.0.0" {
		t.Errorf("the link moved to %s", got)
	}
	if fake.calls != 1 {
		t.Errorf("bootc ran %d times, want 1", fake.calls)
	}
}

func TestRunFeaturesUpdate_MinImageVersion_JSONHasFields(t *testing.T) {
	h, _ := minImageHost(t, "20270101000000", "20261001000000")

	out, err := h.run(t, true, runFeaturesUpdate)
	if err != nil {
		t.Fatalf("a blocked image must exit 0: %v\n%s", err, out)
	}
	var raw []map[string]any
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatalf("update JSON: %v\n%s", err, out)
	}
	r := raw[0]["results"].([]any)[0].(map[string]any)
	if r["needs_image"] != "needs image >= 20270101000000" || r["next_boot_image_version"] != "20261001000000" {
		t.Errorf("result = %v", r)
	}
	if _, ok := r["error"]; ok {
		t.Errorf("a blocked image must not set error: %v", r)
	}
	if r["installed"] != false {
		t.Errorf("installed = %v, want false", r["installed"])
	}
}

func TestRunFeaturesUpdate_MinImageVersion_NewEnoughLinks(t *testing.T) {
	h, _ := minImageHost(t, "20260101000000", "20261001000000")
	out, err := h.run(t, false, runFeaturesUpdate)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if strings.Contains(out, "needs image") {
		t.Errorf("unexpected reason:\n%s", out)
	}
	if got := h.linkVersion(t, "alpha"); got != "2.0.0" {
		t.Errorf("linked %s, want 2.0.0", got)
	}
}

func TestRunFeaturesCheck_MinImageVersion(t *testing.T) {
	h, fake := minImageHost(t, "20270101000000", "20261001000000")

	out, err := h.run(t, false, runFeaturesCheck)
	if err != nil {
		t.Fatalf("runFeaturesCheck: %v\n%s", err, out)
	}
	fields := rowFields(t, out, "alpha")
	if got := strings.Join(fields[len(fields)-4:], " "); got != "needs image >= 20270101000000" {
		t.Errorf("UPDATE column = %q\n%s", got, out)
	}
	footer := "NEEDS NEWER IMAGE: 1 staged sysext(s)"
	if !strings.Contains(out, footer) || !strings.Contains(out, "alpha (needs image >= 20270101000000)") || !strings.Contains(out, "next-boot image 20261001000000") {
		t.Errorf("check output lacks the footer:\n%s", out)
	}
	if got := h.linkVersion(t, "alpha"); got != "1.0.0" {
		t.Errorf("check moved the link to %s", got)
	}

	// JSON.
	out, err = h.run(t, true, runFeaturesCheck)
	if err != nil {
		t.Fatal(err)
	}
	var results []updex.CheckFeaturesResult
	if err := json.Unmarshal([]byte(out), &results); err != nil || len(results) != 1 {
		t.Fatalf("check JSON: %v\n%s", err, out)
	}
	r := results[0].Results[0]
	if r.NeedsImage != "needs image >= 20270101000000" || r.NextBootImageVersion != "20261001000000" || r.Error != "" {
		t.Errorf("check result = %+v", r)
	}
	if fake.calls != 2 {
		t.Errorf("bootc ran %d times for 2 checks, want 2 (once per operation)", fake.calls)
	}
}

func TestRunFeaturesCheck_MinImageVersion_KeyAbsentRunsNoBootc(t *testing.T) {
	h, fake := minImageHost(t, "", "20261001000000")
	out, err := h.run(t, false, runFeaturesCheck)
	if err != nil || strings.Contains(out, "NEEDS NEWER IMAGE") || fake.calls != 0 {
		t.Errorf("err %v, bootc calls %d\n%s", err, fake.calls, out)
	}
}

func TestRunFeaturesList_MinImageVersion_Footer(t *testing.T) {
	h, _ := minImageHost(t, "20270101000000", "20261001000000")
	out, err := h.run(t, false, runFeaturesList)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "NEEDS NEWER IMAGE: 1 staged sysext(s)") {
		t.Errorf("list output lacks the footer:\n%s", out)
	}
	out, err = h.run(t, true, runFeaturesList)
	var features []updex.FeatureInfo
	if jsonErr := json.Unmarshal([]byte(out), &features); err != nil || jsonErr != nil || len(features) != 1 {
		t.Fatalf("list JSON: %v %v\n%s", err, jsonErr, out)
	}
	if got := features[0].TransferStatus[0].NeedsImage; got != "needs image >= 20270101000000" {
		t.Errorf("list JSON needs_image = %q", got)
	}
}
