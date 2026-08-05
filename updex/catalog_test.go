package updex

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/frostyard/updex/catalog"
	"github.com/frostyard/updex/config"
	"github.com/frostyard/updex/sysext"
)

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
	writeCatalogFileContent(t, dir, name, content)
}

func writeCatalogFileContent(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name+".catalog"), []byte(content), 0644); err != nil {
		t.Fatalf("failed to write catalog file: %v", err)
	}
}

// newCatalogServer serves a catalog repo with a single sysext: its .conf,
// SHA256SUMS manifest, and one .raw image, all under /<name>/. The conf's
// transfer downloads into targetDir.
func newCatalogServer(t *testing.T, name, version, targetDir string) *httptest.Server {
	t.Helper()

	rawName := fmt.Sprintf("%s-%s.raw", name, version)
	rawContent := []byte("fake sysext image for " + name)

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
MatchPattern=%s-@v.raw
CurrentSymlink=/var/lib/extensions/%s.raw
`, server.URL, name, name, targetDir, name, name)
			_, _ = w.Write([]byte(conf))
		case "/" + name + "/SHA256SUMS":
			_, _ = fmt.Fprintf(w, "%s  %s\n", hashContent(rawContent), rawName)
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
	writeCatalogFileContent(t, catalogRoot, "alpha", "[Catalog]\nSiteURL="+serverA.URL+"\nComponent=shared\n")
	writeCatalogFileContent(t, catalogRoot, "beta", "[Catalog]\nSiteURL="+serverB.URL+"\nComponent=shared\n")

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

	sysextDir := t.TempDir()
	origSysextDir := sysext.SysextDir
	sysext.SysextDir = sysextDir
	t.Cleanup(func() { sysext.SysextDir = origSysextDir })

	// Serves a working sysext first; breakManifest makes later downloads fail.
	var breakManifest atomic.Bool
	rawContent := []byte("fake sysext image for zoxide")
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
			_, _ = fmt.Fprintf(w, "%s  zoxide-1.0.0.raw\n", hashContent(rawContent))
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

	// zoxide is already added and enabled in the fedora catalog component.
	componentDir := filepath.Join(roots[0], "sysupdate.catalog-fedora.d")
	writeComponentFeature(t, componentDir, "zoxide", true)

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
