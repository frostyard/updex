package updex

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostyard/clix"
	"github.com/frostyard/updex/internal/testutil"
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
