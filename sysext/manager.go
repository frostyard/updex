package sysext

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/frostyard/updex/v2/config"
	"github.com/frostyard/updex/v2/version"
)

// parseTargetPatterns extracts and parses the version patterns from a transfer's
// target configuration, returning an error if no valid patterns are found.
func parseTargetPatterns(t *config.Transfer) ([]*version.Pattern, error) {
	patternStrs := t.Target.Patterns()
	if len(patternStrs) == 0 {
		return nil, fmt.Errorf("no target match patterns defined")
	}

	patterns, firstErr := version.ParsePatterns(patternStrs)
	if len(patterns) == 0 {
		return nil, fmt.Errorf("invalid target pattern: %w", firstErr)
	}

	return patterns, nil
}

func targetDirAt(t *config.Transfer, defaultDir string) string {
	if t.Target.Path != "" {
		return t.Target.Path
	}
	return defaultDir
}

// GetInstalledVersions returns the list of installed versions for a transfer config
// Also returns the current version (pointed to by symlink or newest)
func GetInstalledVersions(t *config.Transfer) ([]string, string, error) {
	return GetInstalledVersionsAt(t, SysextDir)
}

// GetInstalledVersionsAt is GetInstalledVersions with an explicit fallback
// directory for transfers that omit Target.Path.
func GetInstalledVersionsAt(t *config.Transfer, defaultDir string) ([]string, string, error) {
	patterns, err := parseTargetPatterns(t)
	if err != nil {
		return nil, "", err
	}

	targetDir := targetDirAt(t, defaultDir)

	entries, err := os.ReadDir(targetDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", nil
		}
		return nil, "", fmt.Errorf("failed to read directory: %w", err)
	}

	var versions []string
	var current string

	// Check for current symlink
	if t.Target.CurrentSymlink != "" {
		symlinkPath := filepath.Join(targetDir, t.Target.CurrentSymlink)
		if target, err := os.Readlink(symlinkPath); err == nil {
			// Extract version from symlink target using any pattern
			if v, _, ok := version.ExtractVersionParsed(filepath.Base(target), patterns); ok {
				current = v
			}
		}
	}

	for _, entry := range entries {
		name := entry.Name()

		// Skip symlinks when counting versions
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}

		if v, _, ok := version.ExtractVersionParsed(name, patterns); ok {
			versions = append(versions, v)
		}
	}

	// If no current symlink, newest is current
	if current == "" && len(versions) > 0 {
		version.Sort(versions)
		current = versions[0]
	}

	return versions, current, nil
}

// GetActiveVersion returns the version currently active in systemd-sysext.
// It checks the legacy current symlink, the merged-state metadata under the
// production hierarchies, and the production runtime image search directory.
func GetActiveVersion(t *config.Transfer) (string, error) {
	return GetActiveVersionWith(t, ActiveStatePaths{
		DefaultDir:       SysextDir,
		RunExtensionsDir: RunExtensionsDir,
		Hierarchies:      slices.Clone(MergedHierarchies),
	})
}

// GetActiveVersionAt is GetActiveVersion with an explicit fallback directory
// for transfers that omit Target.Path.
func GetActiveVersionAt(t *config.Transfer, defaultDir string) (string, error) {
	return GetActiveVersionWith(t, ActiveStatePaths{
		DefaultDir:       defaultDir,
		RunExtensionsDir: RunExtensionsDir,
		Hierarchies:      slices.Clone(MergedHierarchies),
	})
}

// GetActiveVersionIn is GetActiveVersion with explicit target fallback and
// runtime image search directories. It does not read the merged-state metadata
// under systemd-sysext hierarchies; prefer GetActiveVersionWith.
func GetActiveVersionIn(t *config.Transfer, defaultDir, runExtensionsDir string) (string, error) {
	return GetActiveVersionWith(t, ActiveStatePaths{
		DefaultDir:       defaultDir,
		RunExtensionsDir: runExtensionsDir,
	})
}

