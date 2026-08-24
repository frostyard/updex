package updex

import (
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/frostyard/updex/sysext"
	"github.com/frostyard/updex/updex"
	"github.com/spf13/cobra"
)

// catalogCLIFlags is the package-level flag and seam state the catalog
// mutation handlers (runCatalogAdd / runCatalogRemove) read. Tests set it
// through setCatalogCLIFlags so every field is restored on cleanup.
type catalogCLIFlags struct {
	definitions string
	repo        string
	force       bool
	noRefresh   bool
	dryRun      bool
	jsonOutput  bool
	euid        int
	runner      sysext.SysextRunner
}

func setCatalogCLIFlags(t *testing.T, f catalogCLIFlags) {
	t.Helper()
	oldDefinitions, oldRepo, oldForce, oldNoRefresh := definitions, catalogRepo, catalogRemoveForce, noRefresh
	oldDryRun, oldJSONOutput, oldSilent := clix.DryRun, clix.JSONOutput, clix.Silent
	oldGetEUID, oldRunner := getEUID, sysextRunner
	t.Cleanup(func() {
		definitions, catalogRepo, catalogRemoveForce, noRefresh = oldDefinitions, oldRepo, oldForce, oldNoRefresh
		clix.DryRun, clix.JSONOutput, clix.Silent = oldDryRun, oldJSONOutput, oldSilent
		getEUID, sysextRunner = oldGetEUID, oldRunner
	})

	definitions = f.definitions
	catalogRepo = f.repo
	catalogRemoveForce = f.force
	noRefresh = f.noRefresh
	clix.DryRun = f.dryRun
	clix.JSONOutput = f.jsonOutput
	// Silence the progress reporter so JSON mode leaves only the result on
	// stdout, as `updex --silent --json` does in production.
	clix.Silent = true
	getEUID = func() int { return f.euid }
	sysextRunner = f.runner
}

// catalogCLIFixture is a rootless stand-in for a host with one or more
// catalogs configured: temporary definition search roots (generated files
// land under roots[0]/sysupdate.catalog-<repo>.d), a temporary catalog
// config root, cache dir, target (staging) dir, and sysext link dir, plus a
// fake catalog site serving zoxide/zoxide.conf, SHA256SUMS, and one image.
type catalogCLIFixture struct {
	roots       []string
	catalogRoot string
	targetDir   string
	sysextDir   string
	siteURL     string
}

const (
	catalogTestSysext  = "zoxide"
	catalogTestVersion = "1.0.0"
)

