package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// collectConfigFiles scans searchPaths for files ending in suffix and returns
// a name -> filepath map. Earlier paths take priority (first occurrence wins).
// The name is derived by trimming the suffix from the filename.
func collectConfigFiles(searchPaths []string, suffix string) (map[string]string, error) {
	files := make(map[string]string)

	for _, dir := range searchPaths {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("failed to read directory %s: %w", dir, err)
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			if !strings.HasSuffix(entry.Name(), suffix) {
				continue
			}

			name := strings.TrimSuffix(entry.Name(), suffix)
			if _, exists := files[name]; !exists {
				files[name] = filepath.Join(dir, entry.Name())
			}
		}
	}

	return files, nil
}