// ActiveStatePaths names the places GetActiveVersionWith reads to decide
// whether a transfer's image is merged into the running system.
type ActiveStatePaths struct {
	// DefaultDir is the fallback directory for a transfer that omits
	// Target.Path. It only locates a legacy CurrentSymlink.
	DefaultDir string

	// RunExtensionsDir is systemd-sysext's runtime image search directory.
	// An image there is merged on the next refresh, so it counts as active.
	// An empty value skips this check.
	RunExtensionsDir string

	// Hierarchies are the hierarchies whose .systemd-sysext directory records
	// the images systemd-sysext merged into them. An empty slice skips this
	// check.
	Hierarchies []string
}

// GetActiveVersionWith reports the version of the transfer's image that is
// active in systemd-sysext, or "" when it is not merged. It checks, in order:
// a legacy CurrentSymlink, the merged-state metadata under each hierarchy, and
// the runtime image search directory. UnknownVersion reports a merged image
// whose merged-state metadata does not record a version.
func GetActiveVersionWith(t *config.Transfer, p ActiveStatePaths) (string, error) {
	patterns, err := parseTargetPatterns(t)
	if err != nil {
		return "", err
	}

	// First try the current symlink in the target directory
	if t.Target.CurrentSymlink != "" {
		symlinkPath := filepath.Join(targetDirAt(t, p.DefaultDir), t.Target.CurrentSymlink)
		if target, err := os.Readlink(symlinkPath); err == nil {
			if v, _, ok := version.ExtractVersionParsed(filepath.Base(target), patterns); ok {
				return v, nil
			}
		}
	}

	// Then read the merged-state metadata systemd-sysext records in each
	// hierarchy. The first hierarchy that names this image wins.
	for _, hierarchy := range p.Hierarchies {
		v, err := mergedVersionInHierarchy(t, patterns, hierarchy)
		if err != nil {
			return "", err
		}
		if v != "" {
			return v, nil
		}
	}

	// Finally, an image in the runtime search directory is merged on the next
	// refresh, so it remains a conservative active signal.
	if p.RunExtensionsDir == "" {
		return "", nil
	}

	entries, err := os.ReadDir(p.RunExtensionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}

	for _, entry := range entries {
		if v, _, ok := version.ExtractVersionParsed(entry.Name(), patterns); ok {
			return v, nil
		}
	}

	return "", nil
}

// MergedVersionIn reports the version of the transfer's image that the
// merged-state metadata under hierarchies records, or "" when no hierarchy
// records it. UnknownVersion reports a merged image whose metadata names no
// version. It reads only the hierarchy metadata (what systemd-sysext merged
// at its last merge or refresh): unlike GetActiveVersionWith it ignores a
// legacy CurrentSymlink and the runtime image search directory, which name an
// image that is staged or will merge, not one that is merged now. The first
// hierarchy that records the image wins. A read error returns
// "read merged state: ...".
func MergedVersionIn(t *config.Transfer, hierarchies []string) (string, error) {
	patterns, err := parseTargetPatterns(t)
	if err != nil {
		return "", err
	}
	for _, hierarchy := range hierarchies {
		v, err := mergedVersionInHierarchy(t, patterns, hierarchy)
		if err != nil {
			return "", err
		}
		if v != "" {
			return v, nil
		}
	}
	return "", nil
}

// mergedExtensionEntry is the part of a systemd-sysext origin entry that
// records the merged image's path.
type mergedExtensionEntry struct {
	Path string `json:"path"`
}

// mergedExtensionOrigin is the part of a systemd-sysext origin file that
// records the images merged into one hierarchy.
type mergedExtensionOrigin struct {
	Extensions map[string]mergedExtensionEntry `json:"extensions"`
}

