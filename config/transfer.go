package config

import (
	"cmp"
	"fmt"
	"os"
	"runtime"
	"slices"
	"strings"

	"gopkg.in/ini.v1"
)

const transferSuffix = ".transfer"

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
	Type           string   // Target type (regular-file, directory, partition, etc.)
	Path           string   // Target directory path
	PathRelativeTo string   // Base directory Path is relative to (e.g. "boot"); used by non-sysext OS transfers
	MatchPattern   string   // Primary pattern with @v placeholder for version (first pattern)
	MatchPatterns  []string // All patterns (for matching different compression formats)
	CurrentSymlink string   // Optional legacy staging symlink name
	Mode           uint32   // File mode (e.g., 0644)
	ReadOnly       bool     // Whether to set read-only flag
}

// Patterns returns MatchPatterns if non-empty, falling back to
// []string{MatchPattern} if MatchPattern is set. Returns nil when both are empty.
func (s SourceSection) Patterns() []string {
	if len(s.MatchPatterns) > 0 {
		return s.MatchPatterns
	}
	if s.MatchPattern != "" {
		return []string{s.MatchPattern}
	}
	return nil
}

// Patterns returns MatchPatterns if non-empty, falling back to
// []string{MatchPattern} if MatchPattern is set. Returns nil when both are empty.
func (t TargetSection) Patterns() []string {
	if len(t.MatchPatterns) > 0 {
		return t.MatchPatterns
	}
	if t.MatchPattern != "" {
		return []string{t.MatchPattern}
	}
	return nil
}

// LoadTransfers loads all .transfer files from the specified directory or
// the legacy default search paths (/etc/sysupdate.d, /run/sysupdate.d, ...).
// It does not discover named components or filter non-sysext transfers; see
// LoadAllTransfers and LoadComponentTransfers for that.
func LoadTransfers(customPath string) ([]*Transfer, error) {
	if customPath != "" {
		return loadTransfersFromPaths([]string{customPath})
	}
	return loadTransfersFromPaths(defaultSearchPaths())
}

// LoadComponentTransfers loads .transfer files for a single named component,
// following its own /etc > /run > /usr/local/lib > /usr/lib precedence (see
// ComponentSearchPaths). Pass "" for the legacy default component. It does
// not filter non-sysext transfers; see FilterSysextTransfers.
func LoadComponentTransfers(name string) ([]*Transfer, error) {
	return loadTransfersFromPaths(ComponentSearchPaths(name))
}

// LoadAllTransfers loads the transfer domain updex operates on by default:
// the union of the legacy default sysupdate.d directory and every
// discovered named component (see DiscoverComponents), keeping only
// sysext-shaped transfers (see FilterSysextTransfers). If customPath is
// non-empty, component discovery is bypassed entirely and this behaves like
// LoadTransfers(customPath) with the sysext filter applied, matching the
// explicit single-directory override semantics of the --definitions flag.
//
// Transfer names are expected to be globally unique across the union, since
// they're derived from distinct sysext names. When the same name is defined
// by more than one source, the most specific source wins — a named
// component beats the legacy default directory, and among colliding
// components the alphabetically last one wins — and the collision is
// reported as a warning string rather than an error.
func LoadAllTransfers(customPath string) ([]*Transfer, []string, error) {
	if customPath != "" {
		t, err := LoadTransfers(customPath)
		if err != nil {
			return nil, nil, err
		}
		return FilterSysextTransfers(t), nil, nil
	}

	legacy, err := LoadTransfers("")
	if err != nil {
		return nil, nil, err
	}
	components, err := DiscoverComponents()
	if err != nil {
		return nil, nil, err
	}

	byName := make(map[string]*Transfer)
	sourceOf := make(map[string]string)
	var order []string
	var warnings []string

	put := func(t *Transfer, source string) {
		if prevSource, exists := sourceOf[t.Component]; exists {
			warnings = append(warnings, fmt.Sprintf(
				"transfer %q defined in both %s and %s; using %s", t.Component, prevSource, source, source))
		} else {
			order = append(order, t.Component)
		}
		byName[t.Component] = t
		sourceOf[t.Component] = source
	}

	for _, t := range FilterSysextTransfers(legacy) {
		put(t, "the default directory")
	}
	for _, comp := range components {
		ct, err := LoadComponentTransfers(comp.Name)
		if err != nil {
			return nil, nil, err
		}
		for _, t := range FilterSysextTransfers(ct) {
			put(t, fmt.Sprintf("component %q", comp.Name))
		}
	}

	transfers := make([]*Transfer, 0, len(order))
	for _, name := range order {
		transfers = append(transfers, byName[name])
	}
	slices.SortFunc(transfers, func(a, b *Transfer) int {
		return cmp.Compare(a.Component, b.Component)
	})

	return transfers, warnings, nil
}

