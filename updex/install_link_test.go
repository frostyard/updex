package updex

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostyard/std/reporter"
	"github.com/frostyard/updex/internal/testutil"
	"github.com/frostyard/updex/sysext"
)

// stagedCurrentFixture stages testext_1.0.0.raw as the only (and therefore
// current) image for an enabled feature, serves the same version from a test
// server, and returns a client with the real DefaultRunner and an instance
// SysextLinkDir under t.TempDir(). installTransfer will find the image
// already current and must not download.
type stagedCurrentFixture struct {
	client    *Client
	out       *bytes.Buffer
	root      string // definition root (drop-ins land under it)
	targetDir string
	linkDir   string
	linkPath  string
	extPath   string
}

func newStagedCurrentFixture(t *testing.T, enabled bool) *stagedCurrentFixture {
	t.Helper()
	root := t.TempDir()
	defDir := filepath.Join(root, "sysupdate.d")
	if err := os.MkdirAll(defDir, 0755); err != nil {
		t.Fatal(err)
	}
	targetDir := t.TempDir()
	linkDir := t.TempDir()

	extContent := []byte("already installed extension content")
	server := testutil.NewTestServer(t, testutil.TestServerFiles{
		Files:   map[string]string{"testext_1.0.0.raw": hashContent(extContent)},
		Content: map[string][]byte{"testext_1.0.0.raw": extContent},
	})
	t.Cleanup(server.Close)

	createFeatureFile(t, defDir, "testfeature", enabled)
	createFeatureTransferFileWithoutCurrentSymlink(t, defDir, "testext", "testfeature", server.URL, targetDir)

	extPath := filepath.Join(targetDir, "testext_1.0.0.raw")
	if err := os.WriteFile(extPath, extContent, 0644); err != nil {
		t.Fatalf("failed to stage installed extension: %v", err)
	}

	out := &bytes.Buffer{}
	client := NewClient(ClientConfig{
		Paths:        RuntimePaths{DefinitionRoots: []string{root}, SysextLinkDir: linkDir},
		SysextRunner: &sysext.DefaultRunner{},
		Progress:     reporter.NewTextReporter(out),
	})
	return &stagedCurrentFixture{
		client:    client,
		out:       out,
		root:      root,
		targetDir: targetDir,
		linkDir:   linkDir,
		linkPath:  filepath.Join(linkDir, "testext.raw"),
		extPath:   extPath,
	}
}

func (f *stagedCurrentFixture) updateOnce(t *testing.T) UpdateResult {
	t.Helper()
	results, err := f.client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{NoRefresh: true})
	if err != nil {
		t.Fatalf("UpdateFeatures failed: %v", err)
	}
	if len(results) != 1 || len(results[0].Results) != 1 {
		t.Fatalf("expected 1 feature result with 1 component, got %+v", results)
	}
	r := results[0].Results[0]
	if r.Error != "" {
		t.Fatalf("component update failed: %s", r.Error)
	}
	return r
}

func assertLinkResolvesTo(t *testing.T, linkPath, want string) {
	t.Helper()
	got, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatalf("expected sysext link at %s: %v", linkPath, err)
	}
	if got != want {
		t.Errorf("sysext link target = %q, want %q", got, want)
	}
	if info, err := os.Stat(linkPath); err != nil || !info.Mode().IsRegular() {
		t.Errorf("sysext link does not resolve to a regular file: info=%v err=%v", info, err)
	}
}

