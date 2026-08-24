package updex

// Concurrent two-client filesystem isolation tests (ADR-0010).
//
// These tests prove:
//  1. Two clients targeting independent filesystem trees coexist in one
//     process without interfering with each other.
//  2. Mutating a compatibility package variable (e.g. config.SearchRoots)
//     after client construction cannot redirect an existing client.
//  3. Tests using independently configured clients can run in parallel
//     without save/mutate/restore discipline.
//
// Run with -race to surface any concurrent access to package-level state:
//
//	go test -race -run TestClientIsolation ./updex/

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/frostyard/updex/catalog"
	"github.com/frostyard/updex/config"
	"github.com/frostyard/updex/internal/testutil"
)

// TestClientIsolation_DefinitionRoots proves two clients each see only their
// own definition trees. Neither client shares state, so they can run
// concurrently without interfering.
func TestClientIsolation_DefinitionRoots(t *testing.T) {
	t.Parallel()

	// Client A's tree
	rootA := t.TempDir()
	writeComponentFeature(t, filepath.Join(rootA, "sysupdate.d"), "alpha", true)

	// Client B's tree
	rootB := t.TempDir()
	writeComponentFeature(t, filepath.Join(rootB, "sysupdate.d"), "beta", false)

	clientA := NewClient(ClientConfig{
		Paths: RuntimePaths{DefinitionRoots: []string{rootA}},
	})
	clientB := NewClient(ClientConfig{
		Paths: RuntimePaths{DefinitionRoots: []string{rootB}},
	})

	featA, _, err := clientA.loadDomain("")
	if err != nil {
		t.Fatalf("clientA.loadDomain: %v", err)
	}
	featB, _, err := clientB.loadDomain("")
	if err != nil {
		t.Fatalf("clientB.loadDomain: %v", err)
	}

	if len(featA) != 1 || featA[0].Name != "alpha" {
		t.Errorf("clientA: expected [alpha], got %v", featA)
	}
	if len(featB) != 1 || featB[0].Name != "beta" {
		t.Errorf("clientB: expected [beta], got %v", featB)
	}
}

// TestClientIsolation_MutatingGlobalCannotRedirectExistingClient proves the
// core invariant of ADR-0010: a client that has already been constructed
// continues to use the paths it captured at NewClient time. Subsequent
// mutations to package-level compatibility variables do not redirect it.
func TestClientIsolation_MutatingGlobalCannotRedirectExistingClient(t *testing.T) {
	// originalRoot holds the feature the client should always find.
	originalRoot := t.TempDir()
	writeComponentFeature(t, filepath.Join(originalRoot, "sysupdate.d"), "original", true)

	originalRoots := config.SearchRoots
	config.SearchRoots = []string{originalRoot}
	t.Cleanup(func() { config.SearchRoots = originalRoots })
	client := NewClient(ClientConfig{})

	// Now mutate the package global to point somewhere else. The already-
	// constructed client must not be affected.
	otherRoot := t.TempDir()
	writeComponentFeature(t, filepath.Join(otherRoot, "sysupdate.d"), "interloper", true)
	config.SearchRoots = []string{otherRoot}

	features, _, err := client.loadDomain("")
	if err != nil {
		t.Fatalf("loadDomain after global mutation: %v", err)
	}

	if len(features) != 1 || features[0].Name != "original" {
		t.Errorf("global mutation redirected existing client: got features %v, want [original]", features)
	}
}

// TestClientIsolation_CatalogConfigRoots proves two clients each load catalog
// repos from their own config roots.
func TestClientIsolation_CatalogConfigRoots(t *testing.T) {
	t.Parallel()

	rootA := t.TempDir()
	cacheA := t.TempDir()
	writeCatalogRepo(t, rootA, "fedora", "https://extensions-a.example.com", "")

	rootB := t.TempDir()
	cacheB := t.TempDir()
	writeCatalogRepo(t, rootB, "alpine", "https://extensions-b.example.com", "")

	clientA := NewClient(ClientConfig{
		Paths: RuntimePaths{
			CatalogConfigRoots: []string{rootA},
			CatalogCacheDir:    cacheA,
		},
	})
	clientB := NewClient(ClientConfig{
		Paths: RuntimePaths{
			CatalogConfigRoots: []string{rootB},
			CatalogCacheDir:    cacheB,
		},
	})

	reposA, err := clientA.catalogRepos()
	if err != nil {
		t.Fatalf("clientA.catalogRepos: %v", err)
	}
	reposB, err := clientB.catalogRepos()
	if err != nil {
		t.Fatalf("clientB.catalogRepos: %v", err)
	}
	if len(reposA) != 1 || reposA[0].Name != "fedora" {
		t.Errorf("clientA repos = %v, want [fedora]", reposA)
	}
	if len(reposB) != 1 || reposB[0].Name != "alpine" {
		t.Errorf("clientB repos = %v, want [alpine]", reposB)
	}
}