// loadTransfersFromPaths loads all .transfer files found across
// searchPaths, with earlier paths taking priority for a given filename.
func loadTransfersFromPaths(searchPaths []string) ([]*Transfer, error) {
	// Collect all .transfer files, with earlier paths taking priority
	transferFiles, err := collectConfigFiles(searchPaths, transferSuffix)
	if err != nil {
		return nil, err
	}

	if len(transferFiles) == 0 {
		return nil, nil
	}

	// Parse all transfer files
	specCtx := newSpecifierContext()
	var transfers []*Transfer
	for component, filePath := range transferFiles {
		t, err := parseTransferFile(filePath, component, specCtx)
		if err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", filePath, err)
		}
		transfers = append(transfers, t)
	}

	// Sort by component name for consistent ordering
	slices.SortFunc(transfers, func(a, b *Transfer) int {
		return cmp.Compare(a.Component, b.Component)
	})

	return transfers, nil
}

// IsSysextTransfer reports whether t has the shape updex supports: a
// url-file source downloaded to a regular-file target inside an extensions
// staging directory. Native OS images share the legacy default sysupdate.d
// directory with non-sysext transfers — GPT "partition" targets for the A/B
// root, and a "regular-file" target relative to the ESP for the UKI (see
// sysupdate.d(5), Target's PathRelativeTo=) — which updex must ignore
// rather than fail on.
func IsSysextTransfer(t *Transfer) bool {
	if t.Source.Type != "url-file" {
		return false
	}
	if t.Target.Type != "" && t.Target.Type != "regular-file" {
		return false
	}
	if t.Target.PathRelativeTo != "" {
		return false
	}
	return true
}