// mergedVersionInHierarchy reports the version of the transfer's image that
// the merged-state metadata under hierarchy records. It first reads origin; a
// match there returns its version. When origin is absent, does not parse, or
// does not name the image, it also reads the older extensions list, and a
// match there returns UnknownVersion. It returns "" when the hierarchy does
// not record the image.
func mergedVersionInHierarchy(t *config.Transfer, patterns []*version.Pattern, hierarchy string) (string, error) {
	stateDir := filepath.Join(hierarchy, MergedStateDirName)

	data, err := readLimitedFile(filepath.Join(stateDir, "origin"), 1<<20)
	switch {
	case err == nil:
		var origin mergedExtensionOrigin
		if json.Unmarshal(data, &origin) == nil {
			if v, ok := mergedVersionFromOrigin(t, patterns, origin); ok {
				return v, nil
			}
		}
		// origin is malformed or does not name the image: the extensions list
		// can still record it, so do not report "not merged" yet.
	case !errors.Is(err, fs.ErrNotExist):
		return "", fmt.Errorf("read merged state: %w", err)
	}

	data, err = readLimitedFile(filepath.Join(stateDir, "extensions"), 64<<10)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("read merged state: %w", err)
	}

	for line := range strings.SplitSeq(string(data), "\n") {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		if t.Component != "" && name == t.Component {
			return UnknownVersion, nil
		}
		if _, _, ok := version.ExtractVersionParsed(name+targetExtension(t), patterns); ok {
			return UnknownVersion, nil
		}
	}

	return "", nil
}

// mergedVersionFromOrigin reports whether an origin file records the transfer's
// image, and the version its source path yields.
func mergedVersionFromOrigin(t *config.Transfer, patterns []*version.Pattern, origin mergedExtensionOrigin) (string, bool) {
	if t.Component != "" {
		if entry, ok := origin.Extensions[t.Component]; ok {
			if v, _, matched := version.ExtractVersionParsed(filepath.Base(entry.Path), patterns); matched {
				return v, true
			}
			return UnknownVersion, true
		}
	}

	for _, entry := range origin.Extensions {
		if v, _, ok := version.ExtractVersionParsed(filepath.Base(entry.Path), patterns); ok {
			return v, true
		}
	}

	return "", false
}

// readLimitedFile reads at most limit bytes from path.
func readLimitedFile(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return io.ReadAll(io.LimitReader(f, limit))
}

type versionFile struct {
	version  string
	filename string
}

// VacuumPaths names the places the vacuum reads to decide which staged
// images it must keep, besides the newest InstancesMax versions and
// ProtectVersion=. See VacuumWith.
type VacuumPaths struct {
	// DefaultDir is the fallback directory for a transfer that omits
	// Target.Path. It also locates a legacy CurrentSymlink, whose target is
	// always kept.
	DefaultDir string

	// LinkDir is the directory that holds the systemd-sysext link
	// (<LinkDir>/<SysextLinkName(t)>). The version that link names is kept,
	// because systemd-sysext merges it at the next boot. An empty value skips
	// this protection.
	LinkDir string

	// Hierarchies are the hierarchies whose .systemd-sysext directory records
	// the images merged now (ADR-0015). The version recorded there is kept,
	// because the running system uses it. A merged image whose version the
	// metadata does not record (UnknownVersion) protects nothing. An empty
	// slice skips this protection.
	Hierarchies []string

	// ProtectVersions are further versions to keep, such as a version that a
	// rollback rejected. Empty entries are ignored.
	ProtectVersions []string
}

// PlanVacuumAfterInstall returns the versions that vacuum would remove/keep
// after installing activeVersion, without deleting files.
func PlanVacuumAfterInstall(t *config.Transfer, activeVersion string) (removed []string, kept []string, err error) {
	return PlanVacuumAfterInstallAt(t, activeVersion, SysextDir)
}

// PlanVacuumAfterInstallAt is PlanVacuumAfterInstall with an explicit
// fallback directory for transfers that omit Target.Path. It does not protect
// the linked or the merged version; prefer PlanVacuumWith.
func PlanVacuumAfterInstallAt(t *config.Transfer, activeVersion, defaultDir string) (removed []string, kept []string, err error) {
	return PlanVacuumWith(t, activeVersion, VacuumPaths{DefaultDir: defaultDir})
}

// PlanVacuumWith reports what VacuumWith would remove and keep after
// installing activeVersion, without deleting files. activeVersion is always
// kept; it counts as installed even when its file is not on disk yet, so a
// dry run can plan for a download. An empty activeVersion reads the legacy
// CurrentSymlink instead.
func PlanVacuumWith(t *config.Transfer, activeVersion string, p VacuumPaths) (removed []string, kept []string, err error) {
	return planVacuum(t, activeVersion, false, p)
}

// Vacuum removes old versions according to InstancesMax
func Vacuum(t *config.Transfer) error {
	return VacuumAt(t, SysextDir)
}

