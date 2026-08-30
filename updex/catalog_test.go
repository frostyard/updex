package updex

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"

	"github.com/frostyard/updex/catalog"
	"github.com/frostyard/updex/config"
	"github.com/frostyard/updex/internal/testutil"
	"github.com/frostyard/updex/sysext"
)

// recordingReporter is a minimal reporter.Reporter that only captures
// Warning calls, for asserting a specific warning was surfaced.
type recordingReporter struct {
	warnings []string
}

func (r *recordingReporter) Step(int, int, string)       {}
func (r *recordingReporter) Progress(int, string)        {}
func (r *recordingReporter) Message(string, ...any)      {}
func (r *recordingReporter) MessagePlain(string, ...any) {}
func (r *recordingReporter) Warning(format string, args ...any) {
	r.warnings = append(r.warnings, fmt.Sprintf(format, args...))
}
func (r *recordingReporter) Error(error, string)  {}
func (r *recordingReporter) Complete(string, any) {}
func (r *recordingReporter) IsJSON() bool         { return false }

func (r *recordingReporter) hasWarningContaining(substr string) bool {
	for _, w := range r.warnings {
		if strings.Contains(w, substr) {
			return true
		}
	}
	return false
}

type catalogPathRunner struct {
	refreshErr error
	linkErr    error
	onRefresh  func()
}

func (r *catalogPathRunner) Refresh() error {
	if r.onRefresh != nil {
		r.onRefresh()
	}
	return r.refreshErr
}

func (*catalogPathRunner) Merge() error   { return nil }
func (*catalogPathRunner) Unmerge() error { return nil }

func (r *catalogPathRunner) LinkToSysext(transfer *config.Transfer) error {
	if r.linkErr != nil {
		return r.linkErr
	}
	return sysext.LinkToSysextAt(transfer, sysext.SysextDir)
}

func (r *catalogPathRunner) LinkToSysextAt(transfer *config.Transfer, sysextDir string) error {
	if r.linkErr != nil {
		return r.linkErr
	}
	return sysext.LinkToSysextAt(transfer, sysextDir)
}

// withCatalogConfigRoots points catalog.ConfigRoots at a fresh temp
// directory and returns it. It also redirects catalog.CacheDir to a temp
// directory so no test ever reads or writes the real user cache.
func withCatalogConfigRoots(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	original := catalog.ConfigRoots
	catalog.ConfigRoots = []string{root}
	originalCache := catalog.CacheDir
	catalog.CacheDir = t.TempDir()
	t.Cleanup(func() {
		catalog.ConfigRoots = original
		catalog.CacheDir = originalCache
	})
	return root
}

func writeCatalogRepo(t *testing.T, dir, name, siteURL, listURL string) {
	t.Helper()
	content := "[Catalog]\nSiteURL=" + siteURL + "\n"
	if listURL != "" {
		content += "ListURL=" + listURL + "\n"
	}
	if strings.HasPrefix(siteURL, "http://") || strings.HasPrefix(listURL, "http://") {
		content += "AllowInsecure=yes\n"
	}
	writeCatalogFileContent(t, dir, name, content)
}

func writeCatalogFileContent(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name+".catalog"), []byte(content), 0644); err != nil {
		t.Fatalf("failed to write catalog file: %v", err)
	}
}

// writeGeneratedFeature writes a .feature carrying repo's generated
// marker, i.e. what CatalogAdd would leave behind.
func writeGeneratedFeature(t *testing.T, dir, repoName, name string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	repo := catalog.Repo{Name: repoName, SiteURL: "https://example.com/" + repoName}
	if err := os.WriteFile(filepath.Join(dir, name+".feature"), catalog.RenderFeature(repo, name), 0644); err != nil {
		t.Fatalf("failed to write generated feature: %v", err)
	}
}

// writeEnableDropIn writes the standard 00-updex.conf enable drop-in.
func writeEnableDropIn(t *testing.T, dir, name string, enabled bool) {
	t.Helper()
	dropInDir := filepath.Join(dir, name+".feature.d")
	if err := os.MkdirAll(dropInDir, 0755); err != nil {
		t.Fatal(err)
	}
	content := fmt.Sprintf("[Feature]\nEnabled=%v\n", enabled)
	if err := os.WriteFile(filepath.Join(dropInDir, updexDropInName), []byte(content), 0644); err != nil {
		t.Fatalf("failed to write drop-in: %v", err)
	}
}

// newCatalogServer serves a catalog repo with a single sysext: its .conf,
// SHA256SUMS manifest, and one .raw image, all under /<name>/. The conf's
// transfer downloads into targetDir.
func newCatalogServer(t *testing.T, name, version, targetDir string) *httptest.Server {
	return newCatalogServerWithTargetPattern(t, name, version, targetDir, name+"-@v.raw")
}

