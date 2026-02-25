package updex

import (
	"os"
	"path/filepath"
	"testing"
)

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