// VacuumAt is Vacuum with an explicit fallback directory for transfers that
// omit Target.Path. It does not protect the linked or the merged version;
// prefer VacuumWith.
func VacuumAt(t *config.Transfer, defaultDir string) error {
	_, _, err := vacuumWithDetailsAt(t, defaultDir)
	return err
}

// VacuumWithDetails removes old versions and returns what was removed/kept
func VacuumWithDetails(t *config.Transfer) (removed []string, kept []string, err error) {
	return vacuumWithDetailsAt(t, SysextDir)
}

func vacuumWithDetailsAt(t *config.Transfer, defaultDir string) (removed []string, kept []string, err error) {
	return VacuumWith(t, VacuumPaths{DefaultDir: defaultDir})
}

// VacuumWith removes the transfer's staged images that nothing needs, and
// returns the versions it removed and kept. It keeps, for the transfer:
//
//   - the version that the sysext link names (p.LinkDir);
//   - the version merged now (p.Hierarchies);
//   - p.ProtectVersions and the transfer's ProtectVersion=;
//   - the newest InstancesMax versions (default 2); and
//   - the version that the legacy CurrentSymlink names.
//
// Protected versions are kept in addition to the newest InstancesMax
// versions; a protected version that is among the newest counts as one of
// them. A host that has linked a new revision but still runs an older one
// therefore keeps up to InstancesMax+1 images.
//
// The vacuum never guesses: when it cannot read the link or the merged-state
// metadata, it returns an error and deletes nothing. A dangling link protects
// nothing, because it names no image.
func VacuumWith(t *config.Transfer, p VacuumPaths) (removed []string, kept []string, err error) {
	return planVacuum(t, "", true, p)
}

// protectedVersions returns the versions the vacuum must keep for reasons
// other than their rank: linked, merged and explicitly protected versions.
// It reads the link and the merged-state metadata before the vacuum deletes
// anything, and fails on a read error instead of guessing.
func protectedVersions(t *config.Transfer, patterns []*version.Pattern, p VacuumPaths) (map[string]bool, error) {
	protected := make(map[string]bool)

	if t.Transfer.ProtectVersion != "" {
		protected[t.Transfer.ProtectVersion] = true
	}
	for _, v := range p.ProtectVersions {
		if v != "" {
			protected[v] = true
		}
	}

	if p.LinkDir != "" {
		linked, err := LinkedVersionAt(t, p.LinkDir)
		if err != nil {
			return nil, fmt.Errorf("read linked version: %w", err)
		}
		if linked != "" {
			protected[linked] = true
		}
	}

	for _, hierarchy := range p.Hierarchies {
		merged, err := mergedVersionInHierarchy(t, patterns, hierarchy)
		if err != nil {
			return nil, fmt.Errorf("read merged version: %w", err)
		}
		if merged != "" && merged != UnknownVersion {
			protected[merged] = true
		}
	}

	return protected, nil
}

