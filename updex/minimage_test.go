package updex

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostyard/std/reporter"
	"github.com/frostyard/updex/v2/internal/testutil"
	"github.com/frostyard/updex/v2/sysext"
)

// Tests for the minimum image version check (ADR-0016, plan 0001 Phase 5).
// Images are real fixtures (a GPT with an EROFS partition, built by
// testutil.SysextImage), served over HTTP with a correct SHA256SUMS. bootc is
// a fake; no test runs the real one.

// fakeBootc is a BootcRunner that returns a fixed answer and counts calls.
type fakeBootc struct {
	out   string
	err   error
	calls int
}

func (f *fakeBootc) Status(context.Context) ([]byte, error) {
	f.calls++
	return []byte(f.out), f.err
}

// bootcNotInstalled is what the real runner returns without bootc in PATH.
func bootcNotInstalled() *fakeBootc {
	return &fakeBootc{err: fmt.Errorf("bootc: %w", exec.ErrNotFound)}
}

// bootcStaged reports a staged deployment with the given image version.
func bootcStaged(version string) *fakeBootc {
	return &fakeBootc{out: `{"status":{"staged":{"image":{"version":"` + version + `"}},"booted":{"image":{"version":"20260101000000"}}}}`}
}

// sysextImage returns a snosi-shaped image whose extension-release file sets
// UPDEX_MIN_IMAGE_VERSION=minValue; an empty minValue leaves the key out.
func sysextImage(t *testing.T, name, minValue string) []byte {
	t.Helper()
	release := "ID=_any\nSYSEXT_ID=" + name + "\n"
	if minValue != "" {
		release += "UPDEX_MIN_IMAGE_VERSION=" + minValue + "\n"
	}
	return testutil.SysextImage(t, name, release)
}

// minComp describes one transfer of a minImageHost: the images the server
// publishes, by version.
type minComp struct {
	name     string
	feature  string
	versions map[string][]byte
}

// minImageHost is a rootless host with fixture transfers, a link directory,
// and an os-release file the client reads through RuntimePaths.OSReleasePaths.
type minImageHost struct {
	t         *testing.T
	root      string
	targetDir string
	linkDir   string
	osRelease string
	bootc     *fakeBootc
	out       *bytes.Buffer
	client    *Client
}

// writeOSRelease sets the host's os-release content.
func (h *minImageHost) writeOSRelease(content string) {
	h.t.Helper()
	if err := os.WriteFile(h.osRelease, []byte(content), 0o644); err != nil {
		h.t.Fatal(err)
	}
}

func newMinImageHost(t *testing.T, bootc *fakeBootc, osRelease string, comps ...minComp) *minImageHost {
	t.Helper()
	files := map[string]string{}
	content := map[string][]byte{}
	for _, c := range comps {
		for v, data := range c.versions {
			file := c.name + "_" + v + ".raw"
			files[file] = hashContent(data)
			content[file] = data
		}
	}
	server := testutil.NewTestServer(t, testutil.TestServerFiles{Files: files, Content: content})
	t.Cleanup(server.Close)

	h := &minImageHost{
		t:         t,
		root:      t.TempDir(),
		targetDir: t.TempDir(),
		linkDir:   t.TempDir(),
		bootc:     bootc,
		out:       &bytes.Buffer{},
	}
	h.osRelease = filepath.Join(t.TempDir(), "os-release")
	h.writeOSRelease(osRelease)

	defDir := filepath.Join(h.root, "sysupdate.d")
	if err := os.MkdirAll(defDir, 0o755); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, c := range comps {
		if !seen[c.feature] {
			seen[c.feature] = true
			createFeatureFile(t, defDir, c.feature, true)
		}
		createFeatureTransferFileWithoutCurrentSymlink(t, defDir, c.name, c.feature, server.URL, h.targetDir)
	}

	cfg := ClientConfig{
		Paths: RuntimePaths{
			DefinitionRoots:   []string{h.root},
			OSReleasePaths:    []string{h.osRelease},
			SysextLinkDir:     h.linkDir,
			SysextHierarchies: []string{t.TempDir()},
			RunExtensionsDir:  t.TempDir(),
			RollbackDir:       filepath.Join(t.TempDir(), "rollback"),
		},
		SysextRunner: &sysext.DefaultRunner{},
		Progress:     reporter.NewTextReporter(h.out),
	}
	if bootc != nil {
		cfg.BootcRunner = bootc
	}
	h.client = NewClient(cfg)
	return h
}