// FilterSysextTransfers returns the subset of transfers that are
// sysext-shaped url-file-to-regular-file transfers (see IsSysextTransfer),
// silently dropping OS transfers such as A/B partition updates or the UKI.
func FilterSysextTransfers(transfers []*Transfer) []*Transfer {
	var filtered []*Transfer
	for _, t := range transfers {
		if IsSysextTransfer(t) {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

func parseTransferFile(filePath, component string, specCtx *specifierContext) (*Transfer, error) {
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
			Path: "/var/lib/extensions.d", // Default staging path
			Mode: 0644,                    // Default file mode
		},
	}

	// Parse [Transfer] section
	if sec, err := cfg.GetSection("Transfer"); err == nil {
		if key, err := sec.GetKey("MinVersion"); err == nil {
			t.Transfer.MinVersion = key.String()
		}
		if key, err := sec.GetKey("ProtectVersion"); err == nil {
			t.Transfer.ProtectVersion = expandSpecifiers(key.String(), specCtx)
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
			// Handle multiple patterns (space-separated alternatives).
			// Specifiers (%a, %v, %w, …) are expanded before the patterns are used.
			patterns := parsePatterns(key.String())
			for i, p := range patterns {
				patterns[i] = expandSpecifiers(p, specCtx)
			}
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
		if key, err := sec.GetKey("PathRelativeTo"); err == nil {
			t.Target.PathRelativeTo = key.String()
		}
		if key, err := sec.GetKey("MatchPattern"); err == nil {
			// Handle multiple patterns (space-separated alternatives).
			// Specifiers are expanded here for the same reason as Source.MatchPattern.
			patterns := parsePatterns(key.String())
			for i, p := range patterns {
				patterns[i] = expandSpecifiers(p, specCtx)
			}
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

// specifierContext caches values that are constant for the lifetime of a
// LoadTransfers call, avoiding repeated syscalls and file reads.
type specifierContext struct {
	osRelease     map[string]string
	hostname      string
	shortHostname string
	bootID        string
	machineID     string
	kernelRelease string
}

func newSpecifierContext() *specifierContext {
	osRelease := readOSRelease()
	hostname, _ := os.Hostname()
	shortHostname := hostname
	if dot := strings.IndexByte(shortHostname, '.'); dot >= 0 {
		shortHostname = shortHostname[:dot]
	}
	return &specifierContext{
		osRelease:     osRelease,
		hostname:      hostname,
		shortHostname: shortHostname,
		bootID:        readFileOneLine("/proc/sys/kernel/random/boot_id"),
		machineID:     readFileOneLine("/etc/machine-id"),
		kernelRelease: readFileOneLine("/proc/sys/kernel/osrelease"),
	}
}

// expandSpecifiers expands systemd-style %x specifiers per sysupdate.d(5).
// It performs a single left-to-right scan so that %% → % cannot trigger
// a second round of expansion.
func expandSpecifiers(s string, ctx *specifierContext) string {
	if !strings.ContainsRune(s, '%') {
		return s
	}

	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] != '%' || i+1 >= len(s) {
			b.WriteByte(s[i])
			i++
			continue
		}
		// We have a % followed by at least one character.
		var repl string
		switch s[i+1] {
		case 'A':
			repl = ctx.osRelease["IMAGE_VERSION"]
		case 'a':
			repl = goarchToSystemdArch()
		case 'B':
			repl = ctx.osRelease["BUILD_ID"]
		case 'b':
			repl = ctx.bootID
		case 'H':
			repl = ctx.hostname
		case 'l':
			repl = ctx.shortHostname
		case 'M':
			repl = ctx.osRelease["IMAGE_ID"]
		case 'm':
			repl = ctx.machineID
		case 'o':
			repl = ctx.osRelease["ID"]
		case 'T':
			repl = "/tmp"
		case 'V':
			repl = "/var/tmp"
		case 'v':
			repl = ctx.kernelRelease
		case 'w':
			repl = ctx.osRelease["VERSION_ID"]
		case 'W':
			repl = ctx.osRelease["VARIANT_ID"]
		case '%':
			repl = "%"
		default:
			// Unknown specifier — leave as-is.
			b.WriteByte(s[i])
			i++
			continue
		}
		b.WriteString(repl)
		i += 2
	}
	return b.String()
}

// goarchToSystemd maps Go architecture identifiers to systemd's naming convention.
// See the systemd architecture table in systemd.unit(5).
var goarchToSystemd = map[string]string{
	"amd64":    "x86-64",
	"386":      "x86",
	"arm64":    "arm64",
	"arm":      "arm",
	"riscv64":  "riscv64",
	"ppc64":    "ppc64",
	"ppc64le":  "ppc64-le",
	"s390x":    "s390x",
	"mips":     "mips",
	"mipsle":   "mips-le",
	"mips64":   "mips64",
	"mips64le": "mips64-le",
	"loong64":  "loongarch64",
}

func goarchToSystemdArch() string {
	if arch, ok := goarchToSystemd[runtime.GOARCH]; ok && arch != "" {
		return arch
	}
	return runtime.GOARCH
}

// readFileOneLine returns the first (and usually only) line of a file, trimmed.
func readFileOneLine(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	line, _, _ := strings.Cut(strings.TrimRight(string(data), "\n"), "\n")
	return strings.TrimSpace(line)
}

// readOSRelease reads /etc/os-release and returns key-value pairs.
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

	for line := range strings.SplitSeq(string(data), "\n") {
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
		if slices.Contains(t.Transfer.Features, featureName) {
			result = append(result, t)
			continue
		}
		// Also check RequisiteFeatures
		if slices.Contains(t.Transfer.RequisiteFeatures, featureName) {
			result = append(result, t)
		}
	}
	return result
}
