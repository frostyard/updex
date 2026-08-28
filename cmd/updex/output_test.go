package updex

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostyard/clix"
	"github.com/frostyard/updex/catalog"
	"github.com/frostyard/updex/config"
	"github.com/frostyard/updex/internal/testutil"
	"github.com/spf13/cobra"
)

// errOutputFailed is the sentinel every failing writer below returns, so a
// test can assert with errors.Is that the output failure — and not some
// unrelated error — reached the command's return value.
var errOutputFailed = errors.New("simulated stdout write failure")

// failingWriter accepts exactly allow bytes and then fails with
// errOutputFailed, modelling a redirected stdout that stops accepting data
// (a full filesystem, a closed pipe). allow: 0 fails on the very first write.
type failingWriter struct {
	allow   int
	written int
	calls   int
}

func (w *failingWriter) Write(p []byte) (int, error) {
	w.calls++
	remaining := w.allow - w.written
	if remaining <= 0 {
		return 0, errOutputFailed
	}
	if len(p) <= remaining {
		w.written += len(p)
		return len(p), nil
	}
	w.written = w.allow
	return remaining, errOutputFailed
}

// cmdWithOut returns a bare command whose output writer is out, which is what
// every table-rendering RunE resolves through cobra's OutOrStdout.
func cmdWithOut(t *testing.T, out *failingWriter) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.SetContext(t.Context())
	cmd.SetOut(out)
	return cmd
}

func TestTextTable_FlushReportsWriteFailure(t *testing.T) {
	out := &failingWriter{}
	table := newTextTable(out)
	table.Rowf("A\tB\n")
	table.Rowf("%s\t%s\n", "one", "two")

	if err := table.Flush(); !errors.Is(err, errOutputFailed) {
		t.Fatalf("Flush() = %v, want %v", err, errOutputFailed)
	}
}

// TestTextTable_RowFailureIsReportedByFlush covers the row path directly.
// text/tabwriter buffers a multi-cell line until Flush, but writes a line
// with a single cell (no tab) straight through, so this is the deterministic
// way to make a row write — rather than the flush — fail first.
func TestTextTable_RowFailureIsReportedByFlush(t *testing.T) {
	out := &failingWriter{}
	table := newTextTable(out)
	table.Rowf("single-cell-line\n")

	if table.err == nil {
		t.Fatal("expected the row write failure to be recorded on the table")
	}
	callsAfterFailure := out.calls
	table.Rowf("another-single-cell-line\n")
	if out.calls != callsAfterFailure {
		t.Errorf("rows after a failure still reached the writer: %d calls, want %d", out.calls, callsAfterFailure)
	}

	if err := table.Flush(); !errors.Is(err, errOutputFailed) {
		t.Fatalf("Flush() = %v, want %v", err, errOutputFailed)
	}
}

func TestTextTable_SuccessfulTableIsUnchanged(t *testing.T) {
	var buf bytes.Buffer
	table := newTextTable(&buf)
	table.Rowf("NAME\tCOUNT\n")
	table.Rowf("%s\t%d\n", "alpha", 1)
	table.Rowf("%s\t%d\n", "b", 22)
	if err := table.Flush(); err != nil {
		t.Fatalf("Flush() = %v, want nil", err)
	}

	want := "NAME   COUNT\nalpha  1\nb      22\n"
	if buf.String() != want {
		t.Errorf("table output = %q, want %q", buf.String(), want)
	}
}

func TestWriteLine(t *testing.T) {
	var buf bytes.Buffer
	if err := writeLine(&buf, "No features configured."); err != nil {
		t.Fatalf("writeLine() = %v, want nil", err)
	}
	if buf.String() != "No features configured.\n" {
		t.Errorf("writeLine wrote %q", buf.String())
	}

	if err := writeLine(&failingWriter{}, "No features configured."); !errors.Is(err, errOutputFailed) {
		t.Fatalf("writeLine() = %v, want %v", err, errOutputFailed)
	}
}

func TestRunFeaturesList_ReportsOutputFailure(t *testing.T) {
	oldDefinitions, oldComponent, oldJSONOutput := definitions, featureComponent, clix.JSONOutput
	t.Cleanup(func() {
		definitions = oldDefinitions
		featureComponent = oldComponent
		clix.JSONOutput = oldJSONOutput
	})

	configDir := t.TempDir()
	writeFeatureFile(t, configDir, "testfeature", true)
	definitions = configDir
	featureComponent = ""
	clix.JSONOutput = false

	// The successful table is unchanged and reports no error.
	var buf bytes.Buffer
	okCmd := &cobra.Command{}
	okCmd.SetContext(t.Context())
	okCmd.SetOut(&buf)
	if err := runFeaturesList(okCmd, nil); err != nil {
		t.Fatalf("runFeaturesList on a working writer = %v, want nil", err)
	}
	if !strings.Contains(buf.String(), "FEATURE") || !strings.Contains(buf.String(), "testfeature") {
		t.Fatalf("unexpected table on the success path:\n%s", buf.String())
	}

	if err := runFeaturesList(cmdWithOut(t, &failingWriter{}), nil); !errors.Is(err, errOutputFailed) {
		t.Fatalf("runFeaturesList on a failing writer = %v, want %v", err, errOutputFailed)
	}

	// A writer that dies partway through the flush is reported too.
	partial := &failingWriter{allow: 4}
	if err := runFeaturesList(cmdWithOut(t, partial), nil); !errors.Is(err, errOutputFailed) {
		t.Fatalf("runFeaturesList on a partial writer = %v, want %v", err, errOutputFailed)
	}
	if partial.written != partial.allow {
		t.Errorf("partial writer accepted %d bytes, want %d", partial.written, partial.allow)
	}
}