// preinstall stages version of component and links it, as an earlier run did.
func (h *minImageHost) preinstall(component, version string, link bool) {
	h.t.Helper()
	path := filepath.Join(h.targetDir, component+"_"+version+".raw")
	if err := os.WriteFile(path, []byte("old image "+version), 0o644); err != nil {
		h.t.Fatal(err)
	}
	if link {
		if err := os.Symlink(path, filepath.Join(h.linkDir, component+".raw")); err != nil {
			h.t.Fatal(err)
		}
	}
}

// linked returns the version the component's link names ("" when no link).
func (h *minImageHost) linked(component string) string {
	h.t.Helper()
	target, err := os.Readlink(filepath.Join(h.linkDir, component+".raw"))
	if err != nil {
		return ""
	}
	base := strings.TrimSuffix(filepath.Base(target), ".raw")
	return strings.TrimPrefix(base, component+"_")
}

func (h *minImageHost) staged(component, version string) bool {
	_, err := os.Stat(filepath.Join(h.targetDir, component+"_"+version+".raw"))
	return err == nil
}

// update runs UpdateFeatures without a refresh and returns the results by
// component.
func (h *minImageHost) update(opts UpdateFeaturesOptions) (map[string]UpdateResult, error) {
	h.t.Helper()
	opts.NoRefresh = true
	results, err := h.client.UpdateFeatures(h.t.Context(), opts)
	byComp := map[string]UpdateResult{}
	for _, fr := range results {
		for _, r := range fr.Results {
			byComp[r.Component] = r
		}
	}
	return byComp, err
}

const (
	osRelease20261001 = "ID=snow\nIMAGE_VERSION=20261001000000\n"
	oldVersion        = "1.0.0"
	newVersion        = "2.0.0"
)

