package updex

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/frostyard/clix"
	"github.com/frostyard/updex/catalog"
	"github.com/frostyard/updex/config"
	sdk "github.com/frostyard/updex/updex"
)

func runCLI(t *testing.T, args ...string) (stdout, stderr string, runErr error) {
	t.Helper()

	outputDir := t.TempDir()
	stdoutFile, err := os.Create(filepath.Join(outputDir, "stdout"))
	if err != nil {
		t.Fatal(err)
	}
	stderrFile, err := os.Create(filepath.Join(outputDir, "stderr"))
	if err != nil {
		_ = stdoutFile.Close()
		t.Fatal(err)
	}

	originalStdout, originalStderr := os.Stdout, os.Stderr
	func() {
		defer func() {
			os.Stdout = originalStdout
			os.Stderr = originalStderr
		}()
		os.Stdout = stdoutFile
		os.Stderr = stderrFile

		cmd := NewRootCmd()
		cmd.SetArgs(args)
		app := clix.App{
			Version: "integration-test",
			Commit:  "test",
			Date:    "test",
			BuiltBy: "test",
		}
		runErr = app.Run(cmd)
	}()

	if err := stdoutFile.Close(); err != nil {
		t.Fatal(err)
	}
	if err := stderrFile.Close(); err != nil {
		t.Fatal(err)
	}
	stdoutBytes, err := os.ReadFile(stdoutFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	stderrBytes, err := os.ReadFile(stderrFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(stdoutBytes), string(stderrBytes), runErr
}

func TestCLIIntegration_DefaultConfigurationDiscovery(t *testing.T) {
	roots := []string{t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()}
	originalRoots := config.SearchRoots
	config.SearchRoots = roots
	t.Cleanup(func() { config.SearchRoots = originalRoots })

	componentDir := filepath.Join(roots[1], "sysupdate.demo.d")
	if err := os.MkdirAll(componentDir, 0755); err != nil {
		t.Fatal(err)
	}
	writeFeatureFile(t, componentDir, "demo", true)

	stdout, stderr, err := runCLI(t, "--silent", "--json", "components")
	if err != nil {
		t.Fatalf("components failed: %v\n%s", err, stderr)
	}
	var components []sdk.ComponentInfo
	if err := json.Unmarshal([]byte(stdout), &components); err != nil {
		t.Fatalf("components returned invalid JSON: %v\n%s", err, stdout)
	}
	if len(components) != 1 || components[0].Name != "demo" ||
		components[0].SourceDir != componentDir || components[0].FeatureCount != 1 {
		t.Errorf("unexpected components: %+v", components)
	}

	stdout, stderr, err = runCLI(t, "--silent", "--json", "features", "list")
	if err != nil {
		t.Fatalf("features list failed: %v\n%s", err, stderr)
	}
	var features []sdk.FeatureInfo
	if err := json.Unmarshal([]byte(stdout), &features); err != nil {
		t.Fatalf("features list returned invalid JSON: %v\n%s", err, stdout)
	}
	if len(features) != 1 || features[0].Name != "demo" || !features[0].Enabled {
		t.Errorf("unexpected default-domain features: %+v", features)
	}
}

func TestCLIIntegration_CatalogDoesNotSendGitHubTokenToCustomOrigin(t *testing.T) {
	authHeader := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader <- r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `[{"name":"zoxide","type":"dir"},{"name":"README.md","type":"file"}]`)
	}))
	defer server.Close()

	catalogRoot := t.TempDir()
	repoConfig := fmt.Sprintf(
		"[Catalog]\nSiteURL=%s\nListURL=%s\nAllowInsecure=yes\n",
		server.URL,
		server.URL,
	)
	if err := os.WriteFile(filepath.Join(catalogRoot, "test.catalog"), []byte(repoConfig), 0644); err != nil {
		t.Fatal(err)
	}

	originalCatalogRoots, originalCacheDir := catalog.ConfigRoots, catalog.CacheDir
	originalSearchRoots := config.SearchRoots
	catalog.ConfigRoots = []string{catalogRoot}
	catalog.CacheDir = t.TempDir()
	config.SearchRoots = []string{t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()}
	t.Cleanup(func() {
		catalog.ConfigRoots = originalCatalogRoots
		catalog.CacheDir = originalCacheDir
		config.SearchRoots = originalSearchRoots
	})
	t.Setenv("GITHUB_TOKEN", "cli-integration-token")

	stdout, stderr, err := runCLI(t,
		"--silent", "--json",
		"catalog", "list", "--repo", "test", "--no-cache",
	)
	if err != nil {
		t.Fatalf("catalog list failed: %v\n%s", err, stderr)
	}
	var entries []sdk.CatalogEntry
	if err := json.Unmarshal([]byte(stdout), &entries); err != nil {
		t.Fatalf("catalog list returned invalid JSON: %v\n%s", err, stdout)
	}
	if len(entries) != 1 || entries[0].Repo != "test" || entries[0].Name != "zoxide" {
		t.Errorf("unexpected catalog entries: %+v", entries)
	}
	if got := <-authHeader; got != "" {
		t.Error("custom catalog origin received authorization")
	}
}
