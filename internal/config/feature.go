package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/ini.v1"
)

// Feature represents a parsed .feature configuration file
type Feature struct {
	Name          string   // Derived from filename (e.g., "devel" from "devel.feature")
	FilePath      string   // Path to the .feature file
	Description   string   // Human-readable description
	Documentation string   // URL to documentation
	AppStream     string   // URL to AppStream catalog XML
	Enabled       bool     // Whether the feature is enabled
	Masked        bool     // Whether the feature is masked (symlink to /dev/null)
	Transfers     []string // Names of transfers belonging to this feature
}

// LoadFeatures loads all .feature files from the specified directory or default paths
func LoadFeatures(customPath string) ([]*Feature, error) {
	var searchPaths []string

	if customPath != "" {
		searchPaths = []string{customPath}
	} else {
		searchPaths = defaultSearchPaths
	}

	// Collect all .feature files, with earlier paths taking priority
	featureFiles := make(map[string]string) // feature name -> file path

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
			if !strings.HasSuffix(entry.Name(), ".feature") {
				continue
			}

			featureName := strings.TrimSuffix(entry.Name(), ".feature")
			if _, exists := featureFiles[featureName]; !exists {
				// Earlier paths take priority
				featureFiles[featureName] = filepath.Join(dir, entry.Name())
			}
		}
	}

	if len(featureFiles) == 0 {
		return nil, nil
	}

	// Parse all feature files
	var features []*Feature
	for name, filePath := range featureFiles {
		f, err := parseFeatureFile(filePath, name, searchPaths)
		if err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", filePath, err)
		}
		if f != nil { // nil means masked
			features = append(features, f)
		}
	}

	// Sort by feature name for consistent ordering
	sort.Slice(features, func(i, j int) bool {
		return features[i].Name < features[j].Name
	})

	return features, nil
}

// parseFeatureFile parses a .feature file and applies drop-ins
func parseFeatureFile(filePath, name string, searchPaths []string) (*Feature, error) {
	// Check if masked (symlink to /dev/null)
	linkTarget, err := os.Readlink(filePath)
	if err == nil && linkTarget == "/dev/null" {
		return &Feature{
			Name:     name,
			FilePath: filePath,
			Masked:   true,
			Enabled:  false,
		}, nil
	}

	cfg, err := ini.Load(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to load INI file: %w", err)
	}

	f := &Feature{
		Name:     name,
		FilePath: filePath,
		Enabled:  false, // Default to disabled
	}

	// Parse [Feature] section
	if sec, err := cfg.GetSection("Feature"); err == nil {
		if key, err := sec.GetKey("Description"); err == nil {
			f.Description = key.String()
		}
		if key, err := sec.GetKey("Documentation"); err == nil {
			f.Documentation = key.String()
		}
		if key, err := sec.GetKey("AppStream"); err == nil {
			f.AppStream = key.String()
		}
		if key, err := sec.GetKey("Enabled"); err == nil {
			f.Enabled = key.MustBool(false)
		}
	}

	// Apply drop-ins from all search paths
	if err := applyFeatureDropIns(f, name, searchPaths); err != nil {
		return nil, err
	}

	return f, nil
}

// applyFeatureDropIns applies drop-in configuration files for a feature
func applyFeatureDropIns(f *Feature, name string, searchPaths []string) error {
	// Collect all drop-in files from all search paths
	dropInFiles := make(map[string]string) // filename -> full path (earliest path wins)

	for _, dir := range searchPaths {
		dropInDir := filepath.Join(dir, name+".feature.d")
		entries, err := os.ReadDir(dropInDir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("failed to read drop-in directory %s: %w", dropInDir, err)
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			if !strings.HasSuffix(entry.Name(), ".conf") {
				continue
			}

			if _, exists := dropInFiles[entry.Name()]; !exists {
				dropInFiles[entry.Name()] = filepath.Join(dropInDir, entry.Name())
			}
		}
	}

	if len(dropInFiles) == 0 {
		return nil
	}

	// Sort drop-in files alphabetically and apply in order
	var sortedNames []string
	for name := range dropInFiles {
		sortedNames = append(sortedNames, name)
	}
	sort.Strings(sortedNames)

	for _, dropInName := range sortedNames {
		dropInPath := dropInFiles[dropInName]
		if err := applyFeatureDropIn(f, dropInPath); err != nil {
			return fmt.Errorf("failed to apply drop-in %s: %w", dropInPath, err)
		}
	}

	return nil
}

// applyFeatureDropIn applies a single drop-in file to a feature
func applyFeatureDropIn(f *Feature, dropInPath string) error {
	cfg, err := ini.Load(dropInPath)
	if err != nil {
		return fmt.Errorf("failed to load drop-in file: %w", err)
	}

	if sec, err := cfg.GetSection("Feature"); err == nil {
		if key, err := sec.GetKey("Description"); err == nil {
			f.Description = key.String()
		}
		if key, err := sec.GetKey("Documentation"); err == nil {
			f.Documentation = key.String()
		}
		if key, err := sec.GetKey("AppStream"); err == nil {
			f.AppStream = key.String()
		}
		if key, err := sec.GetKey("Enabled"); err == nil {
			f.Enabled = key.MustBool(f.Enabled)
		}
	}

	return nil
}

// GetEnabledFeatureNames returns a list of enabled feature names
func GetEnabledFeatureNames(features []*Feature) []string {
	var enabled []string
	for _, f := range features {
		if f.Enabled && !f.Masked {
			enabled = append(enabled, f.Name)
		}
	}
	return enabled
}

// IsFeatureEnabled checks if a feature with the given name is enabled
func IsFeatureEnabled(features []*Feature, name string) bool {
	for _, f := range features {
		if f.Name == name {
			return f.Enabled && !f.Masked
		}
	}
	return false
}
