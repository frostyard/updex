package updex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostyard/clix"
	"github.com/frostyard/updex/v2/internal/testutil"
	"github.com/spf13/cobra"
)

// TestRunFeaturesCheck_FilterUnknownFeatureIsAnError verifies that
// `updex features check <name>` fails, naming the feature, when the name is
// not a feature of the loaded domain.
func TestRunFeaturesCheck_FilterUnknownFeatureIsAnError(t *testing.T) {
	oldDefinitions, oldComponent, oldJSONOutput := definitions, featureComponent, clix.JSONOutput
	t.Cleanup(func() {
		definitions = oldDefinitions
		featureComponent = oldComponent
		clix.JSONOutput = oldJSONOutput
	})

	configDir := t.TempDir()
	targetDir := t.TempDir()
	server := testutil.NewTestServer(t, testutil.TestServerFiles{
		Files:   map[string]string{"testext_1.0.0.raw": sha256Hex([]byte("fake extension content"))},
		Content: map[string][]byte{"testext_1.0.0.raw": []byte("fake extension content")},
	})
	defer server.Close()

	writeFeatureFile(t, configDir, "testfeature", true)
	writeFeatureTransferFile(t, configDir, targetDir, "testext", "testfeature", server.URL)

	definitions = configDir
	featureComponent = ""
	clix.JSONOutput = false

	output, err := captureStdout(t, func() error {
		cmd := &cobra.Command{}
		cmd.SetContext(t.Context())
		return runFeaturesCheck(cmd, []string{"extra"})
	})
	if err == nil {
		t.Fatal("expected an error for an unknown feature name")
	}
	if !strings.Contains(err.Error(), "feature 'extra' not found") {
		t.Errorf("error = %q, want it to name the feature", err)
	}
	if strings.TrimSpace(output) != "" {
		t.Errorf("a failed check printed to stdout:\n%s", output)
	}
}

// TestRunFeaturesCheck_FilterListsOnlyNamedFeature verifies that
// `updex features check <name>` reports the named feature and leaves every
// other enabled feature out of the table.
func TestRunFeaturesCheck_FilterListsOnlyNamedFeature(t *testing.T) {
	oldDefinitions, oldComponent, oldJSONOutput := definitions, featureComponent, clix.JSONOutput
	t.Cleanup(func() {
		definitions = oldDefinitions
		featureComponent = oldComponent
		clix.JSONOutput = oldJSONOutput
	})

	configDir := t.TempDir()
	targetDir := t.TempDir()
	dockerContent := []byte("fake docker extension content")
	incusContent := []byte("fake incus extension content")
	server := testutil.NewTestServer(t, testutil.TestServerFiles{
		Files: map[string]string{
			"dockerext_1.0.0.raw": sha256Hex(dockerContent),
			"incusext_1.0.0.raw":  sha256Hex(incusContent),
		},
		Content: map[string][]byte{
			"dockerext_1.0.0.raw": dockerContent,
			"incusext_1.0.0.raw":  incusContent,
		},
	})
	defer server.Close()

	writeFeatureFile(t, configDir, "docker", true)
	writeFeatureFile(t, configDir, "incus", true)
	writeFeatureTransferFile(t, configDir, targetDir, "dockerext", "docker", server.URL)
	writeFeatureTransferFile(t, configDir, targetDir, "incusext", "incus", server.URL)

	definitions = configDir
	featureComponent = ""
	clix.JSONOutput = false

	output, err := captureStdout(t, func() error {
		cmd := &cobra.Command{}
		cmd.SetContext(t.Context())
		return runFeaturesCheck(cmd, []string{"docker"})
	})
	if err != nil {
		t.Fatalf("runFeaturesCheck failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "docker") {
		t.Errorf("output missing the named feature:\n%s", output)
	}
	if strings.Contains(output, "incus") {
		t.Errorf("output lists the unnamed feature:\n%s", output)
	}
}

// TestRunFeaturesUpdate_FilterPassesNames verifies that the `features update`
// handler passes its FEATURE arguments into the SDK options: a dry run with
// one name previews that feature only, and a name the domain does not define
// fails the command. Every mutating handler needs root first, so the test
// stubs getEUID the way the other update handler tests do.
func TestRunFeaturesUpdate_FilterPassesNames(t *testing.T) {
	oldDefinitions, oldNoRefresh, oldNoVac := definitions, noRefresh, featureUpdateNoVac
	oldDryRun, oldJSONOutput := clix.DryRun, clix.JSONOutput
	oldGetEUID := getEUID
	t.Cleanup(func() {
		definitions, noRefresh, featureUpdateNoVac = oldDefinitions, oldNoRefresh, oldNoVac
		clix.DryRun, clix.JSONOutput = oldDryRun, oldJSONOutput
		getEUID = oldGetEUID
	})

	configDir := t.TempDir()
	targetDir := t.TempDir()
	dockerContent := []byte("fake docker extension content")
	incusContent := []byte("fake incus extension content")
	server := testutil.NewTestServer(t, testutil.TestServerFiles{
		Files: map[string]string{
			"dockerext_1.0.0.raw": sha256Hex(dockerContent),
			"incusext_1.0.0.raw":  sha256Hex(incusContent),
		},
		Content: map[string][]byte{
			"dockerext_1.0.0.raw": dockerContent,
			"incusext_1.0.0.raw":  incusContent,
		},
	})
	defer server.Close()

	writeFeatureFile(t, configDir, "docker", true)
	writeFeatureFile(t, configDir, "incus", true)
	writeFeatureTransferFile(t, configDir, targetDir, "dockerext", "docker", server.URL)
	writeFeatureTransferFile(t, configDir, targetDir, "incusext", "incus", server.URL)

	definitions = configDir
	noRefresh = false
	featureUpdateNoVac = false
	clix.DryRun = true
	clix.JSONOutput = false
	getEUID = func() int { return 0 }

	output, err := captureStdout(t, func() error {
		cmd := &cobra.Command{}
		cmd.SetContext(t.Context())
		return runFeaturesUpdate(cmd, []string{"docker"})
	})
	if err != nil {
		t.Fatalf("runFeaturesUpdate failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "docker") {
		t.Errorf("output missing the named feature:\n%s", output)
	}
	if strings.Contains(output, "incus") {
		t.Errorf("output lists the unnamed feature:\n%s", output)
	}

	clix.DryRun = false
	output, err = captureStdout(t, func() error {
		cmd := &cobra.Command{}
		cmd.SetContext(t.Context())
		return runFeaturesUpdate(cmd, []string{"extra"})
	})
	if err == nil {
		t.Fatal("expected an error for an unknown feature name")
	}
	if !strings.Contains(err.Error(), "feature 'extra' not found") {
		t.Errorf("error = %q, want it to name the feature", err)
	}
	if strings.TrimSpace(output) != "" {
		t.Errorf("a failed update printed to stdout:\n%s", output)
	}
	if _, statErr := os.Stat(filepath.Join(targetDir, "dockerext_1.0.0.raw")); !os.IsNotExist(statErr) {
		t.Error("a failed update must download nothing")
	}
}
