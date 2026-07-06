package updex

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/frostyard/updex/internal/testutil"
	"github.com/frostyard/updex/sysext"
	"github.com/klauspost/compress/zstd"
)

// createTransferFileWithPatterns creates a .transfer file with explicit
// (possibly multi-valued) source and target MatchPattern lines.
func createTransferFileWithPatterns(t *testing.T, configDir, component, featureName, baseURL, sourcePatterns, targetPatterns string) {
	t.Helper()
	content := `[Transfer]
Features=` + featureName + `

[Source]
Type=url-file
Path=` + baseURL + `
MatchPattern=` + sourcePatterns + `

[Target]
MatchPattern=` + targetPatterns + `
CurrentSymlink=` + component + `.raw
`
	path := filepath.Join(configDir, component+".transfer")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create transfer file: %v", err)
	}
}

// TestUpdateFeatures_TargetFilename_UncompressedSource verifies that when the
// manifest lists an uncompressed file but the target MatchPattern list starts
// with a compressed variant, the installed file is named after its actual
// (uncompressed) content instead of the first target pattern.
func TestUpdateFeatures_TargetFilename_UncompressedSource(t *testing.T) {
	configDir := t.TempDir()
	targetDir := t.TempDir()
	mockRunner := &sysext.MockRunner{}

	content := []byte("uncompressed raw ddi content")
	server := testutil.NewTestServer(t, testutil.TestServerFiles{
		Files: map[string]string{
			"testext_1.0.0.raw": hashContent(content),
		},
		Content: map[string][]byte{
			"testext_1.0.0.raw": content,
		},
	})
	defer server.Close()

	createFeatureFile(t, configDir, "testfeature", true)
	createTransferFileWithPatterns(t, configDir, "testext", "testfeature", server.URL,
		"testext_@v.raw.zst testext_@v.raw",
		"testext_@v.raw.zst testext_@v.raw")
	updateTransferTargetPath(t, configDir, targetDir)

	client := NewClient(ClientConfig{Definitions: configDir, SysextRunner: mockRunner})
	results, err := client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{NoRefresh: true})
	if err != nil {
		t.Fatalf("UpdateFeatures failed: %v", err)
	}
	if len(results) != 1 || len(results[0].Results) != 1 {
		t.Fatalf("expected 1 feature result with 1 component, got %+v", results)
	}
	if results[0].Results[0].Error != "" {
		t.Fatalf("component update failed: %s", results[0].Results[0].Error)
	}

	got, err := os.ReadFile(filepath.Join(targetDir, "testext_1.0.0.raw"))
	if err != nil {
		t.Fatalf("expected testext_1.0.0.raw to exist: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("installed file content mismatch: got %q, want %q", got, content)
	}
	if _, err := os.Stat(filepath.Join(targetDir, "testext_1.0.0.raw.zst")); !os.IsNotExist(err) {
		t.Error("expected testext_1.0.0.raw.zst to NOT exist (content is uncompressed)")
	}
}

// TestUpdateFeatures_TargetFilename_CompressedSource verifies that a
// zstd-compressed source file is stored decompressed under a name without the
// compression suffix.
func TestUpdateFeatures_TargetFilename_CompressedSource(t *testing.T) {
	configDir := t.TempDir()
	targetDir := t.TempDir()
	mockRunner := &sysext.MockRunner{}

	raw := []byte("raw ddi payload that will be zstd compressed")
	var buf bytes.Buffer
	zw, err := zstd.NewWriter(&buf)
	if err != nil {
		t.Fatalf("failed to create zstd writer: %v", err)
	}
	if _, err := zw.Write(raw); err != nil {
		t.Fatalf("failed to compress: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("failed to close zstd writer: %v", err)
	}
	compressed := buf.Bytes()

	server := testutil.NewTestServer(t, testutil.TestServerFiles{
		Files: map[string]string{
			"testext_1.0.0.raw.zst": hashContent(compressed),
		},
		Content: map[string][]byte{
			"testext_1.0.0.raw.zst": compressed,
		},
	})
	defer server.Close()

	createFeatureFile(t, configDir, "testfeature", true)
	createTransferFileWithPatterns(t, configDir, "testext", "testfeature", server.URL,
		"testext_@v.raw.zst testext_@v.raw",
		"testext_@v.raw.zst testext_@v.raw")
	updateTransferTargetPath(t, configDir, targetDir)

	client := NewClient(ClientConfig{Definitions: configDir, SysextRunner: mockRunner})
	results, err := client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{NoRefresh: true})
	if err != nil {
		t.Fatalf("UpdateFeatures failed: %v", err)
	}
	if len(results) != 1 || len(results[0].Results) != 1 {
		t.Fatalf("expected 1 feature result with 1 component, got %+v", results)
	}
	if results[0].Results[0].Error != "" {
		t.Fatalf("component update failed: %s", results[0].Results[0].Error)
	}

	got, err := os.ReadFile(filepath.Join(targetDir, "testext_1.0.0.raw"))
	if err != nil {
		t.Fatalf("expected testext_1.0.0.raw to exist: %v", err)
	}
	if !bytes.Equal(got, raw) {
		t.Errorf("installed file should be decompressed: got %q, want %q", got, raw)
	}
	if _, err := os.Stat(filepath.Join(targetDir, "testext_1.0.0.raw.zst")); !os.IsNotExist(err) {
		t.Error("expected testext_1.0.0.raw.zst to NOT exist (stored decompressed)")
	}
}