func TestUpdateFeatures_MinImageVersion(t *testing.T) {
	noise := make([]byte, 8192)
	if _, err := rand.Read(noise); err != nil {
		t.Fatal(err)
	}
	noRelease := testutil.BuildGPT(t, testutil.GPTOptions{}, testutil.GPTPartition{
		TypeGUID: testutil.RootX86GUID,
		Data:     testutil.BuildEROFS(t, map[string]string{"usr/bin/tool": "x"}),
	})

	tests := []struct {
		name      string
		image     []byte
		osRelease string
		bootc     *fakeBootc
		// wantLinked is true when the new version must be linked.
		wantLinked bool
		wantNeeds  string
		wantErr    string // substring of the component error; the run fails
		wantBootc  int
	}{
		{name: "key absent: linked", image: sysextImage(t, "tool", ""), osRelease: osRelease20261001, bootc: bootcNotInstalled(), wantLinked: true},
		{name: "image new enough: linked", image: sysextImage(t, "tool", "20260901000000"), osRelease: osRelease20261001, bootc: bootcNotInstalled(), wantLinked: true, wantBootc: 1},
		{name: "image equal to the minimum: linked", image: sysextImage(t, "tool", "20261001000000"), osRelease: osRelease20261001, bootc: bootcNotInstalled(), wantLinked: true, wantBootc: 1},
		{name: "image too old: not linked, no error", image: sysextImage(t, "tool", "20270101000000"), osRelease: osRelease20261001, bootc: bootcNotInstalled(), wantNeeds: "needs image >= 20270101000000", wantBootc: 1},
		{name: "staged deployment newer than booted: linked", image: sysextImage(t, "tool", "20261022021319"), osRelease: osRelease20261001, bootc: bootcStaged("20261101000000"), wantLinked: true, wantBootc: 1},
		{name: "staged deployment still too old: not linked", image: sysextImage(t, "tool", "20261201000000"), osRelease: osRelease20261001, bootc: bootcStaged("20261101000000"), wantNeeds: "needs image >= 20261201000000", wantBootc: 1},
		{name: "minimum not numeric: not linked", image: sysextImage(t, "tool", "soon"), osRelease: osRelease20261001, bootc: bootcNotInstalled(), wantNeeds: "needs image >= soon", wantBootc: 1},
		{name: "no IMAGE_VERSION anywhere: not linked", image: sysextImage(t, "tool", "1"), osRelease: "ID=snow\n", bootc: bootcNotInstalled(), wantNeeds: "needs image >= 1", wantBootc: 1},
		{name: "image version not numeric: not linked", image: sysextImage(t, "tool", "1"), osRelease: "IMAGE_VERSION=latest\n", bootc: bootcNotInstalled(), wantNeeds: "needs image >= 1", wantBootc: 1},
		{name: "unreadable image format: linked, bootc not run", image: noise, osRelease: osRelease20261001, bootc: bootcStaged("1"), wantLinked: true},
		{name: "bootc fails: not linked, error", image: sysextImage(t, "tool", "20260901000000"), osRelease: osRelease20261001, bootc: &fakeBootc{err: errors.New("exit status 1: no sysroot")}, wantErr: "no sysroot", wantBootc: 1},
		{name: "bootc fails but the image has no key: linked", image: sysextImage(t, "tool", ""), osRelease: osRelease20261001, bootc: &fakeBootc{err: errors.New("exit status 1")}, wantLinked: true},
		{name: "EROFS without extension-release: not linked, error", image: noRelease, osRelease: osRelease20261001, bootc: bootcNotInstalled(), wantErr: "extension-release"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newMinImageHost(t, tt.bootc, tt.osRelease, minComp{name: "tool", feature: "toolfeature", versions: map[string][]byte{newVersion: tt.image}})
			h.preinstall("tool", oldVersion, true)

			results, err := h.update(UpdateFeaturesOptions{})
			r := results["tool"]

			if tt.wantErr != "" {
				if err == nil {
					t.Fatal("UpdateFeatures returned no error, want one")
				}
				if !strings.Contains(r.Error, tt.wantErr) {
					t.Errorf("result error = %q, want it to contain %q", r.Error, tt.wantErr)
				}
			} else {
				if err != nil {
					t.Fatalf("UpdateFeatures: %v (a blocked image is not an error)", err)
				}
				if r.Error != "" {
					t.Fatalf("result error = %q", r.Error)
				}
			}
			if r.NeedsImage != tt.wantNeeds {
				t.Errorf("NeedsImage = %q, want %q", r.NeedsImage, tt.wantNeeds)
			}
			if tt.wantLinked {
				if got := h.linked("tool"); got != newVersion {
					t.Errorf("linked version = %q, want %q", got, newVersion)
				}
				if !r.Installed {
					t.Errorf("Installed = false, want true")
				}
			} else {
				if got := h.linked("tool"); got != oldVersion {
					t.Errorf("linked version = %q, want the old %q kept", got, oldVersion)
				}
				if r.Installed {
					t.Errorf("Installed = true for an image that was not linked")
				}
				// The image stays on disk for the next run.
				if !h.staged("tool", newVersion) {
					t.Error("the downloaded image was removed")
				}
			}
			if tt.wantNeeds != "" && !strings.Contains(r.NextActionMessage, tt.wantNeeds) {
				t.Errorf("NextActionMessage = %q, want it to contain %q", r.NextActionMessage, tt.wantNeeds)
			}
			if tt.bootc.calls != tt.wantBootc {
				t.Errorf("bootc ran %d times, want %d", tt.bootc.calls, tt.wantBootc)
			}
		})
	}
}