func planVacuum(t *config.Transfer, activeVersionOverride string, remove bool, p VacuumPaths) (removed []string, kept []string, err error) {
	patterns, err := parseTargetPatterns(t)
	if err != nil {
		return nil, nil, err
	}

	targetDir := targetDirAt(t, p.DefaultDir)

	entries, err := os.ReadDir(targetDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("failed to read directory: %w", err)
	}

	var installed []versionFile
	seenVersions := make(map[string]bool)

	for _, entry := range entries {
		name := entry.Name()

		// Skip symlinks
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}

		if v, _, ok := version.ExtractVersionParsed(name, patterns); ok {
			installed = append(installed, versionFile{v, name})
			seenVersions[v] = true
		}
	}

	if activeVersionOverride != "" && !seenVersions[activeVersionOverride] {
		installed = append(installed, versionFile{version: activeVersionOverride})
	}

	if len(installed) == 0 {
		return nil, nil, nil
	}

	// Decide what is protected before anything is removed, so an unreadable
	// link or merged state fails the vacuum with every image still on disk.
	protected, err := protectedVersions(t, patterns, p)
	if err != nil {
		return nil, nil, err
	}

	// Sort by version (newest first)
	slices.SortFunc(installed, func(a, b versionFile) int {
		return version.Compare(b.version, a.version) // Reversed for descending order
	})

	// Determine which to keep
	instancesMax := t.Transfer.InstancesMax
	if instancesMax <= 0 {
		instancesMax = 2
	}

	// Resolve the active version (the one the CurrentSymlink points at). It must
	// never be removed regardless of how it sorts, otherwise vacuum would leave
	// a dangling symlink — which is exactly what happens when the version
	// comparator can't rank a freshly downloaded image above older ones.
	activeVersion := activeVersionOverride
	if activeVersion == "" && t.Target.CurrentSymlink != "" {
		symlinkPath := filepath.Join(targetDir, t.Target.CurrentSymlink)
		if target, err := os.Readlink(symlinkPath); err == nil {
			if v, _, ok := version.ExtractVersionParsed(filepath.Base(target), patterns); ok {
				activeVersion = v
			}
		}
	}

	for i, vf := range installed {
		v := vf.version
		fullPath := filepath.Join(targetDir, vf.filename)

		// Always keep the active (symlinked) version.
		if activeVersion != "" && v == activeVersion {
			kept = append(kept, v)
			continue
		}

		// Always keep the linked, merged and protected versions.
		if protected[v] {
			kept = append(kept, v)
			continue
		}

		// Keep up to InstancesMax versions
		if i < instancesMax {
			kept = append(kept, v)
			continue
		}

		// Remove old version
		if remove {
			if err := os.Remove(fullPath); err != nil {
				return removed, kept, fmt.Errorf("failed to remove %s: %w", vf.filename, err)
			}
		}
		removed = append(removed, v)
	}

	return removed, kept, nil
}

// GetExtensionName extracts the extension name from a filename
// e.g., "myext_1.2.3.raw" -> "myext"
func GetExtensionName(filename string) string {
	// Remove common suffixes
	name := filename
	for _, suffix := range []string{".raw", ".raw.xz", ".raw.gz", ".raw.zst"} {
		name = strings.TrimSuffix(name, suffix)
	}

	// Remove version part (everything after last underscore followed by digits)
	parts := strings.Split(name, "_")
	if len(parts) > 1 {
		// Check if last part looks like a version
		lastPart := parts[len(parts)-1]
		if len(lastPart) > 0 && (lastPart[0] >= '0' && lastPart[0] <= '9') {
			return strings.Join(parts[:len(parts)-1], "_")
		}
	}

	return name
}

// SysextDir is the directory where systemd-sysext looks for extensions.
// Deprecated: inject a SysextLinkDir via updex.RuntimePaths instead of
// mutating this variable. It remains for compatibility wrappers and tests
// that have not yet migrated.
var SysextDir = "/var/lib/extensions"

// RunExtensionsDir is systemd-sysext's runtime image search directory. An
// image there is merged on the next refresh; it does not record which images
// are merged now.
const RunExtensionsDir = "/run/extensions"

// MergedHierarchies lists the hierarchies whose .systemd-sysext metadata
// records the images systemd-sysext merged into them. It is the production
// default of systemd-sysext's hierarchies, and a compatibility default: inject
// a client's own value through updex.RuntimePaths.SysextHierarchies.
var MergedHierarchies = []string{"/usr", "/opt"}

// MergedStateDirName is the directory inside each hierarchy in which
// systemd-sysext records its merged extension state.
const MergedStateDirName = ".systemd-sysext"

// UnknownVersion reports that an image is merged although its merged-state
// metadata does not record the version of the source image.
const UnknownVersion = "unknown"

// SysextLinkName returns the /var/lib/extensions link name for a transfer.
func SysextLinkName(t *config.Transfer) string {
	if t.Component == "" || len(t.Target.Patterns()) == 0 {
		return ""
	}
	return t.Component + targetExtension(t)
}

func targetExtension(t *config.Transfer) string {
	patterns := t.Target.Patterns()
	if len(patterns) == 0 {
		return ""
	}
	name := stripCompressionSuffix(patterns[0])
	return filepath.Ext(name)
}