func newCatalogServerWithTargetPattern(t *testing.T, name, version, targetDir, targetPattern string) *httptest.Server {
	t.Helper()

	originalTargetPath := catalog.TargetPath
	catalog.TargetPath = targetDir
	t.Cleanup(func() { catalog.TargetPath = originalTargetPath })

	rawName := fmt.Sprintf("%s-%s.raw", name, version)
	rawContent := []byte("fake sysext image for " + name)
	manifestContent := []byte(fmt.Sprintf("%s  %s\n", hashContent(rawContent), rawName))
	manifestSig := testutil.SignManifest(t, manifestContent)

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/" + name + "/" + name + ".conf":
			conf := fmt.Sprintf(`[Transfer]
Verify=false

[Source]
Type=url-file
Path=%s/%s/
MatchPattern=%s-@v.raw

[Target]
InstancesMax=2
Type=regular-file
Path=%s
MatchPattern=%s
CurrentSymlink=/var/lib/extensions/%s.raw
`, server.URL, name, name, targetDir, targetPattern, name)
			_, _ = w.Write([]byte(conf))
		case "/" + name + "/SHA256SUMS":
			_, _ = w.Write(manifestContent)
		case "/" + name + "/SHA256SUMS.gpg":
			_, _ = w.Write(manifestSig)
		case "/" + name + "/" + rawName:
			_, _ = w.Write(rawContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestCatalogAdd_EndToEnd(t *testing.T) {
	roots := withComponentSearchRoots(t)
	catalogRoot := withCatalogConfigRoots(t)
	targetDir := t.TempDir()

	sysextDir := t.TempDir()
	origSysextDir := sysext.SysextDir
	sysext.SysextDir = sysextDir
	t.Cleanup(func() { sysext.SysextDir = origSysextDir })

	server := newCatalogServer(t, "zoxide", "1.0.0", targetDir)
	writeCatalogRepo(t, catalogRoot, "fedora", server.URL, "")

	mockRunner := &sysext.MockRunner{}
	client := NewClient(ClientConfig{SysextRunner: mockRunner})

	result, err := client.CatalogAdd(t.Context(), "zoxide", CatalogAddOptions{})
	if err != nil {
		t.Fatalf("CatalogAdd failed: %v", err)
	}

	if result.Repo != "fedora" || result.Component != "catalog-fedora" {
		t.Errorf("unexpected result repo/component: %s/%s", result.Repo, result.Component)
	}

	componentDir := filepath.Join(roots[0], "sysupdate.catalog-fedora.d")
	transferData, err := os.ReadFile(filepath.Join(componentDir, "zoxide.transfer"))
	if err != nil {
		t.Fatalf("transfer file not written: %v", err)
	}
	if !strings.Contains(string(transferData), "Features=zoxide") {
		t.Errorf("transfer file missing Features line:\n%s", transferData)
	}
	if strings.Contains(string(transferData), "CurrentSymlink") {
		t.Errorf("transfer file kept CurrentSymlink:\n%s", transferData)
	}
	if _, err := os.Stat(filepath.Join(componentDir, "zoxide.feature")); err != nil {
		t.Fatalf("feature file not written: %v", err)
	}

	// Enable ran with Now: the image must be downloaded and the feature
	// enabled via the standard drop-in.
	if result.Enable == nil || !result.Enable.Success {
		t.Fatalf("expected successful enable, got %+v", result.Enable)
	}
	if _, err := os.Stat(filepath.Join(targetDir, "zoxide-1.0.0.raw")); err != nil {
		t.Errorf("image not downloaded: %v", err)
	}
	dropIn := filepath.Join(componentDir, "zoxide.feature.d", "00-updex.conf")
	if data, err := os.ReadFile(dropIn); err != nil || !strings.Contains(string(data), "Enabled=true") {
		t.Errorf("enable drop-in missing or wrong (%v):\n%s", err, data)
	}

	// The added sysext is now managed by the standard feature machinery.
	features, err := client.Features(t.Context())
	if err != nil {
		t.Fatalf("Features failed: %v", err)
	}
	found := false
	for _, f := range features {
		if f.Name == "zoxide" {
			found = true
			if !f.Enabled {
				t.Error("expected zoxide feature to be enabled")
			}
			if len(f.Transfers) != 1 || f.Transfers[0] != "zoxide" {
				t.Errorf("expected zoxide transfer bound to feature, got %v", f.Transfers)
			}
		}
	}
	if !found {
		t.Error("added feature not visible in the default domain")
	}
}

func TestCatalogAdd_DryRun(t *testing.T) {
	roots := withComponentSearchRoots(t)
	catalogRoot := withCatalogConfigRoots(t)
	targetDir := t.TempDir()

	server := newCatalogServer(t, "zoxide", "1.0.0", targetDir)
	writeCatalogRepo(t, catalogRoot, "fedora", server.URL, "")

	client := NewClient(ClientConfig{SysextRunner: &sysext.MockRunner{}})

	result, err := client.CatalogAdd(t.Context(), "zoxide", CatalogAddOptions{DryRun: true})
	if err != nil {
		t.Fatalf("CatalogAdd dry-run failed: %v", err)
	}
	if !result.DryRun {
		t.Error("expected DryRun=true in result")
	}
	if _, err := os.Stat(filepath.Join(roots[0], "sysupdate.catalog-fedora.d")); !os.IsNotExist(err) {
		t.Error("dry-run must not create the component directory")
	}
}

func TestCatalogAdd_AmbiguousAndNotFound(t *testing.T) {
	withComponentSearchRoots(t)
	catalogRoot := withCatalogConfigRoots(t)
	targetDir := t.TempDir()

	server1 := newCatalogServer(t, "zoxide", "1.0.0", targetDir)
	server2 := newCatalogServer(t, "zoxide", "1.0.0", targetDir)
	writeCatalogRepo(t, catalogRoot, "fedora", server1.URL, "")
	writeCatalogRepo(t, catalogRoot, "community", server2.URL, "")

	client := NewClient(ClientConfig{SysextRunner: &sysext.MockRunner{}})

	_, err := client.CatalogAdd(t.Context(), "zoxide", CatalogAddOptions{})
	if err == nil || !strings.Contains(err.Error(), "multiple catalogs") {
		t.Fatalf("expected ambiguity error, got %v", err)
	}
	if !strings.Contains(err.Error(), "fedora/zoxide") || !strings.Contains(err.Error(), "community/zoxide") {
		t.Errorf("ambiguity error should name candidates: %v", err)
	}

	// Explicit repo resolves the ambiguity.
	result, err := client.CatalogAdd(t.Context(), "zoxide", CatalogAddOptions{Repo: "fedora", DryRun: true})
	if err != nil {
		t.Fatalf("explicit repo failed: %v", err)
	}
	if result.Repo != "fedora" {
		t.Errorf("expected repo fedora, got %s", result.Repo)
	}

	if _, err := client.CatalogAdd(t.Context(), "missing", CatalogAddOptions{}); err == nil ||
		!strings.Contains(err.Error(), "not found in any configured catalog") {
		t.Fatalf("expected not-found error, got %v", err)
	}

	if _, err := client.CatalogAdd(t.Context(), "zoxide", CatalogAddOptions{Repo: "nope"}); err == nil ||
		!strings.Contains(err.Error(), "unknown catalog") {
		t.Fatalf("expected unknown-catalog error, got %v", err)
	}
}

func TestCatalogAdd_NoCatalogsConfigured(t *testing.T) {
	withComponentSearchRoots(t)
	withCatalogConfigRoots(t)

	client := NewClient(ClientConfig{SysextRunner: &sysext.MockRunner{}})

	_, err := client.CatalogAdd(t.Context(), "zoxide", CatalogAddOptions{})
	if err == nil || !strings.Contains(err.Error(), "no catalogs configured") {
		t.Fatalf("expected setup guidance error, got %v", err)
	}
}

func TestCatalogAdd_DefinitionsIncompatible(t *testing.T) {
	withCatalogConfigRoots(t)

	client := NewClient(ClientConfig{Definitions: t.TempDir(), SysextRunner: &sysext.MockRunner{}})

	_, err := client.CatalogAdd(t.Context(), "zoxide", CatalogAddOptions{})
	if err == nil || !strings.Contains(err.Error(), "definitions override") {
		t.Fatalf("expected definitions-incompatible error, got %v", err)
	}
}

func TestCatalogRemove_FullCleanup(t *testing.T) {
	roots := withComponentSearchRoots(t)
	catalogRoot := withCatalogConfigRoots(t)
	targetDir := t.TempDir()

	sysextDir := t.TempDir()
	origSysextDir := sysext.SysextDir
	sysext.SysextDir = sysextDir
	t.Cleanup(func() { sysext.SysextDir = origSysextDir })

	server := newCatalogServer(t, "zoxide", "1.0.0", targetDir)
	writeCatalogRepo(t, catalogRoot, "fedora", server.URL, "")

	mockRunner := &sysext.MockRunner{}
	client := NewClient(ClientConfig{SysextRunner: mockRunner})

	if _, err := client.CatalogAdd(t.Context(), "zoxide", CatalogAddOptions{}); err != nil {
		t.Fatalf("CatalogAdd failed: %v", err)
	}

	result, err := client.CatalogRemove(t.Context(), "zoxide", CatalogRemoveOptions{})
	if err != nil {
		t.Fatalf("CatalogRemove failed: %v", err)
	}

	if result.Disable == nil || !result.Disable.Success {
		t.Fatalf("expected successful disable, got %+v", result.Disable)
	}
	if !mockRunner.UnmergeCalled {
		t.Error("expected unmerge to be called")
	}
	if _, err := os.Stat(filepath.Join(targetDir, "zoxide-1.0.0.raw")); !os.IsNotExist(err) {
		t.Error("downloaded image should be removed")
	}
	componentDir := filepath.Join(roots[0], "sysupdate.catalog-fedora.d")
	if _, err := os.Stat(componentDir); !os.IsNotExist(err) {
		t.Error("empty component directory should be removed")
	}
	if len(result.RemovedFiles) == 0 {
		t.Error("expected RemovedFiles to list deleted definition files")
	}

	// Removing again is an error: nothing is catalog-managed anymore.
	if _, err := client.CatalogRemove(t.Context(), "zoxide", CatalogRemoveOptions{}); err == nil ||
		!strings.Contains(err.Error(), "not a catalog-managed sysext") {
		t.Fatalf("expected not-catalog-managed error, got %v", err)
	}
}

func TestCatalogAdd_InvalidName(t *testing.T) {
	withCatalogConfigRoots(t)
	client := NewClient(ClientConfig{SysextRunner: &sysext.MockRunner{}})

	for _, name := range []string{"../evil", ".hidden", "a/b", ""} {
		if _, err := client.CatalogAdd(t.Context(), name, CatalogAddOptions{}); err == nil ||
			!strings.Contains(err.Error(), "invalid sysext name") {
			t.Errorf("CatalogAdd(%q): expected invalid-name error, got %v", name, err)
		}
		if _, err := client.CatalogRemove(t.Context(), name, CatalogRemoveOptions{}); err == nil ||
			!strings.Contains(err.Error(), "invalid sysext name") {
			t.Errorf("CatalogRemove(%q): expected invalid-name error, got %v", name, err)
		}
	}
}

func TestCatalogAdd_RefusesOverwriteUnmanaged(t *testing.T) {
	roots := withComponentSearchRoots(t)
	catalogRoot := withCatalogConfigRoots(t)
	targetDir := t.TempDir()

	server := newCatalogServer(t, "zoxide", "1.0.0", targetDir)
	writeCatalogRepo(t, catalogRoot, "fedora", server.URL, "")

	// A hand-written feature already lives at the target path (e.g. a
	// Component= override pointing at an existing component).
	componentDir := filepath.Join(roots[0], "sysupdate.catalog-fedora.d")
	writeComponentFeature(t, componentDir, "zoxide", true)

	client := NewClient(ClientConfig{SysextRunner: &sysext.MockRunner{}})

	_, err := client.CatalogAdd(t.Context(), "zoxide", CatalogAddOptions{})
	if err == nil || !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Fatalf("expected refusing-to-overwrite error, got %v", err)
	}

	// The hand-written file is untouched.
	data, readErr := os.ReadFile(filepath.Join(componentDir, "zoxide.feature"))
	if readErr != nil || !strings.Contains(string(data), "Enabled=true") {
		t.Errorf("hand-written feature file modified (%v):\n%s", readErr, data)
	}
}

func TestCatalogAdd_ReAddOwnedFiles(t *testing.T) {
	withComponentSearchRoots(t)
	catalogRoot := withCatalogConfigRoots(t)
	targetDir := t.TempDir()

	sysextDir := t.TempDir()
	origSysextDir := sysext.SysextDir
	sysext.SysextDir = sysextDir
	t.Cleanup(func() { sysext.SysextDir = origSysextDir })

	server := newCatalogServer(t, "zoxide", "1.0.0", targetDir)
	writeCatalogRepo(t, catalogRoot, "fedora", server.URL, "")

	client := NewClient(ClientConfig{SysextRunner: &sysext.MockRunner{}})

	if _, err := client.CatalogAdd(t.Context(), "zoxide", CatalogAddOptions{}); err != nil {
		t.Fatalf("first CatalogAdd failed: %v", err)
	}
	// Re-adding catalog-owned files is allowed (refresh/update case).
	if _, err := client.CatalogAdd(t.Context(), "zoxide", CatalogAddOptions{}); err != nil {
		t.Fatalf("re-add of catalog-owned sysext failed: %v", err)
	}
}

func TestCatalogAdd_RollbackOnFailure(t *testing.T) {
	roots := withComponentSearchRoots(t)
	catalogRoot := withCatalogConfigRoots(t)
	targetDir := t.TempDir()

	// Server publishes the conf but no SHA256SUMS: the enable-with-download
	// step fails after the definitions were written.
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/zoxide/zoxide.conf" {
			_, _ = fmt.Fprintf(w, `[Transfer]
Verify=false

[Source]
Type=url-file
Path=%s/zoxide/
MatchPattern=zoxide-@v.raw

[Target]
Type=regular-file
Path=%s
MatchPattern=zoxide-@v.raw
`, server.URL, targetDir)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	writeCatalogRepo(t, catalogRoot, "fedora", server.URL, "")

	client := NewClient(ClientConfig{SysextRunner: &sysext.MockRunner{}})

	_, err := client.CatalogAdd(t.Context(), "zoxide", CatalogAddOptions{})
	if err == nil {
		t.Fatal("expected CatalogAdd to fail when the download fails")
	}

	// The failed fresh add must leave no persistent state behind.
	componentDir := filepath.Join(roots[0], "sysupdate.catalog-fedora.d")
	if _, err := os.Stat(componentDir); !os.IsNotExist(err) {
		entries, _ := os.ReadDir(componentDir)
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("component dir not rolled back; remaining entries: %v", names)
	}
}

// TestCatalogAdd_RefusesOtherRepoFiles verifies that two catalogs sharing
// a Component cannot overwrite each other's same-named sysext.
func TestCatalogAdd_RefusesOtherRepoFiles(t *testing.T) {
	roots := withComponentSearchRoots(t)
	catalogRoot := withCatalogConfigRoots(t)
	targetDir := t.TempDir()

	sysextDir := t.TempDir()
	origSysextDir := sysext.SysextDir
	sysext.SysextDir = sysextDir
	t.Cleanup(func() { sysext.SysextDir = origSysextDir })

	serverA := newCatalogServer(t, "zoxide", "1.0.0", targetDir)
	serverB := newCatalogServer(t, "zoxide", "2.0.0", targetDir)
	// Both repos deliberately share one component.
	writeCatalogFileContent(t, catalogRoot, "alpha",
		"[Catalog]\nSiteURL="+serverA.URL+"\nComponent=shared\nAllowInsecure=yes\n")
	writeCatalogFileContent(t, catalogRoot, "beta",
		"[Catalog]\nSiteURL="+serverB.URL+"\nComponent=shared\nAllowInsecure=yes\n")

	client := NewClient(ClientConfig{SysextRunner: &sysext.MockRunner{}})

	if _, err := client.CatalogAdd(t.Context(), "zoxide", CatalogAddOptions{Repo: "alpha"}); err != nil {
		t.Fatalf("first add failed: %v", err)
	}

	_, err := client.CatalogAdd(t.Context(), "zoxide", CatalogAddOptions{Repo: "beta"})
	if err == nil || !strings.Contains(err.Error(), `generated by catalog "alpha"`) {
		t.Fatalf("expected cross-repo overwrite refusal, got %v", err)
	}

	// alpha's definitions are intact.
	componentDir := filepath.Join(roots[0], "sysupdate.shared.d")
	data, readErr := os.ReadFile(filepath.Join(componentDir, "zoxide.feature"))
	if readErr != nil || !strings.Contains(string(data), "repo: alpha") {
		t.Errorf("alpha's feature file was modified (%v):\n%s", readErr, data)
	}

	// beta also cannot remove alpha's sysext.
	if _, err := client.CatalogRemove(t.Context(), "zoxide", CatalogRemoveOptions{Repo: "beta"}); err == nil ||
		!strings.Contains(err.Error(), "not a catalog-managed sysext") {
		t.Fatalf("expected cross-repo remove refusal, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(componentDir, "zoxide.feature")); err != nil {
		t.Errorf("alpha's feature file was removed by beta: %v", err)
	}
}

// TestCatalogAdd_FailedReAddRestoresPrevious verifies that a re-add whose
// download fails leaves the previously working definitions in place rather
// than the broken replacements.
func TestCatalogAdd_FailedReAddRestoresPrevious(t *testing.T) {
	roots := withComponentSearchRoots(t)
	catalogRoot := withCatalogConfigRoots(t)
	targetDir := t.TempDir()
	originalTargetPath := catalog.TargetPath
	catalog.TargetPath = targetDir
	t.Cleanup(func() { catalog.TargetPath = originalTargetPath })

	sysextDir := t.TempDir()
	origSysextDir := sysext.SysextDir
	sysext.SysextDir = sysextDir
	t.Cleanup(func() { sysext.SysextDir = origSysextDir })

	// Serves a working sysext first; breakManifest makes later downloads fail.
	var breakManifest atomic.Bool
	rawContent := []byte("fake sysext image for zoxide")
	manifestContent := []byte(fmt.Sprintf("%s  zoxide-1.0.0.raw\n", hashContent(rawContent)))
	manifestSig := testutil.SignManifest(t, manifestContent)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/zoxide/zoxide.conf":
			_, _ = fmt.Fprintf(w, `[Transfer]
Verify=false

[Source]
Type=url-file
Path=%s/zoxide/
MatchPattern=zoxide-@v.raw

[Target]
Type=regular-file
Path=%s
MatchPattern=zoxide-@v.raw
`, server.URL, targetDir)
		case "/zoxide/SHA256SUMS":
			if breakManifest.Load() {
				// 404 rather than 5xx: not retried, so the test fails fast.
				http.NotFound(w, r)
				return
			}

			_, _ = w.Write(manifestContent)
		case "/zoxide/SHA256SUMS.gpg":
			_, _ = w.Write(manifestSig)
		case "/zoxide/zoxide-1.0.0.raw":
			_, _ = w.Write(rawContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	writeCatalogRepo(t, catalogRoot, "fedora", server.URL, "")
	client := NewClient(ClientConfig{SysextRunner: &sysext.MockRunner{}})

	if _, err := client.CatalogAdd(t.Context(), "zoxide", CatalogAddOptions{}); err != nil {
		t.Fatalf("initial add failed: %v", err)
	}

	componentDir := filepath.Join(roots[0], "sysupdate.catalog-fedora.d")
	transferPath := filepath.Join(componentDir, "zoxide.transfer")
	dropInPath := filepath.Join(componentDir, "zoxide.feature.d", updexDropInName)
	before, err := os.ReadFile(transferPath)
	if err != nil {
		t.Fatal(err)
	}
	dropInBefore, err := os.ReadFile(dropInPath)
	if err != nil {
		t.Fatal(err)
	}

	// Re-add against a now-broken catalog.
	breakManifest.Store(true)
	if _, err := client.CatalogAdd(t.Context(), "zoxide", CatalogAddOptions{}); err == nil {
		t.Fatal("expected re-add to fail")
	}

	after, err := os.ReadFile(transferPath)
	if err != nil {
		t.Fatalf("previous transfer file not restored: %v", err)
	}
	if string(after) != string(before) {
		t.Errorf("transfer file not restored:\ngot:\n%s\nwant:\n%s", after, before)
	}
	dropInAfter, err := os.ReadFile(dropInPath)
	if err != nil {
		t.Fatalf("previous drop-in not restored: %v", err)
	}
	if string(dropInAfter) != string(dropInBefore) {
		t.Errorf("drop-in not restored:\ngot:\n%s\nwant:\n%s", dropInAfter, dropInBefore)
	}
}

// TestCatalogAdd_WriteFailureRestoresPrevious verifies that a failure
// while writing the definitions — not just during enable/download —
// restores the previous working files. os.WriteFile truncates on open, so
// without rollback a re-add could leave a working transfer destroyed.
func TestCatalogAdd_RefreshFailureRollsBackManagedInstallState(t *testing.T) {
	const name = "zoxide"
	refreshErr := errors.New("injected refresh failure")

	t.Run("fresh add removes staged image and link", func(t *testing.T) {
		definitionRoot := t.TempDir()
		catalogRoot := t.TempDir()
		targetDir := t.TempDir()
		linkDir := t.TempDir()
		unrelatedImage := filepath.Join(targetDir, "operator-owned.raw")
		unrelatedLink := filepath.Join(linkDir, "operator-owned.raw")
		if err := os.WriteFile(unrelatedImage, []byte("operator image"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(unrelatedLink, []byte("operator link entry"), 0644); err != nil {
			t.Fatal(err)
		}

		server := newCatalogServer(t, name, "1.0.0", targetDir)
		writeCatalogRepo(t, catalogRoot, "fedora", server.URL, "")
		client := NewClient(ClientConfig{
			Paths: RuntimePaths{
				DefinitionRoots:    []string{definitionRoot},
				CatalogConfigRoots: []string{catalogRoot},
				CatalogCacheDir:    DisableCatalogCache,
				CatalogTargetPath:  targetDir,
				SysextLinkDir:      linkDir,
				RunExtensionsDir:   t.TempDir(),
			},
			SysextRunner: &catalogPathRunner{refreshErr: refreshErr},
		})

		_, err := client.CatalogAdd(t.Context(), name, CatalogAddOptions{})
		if !errors.Is(err, refreshErr) {
			t.Fatalf("CatalogAdd error = %v, want injected refresh failure", err)
		}

		componentDir := filepath.Join(definitionRoot, "sysupdate.catalog-fedora.d")
		if _, err := os.Stat(componentDir); !os.IsNotExist(err) {
			t.Errorf("fresh add left generated definitions at %s: %v", componentDir, err)
		}
		if _, err := os.Stat(filepath.Join(targetDir, name+"-1.0.0.raw")); !os.IsNotExist(err) {
			t.Errorf("fresh add left staged image: %v", err)
		}
		if _, err := os.Lstat(filepath.Join(linkDir, name+".raw")); !os.IsNotExist(err) {
			t.Errorf("fresh add left sysext link: %v", err)
		}
		assertFileContent(t, unrelatedImage, "operator image")
		assertFileContent(t, unrelatedLink, "operator link entry")
	})

	t.Run("re-add restores previous files and link target", func(t *testing.T) {
		definitionRoot := t.TempDir()
		catalogRoot := t.TempDir()
		targetDir := t.TempDir()
		linkDir := t.TempDir()
		var publishV2 atomic.Bool

		rawV1 := []byte("catalog image v1")
		rawV2 := []byte("catalog image v2")
		manifestV1 := []byte(fmt.Sprintf("%s  %s-1.0.0.raw\n", hashContent(rawV1), name))
		manifestV2 := []byte(fmt.Sprintf("%s  %s-2.0.0.raw\n", hashContent(rawV2), name))
		sigV1 := testutil.SignManifest(t, manifestV1)
		sigV2 := testutil.SignManifest(t, manifestV2)
		var server *httptest.Server
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			version := "1.0.0"
			raw := rawV1
			manifestContent := manifestV1
			manifestSig := sigV1
			minVersion := ""
			if publishV2.Load() {
				version = "2.0.0"
				raw = rawV2
				manifestContent = manifestV2
				manifestSig = sigV2
				minVersion = "MinVersion=2.0.0\n"
			}
			switch r.URL.Path {
			case "/" + name + "/" + name + ".conf":
				_, _ = fmt.Fprintf(w, `[Transfer]
Verify=false
InstancesMax=1
%s
[Source]
Type=url-file
Path=%s/%s/
MatchPattern=%s-@v.raw

[Target]
Type=regular-file
Path=%s
MatchPattern=%s-@v.raw
`, minVersion, server.URL, name, name, targetDir, name)
			case "/" + name + "/SHA256SUMS":
				_, _ = w.Write(manifestContent)
			case "/" + name + "/SHA256SUMS.gpg":
				_, _ = w.Write(manifestSig)
			case "/" + name + "/" + name + "-" + version + ".raw":
				_, _ = w.Write(raw)
			default:
				http.NotFound(w, r)
			}
		}))
		t.Cleanup(server.Close)
		writeCatalogRepo(t, catalogRoot, "fedora", server.URL, "")

		runner := &catalogPathRunner{}
		client := NewClient(ClientConfig{
			Paths: RuntimePaths{
				DefinitionRoots:    []string{definitionRoot},
				CatalogConfigRoots: []string{catalogRoot},
				CatalogCacheDir:    DisableCatalogCache,
				CatalogTargetPath:  targetDir,
				SysextLinkDir:      linkDir,
				RunExtensionsDir:   t.TempDir(),
			},
			SysextRunner: runner,
		})
		if _, err := client.CatalogAdd(t.Context(), name, CatalogAddOptions{}); err != nil {
			t.Fatalf("initial CatalogAdd: %v", err)
		}

		componentDir := filepath.Join(definitionRoot, "sysupdate.catalog-fedora.d")
		paths := []string{
			filepath.Join(componentDir, name+".transfer"),
			filepath.Join(componentDir, name+".feature"),
			filepath.Join(componentDir, name+".feature.d", updexDropInName),
		}
		before := make(map[string][]byte, len(paths))
		for _, path := range paths {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			before[path] = data
		}
		v1Path := filepath.Join(targetDir, name+"-1.0.0.raw")
		assertFileContent(t, v1Path, string(rawV1))
		linkPath := filepath.Join(linkDir, name+".raw")
		linkBefore, err := os.Readlink(linkPath)
		if err != nil {
			t.Fatal(err)
		}

		publishV2.Store(true)
		runner.refreshErr = refreshErr
		_, err = client.CatalogAdd(t.Context(), name, CatalogAddOptions{})
		if !errors.Is(err, refreshErr) {
			t.Fatalf("re-add error = %v, want injected refresh failure", err)
		}

		for path, want := range before {
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read restored %s: %v", path, err)
			}
			if string(got) != string(want) {
				t.Errorf("%s was not restored", path)
			}
		}
		assertFileContent(t, v1Path, string(rawV1))
		if _, err := os.Stat(filepath.Join(targetDir, name+"-2.0.0.raw")); !os.IsNotExist(err) {
			t.Errorf("failed re-add left v2 staged image: %v", err)
		}
		linkAfter, err := os.Readlink(linkPath)
		if err != nil {
			t.Fatalf("read restored sysext link: %v", err)
		}
		if linkAfter != linkBefore {
			t.Errorf("sysext link target = %q, want restored %q", linkAfter, linkBefore)
		}
	})
}

func TestCatalogAdd_LinkFailureRemovesDownloadedImage(t *testing.T) {
	const name = "zoxide"
	linkErr := errors.New("injected link failure")
	definitionRoot := t.TempDir()
	catalogRoot := t.TempDir()
	targetDir := t.TempDir()
	linkDir := t.TempDir()

	server := newCatalogServer(t, name, "1.0.0", targetDir)
	writeCatalogRepo(t, catalogRoot, "fedora", server.URL, "")
	client := NewClient(ClientConfig{
		Paths: RuntimePaths{
			DefinitionRoots:    []string{definitionRoot},
			CatalogConfigRoots: []string{catalogRoot},
			CatalogCacheDir:    DisableCatalogCache,
			CatalogTargetPath:  targetDir,
			SysextLinkDir:      linkDir,
			RunExtensionsDir:   t.TempDir(),
		},
		SysextRunner: &catalogPathRunner{linkErr: linkErr},
	})

	_, err := client.CatalogAdd(t.Context(), name, CatalogAddOptions{})
	if !errors.Is(err, linkErr) {
		t.Fatalf("CatalogAdd error = %v, want injected link failure", err)
	}
	if _, err := os.Stat(filepath.Join(targetDir, name+"-1.0.0.raw")); !os.IsNotExist(err) {
		t.Errorf("link failure left downloaded image: %v", err)
	}
	if _, err := os.Stat(filepath.Join(definitionRoot, "sysupdate.catalog-fedora.d")); !os.IsNotExist(err) {
		t.Errorf("link failure left generated definitions: %v", err)
	}
}

func TestCatalogAdd_CompressedTargetPatternRollback(t *testing.T) {
	const name = "zoxide"
	linkErr := errors.New("injected link failure")
	definitionRoot := t.TempDir()
	catalogRoot := t.TempDir()
	targetDir := t.TempDir()
	linkDir := t.TempDir()

	server := newCatalogServerWithTargetPattern(t, name, "1.0.0", targetDir, name+"-@v.raw.zst")
	writeCatalogRepo(t, catalogRoot, "fedora", server.URL, "")
	client := NewClient(ClientConfig{
		Paths: RuntimePaths{
			DefinitionRoots:    []string{definitionRoot},
			CatalogConfigRoots: []string{catalogRoot},
			CatalogCacheDir:    DisableCatalogCache,
			CatalogTargetPath:  targetDir,
			SysextLinkDir:      linkDir,
			RunExtensionsDir:   t.TempDir(),
		},
		SysextRunner: &catalogPathRunner{linkErr: linkErr},
	})

	_, err := client.CatalogAdd(t.Context(), name, CatalogAddOptions{})
	if !errors.Is(err, linkErr) {
		t.Fatalf("CatalogAdd error = %v, want injected link failure", err)
	}
	if _, err := os.Stat(filepath.Join(targetDir, name+"-1.0.0.raw")); !os.IsNotExist(err) {
		t.Errorf("compressed-pattern rollback left decompressed image: %v", err)
	}
}

func TestCatalogAdd_RollbackFailureJoinsOperationError(t *testing.T) {
	const name = "zoxide"
	refreshErr := errors.New("injected refresh failure")
	definitionRoot := t.TempDir()
	catalogRoot := t.TempDir()
	targetDir := t.TempDir()
	linkDir := t.TempDir()
	linkPath := filepath.Join(linkDir, name+".raw")

	server := newCatalogServer(t, name, "1.0.0", targetDir)
	writeCatalogRepo(t, catalogRoot, "fedora", server.URL, "")
	runner := &catalogPathRunner{refreshErr: refreshErr}
	runner.onRefresh = func() {
		if err := os.Remove(linkPath); err != nil {
			t.Fatalf("remove sysext link before injected rollback failure: %v", err)
		}
		if err := os.Mkdir(linkPath, 0755); err != nil {
			t.Fatalf("replace sysext link with directory: %v", err)
		}
		if err := os.WriteFile(filepath.Join(linkPath, "operator-state"), []byte("keep"), 0644); err != nil {
			t.Fatalf("make replacement directory non-empty: %v", err)
		}
	}
	client := NewClient(ClientConfig{
		Paths: RuntimePaths{
			DefinitionRoots:    []string{definitionRoot},
			CatalogConfigRoots: []string{catalogRoot},
			CatalogCacheDir:    DisableCatalogCache,
			CatalogTargetPath:  targetDir,
			SysextLinkDir:      linkDir,
			RunExtensionsDir:   t.TempDir(),
		},
		SysextRunner: runner,
	})

	_, err := client.CatalogAdd(t.Context(), name, CatalogAddOptions{})
	if !errors.Is(err, refreshErr) {
		t.Fatalf("CatalogAdd error = %v, want original refresh failure preserved", err)
	}
	if !strings.Contains(err.Error(), "remove newly created") || !strings.Contains(err.Error(), linkPath) {
		t.Fatalf("CatalogAdd error does not include actionable rollback failure for %s: %v", linkPath, err)
	}
}

func TestSnapshotFilesystemEntryUsesBoundedHeap(t *testing.T) {
	const childEnv = "UPDEX_TEST_BOUNDED_CATALOG_SNAPSHOT"
	if os.Getenv(childEnv) == "" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestSnapshotFilesystemEntryUsesBoundedHeap$")
		cmd.Env = append(os.Environ(), childEnv+"=1")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("bounded snapshot subprocess: %v\n%s", err, output)
		}
		return
	}

	previousLimit := debug.SetMemoryLimit(32 << 20)
	defer debug.SetMemoryLimit(previousLimit)
	dir := t.TempDir()
	path := filepath.Join(dir, "zoxide-1.0.0.raw")
	image, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := image.Truncate(128 << 20); err != nil {
		_ = image.Close()
		t.Fatal(err)
	}
	if err := image.Close(); err != nil {
		t.Fatal(err)
	}

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	snapshot, err := snapshotFilesystemEntry(path)
	if err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(snapshot)
	if after.HeapAlloc > before.HeapAlloc+(16<<20) {
		t.Fatalf("snapshot retained %d MiB of heap for a sparse image", (after.HeapAlloc-before.HeapAlloc)>>20)
	}
	if err := snapshot.restore(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(path) {
		t.Fatalf("snapshot restore left backup artifacts: %v", entries)
	}
	snapshot, err = snapshotFilesystemEntry(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := snapshot.cleanup(); err != nil {
		t.Fatal(err)
	}
	entries, err = os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(path) {
		t.Fatalf("successful snapshot cleanup left backup artifacts: %v", entries)
	}
}

func TestCatalogAdd_RefusesSpecialManagedInstallEntries(t *testing.T) {
	const name = "zoxide"
	for _, test := range []struct {
		name string
		path func(targetDir, linkDir string) string
	}{
		{name: "matching staged image", path: func(targetDir, _ string) string {
			return filepath.Join(targetDir, name+"-1.0.0.raw")
		}},
		{name: "sysext link", path: func(_, linkDir string) string {
			return filepath.Join(linkDir, name+".raw")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			definitionRoot := t.TempDir()
			catalogRoot := t.TempDir()
			targetDir := t.TempDir()
			linkDir := t.TempDir()
			priorImage := filepath.Join(targetDir, name+"-0.9.0.raw")
			if err := os.WriteFile(priorImage, []byte("prior image"), 0644); err != nil {
				t.Fatal(err)
			}
			specialPath := test.path(targetDir, linkDir)
			if err := syscall.Mkfifo(specialPath, 0600); err != nil {
				t.Fatal(err)
			}

			server := newCatalogServer(t, name, "1.0.0", targetDir)
			writeCatalogRepo(t, catalogRoot, "fedora", server.URL, "")
			client := NewClient(ClientConfig{
				Paths: RuntimePaths{
					DefinitionRoots:    []string{definitionRoot},
					CatalogConfigRoots: []string{catalogRoot},
					CatalogCacheDir:    DisableCatalogCache,
					CatalogTargetPath:  targetDir,
					SysextLinkDir:      linkDir,
					RunExtensionsDir:   t.TempDir(),
				},
				SysextRunner: &catalogPathRunner{refreshErr: errors.New("injected refresh failure")},
			})

			_, err := client.CatalogAdd(t.Context(), name, CatalogAddOptions{})
			if err == nil || !strings.Contains(err.Error(), "is not a regular file") {
				t.Fatalf("CatalogAdd error = %v, want special-entry refusal", err)
			}
			info, err := os.Lstat(specialPath)
			if err != nil {
				t.Fatalf("special entry was removed: %v", err)
			}
			if info.Mode()&os.ModeNamedPipe == 0 {
				t.Fatalf("special entry mode = %s, want named pipe", info.Mode())
			}
			assertFileContent(t, priorImage, "prior image")
			entries, err := os.ReadDir(targetDir)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if strings.Contains(entry.Name(), ".rollback-") {
					t.Fatalf("special-entry refusal left rollback artifact %s", entry.Name())
				}
			}
			componentDir := filepath.Join(definitionRoot, "sysupdate.catalog-fedora.d")
			if _, err := os.Stat(componentDir); !os.IsNotExist(err) {
				t.Fatalf("special-entry refusal left generated definitions: %v", err)
			}
		})
	}
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != want {
		t.Errorf("%s content = %q, want %q", path, got, want)
	}
}