func TestRunFeaturesList_ReportsEmptyMessageFailure(t *testing.T) {
	oldDefinitions, oldComponent, oldJSONOutput := definitions, featureComponent, clix.JSONOutput
	t.Cleanup(func() {
		definitions = oldDefinitions
		featureComponent = oldComponent
		clix.JSONOutput = oldJSONOutput
	})

	definitions = t.TempDir() // no .feature files: "No features configured."
	featureComponent = ""
	clix.JSONOutput = false

	if err := runFeaturesList(cmdWithOut(t, &failingWriter{}), nil); !errors.Is(err, errOutputFailed) {
		t.Fatalf("runFeaturesList = %v, want %v", err, errOutputFailed)
	}
}

func TestRunComponents_ReportsOutputFailure(t *testing.T) {
	oldDefinitions, oldJSONOutput := definitions, clix.JSONOutput
	oldRoots := config.SearchRoots
	t.Cleanup(func() {
		definitions = oldDefinitions
		clix.JSONOutput = oldJSONOutput
		config.SearchRoots = oldRoots
	})

	root := t.TempDir()
	config.SearchRoots = []string{root}
	componentDir := filepath.Join(root, "sysupdate.testcomponent.d")
	if err := os.MkdirAll(componentDir, 0755); err != nil {
		t.Fatal(err)
	}
	writeFeatureFile(t, componentDir, "testfeature", true)
	definitions = ""
	clix.JSONOutput = false

	var buf bytes.Buffer
	okCmd := &cobra.Command{}
	okCmd.SetContext(t.Context())
	okCmd.SetOut(&buf)
	if err := runComponents(okCmd, nil); err != nil {
		t.Fatalf("runComponents on a working writer = %v, want nil", err)
	}
	if !strings.Contains(buf.String(), "testcomponent") {
		t.Fatalf("expected the component in the table, got:\n%s", buf.String())
	}

	if err := runComponents(cmdWithOut(t, &failingWriter{}), nil); !errors.Is(err, errOutputFailed) {
		t.Fatalf("runComponents on a failing writer = %v, want %v", err, errOutputFailed)
	}
}

func TestRunCatalogList_ReportsOutputFailure(t *testing.T) {
	oldDefinitions, oldJSONOutput := definitions, clix.JSONOutput
	oldRepo, oldNoCache := catalogRepo, catalogNoCache
	oldRoots, oldConfigRoots, oldCacheDir := config.SearchRoots, catalog.ConfigRoots, catalog.CacheDir
	t.Cleanup(func() {
		definitions = oldDefinitions
		clix.JSONOutput = oldJSONOutput
		catalogRepo = oldRepo
		catalogNoCache = oldNoCache
		config.SearchRoots = oldRoots
		catalog.ConfigRoots = oldConfigRoots
		catalog.CacheDir = oldCacheDir
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]string{
			{"name": "zoxide", "type": "dir"},
			{"name": "ripgrep", "type": "dir"},
		})
	}))
	t.Cleanup(server.Close)

	catalogConfigRoot := t.TempDir()
	catalog.ConfigRoots = []string{catalogConfigRoot}
	catalog.CacheDir = t.TempDir()
	config.SearchRoots = []string{t.TempDir()}
	catalogFile := "[Catalog]\nSiteURL=" + server.URL + "\nListURL=" + server.URL + "\nAllowInsecure=yes\n"
	if err := os.WriteFile(filepath.Join(catalogConfigRoot, "testcatalog.catalog"), []byte(catalogFile), 0644); err != nil {
		t.Fatal(err)
	}
	definitions = ""
	catalogRepo = ""
	catalogNoCache = true
	clix.JSONOutput = false

	var buf bytes.Buffer
	okCmd := &cobra.Command{}
	okCmd.SetContext(t.Context())
	okCmd.SetOut(&buf)
	if err := runCatalogList(okCmd, ""); err != nil {
		t.Fatalf("runCatalogList on a working writer = %v, want nil", err)
	}
	if !strings.Contains(buf.String(), "zoxide") {
		t.Fatalf("expected the listing in the table, got:\n%s", buf.String())
	}

	if err := runCatalogList(cmdWithOut(t, &failingWriter{}), ""); !errors.Is(err, errOutputFailed) {
		t.Fatalf("runCatalogList on a failing writer = %v, want %v", err, errOutputFailed)
	}

	// catalog search shares runCatalogList; a matching term still renders a
	// table, so the same output failure must surface.
	if err := runCatalogList(cmdWithOut(t, &failingWriter{}), "zoxide"); !errors.Is(err, errOutputFailed) {
		t.Fatalf("runCatalogList (search) on a failing writer = %v, want %v", err, errOutputFailed)
	}
}

