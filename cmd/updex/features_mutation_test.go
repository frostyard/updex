package updex

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostyard/clix"
	"github.com/frostyard/updex/config"
	"github.com/frostyard/updex/internal/testutil"
	"github.com/frostyard/updex/sysext"
	"github.com/frostyard/updex/updex"
	"github.com/spf13/cobra"
)

// featureCLIFlags is the package-level flag and seam state the feature
// mutation handlers (runFeaturesEnable / runFeaturesDisable) read. Tests set
// it through setFeatureCLIFlags so every field is restored on cleanup and no
// value leaks between table cases.
type featureCLIFlags struct {
	definitions    string
	component      string
	now            bool
	force          bool
	noRefresh      bool
	dryRun         bool
	jsonOutput     bool
	reportProgress bool
	euid           int
	runner         sysext.SysextRunner
}

func setFeatureCLIFlags(t *testing.T, f featureCLIFlags) {
	t.Helper()
	oldDefinitions, oldComponent, oldNoRefresh := definitions, featureComponent, noRefresh
	oldEnableNow, oldDisableNow, oldDisableForce := featureEnableNow, featureDisableNow, featureDisableForce
	oldDryRun, oldJSONOutput, oldSilent := clix.DryRun, clix.JSONOutput, clix.Silent
	oldGetEUID, oldRunner := getEUID, sysextRunner
	t.Cleanup(func() {
		definitions, featureComponent, noRefresh = oldDefinitions, oldComponent, oldNoRefresh
		featureEnableNow, featureDisableNow, featureDisableForce = oldEnableNow, oldDisableNow, oldDisableForce
		clix.DryRun, clix.JSONOutput, clix.Silent = oldDryRun, oldJSONOutput, oldSilent
		getEUID, sysextRunner = oldGetEUID, oldRunner
	})

	definitions = f.definitions
	featureComponent = f.component
	noRefresh = f.noRefresh
	featureEnableNow = f.now
	featureDisableNow = f.now
	featureDisableForce = f.force
	clix.DryRun = f.dryRun
	clix.JSONOutput = f.jsonOutput
	// Keep handler tests focused on final command output. Download bars ignored
	// clix.Silent before #299, so a real JSON-mode download still guards it.
	clix.Silent = !f.reportProgress
	getEUID = func() int { return f.euid }
	sysextRunner = f.runner
}

// featureCLIFixture is a rootless stand-in for a host: four temporary search
// roots (so drop-ins land under roots[0], the "/etc" analog), a temporary
// sysext link directory, a staging target directory, and a fake HTTP source
// publishing testext_1.0.0.raw.
type featureCLIFixture struct {
	roots     []string
	sysextDir string
	targetDir string
	serverURL string
	extBytes  []byte
}

func newFeatureCLIFixture(t *testing.T) *featureCLIFixture {
	t.Helper()
	fx := &featureCLIFixture{
		roots:     []string{t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()},
		sysextDir: t.TempDir(),
		targetDir: t.TempDir(),
		extBytes:  []byte("fake extension content"),
	}
	server := testutil.NewTestServer(t, testutil.TestServerFiles{
		Files:   map[string]string{"testext_1.0.0.raw": sha256Hex(fx.extBytes)},
		Content: map[string][]byte{"testext_1.0.0.raw": fx.extBytes},
	})
	t.Cleanup(server.Close)
	fx.serverURL = server.URL

	oldRoots, oldSysextDir := config.SearchRoots, sysext.SysextDir
	t.Cleanup(func() {
		config.SearchRoots = oldRoots
		sysext.SysextDir = oldSysextDir
	})
	config.SearchRoots = fx.roots
	sysext.SysextDir = fx.sysextDir
	return fx
}

// writeDefinitions writes testfeature.feature and testext.transfer into dir.
func (fx *featureCLIFixture) writeDefinitions(t *testing.T, dir string, enabled bool) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	writeFeatureFile(t, dir, "testfeature", enabled)
	writeFeatureTransferFile(t, dir, fx.targetDir, "testext", "testfeature", fx.serverURL)
}

// stageInstalled seeds the target directory as if testext 1.0.0 had already
// been downloaded; active additionally points the CurrentSymlink at it, which
// is what DisableFeature reads as "merged" (see sysext.GetActiveVersionAt).
func (fx *featureCLIFixture) stageInstalled(t *testing.T, active bool) {
	t.Helper()
	if err := os.WriteFile(fx.stagedImage(), fx.extBytes, 0644); err != nil {
		t.Fatal(err)
	}
	if active {
		if err := os.Symlink("testext_1.0.0.raw", fx.currentSymlink()); err != nil {
			t.Fatal(err)
		}
	}
}