func TestCatalogAdd_WriteFailureRestoresPrevious(t *testing.T) {
	roots := withComponentSearchRoots(t)
	catalogRoot := withCatalogConfigRoots(t)
	targetDir := t.TempDir()

	sysextDir := t.TempDir()
	origSysextDir := sysext.SysextDir
	sysext.SysextDir = sysextDir
	t.Cleanup(func() { sysext.SysextDir = origSysextDir })

	server := newCatalogServer(t, "zoxide", "1.0.0", targetDir)
	writeCatalogRepo(t, catalogRoot, "fedora", server.URL, "")

	client := NewClient(ClientConfig{SysextRunner: &sysext.MockRunner{}})
	if _, err := client.CatalogAdd(t.Context(), "zoxide", CatalogAddOptions{}); err != nil {
		t.Fatalf("initial add failed: %v", err)
	}

	componentDir := filepath.Join(roots[0], "sysupdate.catalog-fedora.d")
	transferPath := filepath.Join(componentDir, "zoxide.transfer")
	featurePath := filepath.Join(componentDir, "zoxide.feature")
	transferBefore, err := os.ReadFile(transferPath)
	if err != nil {
		t.Fatal(err)
	}

	// Make the feature write fail by putting a directory in its place, so
	// the transfer write has already truncated and rewritten the real file
	// before the failure hits.
	if err := os.Remove(featurePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(featurePath, 0755); err != nil {
		t.Fatal(err)
	}

	if _, err := client.CatalogAdd(t.Context(), "zoxide", CatalogAddOptions{}); err == nil {
		t.Fatal("expected add to fail when the feature file cannot be written")
	}

	transferAfter, err := os.ReadFile(transferPath)
	if err != nil {
		t.Fatalf("transfer file not restored: %v", err)
	}
	if string(transferAfter) != string(transferBefore) {
		t.Errorf("transfer not restored after write failure:\ngot:\n%s\nwant:\n%s", transferAfter, transferBefore)
	}

	// The directory standing in for the feature file existed but could not
	// be snapshotted, so rollback must leave it strictly alone rather than
	// deleting state it cannot rebuild.
	if info, err := os.Stat(featurePath); err != nil {
		t.Errorf("rollback removed the pre-existing path it could not back up: %v", err)
	} else if !info.IsDir() {
		t.Errorf("expected %s to still be a directory", featurePath)
	}
}

// TestCatalogAdd_StatFailureIsFatal verifies that a stat failure which is
// not "file absent" stops the add instead of being read as "nothing there"
// — that would skip the ownership check guarding existing definitions.
func TestCatalogAdd_StatFailureIsFatal(t *testing.T) {
	roots := withComponentSearchRoots(t)
	catalogRoot := withCatalogConfigRoots(t)
	targetDir := t.TempDir()

	server := newCatalogServer(t, "zoxide", "1.0.0", targetDir)
	writeCatalogRepo(t, catalogRoot, "fedora", server.URL, "")

	// A regular file where the component directory belongs: stat of any
	// path beneath it fails with ENOTDIR, which is not os.IsNotExist.
	componentPath := filepath.Join(roots[0], "sysupdate.catalog-fedora.d")
	if err := os.MkdirAll(filepath.Dir(componentPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(componentPath, []byte("not a directory\n"), 0644); err != nil {
		t.Fatal(err)
	}

	client := NewClient(ClientConfig{SysextRunner: &sysext.MockRunner{}})

	_, err := client.CatalogAdd(t.Context(), "zoxide", CatalogAddOptions{})
	if err == nil || !strings.Contains(err.Error(), "cannot determine whether") {
		t.Fatalf("expected a stat failure to be fatal, got %v", err)
	}

	// The blocking file is untouched.
	data, readErr := os.ReadFile(componentPath)
	if readErr != nil || string(data) != "not a directory\n" {
		t.Errorf("component path was modified (%v): %q", readErr, data)
	}
}

// TestCatalogAdd_RefusesSymlinkedDefinition verifies that a symlink at a
// managed definition path is never followed. A dangling link in
// particular used to stat as absent, skipping the ownership check, after
// which os.WriteFile would follow it and create the target outside the
// component directory — a privileged write, since add runs as root.
func TestCatalogAdd_RefusesSymlinkedDefinition(t *testing.T) {
	roots := withComponentSearchRoots(t)
	catalogRoot := withCatalogConfigRoots(t)
	targetDir := t.TempDir()

	server := newCatalogServer(t, "zoxide", "1.0.0", targetDir)
	writeCatalogRepo(t, catalogRoot, "fedora", server.URL, "")

	componentDir := filepath.Join(roots[0], "sysupdate.catalog-fedora.d")
	if err := os.MkdirAll(componentDir, 0755); err != nil {
		t.Fatal(err)
	}

	// The link target is outside the component directory and does not
	// exist yet: writing through the link would create it.
	outside := filepath.Join(t.TempDir(), "outside.conf")
	if err := os.Symlink(outside, filepath.Join(componentDir, "zoxide.feature")); err != nil {
		t.Fatal(err)
	}

	client := NewClient(ClientConfig{SysextRunner: &sysext.MockRunner{}})

	_, err := client.CatalogAdd(t.Context(), "zoxide", CatalogAddOptions{})
	if err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("expected a symlinked definition to be refused, got %v", err)
	}
	if _, statErr := os.Stat(outside); !os.IsNotExist(statErr) {
		t.Errorf("add wrote through the symlink to %s (stat err %v)", outside, statErr)
	}
	// The transfer is written before the feature, so a refusal that came
	// too late would have left it behind.
	if _, statErr := os.Stat(filepath.Join(componentDir, "zoxide.transfer")); !os.IsNotExist(statErr) {
		t.Errorf("add wrote definitions before refusing (stat err %v)", statErr)
	}
}

// TestCatalogRemove_RefusesSymlinkedDefinition verifies removal validates
// the definition paths before the destructive disable, so a symlink can
// neither be followed nor cause a half-completed teardown.
func TestCatalogRemove_RefusesSymlinkedDefinition(t *testing.T) {
	roots := withComponentSearchRoots(t)
	catalogRoot := withCatalogConfigRoots(t)
	targetDir := t.TempDir()

	sysextDir := t.TempDir()
	origSysextDir := sysext.SysextDir
	sysext.SysextDir = sysextDir
	t.Cleanup(func() { sysext.SysextDir = origSysextDir })

	server := newCatalogServer(t, "zoxide", "1.0.0", targetDir)
	writeCatalogRepo(t, catalogRoot, "fedora", server.URL, "")

	mockRunner := &sysext.MockRunner{}
	client := NewClient(ClientConfig{SysextRunner: mockRunner})
	if _, err := client.CatalogAdd(t.Context(), "zoxide", CatalogAddOptions{}); err != nil {
		t.Fatalf("CatalogAdd failed: %v", err)
	}

	// Replace the generated transfer with a symlink to it: ownership still
	// reads through, but the path is no longer one updex will manage.
	componentDir := filepath.Join(roots[0], "sysupdate.catalog-fedora.d")
	transferPath := filepath.Join(componentDir, "zoxide.transfer")
	kept := filepath.Join(t.TempDir(), "zoxide.transfer")
	if err := os.Rename(transferPath, kept); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(kept, transferPath); err != nil {
		t.Fatal(err)
	}

	_, err := client.CatalogRemove(t.Context(), "zoxide", CatalogRemoveOptions{})
	if err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("expected a symlinked definition to be refused, got %v", err)
	}
	if mockRunner.UnmergeCalled {
		t.Error("teardown ran before the definition paths were validated")
	}
	if _, statErr := os.Lstat(transferPath); statErr != nil {
		t.Errorf("symlink was removed: %v", statErr)
	}
	if _, statErr := os.Stat(kept); statErr != nil {
		t.Errorf("symlink target was deleted: %v", statErr)
	}
}

func TestFileExists(t *testing.T) {
	dir := t.TempDir()

	present := filepath.Join(dir, "present")
	if err := os.WriteFile(present, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	if ok, err := managedFileExists(present); err != nil || !ok {
		t.Errorf("managedFileExists(present) = (%v, %v), want (true, nil)", ok, err)
	}
	if ok, err := managedFileExists(filepath.Join(dir, "missing")); err != nil || ok {
		t.Errorf("managedFileExists(missing) = (%v, %v), want (false, nil)", ok, err)
	}
	// ENOTDIR: a path below a regular file exists in neither sense, but it
	// is not "absent" either — the caller must hear about it.
	if ok, err := managedFileExists(filepath.Join(present, "child")); err == nil || ok {
		t.Errorf("managedFileExists(under a file) = (%v, %v), want (false, error)", ok, err)
	}

	// Symlinks are reported as errors rather than followed, dangling ones
	// included: Stat would call a dangling link absent and let the caller
	// write straight through it.
	live := filepath.Join(dir, "live-link")
	if err := os.Symlink(present, live); err != nil {
		t.Fatal(err)
	}
	if ok, err := managedFileExists(live); err == nil || ok {
		t.Errorf("managedFileExists(symlink) = (%v, %v), want (false, error)", ok, err)
	}
	dangling := filepath.Join(dir, "dangling-link")
	if err := os.Symlink(filepath.Join(dir, "nowhere"), dangling); err != nil {
		t.Fatal(err)
	}
	if ok, err := managedFileExists(dangling); err == nil || ok {
		t.Errorf("managedFileExists(dangling symlink) = (%v, %v), want (false, error)", ok, err)
	}

	// A directory is likewise not something updex manages in place.
	sub := filepath.Join(dir, "subdir")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatal(err)
	}
	if ok, err := managedFileExists(sub); err == nil || ok {
		t.Errorf("managedFileExists(dir) = (%v, %v), want (false, error)", ok, err)
	}
}

func TestFileSnapshotSymlinkIsNotFollowed(t *testing.T) {
	dir := t.TempDir()

	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("original target\n"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	s := snapshotFile(link)
	if !s.existed || s.captured {
		t.Errorf("snapshot of a symlink = (existed %v, captured %v), want (true, false)", s.existed, s.captured)
	}

	// Restore must neither delete the link nor rewrite its target.
	s.restore()
	if _, err := os.Lstat(link); err != nil {
		t.Errorf("restore removed the symlink: %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "original target\n" {
		t.Errorf("restore wrote through the symlink (%v): %q", err, data)
	}
}

func TestFileSnapshotUnreadableIsNotRemoved(t *testing.T) {
	dir := t.TempDir()

	// A directory stands in for any path that exists but whose contents
	// cannot be captured.
	path := filepath.Join(dir, "unreadable")
	if err := os.Mkdir(path, 0755); err != nil {
		t.Fatal(err)
	}

	s := snapshotFile(path)
	if !s.existed {
		t.Error("expected existed=true for a path that is present")
	}
	if s.captured {
		t.Error("expected captured=false for contents that cannot be read")
	}

	s.restore()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("restore removed a path it could not back up: %v", err)
	}
}

// TestCatalogRemove_RefusesForeignTransfer verifies removal aborts before
// anything destructive when the .transfer is not this repo's, rather than
// unmerging and deleting images described by that foreign definition.
func TestCatalogRemove_RefusesForeignTransfer(t *testing.T) {
	roots := withComponentSearchRoots(t)
	catalogRoot := withCatalogConfigRoots(t)
	targetDir := t.TempDir()

	sysextDir := t.TempDir()
	origSysextDir := sysext.SysextDir
	sysext.SysextDir = sysextDir
	t.Cleanup(func() { sysext.SysextDir = origSysextDir })

	server := newCatalogServer(t, "zoxide", "1.0.0", targetDir)
	writeCatalogRepo(t, catalogRoot, "fedora", server.URL, "")

	mockRunner := &sysext.MockRunner{}
	client := NewClient(ClientConfig{SysextRunner: mockRunner})
	if _, err := client.CatalogAdd(t.Context(), "zoxide", CatalogAddOptions{}); err != nil {
		t.Fatalf("CatalogAdd failed: %v", err)
	}

	// An administrator replaces the generated transfer with their own,
	// keeping the generated feature.
	componentDir := filepath.Join(roots[0], "sysupdate.catalog-fedora.d")
	writeComponentTransfer(t, componentDir, "zoxide")

	imagePath := filepath.Join(targetDir, "zoxide-1.0.0.raw")
	if _, err := os.Stat(imagePath); err != nil {
		t.Fatalf("expected downloaded image: %v", err)
	}

	_, err := client.CatalogRemove(t.Context(), "zoxide", CatalogRemoveOptions{})
	if err == nil || !strings.Contains(err.Error(), "was not generated by catalog") {
		t.Fatalf("expected refusal for foreign transfer, got %v", err)
	}

	// Nothing destructive ran: no unmerge, images and definitions intact.
	if mockRunner.UnmergeCalled {
		t.Error("unmerge ran despite the refusal")
	}
	if _, err := os.Stat(imagePath); err != nil {
		t.Errorf("image described by the foreign transfer was removed: %v", err)
	}
	for _, f := range []string{"zoxide.transfer", "zoxide.feature"} {
		if _, err := os.Stat(filepath.Join(componentDir, f)); err != nil {
			t.Errorf("%s was removed: %v", f, err)
		}
	}
}

// TestCatalogList_SharedComponentStatus verifies Installed/Enabled are
// attributed to the repo that actually generated the definition, not to
// every repo sharing the component.
func TestCatalogList_SharedComponentStatus(t *testing.T) {
	roots := withComponentSearchRoots(t)
	catalogRoot := withCatalogConfigRoots(t)

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"name": "zoxide", "type": "dir"}]`))
	}))
	defer apiServer.Close()

	writeCatalogFileContent(t, catalogRoot, "alpha",
		"[Catalog]\nSiteURL=https://example.com/alpha\nListURL="+apiServer.URL+
			"\nComponent=shared\nAllowInsecure=yes\n")
	writeCatalogFileContent(t, catalogRoot, "beta",
		"[Catalog]\nSiteURL=https://example.com/beta\nListURL="+apiServer.URL+
			"\nComponent=shared\nAllowInsecure=yes\n")

	// Only alpha added zoxide, into the component both repos share.
	componentDir := filepath.Join(roots[0], "sysupdate.shared.d")
	writeGeneratedFeature(t, componentDir, "alpha", "zoxide")
	writeEnableDropIn(t, componentDir, "zoxide", true)

	client := NewClient(ClientConfig{SysextRunner: &sysext.MockRunner{}})

	entries, err := client.CatalogList(t.Context(), CatalogListOptions{})
	if err != nil {
		t.Fatalf("CatalogList failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected one entry per repo, got %+v", entries)
	}
	for _, e := range entries {
		switch e.Repo {
		case "alpha":
			if !e.Installed || !e.Enabled {
				t.Errorf("alpha/zoxide should be installed and enabled: %+v", e)
			}
		case "beta":
			if e.Installed || e.Enabled {
				t.Errorf("beta/zoxide is alpha's install, must not be reported: %+v", e)
			}
		default:
			t.Errorf("unexpected repo in entry: %+v", e)
		}
	}
}

// TestCatalogRemove_KeepsAdminDropIns verifies removal deletes only
// updex's own drop-in, leaving administrator files (and therefore the
// directory) in place.
func TestCatalogRemove_KeepsAdminDropIns(t *testing.T) {
	roots := withComponentSearchRoots(t)
	catalogRoot := withCatalogConfigRoots(t)
	targetDir := t.TempDir()

	sysextDir := t.TempDir()
	origSysextDir := sysext.SysextDir
	sysext.SysextDir = sysextDir
	t.Cleanup(func() { sysext.SysextDir = origSysextDir })

	server := newCatalogServer(t, "zoxide", "1.0.0", targetDir)
	writeCatalogRepo(t, catalogRoot, "fedora", server.URL, "")

	client := NewClient(ClientConfig{SysextRunner: &sysext.MockRunner{}})
	if _, err := client.CatalogAdd(t.Context(), "zoxide", CatalogAddOptions{}); err != nil {
		t.Fatalf("CatalogAdd failed: %v", err)
	}

	componentDir := filepath.Join(roots[0], "sysupdate.catalog-fedora.d")
	dropInDir := filepath.Join(componentDir, "zoxide.feature.d")
	adminDropIn := filepath.Join(dropInDir, "50-local.conf")
	if err := os.WriteFile(adminDropIn, []byte("[Feature]\nDescription=local override\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := client.CatalogRemove(t.Context(), "zoxide", CatalogRemoveOptions{}); err != nil {
		t.Fatalf("CatalogRemove failed: %v", err)
	}

	if _, err := os.Stat(adminDropIn); err != nil {
		t.Errorf("administrator drop-in was deleted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dropInDir, updexDropInName)); !os.IsNotExist(err) {
		t.Errorf("updex drop-in should have been removed, stat err = %v", err)
	}
}

func TestCatalogRemove_RefusesUnmanagedSameName(t *testing.T) {
	roots := withComponentSearchRoots(t)
	catalogRoot := withCatalogConfigRoots(t)

	// Catalog configured with a Component pointing at an existing,
	// hand-managed component ("docker"): remove must not touch it.
	writeCatalogFileContent(t, catalogRoot, "fedora", `[Catalog]
SiteURL=https://example.com/fedora
Component=docker
`)
	componentDir := filepath.Join(roots[0], "sysupdate.docker.d")
	writeComponentFeature(t, componentDir, "docker", true)
	writeComponentTransfer(t, componentDir, "docker")

	client := NewClient(ClientConfig{SysextRunner: &sysext.MockRunner{}})

	_, err := client.CatalogRemove(t.Context(), "docker", CatalogRemoveOptions{})
	if err == nil || !strings.Contains(err.Error(), "not a catalog-managed sysext") {
		t.Fatalf("expected not-catalog-managed error, got %v", err)
	}

	// The hand-managed definitions are untouched.
	for _, f := range []string{"docker.feature", "docker.transfer"} {
		if _, err := os.Stat(filepath.Join(componentDir, f)); err != nil {
			t.Errorf("%s was removed: %v", f, err)
		}
	}
}

func TestCatalogRemove_NotCatalogManaged(t *testing.T) {
	roots := withComponentSearchRoots(t)
	catalogRoot := withCatalogConfigRoots(t)

	writeCatalogRepo(t, catalogRoot, "fedora", "https://example.com", "")

	// A feature in the legacy default directory is not catalog-managed.
	writeComponentFeature(t, filepath.Join(roots[0], "sysupdate.d"), "handmade", true)

	client := NewClient(ClientConfig{SysextRunner: &sysext.MockRunner{}})

	_, err := client.CatalogRemove(t.Context(), "handmade", CatalogRemoveOptions{})
	if err == nil || !strings.Contains(err.Error(), "not a catalog-managed sysext") {
		t.Fatalf("expected not-catalog-managed error, got %v", err)
	}
}

func TestCatalogList(t *testing.T) {
	roots := withComponentSearchRoots(t)
	catalogRoot := withCatalogConfigRoots(t)

	var apiRequests int
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiRequests++
		_, _ = w.Write([]byte(`[
			{"name": "btop", "type": "dir"},
			{"name": "zoxide", "type": "dir"},
			{"name": "docs", "type": "dir"}
		]`))
	}))
	defer apiServer.Close()

	writeCatalogRepo(t, catalogRoot, "fedora", "https://example.com", apiServer.URL)
	writeCatalogRepo(t, catalogRoot, "nolist", "https://example.com", "")

	// zoxide is already added by the fedora catalog and enabled: a
	// marker-bearing generated feature plus the standard enable drop-in.
	componentDir := filepath.Join(roots[0], "sysupdate.catalog-fedora.d")
	writeGeneratedFeature(t, componentDir, "fedora", "zoxide")
	writeEnableDropIn(t, componentDir, "zoxide", true)

	client := NewClient(ClientConfig{SysextRunner: &sysext.MockRunner{}})

	entries, err := client.CatalogList(t.Context(), CatalogListOptions{})
	if err != nil {
		t.Fatalf("CatalogList failed: %v", err)
	}
	// The nolist repo is skipped with a warning; docs is filtered out.
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d: %+v", len(entries), entries)
	}
	if entries[0].Name != "btop" || entries[0].Installed {
		t.Errorf("unexpected btop entry: %+v", entries[0])
	}
	if entries[1].Name != "zoxide" || !entries[1].Installed || !entries[1].Enabled {
		t.Errorf("unexpected zoxide entry: %+v", entries[1])
	}

	// Search filters by substring.
	entries, err = client.CatalogList(t.Context(), CatalogListOptions{Search: "zox"})
	if err != nil {
		t.Fatalf("CatalogList search failed: %v", err)
	}
	if len(entries) != 1 || entries[0].Name != "zoxide" {
		t.Errorf("search: expected only zoxide, got %+v", entries)
	}

	// Repeated listings are served from the local cache: the two calls
	// above made exactly one API request between them.
	if apiRequests != 1 {
		t.Errorf("expected 1 API request across cached listings, got %d", apiRequests)
	}

	// NoCache forces a live pull.
	if _, err := client.CatalogList(t.Context(), CatalogListOptions{NoCache: true}); err != nil {
		t.Fatalf("CatalogList with NoCache failed: %v", err)
	}
	if apiRequests != 2 {
		t.Errorf("expected NoCache to make a live request, got %d total", apiRequests)
	}

	// Explicitly selecting a repo without ListURL is an error.
	if _, err := client.CatalogList(t.Context(), CatalogListOptions{Repo: "nolist"}); err == nil {
		t.Error("expected error listing repo without ListURL")
	}

	if _, err := client.CatalogList(t.Context(), CatalogListOptions{Repo: "nope"}); err == nil ||
		!strings.Contains(err.Error(), "unknown catalog") {
		t.Errorf("expected unknown-catalog error, got %v", err)
	}
}

// TestCatalogAdd_ThenStandardLifecycle verifies the added feature is fully
// manageable by the standard enable/disable operations until removed.
func TestCatalogAdd_ThenStandardLifecycle(t *testing.T) {
	withComponentSearchRoots(t)
	catalogRoot := withCatalogConfigRoots(t)
	targetDir := t.TempDir()

	sysextDir := t.TempDir()
	origSysextDir := sysext.SysextDir
	sysext.SysextDir = sysextDir
	t.Cleanup(func() { sysext.SysextDir = origSysextDir })

	server := newCatalogServer(t, "zoxide", "1.0.0", targetDir)
	writeCatalogRepo(t, catalogRoot, "fedora", server.URL, "")

	client := NewClient(ClientConfig{SysextRunner: &sysext.MockRunner{}})

	if _, err := client.CatalogAdd(t.Context(), "zoxide", CatalogAddOptions{}); err != nil {
		t.Fatalf("CatalogAdd failed: %v", err)
	}

	// Standard disable (no component scoping — default union domain).
	if _, err := client.DisableFeature(t.Context(), "zoxide", DisableFeatureOptions{}); err != nil {
		t.Fatalf("standard DisableFeature failed: %v", err)
	}
	features, _, err := config.LoadAllFeatures("")
	if err != nil {
		t.Fatal(err)
	}
	if config.IsFeatureEnabled(features, "zoxide") {
		t.Error("expected zoxide disabled after standard DisableFeature")
	}

	// Standard enable turns it back on.
	if _, err := client.EnableFeature(t.Context(), "zoxide", EnableFeatureOptions{}); err != nil {
		t.Fatalf("standard EnableFeature failed: %v", err)
	}
	features, _, err = config.LoadAllFeatures("")
	if err != nil {
		t.Fatal(err)
	}
	if !config.IsFeatureEnabled(features, "zoxide") {
		t.Error("expected zoxide enabled after standard EnableFeature")
	}
}

// TestCatalogList_CacheWriteFailureIsReportedNotFatal verifies that a
// listing cache that cannot be written still yields a successful CatalogList
// result, and that the write failure is surfaced through the Progress
// reporter's warning path (c.warn in CatalogList) rather than silently
// dropped — CacheResult.WriteErr alone is not visible to SDK/CLI users.
func TestCatalogList_CacheWriteFailureIsReportedNotFatal(t *testing.T) {
	withComponentSearchRoots(t)
	catalogRoot := withCatalogConfigRoots(t)

	names := []string{"zoxide", "btop"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		type contentsEntry struct {
			Name string `json:"name"`
			Type string `json:"type"`
		}
		entries := make([]contentsEntry, 0, len(names))
		for _, n := range names {
			entries = append(entries, contentsEntry{Name: n, Type: "dir"})
		}
		_ = json.NewEncoder(w).Encode(entries)
	}))
	t.Cleanup(server.Close)
	writeCatalogRepo(t, catalogRoot, "fedora", server.URL, server.URL)

	// The cache file path itself is a pre-existing directory, so the
	// listing cache write fails while the live listing still succeeds.
	cacheDir := t.TempDir()
	cachePath := filepath.Join(cacheDir, "list-fedora.json")
	if err := os.Mkdir(cachePath, 0755); err != nil {
		t.Fatal(err)
	}

	reporter := &recordingReporter{}
	client := NewClient(ClientConfig{
		Progress: reporter,
		Paths:    RuntimePaths{CatalogCacheDir: cacheDir},
	})

	entries, err := client.CatalogList(t.Context(), CatalogListOptions{})
	if err != nil {
		t.Fatalf("expected CatalogList to succeed despite the cache write failure, got: %v", err)
	}
	got := make(map[string]bool, len(entries))
	for _, e := range entries {
		got[e.Name] = true
	}
	for _, name := range names {
		if !got[name] {
			t.Errorf("missing expected entry %q in listing %+v", name, entries)
		}
	}

	if !reporter.hasWarningContaining("failed to persist listing cache") {
		t.Errorf("expected a warning referencing the cache write failure, got warnings: %v", reporter.warnings)
	}
}