// A feature's transfers link together: when the second image is too old, the
// first, which is fine, does not link either, and another feature continues.
func TestUpdateFeatures_MinImageVersion_TransfersLinkTogether(t *testing.T) {
	h := newMinImageHost(t, bootcNotInstalled(), osRelease20261001,
		minComp{name: "first", feature: "pair", versions: map[string][]byte{newVersion: sysextImage(t, "first", "")}},
		minComp{name: "second", feature: "pair", versions: map[string][]byte{newVersion: sysextImage(t, "second", "20270101000000")}},
		minComp{name: "other", feature: "single", versions: map[string][]byte{newVersion: sysextImage(t, "other", "20260101000000")}},
	)
	h.preinstall("first", oldVersion, true)
	h.preinstall("second", oldVersion, true)
	h.preinstall("other", oldVersion, true)

	results, err := h.update(UpdateFeaturesOptions{})
	if err != nil {
		t.Fatalf("UpdateFeatures: %v, want nil (a blocked feature is not an error)", err)
	}

	first, second, other := results["first"], results["second"], results["other"]
	if h.linked("first") != oldVersion || h.linked("second") != oldVersion {
		t.Errorf("links moved: first=%q second=%q, want both %q", h.linked("first"), h.linked("second"), oldVersion)
	}
	if second.NeedsImage != "needs image >= 20270101000000" {
		t.Errorf("second.NeedsImage = %q", second.NeedsImage)
	}
	if first.NeedsImage != "" || !first.NotLinked || first.Installed {
		t.Errorf("first = %+v, want NotLinked and no NeedsImage", first)
	}
	if !strings.Contains(first.NextActionMessage, "second of feature pair needs image >= 20270101000000") {
		t.Errorf("first.NextActionMessage = %q, want it to name the transfer that needs the image", first.NextActionMessage)
	}
	if first.Error != "" || second.Error != "" {
		t.Errorf("errors: first=%q second=%q", first.Error, second.Error)
	}
	// Both images stay staged.
	if !h.staged("first", newVersion) || !h.staged("second", newVersion) {
		t.Error("a downloaded image was removed")
	}
	// The other feature continues.
	if h.linked("other") != newVersion || other.NeedsImage != "" || !other.Installed {
		t.Errorf("other = %+v, linked %q, want linked to %s", other, h.linked("other"), newVersion)
	}
	if h.bootc.calls != 1 {
		t.Errorf("bootc ran %d times for 2 images with a key, want 1", h.bootc.calls)
	}
}

// A blocked feature runs no vacuum: the old images it holds stay.
func TestUpdateFeatures_MinImageVersion_BlockedFeatureRunsNoVacuum(t *testing.T) {
	h := newMinImageHost(t, bootcNotInstalled(), osRelease20261001,
		minComp{name: "tool", feature: "toolfeature", versions: map[string][]byte{newVersion: sysextImage(t, "tool", "20270101000000")}})
	h.preinstall("tool", "0.1.0", false)
	h.preinstall("tool", "0.2.0", false)
	h.preinstall("tool", oldVersion, true)

	if _, err := h.update(UpdateFeaturesOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"0.1.0", "0.2.0", oldVersion, newVersion} {
		if !h.staged("tool", v) {
			t.Errorf("version %s was removed; a blocked feature must not vacuum", v)
		}
	}
}

// An image that was blocked stays staged. The next run reaches the relink
// path (nothing to download), checks the image again, and links it once the
// host image is new enough.
func TestUpdateFeatures_MinImageVersion_RelinkPathChecksAgain(t *testing.T) {
	h := newMinImageHost(t, bootcNotInstalled(), osRelease20261001,
		minComp{name: "tool", feature: "toolfeature", versions: map[string][]byte{newVersion: sysextImage(t, "tool", "20261101000000")}})
	h.preinstall("tool", oldVersion, true)

	r := mustUpdate(t, h)["tool"]
	if r.NeedsImage == "" || !r.Downloaded || h.linked("tool") != oldVersion {
		t.Fatalf("first run: %+v, linked %q; want blocked after a download", r, h.linked("tool"))
	}

	// Second run, host image still too old: the image is already staged, so
	// nothing is downloaded, and it is still not linked.
	r = mustUpdate(t, h)["tool"]
	if r.NeedsImage != "needs image >= 20261101000000" || r.Downloaded || h.linked("tool") != oldVersion {
		t.Fatalf("second run: %+v, linked %q; want blocked on the relink path", r, h.linked("tool"))
	}

	// The host image moves on (a reboot into a new deployment): relink.
	h.writeOSRelease("ID=snow\nIMAGE_VERSION=20261201000000\n")
	r = mustUpdate(t, h)["tool"]
	if r.NeedsImage != "" || r.Downloaded || !r.Installed || h.linked("tool") != newVersion {
		t.Fatalf("third run: %+v, linked %q; want the staged image linked without a download", r, h.linked("tool"))
	}
}

