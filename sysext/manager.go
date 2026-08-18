package sysext

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/frostyard/updex/config"
	"github.com/frostyard/updex/version"
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
// It checks the legacy current symlink and the production merged-image
// directory.
func GetActiveVersion(t *config.Transfer) (string, error) {
	return GetActiveVersionIn(t, SysextDir, RunExtensionsDir)
}

// GetActiveVersionAt is GetActiveVersion with an explicit fallback directory
// for transfers that omit Target.Path.
func GetActiveVersionAt(t *config.Transfer, defaultDir string) (string, error) {
	return GetActiveVersionIn(t, defaultDir, RunExtensionsDir)
}

// GetActiveVersionIn is GetActiveVersion with explicit target fallback and
// merged-image directories.
func GetActiveVersionIn(t *config.Transfer, defaultDir, runExtensionsDir string) (string, error) {
	patterns, err := parseTargetPatterns(t)
	if err != nil {
		return "", err
	}

	// First try the current symlink in the target directory
	if t.Target.CurrentSymlink != "" {
		symlinkPath := filepath.Join(targetDirAt(t, defaultDir), t.Target.CurrentSymlink)
		if target, err := os.Readlink(symlinkPath); err == nil {
			if v, _, ok := version.ExtractVersionParsed(filepath.Base(target), patterns); ok {
				return v, nil
			}
		}
	}

	entries, err := os.ReadDir(runExtensionsDir)
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

type versionFile struct {
	version  string
	filename string
}

// PlanVacuumAfterInstall returns the versions that vacuum would remove/keep
// after installing activeVersion, without deleting files.
func PlanVacuumAfterInstall(t *config.Transfer, activeVersion string) (removed []string, kept []string, err error) {
	return PlanVacuumAfterInstallAt(t, activeVersion, SysextDir)
}

// PlanVacuumAfterInstallAt is PlanVacuumAfterInstall with an explicit
// fallback directory for transfers that omit Target.Path.
func PlanVacuumAfterInstallAt(t *config.Transfer, activeVersion, defaultDir string) (removed []string, kept []string, err error) {
	return planVacuum(t, activeVersion, nil, false, defaultDir)
}

// Vacuum removes old versions according to InstancesMax
func Vacuum(t *config.Transfer) error {
	return VacuumAt(t, SysextDir)
}

// VacuumAt is Vacuum with an explicit fallback directory for transfers that
// omit Target.Path.
func VacuumAt(t *config.Transfer, defaultDir string) error {
	_, _, err := vacuumWithDetailsAt(t, defaultDir)
	return err
}

// VacuumWithDetails removes old versions and returns what was removed/kept
func VacuumWithDetails(t *config.Transfer) (removed []string, kept []string, err error) {
	return vacuumWithDetailsAt(t, SysextDir)
}

func vacuumWithDetailsAt(t *config.Transfer, defaultDir string) (removed []string, kept []string, err error) {
	return planVacuum(t, "", nil, true, defaultDir)
}

func planVacuum(t *config.Transfer, activeVersionOverride string, extraVersions []string, remove bool, defaultDir string) (removed []string, kept []string, err error) {
	patterns, err := parseTargetPatterns(t)
	if err != nil {
		return nil, nil, err
	}

	targetDir := targetDirAt(t, defaultDir)

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

	for _, v := range extraVersions {
		if v != "" && !seenVersions[v] {
			installed = append(installed, versionFile{version: v})
			seenVersions[v] = true
		}
	}
	if activeVersionOverride != "" && !seenVersions[activeVersionOverride] {
		installed = append(installed, versionFile{version: activeVersionOverride})
	}

	if len(installed) == 0 {
		return nil, nil, nil
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

		// Always keep protected versions
		if t.Transfer.ProtectVersion != "" && v == t.Transfer.ProtectVersion {
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

// RunExtensionsDir is systemd-sysext's merged-image state directory.
const RunExtensionsDir = "/run/extensions"

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

	actualTargetPath := filepath.Join(targetDirAt(t, sysextDir), files[0].filename)
	destSymlink := filepath.Join(sysextDir, linkName)

	// Ensure the sysext directory exists
	if err := os.MkdirAll(sysextDir, 0755); err != nil {
		return fmt.Errorf("failed to create sysext directory: %w", err)
	}

	// Remove existing symlink or file if present
	if info, err := os.Lstat(destSymlink); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || info.Mode().IsRegular() {
			if err := os.Remove(destSymlink); err != nil {
				return fmt.Errorf("failed to remove existing %s: %w", destSymlink, err)
			}
		}
	}

	// Create symlink to the actual target file
	if err := os.Symlink(actualTargetPath, destSymlink); err != nil {
		return fmt.Errorf("failed to create symlink %s -> %s: %w", destSymlink, actualTargetPath, err)
	}

	return nil
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
