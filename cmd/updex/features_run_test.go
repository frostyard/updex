package updex

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostyard/clix"
	"github.com/frostyard/updex/internal/testutil"
	"github.com/frostyard/updex/sysext"
	"github.com/frostyard/updex/updex"
	"github.com/spf13/cobra"
)

func writeFeatureFile(t *testing.T, configDir, name string, enabled bool) {
	t.Helper()
	enabledStr := "false"
	if enabled {
		enabledStr = "true"
	}
	content := "[Feature]\nDescription=Test feature\nEnabled=" + enabledStr + "\n"
	if err := os.WriteFile(filepath.Join(configDir, name+".feature"), []byte(content), 0644); err != nil {
		t.Fatalf("failed to write feature file: %v", err)
	}
}

func writeFeatureTransferFile(t *testing.T, configDir, targetDir, component, feature, baseURL string) {
	t.Helper()
	content := `[Transfer]
Features=` + feature + `
Verify=false

[Source]
Type=url-file
Path=` + baseURL + `
MatchPattern=` + component + `_@v.raw

[Target]
Path=` + targetDir + `
MatchPattern=` + component + `_@v.raw
CurrentSymlink=` + component + `.raw
`
	if err := os.WriteFile(filepath.Join(configDir, component+".transfer"), []byte(content), 0644); err != nil {
		t.Fatalf("failed to write transfer file: %v", err)
	}
}

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	original := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create stdout pipe: %v", err)
	}
	os.Stdout = w
	defer func() {
		os.Stdout = original
	}()

	runErr := fn()
	if err := w.Close(); err != nil {
		t.Fatalf("failed to close stdout pipe: %v", err)
	}

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("failed to read stdout: %v", err)
	}

	return buf.String(), runErr
}

func TestRunFeaturesUpdate_ThreadsDryRun(t *testing.T) {
	configDir := t.TempDir()
	targetDir := t.TempDir()

	extContent := []byte("fake extension content")
	server := testutil.NewTestServer(t, testutil.TestServerFiles{
		Files: map[string]string{
			"testext_1.0.0.raw": sha256Hex(extContent),
		},
		Content: map[string][]byte{
			"testext_1.0.0.raw": extContent,
		},
	})
	defer server.Close()

	writeFeatureFile(t, configDir, "testfeature", true)
	writeFeatureTransferFile(t, configDir, targetDir, "testext", "testfeature", server.URL)

	oldDefinitions, oldNoRefresh, oldFeatureUpdateNoVac := definitions, noRefresh, featureUpdateNoVac
	oldDryRun, oldJSONOutput := clix.DryRun, clix.JSONOutput
	oldGetEUID := getEUID
	t.Cleanup(func() {
		definitions = oldDefinitions
		noRefresh = oldNoRefresh
		featureUpdateNoVac = oldFeatureUpdateNoVac
		clix.DryRun = oldDryRun
		clix.JSONOutput = oldJSONOutput
		getEUID = oldGetEUID
	})

	definitions = configDir
	noRefresh = false
	featureUpdateNoVac = false
	clix.DryRun = true
	clix.JSONOutput = false
	getEUID = func() int { return 0 }

	output, err := captureStdout(t, func() error {
		cmd := &cobra.Command{}
		cmd.SetContext(t.Context())
		return runFeaturesUpdate(cmd, nil)
	})
	if err != nil {
		t.Fatalf("runFeaturesUpdate failed: %v", err)
	}
	if !strings.Contains(output, "[DRY RUN]") {
		t.Fatalf("expected dry-run header in output, got:\n%s", output)
	}
	if !strings.Contains(output, "would download") {
		t.Fatalf("expected would-download status in output, got:\n%s", output)
	}
	if _, err := os.Stat(filepath.Join(targetDir, "testext_1.0.0.raw")); !os.IsNotExist(err) {
		t.Error("expected CLI dry-run to avoid downloading the extension file")
	}
	if _, err := os.Lstat(filepath.Join(targetDir, "testext.raw")); !os.IsNotExist(err) {
		t.Error("expected CLI dry-run to avoid creating the current symlink")
	}
}