func stripCompressionSuffix(name string) string {
	lower := strings.ToLower(name)
	for _, suffix := range []string{".zstd", ".zst", ".xz", ".gz"} {
		if strings.HasSuffix(lower, suffix) {
			return name[:len(name)-len(suffix)]
		}
	}
	return name
}

func installedVersionFilesAt(t *config.Transfer, defaultDir string) ([]versionFile, error) {
	patterns, err := parseTargetPatterns(t)
	if err != nil {
		return nil, err
	}

	dir := targetDirAt(t, defaultDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	var files []versionFile
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		if v, _, ok := version.ExtractVersionParsed(entry.Name(), patterns); ok {
			files = append(files, versionFile{version: v, filename: entry.Name()})
		}
	}

	slices.SortFunc(files, func(a, b versionFile) int {
		return version.Compare(b.version, a.version)
	})
	return files, nil
}

// RemoveLegacyCurrentSymlink removes the staging CurrentSymlink if one is configured.
func RemoveLegacyCurrentSymlink(t *config.Transfer) error {
	return RemoveLegacyCurrentSymlinkAt(t, SysextDir)
}

// RemoveLegacyCurrentSymlinkAt is RemoveLegacyCurrentSymlink with an explicit
// fallback directory for transfers that omit Target.Path.
func RemoveLegacyCurrentSymlinkAt(t *config.Transfer, defaultDir string) error {
	if t.Target.CurrentSymlink == "" {
		return nil
	}

	symlinkPath := filepath.Join(targetDirAt(t, defaultDir), t.Target.CurrentSymlink)
	info, err := os.Lstat(symlinkPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to stat legacy symlink %s: %w", symlinkPath, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return nil
	}
	if err := os.Remove(symlinkPath); err != nil {
		return fmt.Errorf("failed to remove legacy symlink %s: %w", symlinkPath, err)
	}
	return nil
}

// LinkToSysextAt creates a symlink in sysextDir pointing to the newest
// installed extension file in the staging directory (e.g., /var/lib/extensions.d/).
// The explicit sysextDir allows multiple clients to target independent link
// directories without touching the package-global SysextDir.
func LinkToSysextAt(t *config.Transfer, sysextDir string) error {
	linkName := SysextLinkName(t)
	if linkName == "" {
		return fmt.Errorf("cannot determine sysext link name")
	}

	files, err := installedVersionFilesAt(t, sysextDir)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no installed versions found for %s", t.Component)
	}

	return linkImageAt(t, sysextDir, files[0].filename)
}

// StagedVersionsAt lists the versions of the transfer's images that are
// staged on disk (regular files in the target directory that match the target
// patterns), newest first. defaultDir is the fallback directory for a
// transfer that omits Target.Path. It returns nil with a nil error when the
// directory does not exist.
func StagedVersionsAt(t *config.Transfer, defaultDir string) ([]string, error) {
	files, err := installedVersionFilesAt(t, defaultDir)
	if err != nil {
		return nil, err
	}
	var versions []string
	for _, f := range files {
		if !slices.Contains(versions, f.version) {
			versions = append(versions, f.version)
		}
	}
	return versions, nil
}

// LinkVersionAt creates (or atomically replaces) the symlink
// <sysextDir>/<SysextLinkName(t)> so that it points to the staged image of
// exactly the given version, not the newest one. It is what a rollback uses.
// It errors, and changes nothing, when that version is not staged.
func LinkVersionAt(t *config.Transfer, sysextDir, ver string) error {
	linkName := SysextLinkName(t)
	if linkName == "" {
		return fmt.Errorf("cannot determine sysext link name")
	}

	files, err := installedVersionFilesAt(t, sysextDir)
	if err != nil {
		return err
	}
	for _, f := range files {
		if f.version == ver {
			return linkImageAt(t, sysextDir, f.filename)
		}
	}
	return fmt.Errorf("version %s of %s is not on disk", ver, t.Component)
}

// StagedImageAt returns the path of the staged image of exactly version for
// the transfer: the file LinkVersionAt links for that version. defaultDir is
// the fallback directory for a transfer that omits Target.Path. It reports ""
// with a nil error when no staged file has that version. It reads no image.
func StagedImageAt(t *config.Transfer, defaultDir, ver string) (string, error) {
	files, err := installedVersionFilesAt(t, defaultDir)
	if err != nil {
		return "", err
	}
	for _, f := range files {
		if f.version == ver {
			return filepath.Join(targetDirAt(t, defaultDir), f.filename), nil
		}
	}
	return "", nil
}