func mustUpdate(t *testing.T, h *minImageHost) map[string]UpdateResult {
	t.Helper()
	results, err := h.update(UpdateFeaturesOptions{})
	if err != nil {
		t.Fatalf("UpdateFeatures: %v", err)
	}
	return results
}

// EnableFeature with Now installs through the same path, so it checks too.
func TestEnableFeature_Now_MinImageVersion(t *testing.T) {
	h := newMinImageHost(t, bootcNotInstalled(), osRelease20261001,
		minComp{name: "tool", feature: "toolfeature", versions: map[string][]byte{newVersion: sysextImage(t, "tool", "20270101000000")}},
		minComp{name: "partner", feature: "toolfeature", versions: map[string][]byte{newVersion: sysextImage(t, "partner", "")}})
	h.preinstall("tool", oldVersion, true)

	result, err := h.client.EnableFeature(t.Context(), "toolfeature", EnableFeatureOptions{Now: true, NoRefresh: true})
	if err != nil {
		t.Fatalf("EnableFeature: %v, want nil (a blocked image is not an error)", err)
	}
	if !result.Success {
		t.Errorf("Success = false, want true: the feature is enabled")
	}
	if h.linked("tool") != oldVersion || h.linked("partner") != "" {
		t.Errorf("links: tool=%q partner=%q, want the old link kept and nothing linked", h.linked("tool"), h.linked("partner"))
	}
	if len(result.NotLinked) != 1 || !strings.Contains(result.NotLinked[0], "needs image >= 20270101000000") {
		t.Errorf("NotLinked = %v", result.NotLinked)
	}
	if !strings.Contains(result.NextActionMessage, "needs image >= 20270101000000") {
		t.Errorf("NextActionMessage = %q", result.NextActionMessage)
	}
	if !h.staged("tool", newVersion) || !h.staged("partner", newVersion) {
		t.Error("a downloaded image was removed")
	}
}

func TestEnableFeature_Now_MinImageVersion_NewEnoughLinks(t *testing.T) {
	h := newMinImageHost(t, bootcNotInstalled(), osRelease20261001,
		minComp{name: "tool", feature: "toolfeature", versions: map[string][]byte{newVersion: sysextImage(t, "tool", "20260101000000")}})
	result, err := h.client.EnableFeature(t.Context(), "toolfeature", EnableFeatureOptions{Now: true, NoRefresh: true})
	if err != nil || len(result.NotLinked) != 0 {
		t.Fatalf("EnableFeature: %v, NotLinked %v", err, result.NotLinked)
	}
	if h.linked("tool") != newVersion {
		t.Errorf("linked = %q, want %s", h.linked("tool"), newVersion)
	}
}

