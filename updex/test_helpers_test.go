package updex

import (
	"os"
	"path/filepath"
	"testing"
)

// createFeatureFile creates a .feature file in the config directory
func createFeatureFile(t *testing.T, configDir, featureName string, enabled bool) string {
	t.Helper()
	enabledStr := "false"
	if enabled {
		enabledStr = "true"
	}
	content := `[Feature]
Description=Test feature
Enabled=` + enabledStr + `
`
	path := filepath.Join(configDir, featureName+".feature")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create feature file: %v", err)
	}
	return path
}

// createFeatureTransferFile creates a .transfer file with Features set
func createFeatureTransferFile(t *testing.T, configDir, component, featureName, baseURL string) {
	t.Helper()
	content := `[Transfer]
Features=` + featureName + `

[Source]
Type=url-file
Path=` + baseURL + `
MatchPattern=` + component + `_@v.raw

[Target]
MatchPattern=` + component + `_@v.raw
CurrentSymlink=` + component + `.raw
`
	path := filepath.Join(configDir, component+".transfer")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create transfer file: %v", err)
	}
}

// updateTransferTargetPath updates all transfer files to use the given target path
func updateTransferTargetPath(t *testing.T, configDir, targetDir string) {
	t.Helper()
	entries, err := os.ReadDir(configDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".transfer" {
			continue
		}
		path := filepath.Join(configDir, entry.Name())
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read transfer file: %v", err)
		}
		// Append Path directive to Target section
		newContent := string(content) + "Path=" + targetDir + "\n"
		if err := os.WriteFile(path, []byte(newContent), 0644); err != nil {
			t.Fatalf("failed to update transfer file: %v", err)
		}
	}
}