func (fx *featureCLIFixture) stagedImage() string {
	return filepath.Join(fx.targetDir, "testext_1.0.0.raw")
}
func (fx *featureCLIFixture) currentSymlink() string {
	return filepath.Join(fx.targetDir, "testext.raw")
}

// dropInPath is where the handler writes updex's drop-in for testfeature:
// under roots[0]/sysupdate.d for the legacy default (or a -C override), and
// under roots[0]/sysupdate.<component>.d for a component-scoped feature.
func (fx *featureCLIFixture) dropInPath(component string) string {
	return filepath.Join(config.EtcComponentDirIn(component, fx.roots), "testfeature.feature.d", "00-updex.conf")
}

func assertNotExists(t *testing.T, path, what string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Errorf("expected %s to be absent at %s (stat err=%v)", what, path, err)
	}
}

func assertExists(t *testing.T, path, what string) {
	t.Helper()
	if _, err := os.Lstat(path); err != nil {
		t.Errorf("expected %s at %s: %v", what, path, err)
	}
}

func assertContains(t *testing.T, output string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(output, want) {
			t.Errorf("expected output to contain %q, got:\n%s", want, output)
		}
	}
}

func assertNotContains(t *testing.T, output string, unwanted ...string) {
	t.Helper()
	for _, s := range unwanted {
		if strings.Contains(output, s) {
			t.Errorf("expected output not to contain %q, got:\n%s", s, output)
		}
	}
}

func decodeActionResult(t *testing.T, output string) updex.FeatureActionResult {
	t.Helper()
	var result updex.FeatureActionResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("expected a single JSON FeatureActionResult on stdout, got %v:\n%s", err, output)
	}
	return result
}

func decodeLastActionResult(t *testing.T, output string) updex.FeatureActionResult {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(output))
	var records []json.RawMessage
	for {
		var record json.RawMessage
		err := decoder.Decode(&record)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("expected a valid JSON stream on stdout, got %v:\n%s", err, output)
		}
		records = append(records, record)
	}
	if len(records) == 0 {
		t.Fatal("expected at least one JSON record on stdout")
	}

	var result updex.FeatureActionResult
	if err := json.Unmarshal(records[len(records)-1], &result); err != nil {
		t.Fatalf("expected the last JSON record to be a FeatureActionResult: %v", err)
	}
	return result
}

func runFeatureHandler(t *testing.T, handler func(*cobra.Command, []string) error, feature string) (string, error) {
	t.Helper()
	return captureStdout(t, func() error {
		cmd := &cobra.Command{}
		cmd.SetContext(t.Context())
		return handler(cmd, []string{feature})
	})
}

var errJSONOutputRefused = errors.New("json output refused")

type refusingJSONWriter struct{}

func (refusingJSONWriter) Write([]byte) (int, error) {
	return 0, errJSONOutputRefused
}

func refuseJSONOutput(t *testing.T) {
	t.Helper()
	old := clix.Stdout
	clix.Stdout = refusingJSONWriter{}
	t.Cleanup(func() { clix.Stdout = old })
}

func TestRunFeatureMutations_PropagateJSONOutputError(t *testing.T) {
	tests := []struct {
		name    string
		handler func(*cobra.Command, []string) error
		enabled bool
	}{
		{name: "enable", handler: runFeaturesEnable, enabled: false},
		{name: "disable", handler: runFeaturesDisable, enabled: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newFeatureCLIFixture(t)
			definitionsDir := t.TempDir()
			fx.writeDefinitions(t, definitionsDir, tt.enabled)
			setFeatureCLIFlags(t, featureCLIFlags{
				definitions: definitionsDir,
				dryRun:      true,
				jsonOutput:  true,
				runner:      &sysext.MockRunner{},
			})
			refuseJSONOutput(t)

			_, err := runFeatureHandler(t, tt.handler, "testfeature")
			if !errors.Is(err, errJSONOutputRefused) {
				t.Fatalf("%s error = %v, want JSON output error", tt.name, err)
			}
		})
	}
}

func TestRunFeaturesEnable_JoinsSDKAndJSONOutputErrors(t *testing.T) {
	fx := newFeatureCLIFixture(t)
	definitionsDir := t.TempDir()
	fx.writeDefinitions(t, definitionsDir, false)
	refreshErr := errors.New("refresh refused")
	setFeatureCLIFlags(t, featureCLIFlags{
		definitions: definitionsDir,
		now:         true,
		jsonOutput:  true,
		runner:      &sysext.MockRunner{RefreshErr: refreshErr},
	})
	refuseJSONOutput(t)

	_, err := runFeatureHandler(t, runFeaturesEnable, "testfeature")
	if !errors.Is(err, refreshErr) {
		t.Errorf("error = %v, want SDK refresh error", err)
	}
	if !errors.Is(err, errJSONOutputRefused) {
		t.Errorf("error = %v, want JSON output error", err)
	}
}

