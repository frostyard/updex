package updex

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostyard/updex/internal/testutil"
	"github.com/frostyard/updex/sysext"
	"github.com/frostyard/updex/version"
	"github.com/klauspost/compress/zstd"
)

// createTransferFileWithPatterns creates a .transfer file with explicit
// (possibly multi-valued) source and target MatchPattern lines.
func createTransferFileWithPatterns(t *testing.T, configDir, component, featureName, baseURL, sourcePatterns, targetPatterns string) {
	t.Helper()
	content := `[Transfer]
Features=` + featureName + `
Verify=false

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

func TestBuildTargetFilename(t *testing.T) {
	tests := []struct {
		name       string
		patterns   []string
		ver        string
		want       string
		wantErr    error
		wantErrMsg string
	}{
		{
			name:     "prefers uncompressed pattern",
			patterns: []string{"compressed_@v.raw.zst", "testext_@v.raw"},
			want:     "testext_1.2.3.raw",
		},
		{
			name:     "strips first compressed fallback",
			patterns: []string{"testext_@v.raw.zst", "testext-alt_@v.raw.gz"},
			want:     "testext_1.2.3.raw",
		},
		{
			name:     "skips invalid pattern",
			patterns: []string{"testext.raw", "testext_@v.raw"},
			want:     "testext_1.2.3.raw",
		},
		{
			name:       "returns first pattern error",
			patterns:   []string{"testext.raw", ""},
			wantErr:    version.ErrMissingVersionPlaceholder,
			wantErrMsg: "invalid target pattern: pattern must contain @v placeholder",
		},
		{
			name:       "rejects missing patterns",
			wantErrMsg: "no target pattern configured",
		},
		{
			// A manifest filename captured against a bare "@v" MatchPattern
			// (no surrounding literal characters) could substitute ".." as
			// the version, which would otherwise escape Target.Path when
			// joined in installTransfer.
			name:       "rejects path-traversal version",
			patterns:   []string{"@v"},
			ver:        "..",
			wantErrMsg: `invalid target pattern: invalid target filename "..": must not contain path separators or traverse directories`,
		},
		{
			name:       "rejects version containing path separator",
			patterns:   []string{"testext_@v.raw"},
			ver:        "1.2.3/../../etc",
			wantErrMsg: `invalid target pattern: invalid target filename "testext_1.2.3/../../etc.raw": must not contain path separators or traverse directories`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ver := tt.ver
			if ver == "" {
				ver = "1.2.3"
			}
			got, err := buildTargetFilename(tt.patterns, ver)
			if got != tt.want {
				t.Errorf("buildTargetFilename() = %q, want %q", got, tt.want)
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Errorf("buildTargetFilename() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErrMsg == "" {
				if err != nil {
					t.Fatalf("buildTargetFilename() unexpected error: %v", err)
				}
				return
			}
			if err == nil || err.Error() != tt.wantErrMsg {
				t.Errorf("buildTargetFilename() error = %v, want %q", err, tt.wantErrMsg)
			}
		})
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

// TestInstallTransfer_RefreshFailure_ReturnsErrorAfterInstall pins the
// direct-caller contract of installTransfer's own refresh path (both SDK
// callers batch with NoRefresh: true): the image is installed and linked,
// the version and downloaded=true are still reported, and the refresh
// failure comes back as the error instead of being swallowed.
func TestInstallTransfer_RefreshFailure_ReturnsErrorAfterInstall(t *testing.T) {
	client, mockRunner, targetDir := refreshFailureFixture(t, true)
	_, transfers, err := client.loadDomain("")
	if err != nil {
		t.Fatalf("loadDomain: %v", err)
	}
	if len(transfers) != 1 {
		t.Fatalf("expected 1 transfer, got %d", len(transfers))
	}

	version, _, downloaded, err := client.installTransfer(t.Context(), transfers[0], installTransferOptions{NoVacuum: true})

	if err == nil || !strings.Contains(err.Error(), "sysext refresh failed") {
		t.Fatalf("expected refresh error, got %v", err)
	}
	if version != "1.0.0" || !downloaded {
		t.Errorf("install outcome must still be reported: version=%q downloaded=%v", version, downloaded)
	}
	if !mockRunner.RefreshCalled {
		t.Error("expected Refresh to be called")
	}
	if _, statErr := os.Stat(filepath.Join(targetDir, "testext_1.0.0.raw")); statErr != nil {
		t.Errorf("installed image missing: %v", statErr)
	}
}
