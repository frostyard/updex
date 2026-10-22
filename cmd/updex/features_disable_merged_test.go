package updex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostyard/updex/v2/config"
	"github.com/frostyard/updex/v2/sysext"
	"github.com/frostyard/updex/v2/updex"
)

// dockerCLIFixture is a snosi-style host for the docker sysext: a docker
// feature and transfer whose image is staged in the target directory, with no
// CurrentSymlink and nothing in the runtime search directory. Only the merged
// state under its hierarchy can mark that image active.
type dockerCLIFixture struct {
	*featureCLIFixture
	definitions string
	dropIn      string
	hierarchy   string
	image       string
}

func newDockerCLIFixture(t *testing.T) *dockerCLIFixture {
	t.Helper()
	fx := newFeatureCLIFixture(t)
	definitions := t.TempDir()
	writeFeatureFile(t, definitions, "docker", true)
	writeTransferFile(t, definitions, fx.targetDir, "docker", "docker", "https://extensions.example.invalid/docker", "")
	image := filepath.Join(fx.targetDir, "docker_1.0.0.raw")
	if err := os.WriteFile(image, []byte("fake docker extension"), 0o644); err != nil {
		t.Fatalf("stage docker image: %v", err)
	}
	return &dockerCLIFixture{
		featureCLIFixture: fx,
		definitions:       definitions,
		dropIn:            filepath.Join(config.EtcComponentDirIn("", fx.roots), "docker.feature.d", "00-updex.conf"),
		hierarchy:         filepath.Join(t.TempDir(), "usr-hierarchy"),
		image:             image,
	}
}

// writeMergedStateFile writes one systemd-sysext merged-state file under the
// hierarchy, copying sysext's own test helper because it is unexported.
func writeMergedStateFile(t *testing.T, hierarchy, name, content string) {
	t.Helper()
	stateDir := filepath.Join(hierarchy, sysext.MergedStateDirName)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("create merged-state directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write merged-state file %s: %v", name, err)
	}
}

// dockerOrigin renders the origin shape systemd 258 and newer writes, as in
// sysext/manager_test.go's originWith helper.
func dockerOrigin(imagePath string) string {
	return `{
  "mutable" : { "mode" : "no" },
  "extensions" : {
    "docker" : {
      "path" : "` + imagePath + `",
      "onMountId" : 2147483764,
      "fileHandle" : { "type" : 77, "handle" : "6a8d" },
      "crtime" : 1791467316045655,
      "mtime" : 1791467316149657
    }
  }
}`
}

func assertNoRunnerCalls(t *testing.T, runner *sysext.MockRunner) {
	t.Helper()
	if runner.UnmergeCalled || runner.RefreshCalled {
		t.Error("refused disable must not touch the sysext runner")
	}
}