func TestRunFeaturesEnable(t *testing.T) {
	type tc struct {
		name string
		// scope selects where the definitions live: "definitions" writes them
		// into a directory passed as -C; "component" writes them into
		// roots[0]/sysupdate.demo.d and scopes the command with --component.
		scope      string
		flags      featureCLIFlags
		refreshErr error
		wantErr    string
		wantOut    []string
		wantNoOut  []string
		check      func(t *testing.T, fx *featureCLIFixture, out string, runner *sysext.MockRunner)
	}
	tests := []tc{
		{
			name:  "requires root before touching anything",
			scope: "definitions",
			flags: featureCLIFlags{euid: 1000},
			// The exact wording is asserted so a future rewording of the
			// requireRoot guard is a deliberate change.
			wantErr: "this operation requires root privileges",
			check: func(t *testing.T, fx *featureCLIFixture, out string, _ *sysext.MockRunner) {
				if out != "" {
					t.Errorf("expected no output for a non-root caller, got %q", out)
				}
				assertNotExists(t, fx.dropInPath(""), "drop-in")
			},
		},
		{
			name:    "dry run text without --now",
			scope:   "definitions",
			flags:   featureCLIFlags{dryRun: true},
			wantOut: []string{"[DRY RUN] Dry run complete. Would enable feature 'testfeature'\n"},
			// Text mode must not leak the "download extensions" tail or the
			// non-dry-run hints.
			wantNoOut: []string{"and download extensions", "enabled.", "Run 'updex features update'"},
			check: func(t *testing.T, fx *featureCLIFixture, _ string, runner *sysext.MockRunner) {
				assertNotExists(t, fx.dropInPath(""), "drop-in")
				assertNotExists(t, fx.stagedImage(), "downloaded image")
				if runner.RefreshCalled {
					t.Error("dry run must not refresh sysext")
				}
			},
		},
		{
			name:      "dry run text with --now",
			scope:     "definitions",
			flags:     featureCLIFlags{dryRun: true, now: true},
			wantOut:   []string{"[DRY RUN] Dry run complete. Would enable feature 'testfeature' and download extensions\n"},
			wantNoOut: []string{"Downloaded"},
			check: func(t *testing.T, fx *featureCLIFixture, _ string, runner *sysext.MockRunner) {
				assertNotExists(t, fx.dropInPath(""), "drop-in")
				assertNotExists(t, fx.stagedImage(), "downloaded image")
				if runner.RefreshCalled {
					t.Error("dry run must not refresh sysext")
				}
			},
		},
		{
			name:  "dry run json with --now reports the plan and no drop-in",
			scope: "definitions",
			flags: featureCLIFlags{dryRun: true, now: true, jsonOutput: true},
			check: func(t *testing.T, fx *featureCLIFixture, out string, _ *sysext.MockRunner) {
				r := decodeActionResult(t, out)
				if !r.Success || !r.DryRun || r.Action != "enable" || r.Feature != "testfeature" || r.Error != "" {
					t.Errorf("unexpected result: %+v", r)
				}
				if r.DropIn != "" {
					t.Errorf("dry run must not report a written drop-in, got %q", r.DropIn)
				}
				if len(r.DownloadedFiles) != 1 || r.DownloadedFiles[0] != "testext (would download)" {
					t.Errorf("unexpected downloaded_files: %v", r.DownloadedFiles)
				}
				if r.NextActionMessage != "Dry run complete. Would enable feature 'testfeature' and download extensions" {
					t.Errorf("unexpected next_action_message: %q", r.NextActionMessage)
				}
				assertNotExists(t, fx.dropInPath(""), "drop-in")
				assertNotExists(t, fx.stagedImage(), "downloaded image")
			},
		},
		{
			name:    "dry run scoped to a named component",
			scope:   "component",
			flags:   featureCLIFlags{dryRun: true, component: "demo"},
			wantOut: []string{"[DRY RUN] Dry run complete. Would enable feature 'testfeature'\n"},
			check: func(t *testing.T, fx *featureCLIFixture, _ string, _ *sysext.MockRunner) {
				assertNotExists(t, fx.dropInPath("demo"), "component drop-in")
				assertNotExists(t, fx.dropInPath(""), "legacy drop-in")
			},
		},
		{
			name:    "component scope excludes features from other components",
			scope:   "component",
			flags:   featureCLIFlags{dryRun: true, component: "other"},
			wantErr: "feature 'testfeature' not found",
			wantOut: []string{"Error: feature 'testfeature' not found\n"},
		},
		{
			name:    "definitions and component together are rejected",
			scope:   "definitions",
			flags:   featureCLIFlags{dryRun: true, component: "demo", jsonOutput: true},
			wantErr: "component",
			check: func(t *testing.T, _ *featureCLIFixture, out string, _ *sysext.MockRunner) {
				r := decodeActionResult(t, out)
				if r.Success || r.Error == "" || r.Action != "enable" {
					t.Errorf("expected a failed enable result carrying the error, got %+v", r)
				}
			},
		},
		{
			name:    "unknown feature reports the error in text mode",
			scope:   "definitions",
			flags:   featureCLIFlags{dryRun: true},
			wantErr: "feature 'missing' not found",
			wantOut: []string{"Error: feature 'missing' not found\n"},
		},
		{
			name:    "writes the drop-in without downloading when --now is absent",
			scope:   "definitions",
			flags:   featureCLIFlags{},
			wantOut: []string{"Feature 'testfeature' enabled.\n", "Run 'updex features update' to download extensions.\n"},
			check: func(t *testing.T, fx *featureCLIFixture, _ string, runner *sysext.MockRunner) {
				content, err := os.ReadFile(fx.dropInPath(""))
				if err != nil {
					t.Fatalf("expected drop-in to be written: %v", err)
				}
				if string(content) != "[Feature]\nEnabled=true\n" {
					t.Errorf("unexpected drop-in content %q", content)
				}
				assertNotExists(t, fx.stagedImage(), "downloaded image")
				if runner.RefreshCalled {
					t.Error("enable without --now must not refresh sysext")
				}
			},
		},
		{
			name:    "--now downloads and refreshes",
			scope:   "definitions",
			flags:   featureCLIFlags{now: true},
			wantOut: []string{"Feature 'testfeature' enabled.\n", "Downloaded 1 extension(s):\n", "  - testext@1.0.0\n"},
			// With --now the "run update" hint is wrong and must not print.
			wantNoOut: []string{"Run 'updex features update'"},
			check: func(t *testing.T, fx *featureCLIFixture, _ string, runner *sysext.MockRunner) {
				assertExists(t, fx.dropInPath(""), "drop-in")
				got, err := os.ReadFile(fx.stagedImage())
				if err != nil {
					t.Fatalf("expected the image to be downloaded: %v", err)
				}
				if string(got) != string(fx.extBytes) {
					t.Error("downloaded image content mismatch")
				}
				if !runner.RefreshCalled {
					t.Error("expected sysext refresh after --now download")
				}
			},
		},
		{
			name:  "--now --no-refresh downloads without refreshing",
			scope: "definitions",
			flags: featureCLIFlags{now: true, noRefresh: true},
			check: func(t *testing.T, fx *featureCLIFixture, _ string, runner *sysext.MockRunner) {
				assertExists(t, fx.stagedImage(), "downloaded image")
				if runner.RefreshCalled {
					t.Error("--no-refresh must be forwarded: refresh was called")
				}
			},
		},
		{
			name:  "--now json and silent emit only the result on stdout",
			scope: "definitions",
			flags: featureCLIFlags{now: true, jsonOutput: true},
			check: func(t *testing.T, fx *featureCLIFixture, out string, runner *sysext.MockRunner) {
				r := decodeActionResult(t, out)
				if !r.Success || r.DryRun || r.Error != "" || r.Action != "enable" {
					t.Errorf("unexpected result: %+v", r)
				}
				if len(r.DownloadedFiles) != 1 || r.DownloadedFiles[0] != "testext@1.0.0" {
					t.Errorf("unexpected downloaded_files: %v", r.DownloadedFiles)
				}
				assertExists(t, fx.stagedImage(), "downloaded image")
				if !runner.RefreshCalled {
					t.Error("expected sysext refresh after --now download")
				}
			},
		},
		{
			name:  "--now json progress remains a valid JSON stream",
			scope: "definitions",
			flags: featureCLIFlags{now: true, jsonOutput: true, reportProgress: true},
			check: func(t *testing.T, fx *featureCLIFixture, out string, runner *sysext.MockRunner) {
				r := decodeLastActionResult(t, out)
				if !r.Success || r.DryRun || r.Error != "" || r.Action != "enable" {
					t.Errorf("unexpected result: %+v", r)
				}
				if len(r.DownloadedFiles) != 1 || r.DownloadedFiles[0] != "testext@1.0.0" {
					t.Errorf("unexpected downloaded_files: %v", r.DownloadedFiles)
				}
				assertExists(t, fx.stagedImage(), "downloaded image")
				if !runner.RefreshCalled {
					t.Error("expected sysext refresh after --now download")
				}
			},
		},
		{
			name:  "json without --now reports the written drop-in",
			scope: "definitions",
			flags: featureCLIFlags{jsonOutput: true},
			check: func(t *testing.T, fx *featureCLIFixture, out string, _ *sysext.MockRunner) {
				r := decodeActionResult(t, out)
				if !r.Success || r.DryRun || r.Error != "" || r.Action != "enable" {
					t.Errorf("unexpected result: %+v", r)
				}
				if r.DropIn != fx.dropInPath("") {
					t.Errorf("drop_in = %q, want %q", r.DropIn, fx.dropInPath(""))
				}
				if len(r.DownloadedFiles) != 0 {
					t.Errorf("expected no downloaded_files without --now, got %v", r.DownloadedFiles)
				}
				if r.NextActionMessage != "Feature 'testfeature' enabled. Run 'updex features update' to download extensions." {
					t.Errorf("unexpected next_action_message: %q", r.NextActionMessage)
				}
			},
		},
		{
			name:    "component-scoped enable writes the drop-in under the component dir",
			scope:   "component",
			flags:   featureCLIFlags{component: "demo"},
			wantOut: []string{"Feature 'testfeature' enabled.\n"},
			check: func(t *testing.T, fx *featureCLIFixture, _ string, _ *sysext.MockRunner) {
				assertExists(t, fx.dropInPath("demo"), "component drop-in")
				assertNotExists(t, fx.dropInPath(""), "legacy drop-in")
			},
		},
		{
			// A failed refresh after a successful download must not read as
			// success: the command exits non-zero and the text shows what was
			// done, the failure, and how to activate.
			name:       "--now with a failing refresh reports the download and the error",
			scope:      "definitions",
			flags:      featureCLIFlags{now: true},
			refreshErr: errors.New("systemd-sysext refresh: exit status 1"),
			wantErr:    "sysext refresh failed",
			wantOut:    []string{"Feature 'testfeature' enabled.\n", "Downloaded 1 extension(s):\n", "Error: sysext refresh failed: systemd-sysext refresh: exit status 1\n", "run 'systemd-sysext refresh' (or reboot)"},
			check: func(t *testing.T, fx *featureCLIFixture, _ string, _ *sysext.MockRunner) {
				assertExists(t, fx.dropInPath(""), "drop-in")
				assertExists(t, fx.stagedImage(), "downloaded image")
			},
		},
		{
			name:       "--now json with a failing refresh still emits the result",
			scope:      "definitions",
			flags:      featureCLIFlags{now: true, jsonOutput: true},
			refreshErr: errors.New("systemd-sysext refresh: exit status 1"),
			wantErr:    "sysext refresh failed",
			check: func(t *testing.T, fx *featureCLIFixture, out string, _ *sysext.MockRunner) {
				r := decodeActionResult(t, out)
				if r.Success || r.RefreshError == "" || r.Error != r.RefreshError {
					t.Errorf("expected a refresh-failed result, got %+v", r)
				}
				if len(r.DownloadedFiles) != 1 || r.DropIn == "" {
					t.Errorf("download and drop-in must still be reported: %+v", r)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newFeatureCLIFixture(t)
			flags := tt.flags
			switch tt.scope {
			case "definitions":
				dir := t.TempDir()
				fx.writeDefinitions(t, dir, false)
				flags.definitions = dir
			case "component":
				fx.writeDefinitions(t, filepath.Join(fx.roots[0], "sysupdate.demo.d"), false)
			default:
				t.Fatalf("unknown scope %q", tt.scope)
			}
			runner := &sysext.MockRunner{RefreshErr: tt.refreshErr}
			flags.runner = runner
			setFeatureCLIFlags(t, flags)

			feature := "testfeature"
			if strings.Contains(tt.name, "unknown feature") {
				feature = "missing"
			}
			out, err := runFeatureHandler(t, runFeaturesEnable, feature)

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

func TestRunFeaturesDisable(t *testing.T) {
	type tc struct {
		name  string
		scope string
		// staged seeds testext_1.0.0.raw in the target dir; active also
		// points the CurrentSymlink at it so the SDK treats it as merged.
		staged, active bool
		flags          featureCLIFlags
		refreshErr     error
		wantErr        string
		wantOut        []string
		wantNoOut      []string
		check          func(t *testing.T, fx *featureCLIFixture, out string, runner *sysext.MockRunner)
	}
	tests := []tc{
		{
			name:    "requires root before touching anything",
			scope:   "definitions",
			staged:  true,
			flags:   featureCLIFlags{euid: 1000, now: true, force: true},
			wantErr: "this operation requires root privileges",
			check: func(t *testing.T, fx *featureCLIFixture, out string, runner *sysext.MockRunner) {
				if out != "" {
					t.Errorf("expected no output for a non-root caller, got %q", out)
				}
				assertNotExists(t, fx.dropInPath(""), "drop-in")
				assertExists(t, fx.stagedImage(), "staged image")
				if runner.UnmergeCalled || runner.RefreshCalled {
					t.Error("non-root caller must not reach the sysext runner")
				}
			},
		},
		{
			name:      "dry run text without --now",
			scope:     "definitions",
			staged:    true,
			flags:     featureCLIFlags{dryRun: true},
			wantOut:   []string{"[DRY RUN] Dry run complete. Would disable feature 'testfeature'\n"},
			wantNoOut: []string{"and remove extension files", "disabled.", "Warning:"},
			check: func(t *testing.T, fx *featureCLIFixture, _ string, runner *sysext.MockRunner) {
				assertNotExists(t, fx.dropInPath(""), "drop-in")
				assertExists(t, fx.stagedImage(), "staged image")
				if runner.UnmergeCalled || runner.RefreshCalled {
					t.Error("dry run must not unmerge or refresh")
				}
			},
		},
		{
			name:      "dry run text with --now keeps files",
			scope:     "definitions",
			staged:    true,
			flags:     featureCLIFlags{dryRun: true, now: true},
			wantOut:   []string{"[DRY RUN] Dry run complete. Would disable feature 'testfeature' and remove extension files\n"},
			wantNoOut: []string{"Removed", "unmerged"},
			check: func(t *testing.T, fx *featureCLIFixture, _ string, runner *sysext.MockRunner) {
				assertNotExists(t, fx.dropInPath(""), "drop-in")
				assertExists(t, fx.stagedImage(), "staged image")
				if runner.UnmergeCalled || runner.RefreshCalled {
					t.Error("dry run must not unmerge or refresh")
				}
			},
		},
		{
			name:   "dry run json with --now reports would-remove and no drop-in",
			scope:  "definitions",
			staged: true,
			flags:  featureCLIFlags{dryRun: true, now: true, jsonOutput: true},
			check: func(t *testing.T, fx *featureCLIFixture, out string, _ *sysext.MockRunner) {
				r := decodeActionResult(t, out)
				if !r.Success || !r.DryRun || r.Action != "disable" || r.Feature != "testfeature" || r.Error != "" || r.Unmerged {
					t.Errorf("unexpected result: %+v", r)
				}
				if r.DropIn != "" {
					t.Errorf("dry run must not report a written drop-in, got %q", r.DropIn)
				}
				if len(r.RemovedFiles) != 1 || r.RemovedFiles[0] != "testext (would remove)" {
					t.Errorf("unexpected removed_files: %v", r.RemovedFiles)
				}
				assertExists(t, fx.stagedImage(), "staged image")
			},
		},
		{
			name:    "dry run --now refuses an active extension without --force",
			scope:   "definitions",
			staged:  true,
			active:  true,
			flags:   featureCLIFlags{dryRun: true, now: true},
			wantErr: "requires --force",
			wantOut: []string{"Error: Extension testext (version 1.0.0) is active. Removing requires --force and a reboot to take effect.\n"},
			check: func(t *testing.T, fx *featureCLIFixture, _ string, _ *sysext.MockRunner) {
				assertExists(t, fx.stagedImage(), "staged image")
				assertExists(t, fx.currentSymlink(), "current symlink")
			},
		},
		{
			name:    "dry run --now --force previews removal of an active extension",
			scope:   "definitions",
			staged:  true,
			active:  true,
			flags:   featureCLIFlags{dryRun: true, now: true, force: true},
			wantOut: []string{"[DRY RUN] Dry run complete. Would disable feature 'testfeature' and remove extension files\n"},
			check: func(t *testing.T, fx *featureCLIFixture, _ string, runner *sysext.MockRunner) {
				assertExists(t, fx.stagedImage(), "staged image")
				assertExists(t, fx.currentSymlink(), "current symlink")
				assertNotExists(t, fx.dropInPath(""), "drop-in")
				if runner.UnmergeCalled {
					t.Error("dry run must not unmerge")
				}
			},
		},
		{
			name:    "dry run scoped to a named component",
			scope:   "component",
			flags:   featureCLIFlags{dryRun: true, component: "demo"},
			wantOut: []string{"[DRY RUN] Dry run complete. Would disable feature 'testfeature'\n"},
			check: func(t *testing.T, fx *featureCLIFixture, _ string, _ *sysext.MockRunner) {
				assertNotExists(t, fx.dropInPath("demo"), "component drop-in")
				assertNotExists(t, fx.dropInPath(""), "legacy drop-in")
			},
		},
		{
			name:    "component scope excludes features from other components",
			scope:   "component",
			flags:   featureCLIFlags{dryRun: true, component: "other"},
			wantErr: "feature 'testfeature' not found",
			wantOut: []string{"Error: feature 'testfeature' not found\n"},
		},
		{
			name:    "definitions and component together are rejected",
			scope:   "definitions",
			flags:   featureCLIFlags{dryRun: true, component: "demo", jsonOutput: true},
			wantErr: "component",
			check: func(t *testing.T, _ *featureCLIFixture, out string, _ *sysext.MockRunner) {
				r := decodeActionResult(t, out)
				if r.Success || r.Error == "" || r.Action != "disable" {
					t.Errorf("expected a failed disable result carrying the error, got %+v", r)
				}
			},
		},
		{
			name:      "writes the drop-in and keeps files when --now is absent",
			scope:     "definitions",
			staged:    true,
			flags:     featureCLIFlags{},
			wantOut:   []string{"Feature 'testfeature' disabled.\n", "Run 'updex features update' to apply changes.\n"},
			wantNoOut: []string{"unmerged", "Removed", "Warning:"},
			check: func(t *testing.T, fx *featureCLIFixture, _ string, runner *sysext.MockRunner) {
				content, err := os.ReadFile(fx.dropInPath(""))
				if err != nil {
					t.Fatalf("expected drop-in to be written: %v", err)
				}
				if string(content) != "[Feature]\nEnabled=false\n" {
					t.Errorf("unexpected drop-in content %q", content)
				}
				assertExists(t, fx.stagedImage(), "staged image")
				if runner.UnmergeCalled || runner.RefreshCalled {
					t.Error("disable without --now must not touch the sysext runner")
				}
			},
		},
		{
			name:      "--now unmerges, removes files, and refreshes",
			scope:     "definitions",
			staged:    true,
			flags:     featureCLIFlags{now: true},
			wantOut:   []string{"Feature 'testfeature' disabled.\n", "Extensions unmerged.\n", "Removed 1 file(s):\n"},
			wantNoOut: []string{"Run 'updex features update'", "Warning:"},
			check: func(t *testing.T, fx *featureCLIFixture, out string, runner *sysext.MockRunner) {
				assertContains(t, out, "  - "+fx.stagedImage()+"\n")
				assertNotExists(t, fx.stagedImage(), "staged image")
				assertExists(t, fx.dropInPath(""), "drop-in")
				if !runner.UnmergeCalled {
					t.Error("expected unmerge with --now")
				}
				if !runner.RefreshCalled {
					t.Error("expected refresh with --now")
				}
			},
		},
		{
			name:   "--now --no-refresh unmerges without refreshing",
			scope:  "definitions",
			staged: true,
			flags:  featureCLIFlags{now: true, noRefresh: true},
			check: func(t *testing.T, fx *featureCLIFixture, _ string, runner *sysext.MockRunner) {
				assertNotExists(t, fx.stagedImage(), "staged image")
				if !runner.UnmergeCalled {
					t.Error("expected unmerge with --now")
				}
				if runner.RefreshCalled {
					t.Error("--no-refresh must be forwarded: refresh was called")
				}
			},
		},
		{
			name:    "--now without --force refuses an active extension and changes nothing",
			scope:   "definitions",
			staged:  true,
			active:  true,
			flags:   featureCLIFlags{now: true},
			wantErr: "requires --force",
			wantOut: []string{"Error: Extension testext (version 1.0.0) is active."},
			check: func(t *testing.T, fx *featureCLIFixture, _ string, runner *sysext.MockRunner) {
				assertNotExists(t, fx.dropInPath(""), "drop-in")
				assertExists(t, fx.stagedImage(), "staged image")
				assertExists(t, fx.currentSymlink(), "current symlink")
				if runner.UnmergeCalled || runner.RefreshCalled {
					t.Error("refused disable must not touch the sysext runner")
				}
			},
		},
		{
			name:    "--now --force removes an active extension and warns about reboot",
			scope:   "definitions",
			staged:  true,
			active:  true,
			flags:   featureCLIFlags{now: true, force: true},
			wantOut: []string{"Feature 'testfeature' disabled.\n", "Extensions unmerged.\n", "Removed 2 file(s):\n", "Warning: Reboot required for changes to take effect.\n"},
			// --force replaces the "run update" hint with the reboot warning.
			wantNoOut: []string{"Run 'updex features update'"},
			check: func(t *testing.T, fx *featureCLIFixture, _ string, runner *sysext.MockRunner) {
				assertNotExists(t, fx.stagedImage(), "staged image")
				assertNotExists(t, fx.currentSymlink(), "current symlink")
				if !runner.UnmergeCalled || !runner.RefreshCalled {
					t.Error("expected unmerge and refresh with --now --force")
				}
			},
		},
		{
			name:   "--now --force json reports unmerge and removed files",
			scope:  "definitions",
			staged: true,
			active: true,
			flags:  featureCLIFlags{now: true, force: true, jsonOutput: true},
			check: func(t *testing.T, fx *featureCLIFixture, out string, _ *sysext.MockRunner) {
				r := decodeActionResult(t, out)
				if !r.Success || r.DryRun || r.Error != "" || !r.Unmerged {
					t.Errorf("unexpected result: %+v", r)
				}
				if r.DropIn != fx.dropInPath("") {
					t.Errorf("drop_in = %q, want %q", r.DropIn, fx.dropInPath(""))
				}
				if len(r.RemovedFiles) != 2 {
					t.Errorf("expected symlink and image in removed_files, got %v", r.RemovedFiles)
				}
				if !strings.Contains(r.NextActionMessage, "Reboot required") {
					t.Errorf("unexpected next_action_message: %q", r.NextActionMessage)
				}
			},
		},
		{
			name:    "component-scoped disable writes the drop-in under the component dir",
			scope:   "component",
			flags:   featureCLIFlags{component: "demo"},
			wantOut: []string{"Feature 'testfeature' disabled.\n"},
			check: func(t *testing.T, fx *featureCLIFixture, _ string, _ *sysext.MockRunner) {
				assertExists(t, fx.dropInPath("demo"), "component drop-in")
				assertNotExists(t, fx.dropInPath(""), "legacy drop-in")
			},
		},
		{
			// Unmerge already ran and the files are gone; a failed re-merge
			// refresh leaves every extension unmerged, so the command must say
			// so and exit non-zero instead of printing the reboot hint.
			name:       "--now --force with a failing refresh reports the unmerged state",
			scope:      "definitions",
			staged:     true,
			active:     true,
			flags:      featureCLIFlags{now: true, force: true},
			refreshErr: errors.New("systemd-sysext refresh: exit status 1"),
			wantErr:    "sysext refresh failed",
			wantOut:    []string{"Feature 'testfeature' disabled.\n", "Extensions unmerged.\n", "Removed 2 file(s):\n", "Error: sysext refresh failed: systemd-sysext refresh: exit status 1\n", "all extensions are currently unmerged"},
			wantNoOut:  []string{"Warning: Reboot required"},
			check: func(t *testing.T, fx *featureCLIFixture, _ string, runner *sysext.MockRunner) {
				assertNotExists(t, fx.stagedImage(), "staged image")
				if !runner.UnmergeCalled || !runner.RefreshCalled {
					t.Error("expected unmerge and refresh")
				}
			},
		},
		{
			name:       "--now --force json with a failing refresh still emits the result",
			scope:      "definitions",
			staged:     true,
			active:     true,
			flags:      featureCLIFlags{now: true, force: true, jsonOutput: true},
			refreshErr: errors.New("systemd-sysext refresh: exit status 1"),
			wantErr:    "sysext refresh failed",
			check: func(t *testing.T, _ *featureCLIFixture, out string, _ *sysext.MockRunner) {
				r := decodeActionResult(t, out)
				if r.Success || !r.Unmerged || r.RefreshError == "" || r.Error != r.RefreshError {
					t.Errorf("expected a refresh-failed result with unmerged=true, got %+v", r)
				}
				if len(r.RemovedFiles) != 2 {
					t.Errorf("removed files must still be reported, got %v", r.RemovedFiles)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newFeatureCLIFixture(t)
			flags := tt.flags
			switch tt.scope {
			case "definitions":
				dir := t.TempDir()
				fx.writeDefinitions(t, dir, true)
				flags.definitions = dir
			case "component":
				fx.writeDefinitions(t, filepath.Join(fx.roots[0], "sysupdate.demo.d"), true)
			default:
				t.Fatalf("unknown scope %q", tt.scope)
			}
			if tt.staged {
				fx.stageInstalled(t, tt.active)
			}
			runner := &sysext.MockRunner{RefreshErr: tt.refreshErr}
			flags.runner = runner
			setFeatureCLIFlags(t, flags)

			out, err := runFeatureHandler(t, runFeaturesDisable, "testfeature")

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