func TestClientIsolation_CatalogCaches(t *testing.T) {
	t.Parallel()

	type fixture struct {
		client    *Client
		entryName string
		cacheDir  string
	}
	fixtures := make([]fixture, 2)
	for i, entryName := range []string{"alpha", "beta"} {
		entryName := entryName
		apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprintf(w, `[{"name":%q,"type":"dir"}]`, entryName)
		}))
		t.Cleanup(apiServer.Close)

		catalogRoot := t.TempDir()
		cacheDir := t.TempDir()
		definitionRoot := t.TempDir()
		writeCatalogRepo(t, catalogRoot, "shared", "https://extensions.example.com", apiServer.URL)
		fixtures[i] = fixture{
			client: NewClient(ClientConfig{
				Paths: RuntimePaths{
					DefinitionRoots:    []string{definitionRoot},
					CatalogConfigRoots: []string{catalogRoot},
					CatalogCacheDir:    cacheDir,
				},
			}),
			entryName: entryName,
			cacheDir:  cacheDir,
		}
	}

	var wg sync.WaitGroup
	for i := range fixtures {
		wg.Go(func() {
			entries, err := fixtures[i].client.CatalogList(t.Context(), CatalogListOptions{})
			if err != nil {
				t.Errorf("client %d CatalogList: %v", i, err)
				return
			}
			if len(entries) != 1 || entries[0].Name != fixtures[i].entryName {
				t.Errorf("client %d entries = %v, want %q", i, entries, fixtures[i].entryName)
			}
		})
	}
	wg.Wait()

	for i, fixture := range fixtures {
		if _, err := os.Stat(filepath.Join(fixture.cacheDir, "list-shared.json")); err != nil {
			t.Errorf("client %d cache: %v", i, err)
		}
	}
}

// TestClientIsolation_MutatingCatalogGlobalCannotRedirectExistingClient
// mirrors the definition-root mutation test for the catalog compatibility
// variables.
func TestClientIsolation_MutatingCatalogGlobalCannotRedirectExistingClient(t *testing.T) {
	originalRoot := t.TempDir()
	writeCatalogRepo(t, originalRoot, "original", "https://original.example.com", "")

	origRoots := catalog.ConfigRoots
	catalog.ConfigRoots = []string{originalRoot}
	t.Cleanup(func() { catalog.ConfigRoots = origRoots })
	client := NewClient(ClientConfig{Paths: RuntimePaths{CatalogCacheDir: DisableCatalogCache}})

	// Mutate the compatibility global to point at a different root.
	otherRoot := t.TempDir()
	writeCatalogRepo(t, otherRoot, "interloper", "https://interloper.example.com", "")
	catalog.ConfigRoots = []string{otherRoot}

	repos, err := client.catalogRepos()
	if err != nil {
		t.Fatalf("catalogRepos after global mutation: %v", err)
	}
	if len(repos) != 1 || repos[0].Name != "original" {
		t.Errorf("catalog global mutation redirected existing client: got repos %v, want [original]", repos)
	}
}

// TestClientIsolation_SysextLinkDir proves two clients write sysext links into
// separate directories and do not interfere under concurrent load.
func TestClientIsolation_CatalogAddPaths(t *testing.T) {
	t.Parallel()

	type fixture struct {
		client         *Client
		definitionRoot string
		targetDir      string
		linkDir        string
	}
	fixtures := make([]fixture, 2)
	for i := range fixtures {
		definitionRoot := t.TempDir()
		catalogRoot := t.TempDir()
		targetDir := t.TempDir()
		linkDir := t.TempDir()
		server := newIsolatedCatalogServer(t, "myext", targetDir)
		writeCatalogRepo(t, catalogRoot, "shared", server.URL, "")

		fixtures[i] = fixture{
			client: NewClient(ClientConfig{
				Paths: RuntimePaths{
					DefinitionRoots:    []string{definitionRoot},
					CatalogConfigRoots: []string{catalogRoot},
					CatalogCacheDir:    DisableCatalogCache,
					CatalogTargetPath:  targetDir,
					SysextLinkDir:      linkDir,
				},
			}),
			definitionRoot: definitionRoot,
			targetDir:      targetDir,
			linkDir:        linkDir,
		}
	}

	var wg sync.WaitGroup
	for i := range fixtures {
		wg.Go(func() {
			if _, err := fixtures[i].client.CatalogAdd(t.Context(), "myext", CatalogAddOptions{
				Repo:      "shared",
				NoRefresh: true,
			}); err != nil {
				t.Errorf("client %d CatalogAdd: %v", i, err)
			}
		})
	}
	wg.Wait()

	for i, fixture := range fixtures {
		transferPath := filepath.Join(fixture.definitionRoot, "sysupdate.catalog-shared.d", "myext.transfer")
		transferData, err := os.ReadFile(transferPath)
		if err != nil {
			t.Errorf("client %d transfer definition: %v", i, err)
		} else if !strings.Contains(string(transferData), "Path="+fixture.targetDir) {
			t.Errorf("client %d transfer does not target %s:\n%s", i, fixture.targetDir, transferData)
		}
		if _, err := os.Stat(filepath.Join(fixture.targetDir, "myext-1.0.0.raw")); err != nil {
			t.Errorf("client %d staged image: %v", i, err)
		}
		if _, err := os.Lstat(filepath.Join(fixture.linkDir, "myext.raw")); err != nil {
			t.Errorf("client %d sysext link: %v", i, err)
		}
	}
}

