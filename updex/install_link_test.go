package updex

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostyard/std/reporter"
	"github.com/frostyard/updex/config"
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
	return newStagedCurrentFixtureWithRunner(t, enabled, &sysext.DefaultRunner{})
}

// newStagedCurrentFixtureWithRunner is newStagedCurrentFixture with the
// injected sysext runner chosen by the caller.
func newStagedCurrentFixtureWithRunner(t *testing.T, enabled bool, runner sysext.SysextRunner) *stagedCurrentFixture {
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
		SysextRunner: runner,
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

// legacyRunner implements only the original sysext.SysextRunner interface —
// no LinkToSysextAt — exactly like a runner written before
// sysext.PathSysextRunner existed. Its LinkToSysext does what the deprecated
// sysext.LinkToSysext wrapper does (spelled out here so the test does not
// call a deprecated API): it links into whatever sysext.SysextDir currently
// holds. If the SDK ever calls it again, the tests below observe a link
// appearing in a directory the client never captured.
type legacyRunner struct {
	linkCalled bool
}

var _ sysext.SysextRunner = (*legacyRunner)(nil)

func (r *legacyRunner) Refresh() error { return nil }
func (r *legacyRunner) Merge() error   { return nil }
func (r *legacyRunner) Unmerge() error { return nil }

func (r *legacyRunner) LinkToSysext(t *config.Transfer) error {
	r.linkCalled = true
	return sysext.LinkToSysextAt(t, sysext.SysextDir)
}

// redirectSysextDirAfterConstruction points the mutable package global at a
// fresh directory the client must never touch, and returns that directory.
// Call it only after NewClient, so it proves the client captured its link
// directory at construction rather than reading the global later.
func redirectSysextDirAfterConstruction(t *testing.T) string {
	t.Helper()
	untouched := t.TempDir()
	old := sysext.SysextDir
	sysext.SysextDir = untouched
	t.Cleanup(func() { sysext.SysextDir = old })
	return untouched
}

// A runner that predates PathSysextRunner cannot be told which directory to
// link into. UpdateFeatures refuses it up front instead of falling back to
// the mutable package global, and refuses before it has removed a legacy
// symlink, downloaded, or linked anything.
func TestUpdateFeatures_LegacyRunnerRefusedBeforeAnyMutation(t *testing.T) {
	runner := &legacyRunner{}
	if _, ok := any(runner).(sysext.PathSysextRunner); ok {
		t.Fatal("legacyRunner must not implement sysext.PathSysextRunner")
	}
	f := newStagedCurrentFixtureWithRunner(t, true, runner)
	untouched := redirectSysextDirAfterConstruction(t)

	results, err := f.client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{NoRefresh: true})
	if err == nil {
		t.Fatal("expected UpdateFeatures to refuse a legacy sysext runner")
	}
	if len(results) != 1 || len(results[0].Results) != 1 {
		t.Fatalf("expected 1 feature result with 1 component, got %+v", results)
	}
	got := results[0].Results[0].Error
	if !strings.Contains(got, "does not implement sysext.PathSysextRunner") {
		t.Errorf("component error = %q, want it to name the unsupported runner", got)
	}
	if runner.linkCalled {
		t.Error("the legacy runner's LinkToSysext must not be called")
	}
	assertOnlyEntries(t, untouched)
	assertOnlyEntries(t, f.linkDir)
	assertOnlyEntries(t, f.targetDir, "testext_1.0.0.raw")
	if strings.Contains(f.out.String(), "restored sysext link") {
		t.Errorf("expected no link work at all, got:\n%s", f.out.String())
	}
}

// EnableFeature --now refuses the same runner before it writes the drop-in,
// so the feature is not left enabled with nothing staged, and the returned
// error is testable with errors.Is.
func TestEnableFeature_Now_LegacyRunnerRefusedBeforeDropIn(t *testing.T) {
	runner := &legacyRunner{}
	f := newStagedCurrentFixtureWithRunner(t, false, runner)
	untouched := redirectSysextDirAfterConstruction(t)

	result, err := f.client.EnableFeature(t.Context(), "testfeature", EnableFeatureOptions{Now: true, NoRefresh: true})
	if !errors.Is(err, ErrLegacySysextRunner) {
		t.Fatalf("EnableFeature error = %v, want ErrLegacySysextRunner", err)
	}
	if result == nil || result.Success {
		t.Fatalf("expected an unsuccessful result, got %+v", result)
	}
	if !strings.Contains(result.Error, "does not implement sysext.PathSysextRunner") {
		t.Errorf("result.Error = %q, want it to name the unsupported runner", result.Error)
	}
	dropIn := filepath.Join(f.root, "sysupdate.d", "testfeature.feature.d", updexDropInName)
	if _, err := os.Lstat(dropIn); !os.IsNotExist(err) {
		t.Errorf("expected no drop-in at %s before the runner error, got err=%v", dropIn, err)
	}
	if runner.linkCalled {
		t.Error("the legacy runner's LinkToSysext must not be called")
	}
	assertOnlyEntries(t, untouched)
	assertOnlyEntries(t, f.linkDir)
}

// A dry run never links, so it stays available to a legacy runner.
func TestEnableFeature_DryRunNowStillWorksWithLegacyRunner(t *testing.T) {
	f := newStagedCurrentFixtureWithRunner(t, false, &legacyRunner{})
	untouched := redirectSysextDirAfterConstruction(t)

	result, err := f.client.EnableFeature(t.Context(), "testfeature", EnableFeatureOptions{Now: true, DryRun: true, NoRefresh: true})
	if err != nil {
		t.Fatalf("dry-run EnableFeature failed: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected Success=true, got error %q", result.Error)
	}
	assertOnlyEntries(t, untouched)
	assertOnlyEntries(t, f.linkDir)
}

// A PathSysextRunner is handed exactly the directory the client captured,
// never the package global, even after the global moves.
func TestUpdateFeatures_PathRunnerReceivesCapturedLinkDir(t *testing.T) {
	runner := &sysext.MockRunner{}
	f := newStagedCurrentFixtureWithRunner(t, true, runner)
	untouched := redirectSysextDirAfterConstruction(t)

	f.updateOnce(t)
	if !runner.LinkToSysextCalled {
		t.Fatal("expected the runner to be asked to restore the missing link")
	}
	if runner.LinkToSysextAtDir != f.linkDir {
		t.Errorf("LinkToSysextAt dir = %q, want the captured %q", runner.LinkToSysextAtDir, f.linkDir)
	}
	if runner.LinkToSysextAtDir == untouched {
		t.Error("the runner was given the mutated package global, not the captured dir")
	}
}