// The staged image is current but the systemd-sysext link is missing,
// dangling, or points at another image: UpdateFeatures restores it without
// re-downloading and says so.
func TestUpdateFeatures_RestoresSysextLinkForCurrentImage(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, f *stagedCurrentFixture)
	}{
		{name: "no link", setup: func(*testing.T, *stagedCurrentFixture) {}},
		{name: "dangling link", setup: func(t *testing.T, f *stagedCurrentFixture) {
			if err := os.Symlink(filepath.Join(f.targetDir, "testext_0.9.0.raw"), f.linkPath); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "link to an older staged image", setup: func(t *testing.T, f *stagedCurrentFixture) {
			older := filepath.Join(f.targetDir, "testext_0.9.0.raw")
			if err := os.WriteFile(older, []byte("older extension content"), 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(older, f.linkPath); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newStagedCurrentFixture(t, true)
			tc.setup(t, f)

			r := f.updateOnce(t)
			if r.Downloaded {
				t.Error("expected already-current component not to download")
			}
			assertLinkResolvesTo(t, f.linkPath, f.extPath)
			if !strings.Contains(f.out.String(), "restored sysext link for testext") {
				t.Errorf("expected a 'restored sysext link' message, got:\n%s", f.out.String())
			}
			assertOnlyEntries(t, f.linkDir, "testext.raw")
		})
	}
}

// A correct link is left exactly as it is: no relink, no message.
func TestUpdateFeatures_LeavesCorrectSysextLinkAlone(t *testing.T) {
	f := newStagedCurrentFixture(t, true)
	if err := os.Symlink(f.extPath, f.linkPath); err != nil {
		t.Fatal(err)
	}
	before, err := os.Lstat(f.linkPath)
	if err != nil {
		t.Fatal(err)
	}

	r := f.updateOnce(t)
	if r.Downloaded {
		t.Error("expected already-current component not to download")
	}
	assertLinkResolvesTo(t, f.linkPath, f.extPath)
	after, err := os.Lstat(f.linkPath)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Error("expected the correct sysext link to be untouched, but it was replaced")
	}
	if strings.Contains(f.out.String(), "restored sysext link") {
		t.Errorf("expected no relink message for a correct link, got:\n%s", f.out.String())
	}
	assertOnlyEntries(t, f.linkDir, "testext.raw")
}

// EnableFeature --now on a staged-current image with no link: the drop-in
// lands under the temp definition root, nothing downloads, and the link is
// restored.
func TestEnableFeature_Now_RestoresSysextLinkForCurrentImage(t *testing.T) {
	f := newStagedCurrentFixture(t, false)

	result, err := f.client.EnableFeature(t.Context(), "testfeature", EnableFeatureOptions{Now: true, NoRefresh: true})
	if err != nil {
		t.Fatalf("EnableFeature failed: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected Success=true, got error %q", result.Error)
	}
	if len(result.DownloadedFiles) != 0 {
		t.Errorf("expected no downloads for an already-current image, got %v", result.DownloadedFiles)
	}
	wantDropIn := filepath.Join(f.root, "sysupdate.d", "testfeature.feature.d", updexDropInName)
	if result.DropIn != wantDropIn {
		t.Errorf("drop-in = %q, want %q", result.DropIn, wantDropIn)
	}
	if _, err := os.Stat(wantDropIn); err != nil {
		t.Errorf("expected drop-in under the temp definition root: %v", err)
	}
	assertLinkResolvesTo(t, f.linkPath, f.extPath)
	if !strings.Contains(f.out.String(), "restored sysext link for testext") {
		t.Errorf("expected a 'restored sysext link' message, got:\n%s", f.out.String())
	}
}

// A target directory that cannot be listed is an error on the already-current
// path now, not a silent fall-through into a download.
func TestUpdateFeatures_InstalledVersionsErrorIsReported(t *testing.T) {
	root := t.TempDir()
	defDir := filepath.Join(root, "sysupdate.d")
	if err := os.MkdirAll(defDir, 0755); err != nil {
		t.Fatal(err)
	}
	notADir := filepath.Join(t.TempDir(), "target-is-a-file")
	if err := os.WriteFile(notADir, []byte("not a directory"), 0644); err != nil {
		t.Fatal(err)
	}
	extContent := []byte("extension content")
	server := testutil.NewTestServer(t, testutil.TestServerFiles{
		Files:   map[string]string{"testext_1.0.0.raw": hashContent(extContent)},
		Content: map[string][]byte{"testext_1.0.0.raw": extContent},
	})
	defer server.Close()
	createFeatureFile(t, defDir, "testfeature", true)
	createFeatureTransferFileWithoutCurrentSymlink(t, defDir, "testext", "testfeature", server.URL, notADir)

	client := NewClient(ClientConfig{
		Paths:        RuntimePaths{DefinitionRoots: []string{root}, SysextLinkDir: t.TempDir()},
		SysextRunner: &sysext.MockRunner{},
	})
	results, err := client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{NoRefresh: true})
	if err == nil {
		t.Fatal("expected UpdateFeatures to report the component failure")
	}
	if len(results) != 1 || len(results[0].Results) != 1 {
		t.Fatalf("expected 1 feature result with 1 component, got %+v", results)
	}
	got := results[0].Results[0].Error
	if !strings.Contains(got, "failed to inspect installed versions") {
		t.Errorf("component error = %q, want it wrapped as 'failed to inspect installed versions'", got)
	}
}