// TestRunFeaturesCheck_JoinsSDKAndOutputErrors is the errors.Join contract:
// a component that cannot be checked and a stdout that cannot be written are
// both reported, so neither failure is masked by the other.
func TestRunFeaturesCheck_JoinsSDKAndOutputErrors(t *testing.T) {
	oldDefinitions, oldComponent, oldJSONOutput := definitions, featureComponent, clix.JSONOutput
	t.Cleanup(func() {
		definitions = oldDefinitions
		featureComponent = oldComponent
		clix.JSONOutput = oldJSONOutput
	})

	configDir := t.TempDir()
	targetDir := t.TempDir()
	server := testutil.NewErrorServer(t, 404)
	defer server.Close()
	writeFeatureFile(t, configDir, "testfeature", true)
	writeFeatureTransferFile(t, configDir, targetDir, "testext", "testfeature", server.URL)

	definitions = configDir
	featureComponent = ""
	clix.JSONOutput = false

	// Baseline: the SDK error alone, with a writer that works.
	var buf bytes.Buffer
	okCmd := &cobra.Command{}
	okCmd.SetContext(t.Context())
	okCmd.SetOut(&buf)
	sdkErr := runFeaturesCheck(okCmd, nil)
	if sdkErr == nil {
		t.Fatal("expected a non-nil SDK error when a component cannot be checked")
	}
	if !strings.Contains(buf.String(), "testext") {
		t.Fatalf("expected the failed component in the table, got:\n%s", buf.String())
	}

	joined := runFeaturesCheck(cmdWithOut(t, &failingWriter{}), nil)
	if !errors.Is(joined, errOutputFailed) {
		t.Fatalf("runFeaturesCheck = %v, want it to wrap %v", joined, errOutputFailed)
	}
	if !strings.Contains(joined.Error(), sdkErr.Error()) {
		t.Fatalf("runFeaturesCheck dropped the SDK error: got %q, want it to contain %q", joined, sdkErr)
	}
}

func TestRunFeaturesUpdate_ReportsOutputFailure(t *testing.T) {
	extContent := []byte("fake extension content")
	server := testutil.NewTestServer(t, testutil.TestServerFiles{
		Files:   map[string]string{"testext_1.0.0.raw": sha256Hex(extContent)},
		Content: map[string][]byte{"testext_1.0.0.raw": extContent},
	})
	defer server.Close()

	configDir := t.TempDir()
	targetDir := t.TempDir()
	writeFeatureFile(t, configDir, "testfeature", true)
	writeFeatureTransferFile(t, configDir, targetDir, "testext", "testfeature", server.URL)

	oldDefinitions, oldNoRefresh, oldNoVac := definitions, noRefresh, featureUpdateNoVac
	oldDryRun, oldJSONOutput, oldGetEUID := clix.DryRun, clix.JSONOutput, getEUID
	t.Cleanup(func() {
		definitions = oldDefinitions
		noRefresh = oldNoRefresh
		featureUpdateNoVac = oldNoVac
		clix.DryRun = oldDryRun
		clix.JSONOutput = oldJSONOutput
		getEUID = oldGetEUID
	})

	definitions = configDir
	noRefresh = true
	featureUpdateNoVac = false
	clix.DryRun = true
	clix.JSONOutput = false
	getEUID = func() int { return 0 }

	// The dry-run banner is the first thing written, so a writer that fails
	// immediately is caught before the table.
	if err := runFeaturesUpdate(cmdWithOut(t, &failingWriter{}), nil); !errors.Is(err, errOutputFailed) {
		t.Fatalf("runFeaturesUpdate on a failing writer = %v, want %v", err, errOutputFailed)
	}

	// Let the banner through and fail during the table flush instead.
	banner := len("[DRY RUN] Previewing feature updates.\n")
	afterBanner := &failingWriter{allow: banner}
	if err := runFeaturesUpdate(cmdWithOut(t, afterBanner), nil); !errors.Is(err, errOutputFailed) {
		t.Fatalf("runFeaturesUpdate on a post-banner failing writer = %v, want %v", err, errOutputFailed)
	}
	if afterBanner.written != banner {
		t.Errorf("post-banner writer accepted %d bytes, want %d", afterBanner.written, banner)
	}
}
