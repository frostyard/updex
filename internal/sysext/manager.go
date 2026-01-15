package sysext

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/frostyard/updex/internal/config"
	"github.com/frostyard/updex/internal/version"
)

// GetInstalledVersions returns the list of installed versions for a transfer config
// Also returns the current version (pointed to by symlink or newest)
func GetInstalledVersions(t *config.Transfer) ([]string, string, error) {
	// Get all target patterns
	patterns := t.Target.MatchPatterns
	if len(patterns) == 0 && t.Target.MatchPattern != "" {
		patterns = []string{t.Target.MatchPattern}
	}
	if len(patterns) == 0 {
		return nil, "", fmt.Errorf("no target match patterns defined")
	}

	// Parse first pattern for symlink checking (backward compat)
	pattern, err := version.ParsePattern(patterns[0])
	if err != nil {
		return nil, "", fmt.Errorf("invalid target pattern: %w", err)
	}

	targetDir := t.Target.Path
	if targetDir == "" {
		targetDir = "/var/lib/extensions"
	}

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
			if v, _, ok := version.ExtractVersionMulti(filepath.Base(target), patterns); ok {
				current = v
			} else if v, ok := pattern.ExtractVersion(filepath.Base(target)); ok {
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

		if v, _, ok := version.ExtractVersionMulti(name, patterns); ok {
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

// GetActiveVersion returns the version currently active in systemd-sysext
// This checks if the extension is currently merged
func GetActiveVersion(t *config.Transfer) (string, error) {
	// Get all target patterns
	patterns := t.Target.MatchPatterns
	if len(patterns) == 0 && t.Target.MatchPattern != "" {
		patterns = []string{t.Target.MatchPattern}
	}
	if len(patterns) == 0 {
		return "", fmt.Errorf("no target match patterns defined")
	}

	// Parse first pattern for symlink checking
	pattern, err := version.ParsePattern(patterns[0])
	if err != nil {
		return "", fmt.Errorf("invalid target pattern: %w", err)
	}

	// First try the current symlink in the target directory
	if t.Target.CurrentSymlink != "" {
		symlinkPath := filepath.Join(t.Target.Path, t.Target.CurrentSymlink)
		if target, err := os.Readlink(symlinkPath); err == nil {
			if v, _, ok := version.ExtractVersionMulti(filepath.Base(target), patterns); ok {
				return v, nil
			} else if v, ok := pattern.ExtractVersion(filepath.Base(target)); ok {
				return v, nil
			}
		}
	}

	// Check /run/extensions for active sysext images
	runExtensions := "/run/extensions"
	entries, err := os.ReadDir(runExtensions)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}

	for _, entry := range entries {
		if v, _, ok := version.ExtractVersionMulti(entry.Name(), patterns); ok {
			return v, nil
		}
	}

	return "", nil
}

// UpdateSymlink updates or creates the current version symlink
func UpdateSymlink(targetDir, symlinkName, targetFile string) error {
	symlinkPath := filepath.Join(targetDir, symlinkName)

	// Remove existing symlink if present
	if _, err := os.Lstat(symlinkPath); err == nil {
		if err := os.Remove(symlinkPath); err != nil {
			return fmt.Errorf("failed to remove existing symlink: %w", err)
		}
	}

	// Create relative symlink
	if err := os.Symlink(targetFile, symlinkPath); err != nil {
		return fmt.Errorf("failed to create symlink: %w", err)
	}

	return nil
}

// Vacuum removes old versions according to InstancesMax
func Vacuum(t *config.Transfer) error {
	_, _, err := VacuumWithDetails(t)
	return err
}

// VacuumWithDetails removes old versions and returns what was removed/kept
func VacuumWithDetails(t *config.Transfer) (removed []string, kept []string, err error) {
	// Get all target patterns
	patterns := t.Target.MatchPatterns
	if len(patterns) == 0 && t.Target.MatchPattern != "" {
		patterns = []string{t.Target.MatchPattern}
	}
	if len(patterns) == 0 {
		return nil, nil, fmt.Errorf("no target match patterns defined")
	}

	targetDir := t.Target.Path
	if targetDir == "" {
		targetDir = "/var/lib/extensions"
	}

	entries, err := os.ReadDir(targetDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("failed to read directory: %w", err)
	}

	// Collect installed versions with their filenames
	type versionFile struct {
		version  string
		filename string
	}
	var installed []versionFile

	for _, entry := range entries {
		name := entry.Name()

		// Skip symlinks
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}

		if v, _, ok := version.ExtractVersionMulti(name, patterns); ok {
			installed = append(installed, versionFile{v, name})
		}
	}

	if len(installed) == 0 {
		return nil, nil, nil
	}

	// Sort by version (newest first)
	versions := make([]string, len(installed))
	for i, vf := range installed {
		versions[i] = vf.version
	}
	version.Sort(versions)

	// Map version to filename
	versionToFile := make(map[string]string)
	for _, vf := range installed {
		versionToFile[vf.version] = vf.filename
	}

	// Determine which to keep
	instancesMax := t.Transfer.InstancesMax
	if instancesMax <= 0 {
		instancesMax = 2
	}

	for i, v := range versions {
		filename := versionToFile[v]
		filepath := filepath.Join(targetDir, filename)

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
		if err := os.Remove(filepath); err != nil {
			return removed, kept, fmt.Errorf("failed to remove %s: %w", filename, err)
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

// SysextDir is the directory where systemd-sysext looks for extensions
const SysextDir = "/var/lib/extensions"

// LinkToSysext creates a symlink in /var/lib/extensions pointing to the extension
// in the staging directory (e.g., /var/lib/extensions.d/)
func LinkToSysext(t *config.Transfer) error {
	if t.Target.CurrentSymlink == "" {
		return fmt.Errorf("no CurrentSymlink defined in transfer config")
	}

	// Source: the symlink in the staging directory (e.g., /var/lib/extensions.d/vscode.raw)
	stagingSymlink := filepath.Join(t.Target.Path, t.Target.CurrentSymlink)

	// Resolve the actual target file the staging symlink points to
	actualTarget, err := os.Readlink(stagingSymlink)
	if err != nil {
		return fmt.Errorf("failed to read staging symlink %s: %w", stagingSymlink, err)
	}

	// Build the full path to the actual file
	var actualTargetPath string
	if filepath.IsAbs(actualTarget) {
		actualTargetPath = actualTarget
	} else {
		actualTargetPath = filepath.Join(t.Target.Path, actualTarget)
	}

	// Destination: symlink in /var/lib/extensions with the extension name
	// Use the CurrentSymlink name (e.g., vscode.raw)
	destSymlink := filepath.Join(SysextDir, t.Target.CurrentSymlink)

	// Ensure the sysext directory exists
	if err := os.MkdirAll(SysextDir, 0755); err != nil {
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

// UnlinkFromSysext removes the extension symlink from /var/lib/extensions
func UnlinkFromSysext(t *config.Transfer) error {
	if t.Target.CurrentSymlink == "" {
		return fmt.Errorf("no CurrentSymlink defined in transfer config")
	}

	destSymlink := filepath.Join(SysextDir, t.Target.CurrentSymlink)

	if _, err := os.Lstat(destSymlink); os.IsNotExist(err) {
		return nil // Already removed
	}

	if err := os.Remove(destSymlink); err != nil {
		return fmt.Errorf("failed to remove symlink %s: %w", destSymlink, err)
	}

	return nil
}

// Refresh calls systemd-sysext refresh to reload extensions
func Refresh() error {
	return runSysextCommand("refresh")
}

// Merge calls systemd-sysext merge to merge extensions
func Merge() error {
	return runSysextCommand("merge")
}

// Unmerge calls systemd-sysext unmerge to unmerge extensions
func Unmerge() error {
	return runSysextCommand("unmerge")
}

// runSysextCommand executes a systemd-sysext subcommand
func runSysextCommand(subcommand string) error {
	cmd := exec.Command("systemd-sysext", subcommand)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("systemd-sysext %s failed: %w", subcommand, err)
	}
	return nil
}
