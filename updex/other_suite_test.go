package updex

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/frostyard/std/reporter"
	"github.com/frostyard/updex/v2/sysext"
)

// otherSuiteFixture is feature "grp" with 1 transfer "foo" whose patterns
// select the suite with %w (foo_@v_%w.raw), on a host whose os-release says
// VERSION_ID=14. The staging directory holds images of suites 13 and 14;
// the link points to foo_1.0_14.raw.
type otherSuiteFixture struct {
	client    *Client
	out       *bytes.Buffer
	server    *revisionServer
	targetDir string
	linkDir   string
}

func newOtherSuiteFixture(t *testing.T, runner sysext.SysextRunner) *otherSuiteFixture {
	t.Helper()
	root := t.TempDir()
	defDir := filepath.Join(root, "sysupdate.d")
	if err := os.MkdirAll(defDir, 0o755); err != nil {
		t.Fatal(err)
	}
	osRelease := filepath.Join(t.TempDir(), "os-release")
	if err := os.WriteFile(osRelease, []byte("ID=debian\nVERSION_ID=14\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &otherSuiteFixture{
		out:       &bytes.Buffer{},
		server:    newRevisionServer(t, "foo"),
		targetDir: t.TempDir(),
		linkDir:   t.TempDir(),
	}
	f.server.publish("2.0_14")
	f.server.publish("4.0_13")
	createFeatureFile(t, defDir, "grp", true)
	transfer := "[Transfer]\nFeatures=grp\nVerify=false\n\n[Source]\nType=url-file\nPath=" + f.server.URL +
		"\nMatchPattern=foo_@v_%w.raw\n\n[Target]\nPath=" + f.targetDir + "\nMatchPattern=foo_@v_%w.raw\n"
	if err := os.WriteFile(filepath.Join(defDir, "foo.transfer"), []byte(transfer), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"foo_1.0_14.raw", "foo_1.0_13.raw", "foo_2.0_13.raw", "foo_3.0_13.raw"} {
		if err := os.WriteFile(filepath.Join(f.targetDir, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(f.targetDir, "foo_1.0_14.raw"), filepath.Join(f.linkDir, "foo.raw")); err != nil {
		t.Fatal(err)
	}
	if runner == nil {
		runner = &sysext.DefaultRunner{}
	}
	f.client = NewClient(ClientConfig{
		SysextRunner: runner,
		Progress:     reporter.NewTextReporter(f.out),
		Paths: RuntimePaths{
			DefinitionRoots:   []string{root},
			OSReleasePaths:    []string{osRelease},
			SysextLinkDir:     f.linkDir,
			SysextHierarchies: []string{t.TempDir()},
			RunExtensionsDir:  t.TempDir(),
			LockPath:          filepath.Join(t.TempDir(), "lock"),
			RollbackDir:       filepath.Join(t.TempDir(), "rollback"),
			LastRunPath:       filepath.Join(t.TempDir(), "last-run.json"),
		},
	})
	return f
}

func (f *otherSuiteFixture) staged(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(f.targetDir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// An update keeps the newest image of the other suite and removes the older
// ones; the dry run reports the same removal. The running suite keeps its
// InstancesMax rule.
func TestUpdateFeatures_VacuumsOtherSuiteImages(t *testing.T) {
	f := newOtherSuiteFixture(t, nil)

	dry, err := f.client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{DryRun: true, NoRefresh: true})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	r := resultsByComponent(t, dry)["foo"]
	wantRemoved := []string{"foo_1.0_13.raw", "foo_2.0_13.raw"}
	if !slices.Equal(r.RemovedOtherSuiteImages, wantRemoved) {
		t.Errorf("dry run RemovedOtherSuiteImages = %v, want %v", r.RemovedOtherSuiteImages, wantRemoved)
	}
	if len(r.RemovedVersions) != 0 {
		t.Errorf("dry run RemovedVersions = %v, want none", r.RemovedVersions)
	}
	if !strings.Contains(r.NextActionMessage, "; would remove other-suite images: [foo_1.0_13.raw foo_2.0_13.raw]") {
		t.Errorf("dry run message = %q", r.NextActionMessage)
	}
	if got := f.staged(t); len(got) != 4 {
		t.Errorf("dry run changed the staging directory: %v", got)
	}

	if _, err := f.client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{NoRefresh: true}); err != nil {
		t.Fatalf("update: %v", err)
	}
	want := []string{"foo_1.0_14.raw", "foo_2.0_14.raw", "foo_3.0_13.raw"}
	if got := f.staged(t); !slices.Equal(got, want) {
		t.Errorf("staged = %v, want %v", got, want)
	}
	if target, _ := os.Readlink(filepath.Join(f.linkDir, "foo.raw")); filepath.Base(target) != "foo_2.0_14.raw" {
		t.Errorf("link = %s, want foo_2.0_14.raw", target)
	}
	if !strings.Contains(f.out.String(), "Removed other-suite image foo_1.0_13.raw of foo") {
		t.Errorf("output does not report the removal:\n%s", f.out.String())
	}
}

// Disable removes the images of every suite.
func TestDisableFeatureNow_RemovesEverySuite(t *testing.T) {
	f := newOtherSuiteFixture(t, &sysext.MockRunner{})

	result, err := f.client.DisableFeature(t.Context(), "grp", DisableFeatureOptions{Now: true, NoRefresh: true})
	if err != nil || !result.Success {
		t.Fatalf("DisableFeature = %+v, %v", result, err)
	}
	if got := f.staged(t); len(got) != 0 {
		t.Errorf("staged after disable = %v, want none", got)
	}
	if len(result.RemovedFiles) != 4 {
		t.Errorf("RemovedFiles = %v, want 4 files", result.RemovedFiles)
	}
}