// linkImageAt points <sysextDir>/<SysextLinkName(t)> at the staged file
// filename (a base name in the transfer's target directory).
func linkImageAt(t *config.Transfer, sysextDir, filename string) error {
	linkName := SysextLinkName(t)
	actualTargetPath := filepath.Join(targetDirAt(t, sysextDir), filename)
	destSymlink := filepath.Join(sysextDir, linkName)

	// Ensure the sysext directory exists
	if err := os.MkdirAll(sysextDir, 0755); err != nil {
		return fmt.Errorf("failed to create sysext directory: %w", err)
	}

	// Replace atomically: create the new symlink beside the destination and
	// rename it over the old one, so systemd-sysext never observes a moment
	// with no link at all. rename(2) replaces an existing symlink or regular
	// file in place and refuses a directory (it is preserved); the temp is
	// removed if anything fails.
	replacing := false
	if info, err := os.Lstat(destSymlink); err == nil {
		replacing = info.Mode()&os.ModeSymlink != 0 || info.Mode().IsRegular()
	}
	fail := func(err error) error {
		if replacing {
			return fmt.Errorf("failed to replace existing %s: %w", destSymlink, err)
		}
		return fmt.Errorf("failed to create symlink %s -> %s: %w", destSymlink, actualTargetPath, err)
	}
	tempSymlink := fmt.Sprintf("%s.tmp-%d-%d", destSymlink, os.Getpid(), time.Now().UnixNano())
	if err := os.Symlink(actualTargetPath, tempSymlink); err != nil {
		return fail(err)
	}
	if err := renameLink(tempSymlink, destSymlink); err != nil {
		_ = os.Remove(tempSymlink)
		return fail(err)
	}

	return nil
}

// renameLink is os.Rename, replaceable by tests that inject a rename failure
// to prove LinkToSysextAt leaves no temp symlink behind.
var renameLink = os.Rename

// LinkTargetAt returns the path LinkToSysextAt links <sysextDir>/<link name>
// to: the newest installed image for the transfer in its staging directory.
// It errors when the transfer has no usable target pattern or no installed
// image.
func LinkTargetAt(t *config.Transfer, sysextDir string) (string, error) {
	if SysextLinkName(t) == "" {
		return "", fmt.Errorf("cannot determine sysext link name")
	}
	files, err := installedVersionFilesAt(t, sysextDir)
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "", fmt.Errorf("no installed versions found for %s", t.Component)
	}
	return filepath.Join(targetDirAt(t, sysextDir), files[0].filename), nil
}

// LinkIsCurrentAt reports whether <sysextDir>/<link name> is a symlink that
// resolves to LinkTargetAt: false when the link is absent, dangling, not a
// symlink, or points somewhere else. The error is LinkTargetAt's.
func LinkIsCurrentAt(t *config.Transfer, sysextDir string) (bool, error) {
	want, err := LinkTargetAt(t, sysextDir)
	if err != nil {
		return false, err
	}
	linkPath := filepath.Join(sysextDir, SysextLinkName(t))
	info, err := os.Lstat(linkPath)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return false, nil
	}
	target, err := os.Readlink(linkPath)
	if err != nil {
		return false, nil
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(linkPath), target)
	}
	if filepath.Clean(target) != filepath.Clean(want) {
		return false, nil
	}
	if _, err := os.Stat(linkPath); err != nil {
		return false, nil // dangling
	}
	return true, nil
}

