package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/ini.v1"
)

// Transfer represents a parsed .transfer configuration file
type Transfer struct {
	Component string          // Derived from filename
	FilePath  string          // Path to the .transfer file
	Transfer  TransferSection // [Transfer] section
	Source    SourceSection   // [Source] section
	Target    TargetSection   // [Target] section
}

// TransferSection represents the [Transfer] section of a .transfer file
type TransferSection struct {
	MinVersion        string   // Minimum version to consider
	ProtectVersion    string   // Version to never remove (supports specifiers)
	Verify            bool     // Verify GPG signatures (default: false for this implementation)
	InstancesMax      int      // Maximum number of versions to keep (default: 2)
	Features          []string // Features this transfer belongs to (OR logic: any enabled activates)
	RequisiteFeatures []string // All of these features must be enabled (AND logic)
}

// SourceSection represents the [Source] section of a .transfer file
type SourceSection struct {
	Type          string   // Source type (url-file, url-tar, etc.)
	Path          string   // Base URL or path
	MatchPattern  string   // Primary pattern with @v placeholder for version (first pattern)
	MatchPatterns []string // All patterns (for matching different compression formats)
}

// TargetSection represents the [Target] section of a .transfer file
type TargetSection struct {
	Type           string   // Target type (regular-file, directory, etc.)
	Path           string   // Target directory path
	MatchPattern   string   // Primary pattern with @v placeholder for version (first pattern)
	MatchPatterns  []string // All patterns (for matching different compression formats)
	CurrentSymlink string   // Name of symlink pointing to current version
	Mode           uint32   // File mode (e.g., 0644)
	ReadOnly       bool     // Whether to set read-only flag
}

// Default search paths for .transfer files (in priority order)
var defaultSearchPaths = []string{
	"/etc/sysupdate.d",
	"/run/sysupdate.d",
	"/usr/local/lib/sysupdate.d",
	"/usr/lib/sysupdate.d",
}

// LoadTransfers loads all .transfer files from the specified directory or default paths
func LoadTransfers(customPath string) ([]*Transfer, error) {
	var searchPaths []string

	if customPath != "" {
		searchPaths = []string{customPath}
	} else {
		searchPaths = defaultSearchPaths
	}

	// Collect all .transfer files, with earlier paths taking priority
	transferFiles := make(map[string]string) // component name -> file path

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
			if !strings.HasSuffix(entry.Name(), ".transfer") {
				continue
			}

			component := strings.TrimSuffix(entry.Name(), ".transfer")
			if _, exists := transferFiles[component]; !exists {
				// Earlier paths take priority
				transferFiles[component] = filepath.Join(dir, entry.Name())
			}
		}
	}

	if len(transferFiles) == 0 {
		return nil, nil
	}

	// Parse all transfer files
	var transfers []*Transfer
	for component, filePath := range transferFiles {
		t, err := parseTransferFile(filePath, component)
		if err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", filePath, err)
		}
		transfers = append(transfers, t)
	}

	// Sort by component name for consistent ordering
	sort.Slice(transfers, func(i, j int) bool {
		return transfers[i].Component < transfers[j].Component
	})

	return transfers, nil
}

func parseTransferFile(filePath, component string) (*Transfer, error) {
	cfg, err := ini.Load(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to load INI file: %w", err)
	}

	t := &Transfer{
		Component: component,
		FilePath:  filePath,
		Transfer: TransferSection{
			Verify:       false, // Default to false
			InstancesMax: 2,     // Default to 2
		},
		Target: TargetSection{
			Path: "/var/lib/extensions", // Default sysext path
			Mode: 0644,                  // Default file mode
		},
	}

	// Parse [Transfer] section
	if sec, err := cfg.GetSection("Transfer"); err == nil {
		if key, err := sec.GetKey("MinVersion"); err == nil {
			t.Transfer.MinVersion = key.String()
		}
		if key, err := sec.GetKey("ProtectVersion"); err == nil {
			t.Transfer.ProtectVersion = expandSpecifiers(key.String())
		}
		if key, err := sec.GetKey("Verify"); err == nil {
			t.Transfer.Verify = key.MustBool(false)
		}
		if key, err := sec.GetKey("InstancesMax"); err == nil {
			t.Transfer.InstancesMax = key.MustInt(2)
		}
		if key, err := sec.GetKey("Features"); err == nil {
			t.Transfer.Features = strings.Fields(key.String())
		}
		if key, err := sec.GetKey("RequisiteFeatures"); err == nil {
			t.Transfer.RequisiteFeatures = strings.Fields(key.String())
		}
	}

	// Parse [Source] section
	if sec, err := cfg.GetSection("Source"); err == nil {
		if key, err := sec.GetKey("Type"); err == nil {
			t.Source.Type = key.String()
		}
		if key, err := sec.GetKey("Path"); err == nil {
			t.Source.Path = strings.TrimRight(key.String(), "/")
		}
		if key, err := sec.GetKey("MatchPattern"); err == nil {
			// Handle multiple patterns (space-separated alternatives)
			patterns := parsePatterns(key.String())
			t.Source.MatchPatterns = patterns
			if len(patterns) > 0 {
				t.Source.MatchPattern = patterns[0] // Keep first for backward compat
			}
		}
	} else {
		return nil, fmt.Errorf("missing [Source] section")
	}

	// Parse [Target] section
	if sec, err := cfg.GetSection("Target"); err == nil {
		if key, err := sec.GetKey("Type"); err == nil {
			t.Target.Type = key.String()
		}
		if key, err := sec.GetKey("Path"); err == nil {
			t.Target.Path = key.String()
		}
		if key, err := sec.GetKey("MatchPattern"); err == nil {
			// Handle multiple patterns (space-separated alternatives)
			patterns := parsePatterns(key.String())
			t.Target.MatchPatterns = patterns
			if len(patterns) > 0 {
				t.Target.MatchPattern = patterns[0] // Keep first for backward compat
			}
		}
		if key, err := sec.GetKey("CurrentSymlink"); err == nil {
			t.Target.CurrentSymlink = key.String()
		}
		if key, err := sec.GetKey("Mode"); err == nil {
			var mode uint32
			if _, err := fmt.Sscanf(key.String(), "%o", &mode); err == nil {
				t.Target.Mode = mode
			}
		}
		if key, err := sec.GetKey("ReadOnly"); err == nil {
			t.Target.ReadOnly = key.MustBool(false)
		}
	} else {
		return nil, fmt.Errorf("missing [Target] section")
	}

	// Validate required fields
	if t.Source.Type == "" {
		return nil, fmt.Errorf("Source.Type is required")
	}
	if t.Source.Path == "" {
		return nil, fmt.Errorf("Source.Path is required")
	}
	if t.Source.MatchPattern == "" {
		return nil, fmt.Errorf("Source.MatchPattern is required")
	}
	if t.Target.MatchPattern == "" {
		return nil, fmt.Errorf("Target.MatchPattern is required")
	}

	return t, nil
}

