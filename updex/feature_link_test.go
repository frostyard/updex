package updex

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/frostyard/std/reporter"
	"github.com/frostyard/updex/v2/config"
	"github.com/frostyard/updex/v2/sysext"
)

// revisionServer serves a SHA256SUMS manifest and the images of the revisions
// published so far for one component. Tests publish a revision with publish,
// and can make every image download fail (a 404, which updex does not retry)
// while the manifest stays readable.
type revisionServer struct {
	*httptest.Server
	component string

	mu        sync.Mutex
	content   map[string][]byte // file name -> content
	failFiles atomic.Bool
	fileGets  atomic.Int32
}

func newRevisionServer(t *testing.T, component string) *revisionServer {
	t.Helper()
	rs := &revisionServer{component: component, content: map[string][]byte{}}
	rs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		rs.mu.Lock()
		defer rs.mu.Unlock()
		if name == "SHA256SUMS" {
			var lines []string
			for file, content := range rs.content {
				lines = append(lines, hashContent(content)+"  "+file)
			}
			_, _ = w.Write([]byte(strings.Join(lines, "\n")))
			return
		}
		rs.fileGets.Add(1)
		content, ok := rs.content[name]
		if !ok || rs.failFiles.Load() {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write(content)
	}))
	t.Cleanup(rs.Close)
	return rs
}

// unpublish withdraws <component>_<version>.raw.
func (rs *revisionServer) unpublish(version string) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	delete(rs.content, rs.component+"_"+version+".raw")
}

// publish makes <component>_<version>.raw available.
func (rs *revisionServer) publish(version string) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.content[rs.component+"_"+version+".raw"] = []byte(rs.component + " image " + version)
}

// featureLinkFixture is one enabled feature whose transfers each have a
// revision server, a shared staging directory, a link directory and a hierarchy.
type featureLinkFixture struct {
	t           *testing.T
	client      *Client
	out         *bytes.Buffer
	servers     map[string]*revisionServer
	components  []string
	targetDir   string
	linkDir     string
	hierarchy   string
	rollbackDir string
	lockPath    string
	defDir      string
}

// newFeatureLinkFixture writes feature "grp" with one transfer per component
// and, for each, stages and links version 1.0.0 (the state before an update).
func newFeatureLinkFixture(t *testing.T, runnerWrap func(sysext.PathSysextRunner) sysext.PathSysextRunner, components ...string) *featureLinkFixture {
	t.Helper()
	var runner sysext.SysextRunner = &sysext.DefaultRunner{}
	if runnerWrap != nil {
		runner = runnerWrap(&sysext.DefaultRunner{})
	}
	return newFeatureLinkFixtureWithRunner(t, runner, components...)
}