// TestRunFeaturesDisableDockerMergedState runs the real CLI disable handler
// against a fake merged docker image in a temporary hierarchy, passed through
// the runtimePaths seam. This is the layout that made the cmd/updex tests read
// the host's /usr/.systemd-sysext before the seam existed.
func TestRunFeaturesDisableDockerMergedState(t *testing.T) {
	type tc struct {
		name string
		// merged selects the fake merged state: "origin" (systemd 258+ JSON),
		// "extensions" (older name list), or "" (nothing recorded).
		merged    string
		flags     featureCLIFlags
		wantErr   string
		wantOut   []string
		wantNoOut []string
		check     func(t *testing.T, fx *dockerCLIFixture, out string, runner *sysext.MockRunner)
	}

	tests := []tc{
		{
			name:    "origin records merged docker: refused without --force",
			merged:  "origin",
			flags:   featureCLIFlags{now: true},
			wantErr: "requires --force",
			wantOut: []string{"Error: Extension docker (version 1.0.0) is active. Removing requires --force and a reboot to take effect.\n"},
			check: func(t *testing.T, fx *dockerCLIFixture, _ string, runner *sysext.MockRunner) {
				assertExists(t, fx.image, "staged image")
				assertNotExists(t, fx.dropIn, "drop-in")
				assertNoRunnerCalls(t, runner)
			},
		},
		{
			name:    "origin records merged docker: dry run refused without --force",
			merged:  "origin",
			flags:   featureCLIFlags{dryRun: true, now: true},
			wantErr: "requires --force",
			wantOut: []string{"Error: Extension docker (version 1.0.0) is active."},
			check: func(t *testing.T, fx *dockerCLIFixture, _ string, runner *sysext.MockRunner) {
				assertExists(t, fx.image, "staged image")
				assertNotExists(t, fx.dropIn, "drop-in")
				assertNoRunnerCalls(t, runner)
			},
		},
		{
			name:      "origin records merged docker: json refusal reports the error",
			merged:    "origin",
			flags:     featureCLIFlags{now: true, jsonOutput: true},
			wantErr:   "requires --force",
			wantNoOut: []string{"Feature 'docker' disabled."},
			check: func(t *testing.T, fx *dockerCLIFixture, out string, runner *sysext.MockRunner) {
				r := decodeActionResult(t, out)
				if r.Success || !strings.Contains(r.Error, "requires --force") {
					t.Errorf("expected a refused disable result carrying --force, got %+v", r)
				}
				assertExists(t, fx.image, "staged image")
				assertNoRunnerCalls(t, runner)
			},
		},
		{
			name:   "origin records merged docker: --force removes the image",
			merged: "origin",
			flags:  featureCLIFlags{now: true, force: true},
			wantOut: []string{
				"Feature 'docker' disabled.\n",
				"Extensions unmerged.\n",
				"Removed 1 file(s):\n",
				"Warning: Reboot required for changes to take effect.\n",
			},
			wantNoOut: []string{"requires --force", "Run 'updex features update'"},
			check: func(t *testing.T, fx *dockerCLIFixture, out string, runner *sysext.MockRunner) {
				assertContains(t, out, "  - "+fx.image+"\n")
				assertNotExists(t, fx.image, "staged image")
				assertExists(t, fx.dropIn, "drop-in")
				if !runner.UnmergeCalled || !runner.RefreshCalled {
					t.Error("expected unmerge and refresh with --now --force")
				}
			},
		},
		{
			name:    "extensions list records docker: refused with unknown version",
			merged:  "extensions",
			flags:   featureCLIFlags{now: true},
			wantErr: "requires --force",
			wantOut: []string{"Error: Extension docker (version unknown) is active. Removing requires --force and a reboot to take effect.\n"},
			check: func(t *testing.T, fx *dockerCLIFixture, _ string, runner *sysext.MockRunner) {
				assertExists(t, fx.image, "staged image")
				assertNotExists(t, fx.dropIn, "drop-in")
				assertNoRunnerCalls(t, runner)
			},
		},
		{
			name:      "no merged state: --now proceeds",
			merged:    "",
			flags:     featureCLIFlags{now: true},
			wantOut:   []string{"Feature 'docker' disabled.\n", "Extensions unmerged.\n", "Removed 1 file(s):\n"},
			wantNoOut: []string{"requires --force", "Reboot required"},
			check: func(t *testing.T, fx *dockerCLIFixture, _ string, runner *sysext.MockRunner) {
				assertNotExists(t, fx.image, "staged image")
				assertExists(t, fx.dropIn, "drop-in")
				if !runner.UnmergeCalled || !runner.RefreshCalled {
					t.Error("expected unmerge and refresh with --now")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newDockerCLIFixture(t)
			switch tt.merged {
			case "origin":
				writeMergedStateFile(t, fx.hierarchy, "origin", dockerOrigin(fx.image))
			case "extensions":
				writeMergedStateFile(t, fx.hierarchy, "extensions", "docker\n")
			}
			setRuntimePaths(t, updex.RuntimePaths{
				SysextHierarchies: []string{fx.hierarchy},
				RunExtensionsDir:  filepath.Join(t.TempDir(), "run-search", "extensions"),
			})

			flags := tt.flags
			flags.definitions = fx.definitions
			runner := &sysext.MockRunner{}
			flags.runner = runner
			setFeatureCLIFlags(t, flags)

			out, err := runFeatureHandler(t, runFeaturesDisable, "docker")
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
