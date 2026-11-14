package sysext

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/frostyard/updex/v2/config"
	"github.com/frostyard/updex/v2/version"
)

// LinkedOtherSuiteAt reports whether the transfer's systemd-sysext link
// (<sysextDir>/<SysextLinkName(t)>) points to an image of another suite: the
// base name of the link target matches one of the transfer's any-suite
// patterns (config.TargetSection.AnySuitePatterns, a target pattern with %w)
// and none of its current target patterns. That is the state right after a
// suite switch (bootc switch from trixie 13 to forky 14): the link still
// names the old suite's image, which systemd-sysext refuses on the new base.
//
// It returns the base name of the link target and the suite it matched, or
// "" when the link does not point to an image of another suite: the
// transfer has no any-suite pattern, the link is absent or not a symlink, or
// its target is an image of the running suite or no image of the transfer.
// The target does not have to exist. An error is returned only when the link
// cannot be inspected or the transfer has no usable target pattern.
func LinkedOtherSuiteAt(t *config.Transfer, sysextDir string) (image, suite string, err error) {
	if len(t.Target.AnySuitePatterns) == 0 {
		return "", "", nil
	}
	linkName := SysextLinkName(t)
	if linkName == "" {
		return "", "", nil
	}
	current, err := parseTargetPatterns(t)
	if err != nil {
		return "", "", err
	}
	anySuite, _ := version.ParseAnySuitePatterns(t.Target.AnySuitePatterns)
	if len(anySuite) == 0 {
		return "", "", nil
	}

	linkPath := filepath.Join(sysextDir, linkName)
	info, err := os.Lstat(linkPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", nil
		}
		return "", "", fmt.Errorf("failed to stat sysext link: %w", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return "", "", nil
	}
	target, err := os.Readlink(linkPath)
	if err != nil {
		return "", "", fmt.Errorf("failed to read sysext link: %w", err)
	}

	base := filepath.Base(target)
	if _, _, ok := version.ExtractVersionParsed(base, current); ok {
		return "", "", nil
	}
	_, suite, ok := version.ExtractSuiteParsed(base, anySuite)
	if !ok {
		return "", "", nil
	}
	return base, suite, nil
}
