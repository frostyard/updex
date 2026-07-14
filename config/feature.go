package config

import (
	"cmp"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/ini.v1"
)

const featureSuffix = ".feature"

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

// LoadFeatures loads all .feature files from the specified directory or the
// legacy default search paths (/etc/sysupdate.d, /run/sysupdate.d, ...). It
// does not discover named components; see LoadAllFeatures and
// LoadComponentFeatures for that.
func LoadFeatures(customPath string) ([]*Feature, error) {
	if customPath != "" {
		return loadFeaturesFromPaths([]string{customPath})
	}
	return loadFeaturesFromPaths(defaultSearchPaths())
}

// LoadComponentFeatures loads .feature files for a single named component,
// following its own /etc > /run > /usr/local/lib > /usr/lib precedence (see
// ComponentSearchPaths). Pass "" for the legacy default component.
func LoadComponentFeatures(name string) ([]*Feature, error) {
	return loadFeaturesFromPaths(ComponentSearchPaths(name))
}

// LoadAllFeatures loads the feature domain updex operates on by default:
// the union of the legacy default sysupdate.d directory and every
// discovered named component (see DiscoverComponents). If customPath is
// non-empty, component discovery is bypassed entirely and this behaves like
// LoadFeatures(customPath), matching the explicit single-directory override
// semantics of the --definitions flag.
//
// Feature names are expected to be globally unique across the union, since
// they're derived from distinct sysext names. When the same name is defined
// by more than one source, the most specific source wins — a named
// component beats the legacy default directory, and among colliding
// components the alphabetically last one wins — and the collision is
// reported as a warning string rather than an error.
func LoadAllFeatures(customPath string) ([]*Feature, []string, error) {
	if customPath != "" {
		f, err := LoadFeatures(customPath)
		return f, nil, err
	}

	legacy, err := LoadFeatures("")
	if err != nil {
		return nil, nil, err
	}
	components, err := DiscoverComponents()
	if err != nil {
		return nil, nil, err
	}

	byName := make(map[string]*Feature)
	sourceOf := make(map[string]string)
	var order []string
	var warnings []string

	put := func(f *Feature, source string) {
		if prevSource, exists := sourceOf[f.Name]; exists {
			warnings = append(warnings, fmt.Sprintf(
				"feature %q defined in both %s and %s; using %s", f.Name, prevSource, source, source))
		} else {
			order = append(order, f.Name)
		}
		byName[f.Name] = f
		sourceOf[f.Name] = source
	}

	for _, f := range legacy {
		put(f, "the default directory")
	}
	for _, comp := range components {
		cf, err := LoadComponentFeatures(comp.Name)
		if err != nil {
			return nil, nil, err
		}
		for _, f := range cf {
			put(f, fmt.Sprintf("component %q", comp.Name))
		}
	}

	features := make([]*Feature, 0, len(order))
	for _, name := range order {
		features = append(features, byName[name])
	}
	slices.SortFunc(features, func(a, b *Feature) int {
		return cmp.Compare(a.Name, b.Name)
	})

	return features, warnings, nil
}

// loadFeaturesFromPaths loads all .feature files found across searchPaths,
// with earlier paths taking priority for a given filename.
func loadFeaturesFromPaths(searchPaths []string) ([]*Feature, error) {
	// Collect all .feature files, with earlier paths taking priority
	featureFiles, err := collectConfigFiles(searchPaths, featureSuffix)
	if err != nil {
		return nil, err
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
	slices.SortFunc(features, func(a, b *Feature) int {
		return cmp.Compare(a.Name, b.Name)
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
	sortedNames := slices.Sorted(maps.Keys(dropInFiles))

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