// expandSpecifiers expands systemd-style specifiers in a string
func expandSpecifiers(s string) string {
	// Read os-release for specifier values
	osRelease := readOSRelease()

	replacements := map[string]string{
		"%A": osRelease["IMAGE_VERSION"], // OS image version
		"%a": osRelease["ARCHITECTURE"],  // Architecture
		"%B": osRelease["BUILD_ID"],      // OS build ID
		"%M": osRelease["IMAGE_ID"],      // OS image ID
		"%m": osRelease["ID"],            // OS ID
		"%o": osRelease["ID"],            // OS ID (alternative)
		"%v": osRelease["VERSION_ID"],    // OS version ID
		"%w": osRelease["VARIANT_ID"],    // OS variant ID
		"%%": "%",                        // Literal %
	}

	result := s
	for spec, value := range replacements {
		result = strings.ReplaceAll(result, spec, value)
	}

	return result
}

// readOSRelease reads /etc/os-release and returns key-value pairs
func readOSRelease() map[string]string {
	result := make(map[string]string)

	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		// Try /usr/lib/os-release as fallback
		data, err = os.ReadFile("/usr/lib/os-release")
		if err != nil {
			return result
		}
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := parts[0]
		value := strings.Trim(parts[1], "\"'")
		result[key] = value
	}

	return result
}

// parsePatterns extracts all patterns from a space-separated list of alternatives
// e.g., "foo_@v.raw.xz foo_@v.raw" -> ["foo_@v.raw.xz", "foo_@v.raw"]
func parsePatterns(patterns string) []string {
	patterns = strings.TrimSpace(patterns)
	if patterns == "" {
		return nil
	}
	return strings.Fields(patterns)
}

// FilterTransfersByFeatures filters transfers based on enabled features.
// A transfer is included if:
// - It has no Features and no RequisiteFeatures (standalone, always included)
// - It has Features and at least one of them is enabled (OR logic)
// - It has RequisiteFeatures and all of them are enabled (AND logic)
// Both conditions must be satisfied if both are specified.
func FilterTransfersByFeatures(transfers []*Transfer, features []*Feature) []*Transfer {
	if len(features) == 0 {
		// No features defined, return all transfers
		return transfers
	}

	var filtered []*Transfer
	for _, t := range transfers {
		if isTransferEnabledByFeatures(t, features) {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

// isTransferEnabledByFeatures checks if a transfer should be active based on features
func isTransferEnabledByFeatures(t *Transfer, features []*Feature) bool {
	// Standalone transfers (no feature requirements) are always enabled
	if len(t.Transfer.Features) == 0 && len(t.Transfer.RequisiteFeatures) == 0 {
		return true
	}

	// Check Features (OR logic): at least one must be enabled
	if len(t.Transfer.Features) > 0 {
		found := false
		for _, featureName := range t.Transfer.Features {
			if IsFeatureEnabled(features, featureName) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check RequisiteFeatures (AND logic): all must be enabled
	if len(t.Transfer.RequisiteFeatures) > 0 {
		for _, featureName := range t.Transfer.RequisiteFeatures {
			if !IsFeatureEnabled(features, featureName) {
				return false
			}
		}
	}

	return true
}

// GetTransfersForFeature returns all transfers that belong to a specific feature
func GetTransfersForFeature(transfers []*Transfer, featureName string) []*Transfer {
	var result []*Transfer
	for _, t := range transfers {
		for _, f := range t.Transfer.Features {
			if f == featureName {
				result = append(result, t)
				break
			}
		}
		// Also check RequisiteFeatures
		for _, f := range t.Transfer.RequisiteFeatures {
			if f == featureName {
				// Avoid duplicates
				alreadyAdded := false
				for _, r := range result {
					if r == t {
						alreadyAdded = true
						break
					}
				}
				if !alreadyAdded {
					result = append(result, t)
				}
				break
			}
		}
	}
	return result
}