// TestRunFeaturesCheck_JSONErrorPathEmitsArray verifies that on the error path
// (here: --definitions combined with --component, which fails loadDomain), the
// --json output on stdout is `[]`, never `null`. The command still returns a
// non-zero error. Regression guard for the null-vs-array contract that broke
// pilothouse.
func TestRunFeaturesCheck_JSONErrorPathEmitsArray(t *testing.T) {
	oldDefinitions, oldComponent, oldJSONOutput := definitions, featureComponent, clix.JSONOutput
	t.Cleanup(func() {
		definitions = oldDefinitions
		featureComponent = oldComponent
		clix.JSONOutput = oldJSONOutput
	})

	definitions = t.TempDir()
	featureComponent = "somecomponent" // combining the two makes loadDomain fail
	clix.JSONOutput = true

	output, err := captureStdout(t, func() error {
		cmd := &cobra.Command{}
		cmd.SetContext(t.Context())
		return runFeaturesCheck(cmd, nil)
	})
	if err == nil {
		t.Fatal("expected an error on the --definitions + --component path")
	}
	if strings.TrimSpace(output) != "[]" {
		t.Errorf("expected stdout []; got %q", output)
	}
}

// TestRunFeaturesCheck_TextMarksFailedComponent verifies that a component
// whose manifest cannot be fetched is rendered as UPDATE=error in the text
// table (never as "no") and that the command returns the SDK's aggregate
// error so the process exits non-zero.
func TestRunFeaturesCheck_TextMarksFailedComponent(t *testing.T) {
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

	output, err := captureStdout(t, func() error {
		cmd := &cobra.Command{}
		cmd.SetContext(t.Context())
		return runFeaturesCheck(cmd, nil)
	})
	if err == nil {
		t.Fatal("expected a non-nil error when a component cannot be checked")
	}
	if !strings.Contains(output, "testext") {
		t.Fatalf("expected the failed component in the table, got %q", output)
	}
	var row string
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "testext") {
			row = line
		}
	}
	fields := strings.Fields(row)
	if len(fields) != 5 || fields[4] != "error" {
		t.Errorf("expected UPDATE column 'error' for the failed component, got row %q", row)
	}
}

// TestRunFeaturesUpdate_RefreshFailure_JSONEmitsResultsAndErrors verifies
// that when the batched sysext refresh fails after a successful install,
// --json still emits the populated results array (Installed=true is
// accurate) and the handler returns the refresh error so the process exits
// non-zero.
func TestRunFeaturesUpdate_RefreshFailure_JSONEmitsResultsAndErrors(t *testing.T) {
	configDir := t.TempDir()
	targetDir := t.TempDir()
	sysextDir := t.TempDir()

	extContent := []byte("fake extension content")
	server := testutil.NewTestServer(t, testutil.TestServerFiles{
		Files:   map[string]string{"testext_1.0.0.raw": sha256Hex(extContent)},
		Content: map[string][]byte{"testext_1.0.0.raw": extContent},
	})
	defer server.Close()
	writeFeatureFile(t, configDir, "testfeature", true)
	writeFeatureTransferFile(t, configDir, targetDir, "testext", "testfeature", server.URL)

	oldDefinitions, oldNoRefresh, oldNoVac := definitions, noRefresh, featureUpdateNoVac
	oldDryRun, oldJSONOutput, oldSilent := clix.DryRun, clix.JSONOutput, clix.Silent
	oldGetEUID, oldRunner, oldSysextDir := getEUID, sysextRunner, sysext.SysextDir
	t.Cleanup(func() {
		definitions, noRefresh, featureUpdateNoVac = oldDefinitions, oldNoRefresh, oldNoVac
		clix.DryRun, clix.JSONOutput, clix.Silent = oldDryRun, oldJSONOutput, oldSilent
		getEUID, sysextRunner, sysext.SysextDir = oldGetEUID, oldRunner, oldSysextDir
	})
	definitions = configDir
	noRefresh = false
	featureUpdateNoVac = true
	clix.DryRun = false
	clix.JSONOutput = true
	clix.Silent = true
	getEUID = func() int { return 0 }
	runner := &sysext.MockRunner{RefreshErr: errors.New("systemd-sysext refresh: exit status 1")}
	sysextRunner = runner
	sysext.SysextDir = sysextDir

	output, err := captureStdout(t, func() error {
		cmd := &cobra.Command{}
		cmd.SetContext(t.Context())
		return runFeaturesUpdate(cmd, nil)
	})
	if err == nil || !strings.Contains(err.Error(), "sysext refresh failed") {
		t.Fatalf("expected the refresh error to be returned, got %v", err)
	}
	if !runner.RefreshCalled {
		t.Error("expected refresh to be attempted")
	}
	var results []updex.UpdateFeaturesResult
	if jsonErr := json.Unmarshal([]byte(output), &results); jsonErr != nil {
		t.Fatalf("stdout is not a JSON array: %v\n%s", jsonErr, output)
	}
	if len(results) != 1 || len(results[0].Results) != 1 || !results[0].Results[0].Installed || results[0].Results[0].Error != "" {
		t.Errorf("expected the successful install to be reported, got %+v", results)
	}
}

