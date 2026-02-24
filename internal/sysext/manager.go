package sysext

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/frostyard/updex/internal/config"
)

// SysextDir is the directory where systemd-sysext looks for extensions.
const SysextDir = "/var/lib/extensions"

// IsExtensionActive checks if an extension appears to be active by checking
// if its CurrentSymlink exists in the target directory and resolves to a file.
// Returns (active, symlinkTarget).
func IsExtensionActive(t *config.Transfer) (bool, string) {
	if t.Target.CurrentSymlink == "" {
		return false, ""
	}
	targetDir := t.Target.Path
	if targetDir == "" {
		targetDir = SysextDir
	}
	symlinkPath := filepath.Join(targetDir, t.Target.CurrentSymlink)
	target, err := os.Readlink(symlinkPath)
	if err != nil {
		return false, ""
	}
	return true, filepath.Base(target)
}

// RemoveMatchingFiles removes all files in the target directory matching the
// transfer's patterns. Uses glob matching (converting @v to *) instead of
// version extraction. Returns the list of removed file paths.
func RemoveMatchingFiles(t *config.Transfer) ([]string, error) {
	patterns := t.Target.MatchPatterns
	if len(patterns) == 0 && t.Target.MatchPattern != "" {
		patterns = []string{t.Target.MatchPattern}
	}
	if len(patterns) == 0 {
		return nil, fmt.Errorf("no target match patterns defined")
	}

	targetDir := t.Target.Path
	if targetDir == "" {
		targetDir = SysextDir
	}

	var removed []string

	// Remove current symlink first if it exists
	if t.Target.CurrentSymlink != "" {
		symlinkPath := filepath.Join(targetDir, t.Target.CurrentSymlink)
		if info, err := os.Lstat(symlinkPath); err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				if err := os.Remove(symlinkPath); err != nil {
					return removed, fmt.Errorf("failed to remove symlink %s: %w", symlinkPath, err)
				}
				removed = append(removed, symlinkPath)
			}
		}
	}

	// Convert patterns like "myext_@v.raw" to globs like "myext_*.raw"
	for _, pattern := range patterns {
		glob := strings.ReplaceAll(pattern, "@v", "*")
		matches, err := filepath.Glob(filepath.Join(targetDir, glob))
		if err != nil {
			continue
		}
		for _, match := range matches {
			info, err := os.Lstat(match)
			if err != nil {
				continue
			}
			// Skip symlinks (already handled above)
			if info.Mode()&os.ModeSymlink != 0 {
				continue
			}
			if err := os.Remove(match); err != nil {
				return removed, fmt.Errorf("failed to remove %s: %w", match, err)
			}
			removed = append(removed, match)
		}
	}

	return removed, nil
}

// UnlinkFromSysext removes the extension symlink from /var/lib/extensions.
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

// Refresh calls systemd-sysext refresh to reload extensions.
func Refresh() error {
	return runner.Refresh()
}

// Merge calls systemd-sysext merge to merge extensions.
func Merge() error {
	return runner.Merge()
}

// Unmerge calls systemd-sysext unmerge to unmerge extensions.
func Unmerge() error {
	return runner.Unmerge()
}