// newFeatureLinkFixtureWithRunner is newFeatureLinkFixture with the sysext
// runner chosen by the caller.
func newFeatureLinkFixtureWithRunner(t *testing.T, runner sysext.SysextRunner, components ...string) *featureLinkFixture {
	t.Helper()
	root := t.TempDir()
	defDir := filepath.Join(root, "sysupdate.d")
	if err := os.MkdirAll(defDir, 0o755); err != nil {
		t.Fatal(err)
	}
	f := &featureLinkFixture{
		t:          t,
		out:        &bytes.Buffer{},
		servers:    map[string]*revisionServer{},
		components: components,
		targetDir:  t.TempDir(),
		linkDir:    t.TempDir(),
		hierarchy:  t.TempDir(),
	}
	createFeatureFile(t, defDir, "grp", true)
	for _, component := range components {
		server := newRevisionServer(t, component)
		server.publish("1.0.0")
		server.publish("2.0.0")
		f.servers[component] = server
		createFeatureTransferFileWithoutCurrentSymlink(t, defDir, component, "grp", server.URL, f.targetDir)

		old := filepath.Join(f.targetDir, component+"_1.0.0.raw")
		if err := os.WriteFile(old, []byte(component+" image 1.0.0"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(old, filepath.Join(f.linkDir, component+".raw")); err != nil {
			t.Fatal(err)
		}
	}

	f.rollbackDir = filepath.Join(t.TempDir(), "rollback")
	f.lockPath = filepath.Join(t.TempDir(), "lock")
	f.defDir = defDir
	f.client = NewClient(ClientConfig{
		SysextRunner: runner,
		Progress:     reporter.NewTextReporter(f.out),
		Paths: RuntimePaths{
			DefinitionRoots:   []string{root},
			SysextLinkDir:     f.linkDir,
			SysextHierarchies: []string{f.hierarchy},
			RunExtensionsDir:  t.TempDir(),
			LockPath:          f.lockPath,
			RollbackDir:       f.rollbackDir,
		},
	})
	return f
}

func (f *featureLinkFixture) update() ([]UpdateFeaturesResult, error) {
	f.t.Helper()
	return f.client.UpdateFeatures(f.t.Context(), UpdateFeaturesOptions{NoRefresh: true})
}

func (f *featureLinkFixture) linkTarget(component string) string {
	f.t.Helper()
	target, err := os.Readlink(filepath.Join(f.linkDir, component+".raw"))
	if err != nil {
		f.t.Fatalf("read link of %s: %v", component, err)
	}
	return filepath.Base(target)
}

func (f *featureLinkFixture) staged(component, version string) bool {
	_, err := os.Stat(filepath.Join(f.targetDir, component+"_"+version+".raw"))
	return err == nil
}

func resultsByComponent(t *testing.T, results []UpdateFeaturesResult) map[string]UpdateResult {
	t.Helper()
	if len(results) != 1 {
		t.Fatalf("expected 1 feature result, got %+v", results)
	}
	by := map[string]UpdateResult{}
	for _, r := range results[0].Results {
		by[r.Component] = r
	}
	return by
}

// A feature's transfers link together: when the second download fails, no
// transfer is linked, the first image stays on disk, and a later run with a
// working source links both without downloading the first image again.
func TestUpdateFeatures_TransfersLinkTogether_SecondDownloadFails(t *testing.T) {
	f := newFeatureLinkFixture(t, nil, "alpha", "beta")
	f.servers["beta"].failFiles.Store(true)

	results, err := f.update()
	if err == nil {
		t.Fatal("expected an error: the second transfer failed")
	}
	by := resultsByComponent(t, results)

	if beta := by["beta"]; !strings.Contains(beta.Error, "download failed") || beta.Installed {
		t.Errorf("beta must report its own download failure, got %+v", beta)
	}
	alpha := by["alpha"]
	if !strings.Contains(alpha.Error, "not linked: another transfer of feature grp failed") || alpha.Installed {
		t.Errorf("alpha must report that it was not linked, got %+v", alpha)
	}
	if !alpha.Downloaded || alpha.Version != "2.0.0" {
		t.Errorf("alpha was downloaded and must say so, got %+v", alpha)
	}

	for _, c := range []string{"alpha", "beta"} {
		if got := f.linkTarget(c); got != c+"_1.0.0.raw" {
			t.Errorf("%s link moved to %s; it must keep its old target", c, got)
		}
	}
	if !f.staged("alpha", "2.0.0") {
		t.Error("the downloaded alpha image must stay on disk for the next run")
	}
	if f.staged("beta", "2.0.0") {
		t.Error("beta never downloaded; no image may exist")
	}
	// The old images stay: nothing was linked, so nothing was vacuumed.
	for _, c := range []string{"alpha", "beta"} {
		if !f.staged(c, "1.0.0") {
			t.Errorf("old %s image is gone", c)
		}
	}

	// Next run: beta works. alpha is relinked without a second download.
	f.servers["beta"].failFiles.Store(false)
	alphaGets := f.servers["alpha"].fileGets.Load()

	results, err = f.update()
	if err != nil {
		t.Fatalf("second UpdateFeatures: %v", err)
	}
	by = resultsByComponent(t, results)
	for _, c := range []string{"alpha", "beta"} {
		if by[c].Error != "" || !by[c].Installed || by[c].Version != "2.0.0" {
			t.Errorf("%s must be installed at 2.0.0, got %+v", c, by[c])
		}
		if got := f.linkTarget(c); got != c+"_2.0.0.raw" {
			t.Errorf("%s link = %s, want 2.0.0", c, got)
		}
	}
	if by["alpha"].Downloaded {
		t.Errorf("alpha was staged by the first run and must not download again: %+v", by["alpha"])
	}
	if got := f.servers["alpha"].fileGets.Load(); got != alphaGets {
		t.Errorf("alpha image requested %d more time(s); want 0", got-alphaGets)
	}
	if got := f.servers["beta"].fileGets.Load(); got == 0 {
		t.Error("beta image was never requested")
	}
}

// A failing transfer in one feature does not stop the next feature.
func TestUpdateFeatures_FeatureFailureDoesNotStopOtherFeatures(t *testing.T) {
	root := t.TempDir()
	defDir := filepath.Join(root, "sysupdate.d")
	if err := os.MkdirAll(defDir, 0o755); err != nil {
		t.Fatal(err)
	}
	targetDir, linkDir := t.TempDir(), t.TempDir()
	good, bad := newRevisionServer(t, "good"), newRevisionServer(t, "bad")
	good.publish("1.0.0")
	bad.publish("1.0.0")
	bad.failFiles.Store(true)
	createFeatureFile(t, defDir, "afeature", true)
	createFeatureFile(t, defDir, "bfeature", true)
	createFeatureTransferFileWithoutCurrentSymlink(t, defDir, "bad", "afeature", bad.URL, targetDir)
	createFeatureTransferFileWithoutCurrentSymlink(t, defDir, "good", "bfeature", good.URL, targetDir)

	client := NewClient(ClientConfig{
		SysextRunner: &sysext.DefaultRunner{},
		Paths: RuntimePaths{
			DefinitionRoots:   []string{root},
			SysextLinkDir:     linkDir,
			SysextHierarchies: []string{t.TempDir()},
			LockPath:          filepath.Join(t.TempDir(), "lock"),
		},
	})
	results, err := client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{NoRefresh: true})
	if err == nil {
		t.Fatal("expected an error: afeature failed")
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 feature results, got %+v", results)
	}
	if _, err := os.Lstat(filepath.Join(linkDir, "bad.raw")); !os.IsNotExist(err) {
		t.Errorf("bad must not be linked (err = %v)", err)
	}
	if got, err := os.Readlink(filepath.Join(linkDir, "good.raw")); err != nil || filepath.Base(got) != "good_1.0.0.raw" {
		t.Errorf("good must be linked despite afeature failing: %q, %v", got, err)
	}
}

// failingLinkRunner links through the real runner but fails for one
// component, as a link that cannot be created would.
type failingLinkRunner struct {
	sysext.PathSysextRunner
	failComponent string
}

func (r failingLinkRunner) LinkToSysextAt(t *config.Transfer, dir string) error {
	if t.Component == r.failComponent {
		return errors.New("injected link failure")
	}
	return r.PathSysextRunner.LinkToSysextAt(t, dir)
}

// A link that fails part-way restores the links this run already moved, and
// no transfer reports success.
func TestUpdateFeatures_LinkFailurePartWayRestoresLinks(t *testing.T) {
	f := newFeatureLinkFixture(t, func(r sysext.PathSysextRunner) sysext.PathSysextRunner {
		return failingLinkRunner{PathSysextRunner: r, failComponent: "beta"}
	}, "alpha", "beta")

	results, err := f.update()
	if err == nil {
		t.Fatal("expected an error: the second link failed")
	}
	by := resultsByComponent(t, results)

	if beta := by["beta"]; !strings.Contains(beta.Error, "injected link failure") || beta.Installed {
		t.Errorf("beta must report its link failure, got %+v", beta)
	}
	if alpha := by["alpha"]; !strings.Contains(alpha.Error, "not linked") || alpha.Installed {
		t.Errorf("alpha was moved and restored; it must report not linked, got %+v", alpha)
	}
	for _, c := range []string{"alpha", "beta"} {
		if got := f.linkTarget(c); got != c+"_1.0.0.raw" {
			t.Errorf("%s link = %s; the old link must be restored", c, got)
		}
		if !f.staged(c, "1.0.0") || !f.staged(c, "2.0.0") {
			t.Errorf("both %s images must stay on disk after a failed link", c)
		}
	}
	// No temp symlink is left behind.
	entries, err := os.ReadDir(f.linkDir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if want := []string{"alpha.raw", "beta.raw"}; !slices.Equal(names, want) {
		t.Errorf("link dir = %v, want %v", names, want)
	}
}

// A link that fails part-way on a first install (no old link) removes the
// link the run already created.
func TestUpdateFeatures_LinkFailurePartWayRemovesNewLinks(t *testing.T) {
	f := newFeatureLinkFixture(t, func(r sysext.PathSysextRunner) sysext.PathSysextRunner {
		return failingLinkRunner{PathSysextRunner: r, failComponent: "beta"}
	}, "alpha", "beta")
	for _, c := range []string{"alpha", "beta"} {
		if err := os.Remove(filepath.Join(f.linkDir, c+".raw")); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := f.update(); err == nil {
		t.Fatal("expected an error: the second link failed")
	}
	if _, err := os.Lstat(filepath.Join(f.linkDir, "alpha.raw")); !os.IsNotExist(err) {
		t.Errorf("alpha link must be removed again (err = %v)", err)
	}
}

// EnableFeature --now stages every transfer first and links none when one
// fails.
func TestEnableFeatureNow_TransfersLinkTogether(t *testing.T) {
	f := newFeatureLinkFixture(t, nil, "alpha", "beta")
	// Start from a disabled feature with no links.
	for _, c := range []string{"alpha", "beta"} {
		if err := os.Remove(filepath.Join(f.linkDir, c+".raw")); err != nil {
			t.Fatal(err)
		}
	}
	f.servers["beta"].failFiles.Store(true)

	result, err := f.client.EnableFeature(t.Context(), "grp", EnableFeatureOptions{Now: true, NoRefresh: true})
	if err == nil || !strings.Contains(err.Error(), "failed to download beta") {
		t.Fatalf("EnableFeature error = %v, want a beta download failure", err)
	}
	if result == nil || result.Success {
		t.Errorf("result = %+v, want a failure", result)
	}
	for _, c := range []string{"alpha", "beta"} {
		if _, err := os.Lstat(filepath.Join(f.linkDir, c+".raw")); !os.IsNotExist(err) {
			t.Errorf("%s must not be linked (err = %v)", c, err)
		}
	}
	if !f.staged("alpha", "2.0.0") {
		t.Error("the downloaded alpha image must stay on disk")
	}

	f.servers["beta"].failFiles.Store(false)
	result, err = f.client.EnableFeature(t.Context(), "grp", EnableFeatureOptions{Now: true, NoRefresh: true})
	if err != nil || !result.Success {
		t.Fatalf("second EnableFeature = %+v, %v", result, err)
	}
	for _, c := range []string{"alpha", "beta"} {
		if got := f.linkTarget(c); got != c+"_2.0.0.raw" {
			t.Errorf("%s link = %s, want 2.0.0", c, got)
		}
	}
}

// A host with 3 published revisions and no reboot keeps the image it runs
// (merged) and the image it will run next (linked).
func TestUpdateFeatures_KeepsMergedAndLinkedImages(t *testing.T) {
	f := newFeatureLinkFixture(t, nil, "alpha")
	server := f.servers["alpha"]
	// The host booted with 1.0.0: it is merged, staged and linked.
	origin := `{"extensions":{"alpha":{"path":"` + filepath.Join(f.targetDir, "alpha_1.0.0.raw") + `"}}}`
	writeMergedState(t, f.hierarchy, origin)

	check := func(step string, wantLinked string, wantStaged ...string) {
		t.Helper()
		if got := f.linkTarget("alpha"); got != "alpha_"+wantLinked+".raw" {
			t.Errorf("%s: link = %s, want %s", step, got, wantLinked)
		}
		var staged []string
		for _, v := range []string{"1.0.0", "2.0.0", "3.0.0", "4.0.0"} {
			if f.staged("alpha", v) {
				staged = append(staged, v)
			}
		}
		if !slices.Equal(staged, wantStaged) {
			t.Errorf("%s: staged = %v, want %v", step, staged, wantStaged)
		}
	}

	// Revision 2: still within InstancesMax.
	if _, err := f.update(); err != nil {
		t.Fatalf("update to 2.0.0: %v", err)
	}
	check("2.0.0", "2.0.0", "1.0.0", "2.0.0")

	// Revision 3, no reboot: 3.0.0 linked, 2.0.0 newest-but-one, 1.0.0 merged.
	server.publish("3.0.0")
	if _, err := f.update(); err != nil {
		t.Fatalf("update to 3.0.0: %v", err)
	}
	check("3.0.0", "3.0.0", "1.0.0", "2.0.0", "3.0.0")

	// Revision 4, still no reboot. The dry run plans the same removal.
	server.publish("4.0.0")
	dry, err := f.client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{DryRun: true})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if got := dry[0].Results[0].RemovedVersions; !slices.Equal(got, []string{"2.0.0"}) {
		t.Errorf("dry run would remove %v, want [2.0.0] (1.0.0 is merged)", got)
	}
	check("after dry run", "3.0.0", "1.0.0", "2.0.0", "3.0.0")

	if _, err := f.update(); err != nil {
		t.Fatalf("update to 4.0.0: %v", err)
	}
	check("4.0.0", "4.0.0", "1.0.0", "3.0.0", "4.0.0")

	// After a reboot the merged image is 4.0.0 and 1.0.0 is no longer kept.
	writeMergedState(t, f.hierarchy, `{"extensions":{"alpha":{"path":"`+filepath.Join(f.targetDir, "alpha_4.0.0.raw")+`"}}}`)
	server.publish("5.0.0")
	if _, err := f.update(); err != nil {
		t.Fatalf("update to 5.0.0: %v", err)
	}
	if f.staged("alpha", "1.0.0") || f.staged("alpha", "3.0.0") {
		t.Error("1.0.0 and 3.0.0 must be removed once nothing needs them")
	}
	if !f.staged("alpha", "4.0.0") || !f.staged("alpha", "5.0.0") {
		t.Error("the running 4.0.0 and the linked 5.0.0 must stay")
	}
}

// A vacuum that cannot read the merged state deletes nothing and the update
// still succeeds.
func TestUpdateFeatures_VacuumFailsClosedOnUnreadableMergedState(t *testing.T) {
	f := newFeatureLinkFixture(t, nil, "alpha")
	f.servers["alpha"].publish("3.0.0")
	// A directory where the origin file belongs cannot be read.
	if err := os.MkdirAll(filepath.Join(f.hierarchy, sysext.MergedStateDirName, "origin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.targetDir, "alpha_0.5.0.raw"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := f.update(); err != nil {
		t.Fatalf("UpdateFeatures: %v", err)
	}
	if got := f.linkTarget("alpha"); got != "alpha_3.0.0.raw" {
		t.Errorf("link = %s, want 3.0.0", got)
	}
	for _, v := range []string{"0.5.0", "1.0.0", "3.0.0"} {
		if !f.staged("alpha", v) {
			t.Errorf("image %s was deleted although the merged state was unreadable", v)
		}
	}
	if !strings.Contains(f.out.String(), "vacuum failed for alpha") {
		t.Errorf("the vacuum failure must be reported; output:\n%s", f.out.String())
	}
}

func writeMergedState(t *testing.T, hierarchy, origin string) {
	t.Helper()
	dir := filepath.Join(hierarchy, sysext.MergedStateDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "origin"), []byte(origin), 0o644); err != nil {
		t.Fatal(err)
	}
}