// TestClientIsolation_Concurrent runs two independent clients concurrently
// against separate filesystem trees while the race detector watches for
// shared-state access. Each goroutine writes features to its own tree and
// then loads them; no locking or coordination is needed.
func TestClientIsolation_Concurrent(t *testing.T) {
	t.Parallel()

	type fixture struct {
		client      *Client
		feature     *config.Feature
		featureName string
		dropInPath  string
	}
	fixtures := make([]fixture, 2)
	for i := range fixtures {
		root := t.TempDir()
		featureName := "feature-" + string(rune('a'+i))
		writeComponentFeature(t, filepath.Join(root, "sysupdate.d"), featureName, true)
		client := NewClient(ClientConfig{
			Paths: RuntimePaths{DefinitionRoots: []string{root}},
		})
		features, _, err := client.loadDomain("")
		if err != nil {
			t.Fatalf("prepare client %d: %v", i, err)
		}
		fixtures[i] = fixture{
			client:      client,
			feature:     features[0],
			featureName: featureName,
			dropInPath:  filepath.Join(root, "sysupdate.d", featureName+".feature.d", updexDropInName),
		}
	}

	var wg sync.WaitGroup
	for i := range fixtures {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := fixtures[i].client.writeFeatureDropIn(fixtures[i].feature, false, false)
			if err != nil {
				t.Errorf("worker %d writeFeatureDropIn: %v", i, err)
				return
			}
			if got != fixtures[i].dropInPath {
				t.Errorf("worker %d wrote %q, want %q", i, got, fixtures[i].dropInPath)
			}
		}()
	}
	wg.Wait()
	for _, fixture := range fixtures {
		if _, err := os.Stat(fixture.dropInPath); err != nil {
			t.Errorf("%s drop-in: %v", fixture.featureName, err)
		}
	}
}

func TestClientIsolation_OSReleasePaths(t *testing.T) {
	t.Parallel()

	type fixture struct {
		client *Client
		want   string
	}
	fixtures := make([]fixture, 2)
	for i, version := range []string{"40", "41"} {
		definitions := t.TempDir()
		osRelease := filepath.Join(t.TempDir(), "os-release")
		if err := os.WriteFile(osRelease, []byte("VERSION_ID="+version+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
		writeSpecifierTransfer(t, definitions)
		fixtures[i] = fixture{
			client: NewClient(ClientConfig{
				Definitions: definitions,
				Paths:       RuntimePaths{OSReleasePaths: []string{osRelease}},
			}),
			want: "test_" + version + "_@v.raw",
		}
	}

	var wg sync.WaitGroup
	for i := range fixtures {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, transfers, err := fixtures[i].client.loadDomain("")
			if err != nil {
				t.Errorf("client %d loadDomain: %v", i, err)
				return
			}
			if len(transfers) != 1 || transfers[0].Source.MatchPattern != fixtures[i].want {
				t.Errorf("client %d source pattern = %v, want %q", i, transfers, fixtures[i].want)
			}
		}()
	}
	wg.Wait()
}

func TestClientIsolation_DefensiveCopies(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeComponentFeature(t, filepath.Join(root, "sysupdate.d"), "original", true)
	roots := []string{root}
	client := NewClient(ClientConfig{Paths: RuntimePaths{DefinitionRoots: roots}})
	roots[0] = t.TempDir()

	features, _, err := client.loadDomain("")
	if err != nil {
		t.Fatal(err)
	}
	if len(features) != 1 || features[0].Name != "original" {
		t.Fatalf("caller slice mutation redirected client: %v", features)
	}
}

func writeSpecifierTransfer(t *testing.T, dir string) {
	t.Helper()
	content := `[Transfer]
Features=test

[Source]
Type=url-file
Path=https://example.test
MatchPattern=test_%w_@v.raw

[Target]
Type=regular-file
Path=/var/lib/extensions.d
MatchPattern=test_@v.raw
`
	if err := os.WriteFile(filepath.Join(dir, "test.transfer"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func newIsolatedCatalogServer(t *testing.T, name, targetDir string) *httptest.Server {
	t.Helper()

	const version = "1.0.0"
	rawName := name + "-" + version + ".raw"
	rawContent := []byte("isolated sysext image")
	manifestContent := []byte(fmt.Sprintf("%s  %s\n", hashContent(rawContent), rawName))
	manifestSig := testutil.SignManifest(t, manifestContent)

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/" + name + "/" + name + ".conf":
			_, _ = fmt.Fprintf(w, `[Transfer]
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
	`, server.URL, name, name, targetDir, name)
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