// A dry run downloads nothing, so it checks only an image that is already
// staged and would be relinked.
func TestUpdateFeatures_MinImageVersion_DryRun(t *testing.T) {
	t.Run("staged and unlinked image is checked", func(t *testing.T) {
		h := newMinImageHost(t, bootcNotInstalled(), osRelease20261001,
			minComp{name: "tool", feature: "toolfeature", versions: map[string][]byte{newVersion: sysextImage(t, "tool", "20270101000000")}})
		h.preinstall("tool", oldVersion, true)
		// The newest image is staged already (blocked by an earlier run).
		writeFile(t, filepath.Join(h.targetDir, "tool_"+newVersion+".raw"), sysextImage(t, "tool", "20270101000000"))

		r, err := h.update(UpdateFeaturesOptions{DryRun: true})
		if err != nil {
			t.Fatal(err)
		}
		if got := r["tool"].NeedsImage; got != "needs image >= 20270101000000" {
			t.Errorf("NeedsImage = %q", got)
		}
		if !strings.HasPrefix(r["tool"].NextActionMessage, "would not link: ") {
			t.Errorf("NextActionMessage = %q", r["tool"].NextActionMessage)
		}
		if h.linked("tool") != oldVersion {
			t.Error("a dry run moved a link")
		}
	})
	t.Run("an image to download is not read", func(t *testing.T) {
		h := newMinImageHost(t, bootcNotInstalled(), osRelease20261001,
			minComp{name: "tool", feature: "toolfeature", versions: map[string][]byte{newVersion: sysextImage(t, "tool", "20270101000000")}})
		h.preinstall("tool", oldVersion, true)

		r, err := h.update(UpdateFeaturesOptions{DryRun: true})
		if err != nil {
			t.Fatal(err)
		}
		if r["tool"].NeedsImage != "" || !strings.HasPrefix(r["tool"].NextActionMessage, "Would download") {
			t.Errorf("result = %+v, want a plain would-download", r["tool"])
		}
		if h.bootc.calls != 0 {
			t.Errorf("bootc ran %d times in a dry run with nothing staged", h.bootc.calls)
		}
		if h.staged("tool", newVersion) {
			t.Error("a dry run downloaded an image")
		}
	})
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// CheckFeatures shows why a staged image is not linked, never writes, and
// runs bootc only when a staged unlinked image has the key.
func TestCheckFeatures_MinImageVersion(t *testing.T) {
	setup := func(bootc *fakeBootc, minValue string, linkNew bool) *minImageHost {
		h := newMinImageHost(t, bootc, osRelease20261001,
			minComp{name: "tool", feature: "toolfeature", versions: map[string][]byte{newVersion: sysextImage(t, "tool", minValue)}})
		h.preinstall("tool", oldVersion, true)
		writeFile(t, filepath.Join(h.targetDir, "tool_"+newVersion+".raw"), sysextImage(t, "tool", minValue))
		if linkNew {
			if err := os.Remove(filepath.Join(h.linkDir, "tool.raw")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(h.targetDir, "tool_"+newVersion+".raw"), filepath.Join(h.linkDir, "tool.raw")); err != nil {
				t.Fatal(err)
			}
		}
		return h
	}
	check := func(t *testing.T, h *minImageHost) (CheckResult, error) {
		t.Helper()
		results, err := h.client.CheckFeatures(t.Context(), CheckFeaturesOptions{})
		if len(results) != 1 || len(results[0].Results) != 1 {
			t.Fatalf("results = %+v", results)
		}
		return results[0].Results[0], err
	}

	t.Run("staged unlinked image that is too old", func(t *testing.T) {
		h := setup(bootcStaged("20261101000000"), "20270101000000", false)
		r, err := check(t, h)
		if err != nil {
			t.Fatal(err)
		}
		if r.NeedsImage != "needs image >= 20270101000000" || r.NextBootImageVersion != "20261101000000" {
			t.Errorf("result = %+v", r)
		}
		if r.Error != "" {
			t.Errorf("error = %q", r.Error)
		}
		if h.linked("tool") != oldVersion {
			t.Error("check moved a link")
		}
		if h.bootc.calls != 1 {
			t.Errorf("bootc ran %d times, want 1", h.bootc.calls)
		}
	})
	t.Run("staged unlinked image that is new enough", func(t *testing.T) {
		h := setup(bootcNotInstalled(), "20260101000000", false)
		r, err := check(t, h)
		if err != nil || r.NeedsImage != "" || r.NextBootImageVersion != "20261001000000" {
			t.Errorf("result = %+v, err %v", r, err)
		}
	})
	t.Run("staged unlinked image without the key: bootc not run", func(t *testing.T) {
		h := setup(bootcStaged("1"), "", false)
		r, err := check(t, h)
		if err != nil || r.NeedsImage != "" || r.NextBootImageVersion != "" || h.bootc.calls != 0 {
			t.Errorf("result = %+v, err %v, bootc calls %d", r, err, h.bootc.calls)
		}
	})
	t.Run("newest image already linked: nothing read", func(t *testing.T) {
		h := setup(bootcStaged("1"), "20270101000000", true)
		r, err := check(t, h)
		if err != nil || r.NeedsImage != "" || h.bootc.calls != 0 {
			t.Errorf("result = %+v, err %v, bootc calls %d", r, err, h.bootc.calls)
		}
	})
	t.Run("bootc fails: the component has an error", func(t *testing.T) {
		h := setup(&fakeBootc{err: errors.New("exit status 1")}, "20270101000000", false)
		r, err := check(t, h)
		if err == nil || !strings.Contains(r.Error, "next-boot image version") {
			t.Errorf("result = %+v, err %v", r, err)
		}
	})
	t.Run("a rollback holds the staged image: nothing read", func(t *testing.T) {
		h := setup(bootcStaged("1"), "20270101000000", false)
		if err := os.MkdirAll(h.client.paths.rollbackDir, 0o755); err != nil {
			t.Fatal(err)
		}
		record := `{"format":1,"feature":"toolfeature","created":"2026-10-23T00:00:00Z","rejected":{"tool":"` + newVersion + `"}}`
		writeFile(t, filepath.Join(h.client.paths.rollbackDir, "toolfeature"), []byte(record))
		r, err := check(t, h)
		if err != nil || r.NeedsImage != "" || h.bootc.calls != 0 {
			t.Errorf("result = %+v, err %v, bootc calls %d", r, err, h.bootc.calls)
		}
	})
}

// Features lists the same reason on the transfer's status.
func TestFeatures_MinImageVersion_StatusShowsNeedsImage(t *testing.T) {
	h := newMinImageHost(t, bootcNotInstalled(), osRelease20261001,
		minComp{name: "tool", feature: "toolfeature", versions: map[string][]byte{newVersion: sysextImage(t, "tool", "20270101000000")}})
	h.preinstall("tool", oldVersion, true)
	writeFile(t, filepath.Join(h.targetDir, "tool_"+newVersion+".raw"), sysextImage(t, "tool", "20270101000000"))

	features, err := h.client.Features(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(features) != 1 || len(features[0].TransferStatus) != 1 {
		t.Fatalf("features = %+v", features)
	}
	if got := features[0].TransferStatus[0].NeedsImage; got != "needs image >= 20270101000000" {
		t.Errorf("NeedsImage = %q", got)
	}
}

// A feature whose image needs a newer base stops alone; a feature whose
// check fails stops with an error and the run exits non-zero, while the
// other features continue.
func TestUpdateFeatures_MinImageVersion_ErrorStopsOnlyItsFeature(t *testing.T) {
	h := newMinImageHost(t, &fakeBootc{err: errors.New("exit status 1")}, osRelease20261001,
		minComp{name: "keyed", feature: "needscheck", versions: map[string][]byte{newVersion: sysextImage(t, "keyed", "20260101000000")}},
		minComp{name: "plain", feature: "nocheck", versions: map[string][]byte{newVersion: sysextImage(t, "plain", "")}},
	)
	h.preinstall("keyed", oldVersion, true)
	h.preinstall("plain", oldVersion, true)

	results, err := h.update(UpdateFeaturesOptions{})
	if err == nil {
		t.Fatal("UpdateFeatures returned no error, want one for the failed check")
	}
	if results["keyed"].Error == "" || h.linked("keyed") != oldVersion {
		t.Errorf("keyed = %+v, linked %q; want an error and the old link", results["keyed"], h.linked("keyed"))
	}
	if results["plain"].Error != "" || h.linked("plain") != newVersion {
		t.Errorf("plain = %+v, linked %q; want it linked", results["plain"], h.linked("plain"))
	}
}

// A rollback links an older image on purpose and does not run the check: an
// older revision needs at most the base the host already runs.
func TestRollbackFeature_DoesNotCheckMinImageVersion(t *testing.T) {
	h := newMinImageHost(t, bootcNotInstalled(), "ID=snow\n",
		minComp{name: "tool", feature: "toolfeature", versions: map[string][]byte{oldVersion: nil}})
	writeFile(t, filepath.Join(h.targetDir, "tool_"+oldVersion+".raw"), sysextImage(t, "tool", "99999999999999"))
	writeFile(t, filepath.Join(h.targetDir, "tool_"+newVersion+".raw"), sysextImage(t, "tool", ""))
	if err := os.Symlink(filepath.Join(h.targetDir, "tool_"+newVersion+".raw"), filepath.Join(h.linkDir, "tool.raw")); err != nil {
		t.Fatal(err)
	}

	result, err := h.client.RollbackFeature(t.Context(), "toolfeature", RollbackFeatureOptions{})
	if err != nil || !result.Success {
		t.Fatalf("RollbackFeature: %v, %+v", err, result)
	}
	if h.linked("tool") != oldVersion {
		t.Errorf("linked = %q, want the rolled-back %q", h.linked("tool"), oldVersion)
	}
	if h.bootc.calls != 0 {
		t.Errorf("bootc ran %d times during a rollback", h.bootc.calls)
	}
}