// TestRunFeaturesUpdate_JSONErrorPathEmitsArray is the update analog. It also
// sets getEUID to root so the requireRoot() guard is not what fails.
func TestRunFeaturesUpdate_JSONErrorPathEmitsArray(t *testing.T) {
	oldDefinitions, oldComponent, oldJSONOutput := definitions, featureComponent, clix.JSONOutput
	oldGetEUID := getEUID
	t.Cleanup(func() {
		definitions = oldDefinitions
		featureComponent = oldComponent
		clix.JSONOutput = oldJSONOutput
		getEUID = oldGetEUID
	})

	definitions = t.TempDir()
	featureComponent = "somecomponent"
	clix.JSONOutput = true
	getEUID = func() int { return 0 }

	output, err := captureStdout(t, func() error {
		cmd := &cobra.Command{}
		cmd.SetContext(t.Context())
		return runFeaturesUpdate(cmd, nil)
	})
	if err == nil {
		t.Fatal("expected an error on the --definitions + --component path")
	}
	if strings.TrimSpace(output) != "[]" {
		t.Errorf("expected stdout []; got %q", output)
	}
}

func TestFormatOrigin(t *testing.T) {
	tests := []struct {
		name string
		info updex.FeatureInfo
		want string
	}{
		{
			name: "catalog shows the bare catalog name",
			info: updex.FeatureInfo{Origin: updex.FeatureOriginCatalog, OriginName: "fedora"},
			want: "fedora",
		},
		{
			name: "image is prefixed so it cannot read as a catalog",
			info: updex.FeatureInfo{Origin: updex.FeatureOriginImage, OriginName: "ucore"},
			want: "image:ucore",
		},
		{
			name: "image without an identifier degrades to the kind",
			info: updex.FeatureInfo{Origin: updex.FeatureOriginImage},
			want: "image",
		},
		{
			name: "local etc",
			info: updex.FeatureInfo{Origin: updex.FeatureOriginLocal, OriginName: "etc"},
			want: "local:etc",
		},
		{
			name: "local usr",
			info: updex.FeatureInfo{Origin: updex.FeatureOriginLocal, OriginName: "usr"},
			want: "local:usr",
		},
		{
			name: "local run",
			info: updex.FeatureInfo{Origin: updex.FeatureOriginLocal, OriginName: "run"},
			want: "local:run",
		},
		{
			name: "unknown",
			info: updex.FeatureInfo{Origin: updex.FeatureOriginUnknown},
			want: "unknown",
		},
		{
			name: "empty origin falls back to unknown",
			info: updex.FeatureInfo{},
			want: "unknown",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatOrigin(tc.info); got != tc.want {
				t.Errorf("formatOrigin() = %q, want %q", got, tc.want)
			}
		})
	}
}