func newCatalogCLIFixture(t *testing.T) *catalogCLIFixture {
	t.Helper()
	fx := &catalogCLIFixture{
		roots:       []string{t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()},
		catalogRoot: t.TempDir(),
		targetDir:   t.TempDir(),
		sysextDir:   t.TempDir(),
	}

	oldRoots, oldConfigRoots, oldCacheDir := config.SearchRoots, catalog.ConfigRoots, catalog.CacheDir
	oldTargetPath, oldSysextDir := catalog.TargetPath, sysext.SysextDir
	t.Cleanup(func() {
		config.SearchRoots, catalog.ConfigRoots, catalog.CacheDir = oldRoots, oldConfigRoots, oldCacheDir
		catalog.TargetPath, sysext.SysextDir = oldTargetPath, oldSysextDir
	})
	config.SearchRoots = fx.roots
	catalog.ConfigRoots = []string{fx.catalogRoot}
	catalog.CacheDir = t.TempDir()
	catalog.TargetPath = fx.targetDir
	sysext.SysextDir = fx.sysextDir

	rawName := fmt.Sprintf("%s-%s.raw", catalogTestSysext, catalogTestVersion)
	rawContent := []byte("fake sysext image for " + catalogTestSysext)
	manifestContent := []byte(fmt.Sprintf("%s  %s\n", sha256Hex(rawContent), rawName))
	manifestSig := testutil.SignManifest(t, manifestContent)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/" + catalogTestSysext + "/" + catalogTestSysext + ".conf":
			// A genuine catalog-published transfer: CurrentSymlink is
			// dropped and Features= injected by catalog.RenderTransfer.
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
CurrentSymlink=/var/lib/extensions/%s.raw
`, server.URL, catalogTestSysext, catalogTestSysext, fx.targetDir, catalogTestSysext, catalogTestSysext)
		case "/" + catalogTestSysext + "/SHA256SUMS":
			_, _ = w.Write(manifestContent)
		case "/" + catalogTestSysext + "/SHA256SUMS.gpg":
			_, _ = w.Write(manifestSig)
		case "/" + catalogTestSysext + "/" + rawName:
			_, _ = w.Write(rawContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	fx.siteURL = server.URL
	return fx
}

// addRepo configures a <name>.catalog pointing at the fake site.
func (fx *catalogCLIFixture) addRepo(t *testing.T, name string) {
	t.Helper()
	content := "[Catalog]\nSiteURL=" + fx.siteURL + "\nAllowInsecure=yes\n"
	if err := os.WriteFile(filepath.Join(fx.catalogRoot, name+".catalog"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// componentDir is where CatalogAdd writes repo's generated definitions.
func (fx *catalogCLIFixture) componentDir(repo string) string {
	return filepath.Join(fx.roots[0], "sysupdate.catalog-"+repo+".d")
}

func (fx *catalogCLIFixture) transferFile(repo string) string {
	return filepath.Join(fx.componentDir(repo), catalogTestSysext+".transfer")
}

func (fx *catalogCLIFixture) featureFile(repo string) string {
	return filepath.Join(fx.componentDir(repo), catalogTestSysext+".feature")
}

func (fx *catalogCLIFixture) dropIn(repo string) string {
	return filepath.Join(fx.componentDir(repo), catalogTestSysext+".feature.d", "00-updex.conf")
}

func (fx *catalogCLIFixture) image() string {
	return filepath.Join(fx.targetDir, fmt.Sprintf("%s-%s.raw", catalogTestSysext, catalogTestVersion))
}

// install performs a real (non-dry-run) CatalogAdd through the SDK so
// remove tests start from the state `catalog add` leaves behind. It uses
// its own mock runner; the fixture's globals are already in place.
func (fx *catalogCLIFixture) install(t *testing.T, repo string) {
	t.Helper()
	client := updex.NewClient(updex.ClientConfig{SysextRunner: &sysext.MockRunner{}})
	if _, err := client.CatalogAdd(t.Context(), catalogTestSysext, updex.CatalogAddOptions{Repo: repo}); err != nil {
		t.Fatalf("seeding catalog add failed: %v", err)
	}
	for _, p := range []string{fx.transferFile(repo), fx.featureFile(repo), fx.dropIn(repo), fx.image()} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("seeding catalog add left %s missing: %v", p, err)
		}
	}
}

// assertUntouched verifies no generated definitions, drop-in, or image
// exist for repo — the invariant every dry run and every refused mutation
// must preserve.
func (fx *catalogCLIFixture) assertUntouched(t *testing.T, repo string) {
	t.Helper()
	assertNotExists(t, fx.componentDir(repo), "component directory")
	assertNotExists(t, fx.image(), "downloaded image")
}

// assertInstalled verifies the full post-add state for repo is present.
func (fx *catalogCLIFixture) assertInstalled(t *testing.T, repo string) {
	t.Helper()
	assertExists(t, fx.transferFile(repo), "generated transfer")
	assertExists(t, fx.featureFile(repo), "generated feature")
	assertExists(t, fx.dropIn(repo), "enable drop-in")
	assertExists(t, fx.image(), "downloaded image")
}

func decodeCatalogAddResult(t *testing.T, output string) updex.CatalogAddResult {
	t.Helper()
	var r updex.CatalogAddResult
	if err := json.Unmarshal([]byte(output), &r); err != nil {
		t.Fatalf("expected a single JSON CatalogAddResult on stdout, got %v:\n%s", err, output)
	}
	return r
}

func decodeCatalogRemoveResult(t *testing.T, output string) updex.CatalogRemoveResult {
	t.Helper()
	var r updex.CatalogRemoveResult
	if err := json.Unmarshal([]byte(output), &r); err != nil {
		t.Fatalf("expected a single JSON CatalogRemoveResult on stdout, got %v:\n%s", err, output)
	}
	return r
}

func runCatalogHandler(t *testing.T, handler func(*cobra.Command, []string) error, arg string) (string, error) {
	t.Helper()
	return captureStdout(t, func() error {
		cmd := &cobra.Command{}
		cmd.SetContext(t.Context())
		return handler(cmd, []string{arg})
	})
}

func TestRunCatalogMutations_PropagateJSONOutputError(t *testing.T) {
	tests := []struct {
		name    string
		handler func(*cobra.Command, []string) error
		install bool
	}{
		{name: "add", handler: runCatalogAdd},
		{name: "remove", handler: runCatalogRemove, install: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newCatalogCLIFixture(t)
			fx.addRepo(t, "fedora")
			if tt.install {
				fx.install(t, "fedora")
			}
			setCatalogCLIFlags(t, catalogCLIFlags{
				repo:       "fedora",
				dryRun:     true,
				jsonOutput: true,
				runner:     &sysext.MockRunner{},
			})
			refuseJSONOutput(t)

			_, err := runCatalogHandler(t, tt.handler, "zoxide")
			if !errors.Is(err, errJSONOutputRefused) {
				t.Fatalf("%s error = %v, want JSON output error", tt.name, err)
			}
		})
	}
}

func TestRunCatalogAdd(t *testing.T) {
	type tc struct {
		name string
		// repos lists the catalogs to configure; every repo serves the
		// same fake site, so two repos make a bare name ambiguous.
		repos     []string
		arg       string
		flags     catalogCLIFlags
		wantErr   string
		wantOut   []string
		wantNoOut []string
		check     func(t *testing.T, fx *catalogCLIFixture, out string, runner *sysext.MockRunner)
	}
	dryRunLine := func(fx *catalogCLIFixture) string {
		return fmt.Sprintf("[DRY RUN] Would add fedora/zoxide (writing %s and %s), enable it, and download extensions.\n",
			fx.transferFile("fedora"), fx.featureFile("fedora"))
	}
	tests := []tc{
		{
			name:    "requires root before parsing or fetching",
			repos:   []string{"fedora"},
			arg:     "zoxide",
			flags:   catalogCLIFlags{euid: 1000},
			wantErr: "this operation requires root privileges",
			check: func(t *testing.T, fx *catalogCLIFixture, out string, _ *sysext.MockRunner) {
				if out != "" {
					t.Errorf("expected no output for a non-root caller, got %q", out)
				}
				fx.assertUntouched(t, "fedora")
			},
		},
		{
			name:    "rejects a malformed reference before touching the client",
			repos:   []string{"fedora"},
			arg:     "a/b/c",
			flags:   catalogCLIFlags{dryRun: true},
			wantErr: `invalid sysext reference "a/b/c"`,
			check: func(t *testing.T, fx *catalogCLIFixture, out string, _ *sysext.MockRunner) {
				if out != "" {
					t.Errorf("expected no output, got %q", out)
				}
				fx.assertUntouched(t, "fedora")
			},
		},
		{
			name:    "rejects a REPO/ prefix that conflicts with --repo",
			repos:   []string{"fedora", "community"},
			arg:     "fedora/zoxide",
			flags:   catalogCLIFlags{dryRun: true, repo: "community"},
			wantErr: `conflicting repos: "fedora" vs --repo "community"`,
		},
		{
			name:  "dry run text with a bare name resolves the only repo",
			repos: []string{"fedora"},
			arg:   "zoxide",
			flags: catalogCLIFlags{dryRun: true},
			check: func(t *testing.T, fx *catalogCLIFixture, out string, runner *sysext.MockRunner) {
				if out != dryRunLine(fx) {
					t.Errorf("unexpected output:\n%s\nwant:\n%s", out, dryRunLine(fx))
				}
				fx.assertUntouched(t, "fedora")
				if runner.RefreshCalled {
					t.Error("dry run must not refresh sysext")
				}
			},
		},
		{
			name:  "dry run text with REPO/NAME",
			repos: []string{"fedora", "community"},
			arg:   "fedora/zoxide",
			flags: catalogCLIFlags{dryRun: true},
			check: func(t *testing.T, fx *catalogCLIFixture, out string, _ *sysext.MockRunner) {
				if out != dryRunLine(fx) {
					t.Errorf("unexpected output:\n%s", out)
				}
				fx.assertUntouched(t, "fedora")
				fx.assertUntouched(t, "community")
			},
		},
		{
			name:  "dry run text with --repo",
			repos: []string{"fedora", "community"},
			arg:   "zoxide",
			flags: catalogCLIFlags{dryRun: true, repo: "fedora"},
			check: func(t *testing.T, fx *catalogCLIFixture, out string, _ *sysext.MockRunner) {
				if out != dryRunLine(fx) {
					t.Errorf("unexpected output:\n%s", out)
				}
				fx.assertUntouched(t, "fedora")
				fx.assertUntouched(t, "community")
			},
		},
		{
			name:    "bare name ambiguous across repos is an error listing candidates",
			repos:   []string{"fedora", "community"},
			arg:     "zoxide",
			flags:   catalogCLIFlags{dryRun: true},
			wantErr: "community/zoxide",
			check: func(t *testing.T, fx *catalogCLIFixture, out string, _ *sysext.MockRunner) {
				if out != "" {
					t.Errorf("expected no output on the error path, got %q", out)
				}
				fx.assertUntouched(t, "fedora")
				fx.assertUntouched(t, "community")
			},
		},
		{
			name:    "unknown --repo is an error with no output",
			repos:   []string{"fedora"},
			arg:     "zoxide",
			flags:   catalogCLIFlags{dryRun: true, repo: "nope"},
			wantErr: "nope",
			check: func(t *testing.T, fx *catalogCLIFixture, out string, _ *sysext.MockRunner) {
				if out != "" {
					t.Errorf("expected no output on the error path, got %q", out)
				}
				fx.assertUntouched(t, "fedora")
			},
		},
		{
			name:    "json error path prints nothing and returns the error",
			repos:   []string{"fedora"},
			arg:     "missing",
			flags:   catalogCLIFlags{dryRun: true, jsonOutput: true},
			wantErr: `"missing" not found`,
			check: func(t *testing.T, _ *catalogCLIFixture, out string, _ *sysext.MockRunner) {
				if out != "" {
					t.Errorf("expected no JSON output when the SDK returns no result, got %q", out)
				}
			},
		},
		{
			name:    "no catalogs configured returns setup guidance",
			repos:   nil,
			arg:     "zoxide",
			flags:   catalogCLIFlags{dryRun: true},
			wantErr: catalog.ErrNoCatalogs.Error(),
		},
		{
			name:    "definitions override is incompatible with catalog operations",
			repos:   []string{"fedora"},
			arg:     "zoxide",
			flags:   catalogCLIFlags{dryRun: true, definitions: "/nonexistent-defs"},
			wantErr: "definitions",
			check: func(t *testing.T, fx *catalogCLIFixture, _ string, _ *sysext.MockRunner) {
				fx.assertUntouched(t, "fedora")
			},
		},
		{
			name:  "dry run json reports the plan without writing",
			repos: []string{"fedora"},
			arg:   "fedora/zoxide",
			flags: catalogCLIFlags{dryRun: true, jsonOutput: true},
			check: func(t *testing.T, fx *catalogCLIFixture, out string, _ *sysext.MockRunner) {
				r := decodeCatalogAddResult(t, out)
				if r.Name != "zoxide" || r.Repo != "fedora" || r.Component != "catalog-fedora" || !r.DryRun {
					t.Errorf("unexpected result: %+v", r)
				}
				if r.TransferFile != fx.transferFile("fedora") || r.FeatureFile != fx.featureFile("fedora") {
					t.Errorf("unexpected file paths: %s / %s", r.TransferFile, r.FeatureFile)
				}
				if r.Enable != nil {
					t.Errorf("dry run must not report an enable step, got %+v", r.Enable)
				}
				fx.assertUntouched(t, "fedora")
			},
		},
		{
			name:    "adds, enables, downloads, and refreshes",
			repos:   []string{"fedora"},
			arg:     "zoxide",
			flags:   catalogCLIFlags{},
			wantOut: []string{"Added fedora/zoxide and enabled feature 'zoxide'.\n", "Downloaded 1 extension(s):\n", "  - zoxide@1.0.0\n"},
			check: func(t *testing.T, fx *catalogCLIFixture, _ string, runner *sysext.MockRunner) {
				fx.assertInstalled(t, "fedora")
				data, err := os.ReadFile(fx.transferFile("fedora"))
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(data), "Features=zoxide") || strings.Contains(string(data), "CurrentSymlink") {
					t.Errorf("generated transfer not rendered by catalog.RenderTransfer:\n%s", data)
				}
				if data, err := os.ReadFile(fx.dropIn("fedora")); err != nil || !strings.Contains(string(data), "Enabled=true") {
					t.Errorf("enable drop-in missing or wrong (%v):\n%s", err, data)
				}
				if !runner.RefreshCalled {
					t.Error("expected sysext refresh after download")
				}
			},
		},
		{
			name:  "--no-refresh is forwarded on a real add",
			repos: []string{"fedora"},
			arg:   "zoxide",
			flags: catalogCLIFlags{noRefresh: true},
			check: func(t *testing.T, fx *catalogCLIFixture, _ string, runner *sysext.MockRunner) {
				fx.assertInstalled(t, "fedora")
				if runner.RefreshCalled {
					t.Error("--no-refresh must be forwarded: refresh was called")
				}
			},
		},
		{
			name:  "--repo selects the repo on a real add",
			repos: []string{"fedora", "community"},
			arg:   "zoxide",
			flags: catalogCLIFlags{repo: "community"},
			wantOut: []string{
				"Added community/zoxide and enabled feature 'zoxide'.\n",
			},
			check: func(t *testing.T, fx *catalogCLIFixture, _ string, _ *sysext.MockRunner) {
				fx.assertInstalled(t, "community")
				assertNotExists(t, fx.componentDir("fedora"), "other repo's component directory")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newCatalogCLIFixture(t)
			for _, repo := range tt.repos {
				fx.addRepo(t, repo)
			}
			runner := &sysext.MockRunner{}
			flags := tt.flags
			flags.runner = runner
			setCatalogCLIFlags(t, flags)

			out, err := runCatalogHandler(t, runCatalogAdd, tt.arg)

			if tt.wantErr == "" && err != nil {
				t.Fatalf("unexpected error: %v\noutput:\n%s", err, out)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("expected error containing %q, got %v\noutput:\n%s", tt.wantErr, err, out)
			}
			assertContains(t, out, tt.wantOut...)
			assertNotContains(t, out, tt.wantNoOut...)
			if tt.check != nil {
				tt.check(t, fx, out, runner)
			}
		})
	}
}

func TestRunCatalogRemove(t *testing.T) {
	type tc struct {
		name  string
		repos []string
		// installed lists the repos whose zoxide is added before the
		// handler runs (through the SDK, mirroring `catalog add`).
		installed []string
		arg       string
		flags     catalogCLIFlags
		wantErr   string
		wantOut   []string
		wantNoOut []string
		check     func(t *testing.T, fx *catalogCLIFixture, out string, runner *sysext.MockRunner)
	}
	const dryRunLine = "[DRY RUN] Would remove fedora/zoxide, its extension files, and generated definitions.\n"
	tests := []tc{
		{
			name:      "requires root before touching anything",
			repos:     []string{"fedora"},
			installed: []string{"fedora"},
			arg:       "zoxide",
			flags:     catalogCLIFlags{euid: 1000, force: true},
			wantErr:   "this operation requires root privileges",
			check: func(t *testing.T, fx *catalogCLIFixture, out string, runner *sysext.MockRunner) {
				if out != "" {
					t.Errorf("expected no output for a non-root caller, got %q", out)
				}
				fx.assertInstalled(t, "fedora")
				if runner.UnmergeCalled || runner.RefreshCalled {
					t.Error("non-root caller must not reach the sysext runner")
				}
			},
		},
		{
			name:      "rejects a malformed reference",
			repos:     []string{"fedora"},
			installed: []string{"fedora"},
			arg:       "/zoxide",
			flags:     catalogCLIFlags{dryRun: true},
			wantErr:   `invalid sysext reference "/zoxide"`,
			check: func(t *testing.T, fx *catalogCLIFixture, _ string, _ *sysext.MockRunner) {
				fx.assertInstalled(t, "fedora")
			},
		},
		{
			name:      "dry run text keeps definitions and images",
			repos:     []string{"fedora"},
			installed: []string{"fedora"},
			arg:       "zoxide",
			flags:     catalogCLIFlags{dryRun: true},
			check: func(t *testing.T, fx *catalogCLIFixture, out string, runner *sysext.MockRunner) {
				if out != dryRunLine {
					t.Errorf("unexpected output:\n%s", out)
				}
				fx.assertInstalled(t, "fedora")
				if runner.UnmergeCalled || runner.RefreshCalled {
					t.Error("dry run must not unmerge or refresh")
				}
			},
		},
		{
			name:      "dry run text with REPO/NAME and --force",
			repos:     []string{"fedora", "community"},
			installed: []string{"fedora"},
			arg:       "fedora/zoxide",
			flags:     catalogCLIFlags{dryRun: true, force: true},
			// The dry-run line replaces the post-removal reboot warning.
			wantNoOut: []string{"Warning:"},
			check: func(t *testing.T, fx *catalogCLIFixture, out string, _ *sysext.MockRunner) {
				if out != dryRunLine {
					t.Errorf("unexpected output:\n%s", out)
				}
				fx.assertInstalled(t, "fedora")
			},
		},
		{
			name:      "dry run json reports would-remove without deleting",
			repos:     []string{"fedora"},
			installed: []string{"fedora"},
			arg:       "zoxide",
			flags:     catalogCLIFlags{dryRun: true, jsonOutput: true, repo: "fedora"},
			check: func(t *testing.T, fx *catalogCLIFixture, out string, _ *sysext.MockRunner) {
				r := decodeCatalogRemoveResult(t, out)
				if r.Name != "zoxide" || r.Repo != "fedora" || r.Component != "catalog-fedora" || !r.DryRun {
					t.Errorf("unexpected result: %+v", r)
				}
				if len(r.RemovedFiles) == 0 {
					t.Error("expected removed_files to preview the definitions")
				}
				for _, f := range r.RemovedFiles {
					if !strings.HasSuffix(f, " (would remove)") {
						t.Errorf("dry run removed_files entry not marked as preview: %q", f)
					}
				}
				if r.Disable == nil || !r.Disable.DryRun || !r.Disable.Success {
					t.Errorf("expected a successful dry-run disable step, got %+v", r.Disable)
				}
				fx.assertInstalled(t, "fedora")
			},
		},
		{
			name:      "unknown --repo is an error with no output",
			repos:     []string{"fedora"},
			installed: []string{"fedora"},
			arg:       "zoxide",
			flags:     catalogCLIFlags{dryRun: true, repo: "nope"},
			wantErr:   "nope",
			check: func(t *testing.T, fx *catalogCLIFixture, out string, _ *sysext.MockRunner) {
				if out != "" {
					t.Errorf("expected no output on the error path, got %q", out)
				}
				fx.assertInstalled(t, "fedora")
			},
		},
		{
			name:      "--repo pointing at a repo that does not manage the sysext is refused",
			repos:     []string{"fedora", "community"},
			installed: []string{"fedora"},
			arg:       "zoxide",
			flags:     catalogCLIFlags{dryRun: true, repo: "community", jsonOutput: true},
			wantErr:   "not a catalog-managed sysext",
			check: func(t *testing.T, fx *catalogCLIFixture, out string, _ *sysext.MockRunner) {
				if out != "" {
					t.Errorf("expected no JSON output when the SDK returns no result, got %q", out)
				}
				fx.assertInstalled(t, "fedora")
			},
		},
		{
			name:    "not catalog-managed is an error",
			repos:   []string{"fedora"},
			arg:     "zoxide",
			flags:   catalogCLIFlags{dryRun: true},
			wantErr: "not a catalog-managed sysext",
		},
		{
			name:      "removes definitions, unmerges, deletes images, and refreshes",
			repos:     []string{"fedora"},
			installed: []string{"fedora"},
			arg:       "zoxide",
			flags:     catalogCLIFlags{},
			wantOut:   []string{"Removed fedora/zoxide.\n", "Deleted 3 definition file(s):\n"},
			wantNoOut: []string{"Warning:"},
			check: func(t *testing.T, fx *catalogCLIFixture, out string, runner *sysext.MockRunner) {
				assertContains(t, out, "  - "+fx.transferFile("fedora")+"\n", "  - "+fx.featureFile("fedora")+"\n", "  - "+fx.dropIn("fedora")+"\n")
				fx.assertUntouched(t, "fedora")
				if !runner.UnmergeCalled {
					t.Error("expected unmerge")
				}
				if !runner.RefreshCalled {
					t.Error("expected refresh")
				}
			},
		},
		{
			name:      "--no-refresh is forwarded on a real remove",
			repos:     []string{"fedora"},
			installed: []string{"fedora"},
			arg:       "fedora/zoxide",
			flags:     catalogCLIFlags{noRefresh: true},
			check: func(t *testing.T, fx *catalogCLIFixture, _ string, runner *sysext.MockRunner) {
				fx.assertUntouched(t, "fedora")
				if !runner.UnmergeCalled {
					t.Error("expected unmerge")
				}
				if runner.RefreshCalled {
					t.Error("--no-refresh must be forwarded: refresh was called")
				}
			},
		},
		{
			name:      "--force warns about reboot and is forwarded to the disable step",
			repos:     []string{"fedora"},
			installed: []string{"fedora"},
			arg:       "zoxide",
			flags:     catalogCLIFlags{force: true},
			wantOut:   []string{"Removed fedora/zoxide.\n", "Warning: Reboot required for changes to take effect.\n"},
			check: func(t *testing.T, fx *catalogCLIFixture, _ string, _ *sysext.MockRunner) {
				fx.assertUntouched(t, "fedora")
			},
		},
		{
			name:      "json reports removed files and the disable step",
			repos:     []string{"fedora", "community"},
			installed: []string{"fedora", "community"},
			arg:       "community/zoxide",
			flags:     catalogCLIFlags{jsonOutput: true, force: true},
			check: func(t *testing.T, fx *catalogCLIFixture, out string, _ *sysext.MockRunner) {
				r := decodeCatalogRemoveResult(t, out)
				if r.Name != "zoxide" || r.Repo != "community" || r.Component != "catalog-community" || r.DryRun {
					t.Errorf("unexpected result: %+v", r)
				}
				if len(r.RemovedFiles) != 3 {
					t.Errorf("expected transfer, feature, and drop-in in removed_files, got %v", r.RemovedFiles)
				}
				if r.Disable == nil || !r.Disable.Success || !r.Disable.Unmerged {
					t.Errorf("expected a successful unmerging disable step, got %+v", r.Disable)
				}
				// Force reaches the SDK: the disable step's message says so.
				if r.Disable != nil && !strings.Contains(r.Disable.NextActionMessage, "Reboot required") {
					t.Errorf("expected --force to reach DisableFeature, got %q", r.Disable.NextActionMessage)
				}
				// The other repo's definitions are untouched; both repos
				// stage the same image, which the shared removal deleted.
				assertExists(t, fx.transferFile("fedora"), "other repo's transfer")
				assertExists(t, fx.featureFile("fedora"), "other repo's feature")
				assertNotExists(t, fx.componentDir("community"), "removed component directory")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newCatalogCLIFixture(t)
			for _, repo := range tt.repos {
				fx.addRepo(t, repo)
			}
			for _, repo := range tt.installed {
				fx.install(t, repo)
			}
			runner := &sysext.MockRunner{}
			flags := tt.flags
			flags.runner = runner
			setCatalogCLIFlags(t, flags)

			out, err := runCatalogHandler(t, runCatalogRemove, tt.arg)

			if tt.wantErr == "" && err != nil {
				t.Fatalf("unexpected error: %v\noutput:\n%s", err, out)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("expected error containing %q, got %v\noutput:\n%s", tt.wantErr, err, out)
			}
			assertContains(t, out, tt.wantOut...)
			assertNotContains(t, out, tt.wantNoOut...)
			if tt.check != nil {
				tt.check(t, fx, out, runner)
			}
		})
	}
}