// LinkedVersionAt reports the version of the image that the transfer's
// systemd-sysext link (<sysextDir>/<SysextLinkName(t)>) points to. That link is
// the one systemd-sysext merges, so this is the image the running host uses
// even when another updater has advanced the staging symlink past it.
//
// It returns "" with a nil error when the link is absent, is not a symlink, is
// dangling, or points at a base name the transfer's target patterns do not
// match. A relative link target is resolved against sysextDir. The error is
// returned only when the link could not be inspected (a failed stat or
// readlink) or the transfer has no usable target pattern.
func LinkedVersionAt(t *config.Transfer, sysextDir string) (string, error) {
	linkName := SysextLinkName(t)
	if linkName == "" {
		return "", nil
	}

	patterns, err := parseTargetPatterns(t)
	if err != nil {
		return "", err
	}

	linkPath := filepath.Join(sysextDir, linkName)
	info, err := os.Lstat(linkPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("failed to stat sysext link: %w", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return "", nil
	}

	target, err := os.Readlink(linkPath)
	if err != nil {
		return "", fmt.Errorf("failed to read sysext link: %w", err)
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(linkPath), target)
	}

	v, _, ok := version.ExtractVersionParsed(filepath.Base(target), patterns)
	if !ok {
		return "", nil
	}
	if _, err := os.Stat(linkPath); err != nil {
		return "", nil // dangling
	}
	return v, nil
}

// LinkToSysext creates a symlink in /var/lib/extensions pointing to the newest
// installed extension file in the staging directory (e.g., /var/lib/extensions.d/).
//
// Deprecated: SDK callers should pass a SysextLinkDir via updex.RuntimePaths
// so that links are written to an instance-owned directory rather than the
// process-wide default. This wrapper exists for compatibility.
func LinkToSysext(t *config.Transfer) error {
	return LinkToSysextAt(t, SysextDir)
}

// UnlinkFromSysextAt removes the extension symlink from sysextDir.
// The explicit sysextDir allows multiple clients to target independent link
// directories without touching the package-global SysextDir.
func UnlinkFromSysextAt(t *config.Transfer, sysextDir string) error {
	linkName := SysextLinkName(t)
	if linkName == "" {
		return fmt.Errorf("cannot determine sysext link name")
	}

	destSymlink := filepath.Join(sysextDir, linkName)

	if _, err := os.Lstat(destSymlink); os.IsNotExist(err) {
		return nil // Already removed
	}

	if err := os.Remove(destSymlink); err != nil {
		return fmt.Errorf("failed to remove symlink %s: %w", destSymlink, err)
	}

	return nil
}

// UnlinkFromSysext removes the extension symlink from /var/lib/extensions.
//
// Deprecated: SDK callers should pass a SysextLinkDir via updex.RuntimePaths
// so that links are removed from an instance-owned directory rather than the
// process-wide default. This wrapper exists for compatibility.
func UnlinkFromSysext(t *config.Transfer) error {
	return UnlinkFromSysextAt(t, SysextDir)
}

// RemoveAllVersions removes all versions of a component from the target directory
// and removes the current symlink if it exists. Returns the list of removed files.
func RemoveAllVersions(t *config.Transfer) ([]string, error) {
	return RemoveAllVersionsAt(t, SysextDir)
}

// RemoveAllVersionsAt is RemoveAllVersions with an explicit fallback
// directory for transfers that omit Target.Path.
func RemoveAllVersionsAt(t *config.Transfer, defaultDir string) ([]string, error) {
	patterns, err := parseTargetPatterns(t)
	if err != nil {
		return nil, err
	}

	targetDir := targetDirAt(t, defaultDir)

	entries, err := os.ReadDir(targetDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	var removed []string

	// Remove the current symlink first if it exists
	if t.Target.CurrentSymlink != "" {
		symlinkPath := filepath.Join(targetDir, t.Target.CurrentSymlink)
		if _, err := os.Lstat(symlinkPath); err == nil {
			if err := os.Remove(symlinkPath); err != nil {
				return removed, fmt.Errorf("failed to remove symlink %s: %w", symlinkPath, err)
			}
			removed = append(removed, symlinkPath)
		}
	}

	// Remove all versioned files
	for _, entry := range entries {
		name := entry.Name()

		// Skip symlinks (already handled above)
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}

		// Check if this file matches any of the patterns
		if _, _, ok := version.ExtractVersionParsed(name, patterns); ok {
			filePath := filepath.Join(targetDir, name)
			if err := os.Remove(filePath); err != nil {
				return removed, fmt.Errorf("failed to remove %s: %w", filePath, err)
			}
			removed = append(removed, filePath)
		}
	}

	return removed, nil
}
