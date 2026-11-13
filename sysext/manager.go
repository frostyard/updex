package sysext

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/frostyard/updex/v2/config"
	"github.com/frostyard/updex/v2/internal/fault"
	"github.com/frostyard/updex/v2/version"
)

// parseTargetPatterns extracts and parses the version patterns from a transfer's
// target configuration, returning an error if no valid patterns are found.
func parseTargetPatterns(t *config.Transfer) ([]*version.Pattern, error) {
	patternStrs := t.Target.Patterns()
	if len(patternStrs) == 0 {
		return nil, fault.Mark(fault.Config, fmt.Errorf("no target match patterns defined"))
	}

	patterns, firstErr := version.ParsePatterns(patternStrs)
	if len(patterns) == 0 {
		return nil, fault.Mark(fault.Config, fmt.Errorf("invalid target pattern: %w", firstErr))
	}

	return patterns, nil
}

// parseAnySuitePatterns parses the transfer's any-suite target patterns
// (config.TargetSection.AnySuitePatterns). It returns nil for a transfer
// whose target patterns do not contain %w, so such a transfer never sees an
// image of another suite. A pattern that does not parse is skipped: it
// derives from a target pattern, which parseTargetPatterns checks.
func parseAnySuitePatterns(t *config.Transfer) []*version.Pattern {
	if len(t.Target.AnySuitePatterns) == 0 {
		return nil
	}
	patterns, _ := version.ParseAnySuitePatterns(t.Target.AnySuitePatterns)
	return patterns
}

// otherSuiteImage reports whether entry is a staged image of another suite:
// a regular file that an any-suite pattern matches and no current target
// pattern matches. It returns the version and the suite the name matched.
func otherSuiteImage(entry fs.DirEntry, current, anySuite []*version.Pattern) (ver, suite string, ok bool) {
	if !entry.Type().IsRegular() {
		return "", "", false
	}
	return otherSuiteName(entry.Name(), current, anySuite)
}

// otherSuiteName reports whether name is the file name of an image of
// another suite: an any-suite pattern matches it and no current target
// pattern matches it. It returns the version and the suite the name matched.
func otherSuiteName(name string, current, anySuite []*version.Pattern) (ver, suite string, ok bool) {
	if len(anySuite) == 0 {
		return "", "", false
	}
	if _, _, matched := version.ExtractVersionParsed(name, current); matched {
		return "", "", false
	}
	return version.ExtractSuiteParsed(name, anySuite)
}

// ActiveOtherSuiteWith reports an image of another suite of the transfer that
// is active in systemd-sysext, as GetActiveVersionWith does for the running
// suite: the target of a legacy CurrentSymlink, an image that the merged-state
// metadata under each hierarchy records (an origin path, or a name in the
// older extensions list), or an image in the runtime image search directory.
// It returns the image's file name, or "" when none is active or the
// transfer's target pattern has no %w. RemoveAllVersionsAt removes the images
// of every suite, so a caller that guards that removal checks both.
//
// It fails closed: a CurrentSymlink that cannot be inspected, metadata that
// cannot be read and a runtime directory that cannot be listed are errors.
func ActiveOtherSuiteWith(t *config.Transfer, p ActiveStatePaths) (string, error) {
	anySuite := parseAnySuitePatterns(t)
	if len(anySuite) == 0 {
		return "", nil
	}
	current, err := parseTargetPatterns(t)
	if err != nil {
		return "", err
	}
	isOther := func(name string) bool {
		_, _, ok := otherSuiteName(name, current, anySuite)
		return ok
	}

	target, err := currentSymlinkTarget(t, p.DefaultDir)
	if err != nil {
		return "", err
	}
	if target != "" && isOther(target) {
		return target, nil
	}

	for _, hierarchy := range p.Hierarchies {
		stateDir := filepath.Join(hierarchy, MergedStateDirName)
		data, err := readLimitedFile(filepath.Join(stateDir, "origin"), 1<<20)
		switch {
		case err == nil:
			var origin mergedExtensionOrigin
			if json.Unmarshal(data, &origin) == nil {
				for _, key := range slices.Sorted(maps.Keys(origin.Extensions)) {
					if name := filepath.Base(origin.Extensions[key].Path); origin.Extensions[key].Path != "" && isOther(name) {
						return name, nil
					}
				}
			}
		case !errors.Is(err, fs.ErrNotExist):
			return "", fmt.Errorf("read merged state: %w", err)
		}

		data, err = readLimitedFile(filepath.Join(stateDir, "extensions"), 64<<10)
		switch {
		case err == nil:
			for line := range strings.SplitSeq(string(data), "\n") {
				if name := strings.TrimSpace(line); name != "" && isOther(name+targetExtension(t)) {
					return name + targetExtension(t), nil
				}
			}
		case !errors.Is(err, fs.ErrNotExist):
			return "", fmt.Errorf("read merged state: %w", err)
		}
	}

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
		if isOther(entry.Name()) {
			return entry.Name(), nil
		}
	}
	return "", nil
}

// currentSymlinkTarget returns the base name of the target of the transfer's
// legacy CurrentSymlink, or "" when the transfer has none, or the path is
// absent or not a symlink. A path that cannot be inspected is an error, so a
// caller that protects the target never guesses.
func currentSymlinkTarget(t *config.Transfer, defaultDir string) (string, error) {
	if t.Target.CurrentSymlink == "" {
		return "", nil
	}
	symlinkPath := filepath.Join(targetDirAt(t, defaultDir), t.Target.CurrentSymlink)
	info, err := os.Lstat(symlinkPath)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", nil
	case err != nil:
		return "", fmt.Errorf("inspect current symlink: %w", err)
	case info.Mode()&os.ModeSymlink == 0:
		return "", nil
	}
	target, err := os.Readlink(symlinkPath)
	if err != nil {
		return "", fmt.Errorf("read current symlink: %w", err)
	}
	return filepath.Base(target), nil
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

// otherSuiteFile is a staged image of another suite (see otherSuiteImage).
type otherSuiteFile struct {
	version  string
	suite    string
	filename string
}

// VacuumReport is what a vacuum removed and kept. Removed and Kept are the
// versions of the running suite, as VacuumWith returns them. The OtherSuite
// lists name the images of other suites (file names in the target
// directory) of a transfer whose target pattern contains %w; they are empty
// for every other transfer.
type VacuumReport struct {
	Removed []string
	Kept    []string

	// OtherSuiteRemoved lists the images of other suites that the vacuum
	// removed (or, in a plan, would remove).
	OtherSuiteRemoved []string
	// OtherSuiteKept lists the images of other suites that the vacuum kept:
	// the newest version of each other suite, the image that the sysext
	// link points to, and an image that the merged state records.
	OtherSuiteKept []string
	// OtherSuiteMergedUnknown reports that the merged-state metadata names
	// the component but no image file of it (older metadata with only the
	// extension name), so the vacuum kept every image of another suite: the
	// merged image could be any of them.
	OtherSuiteMergedUnknown bool
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
	r, err := planVacuum(t, activeVersion, false, p)
	return r.Removed, r.Kept, err
}

// PlanVacuumWithReport is PlanVacuumWith that also reports the images of
// other suites that VacuumWith would remove and keep.
func PlanVacuumWithReport(t *config.Transfer, activeVersion string, p VacuumPaths) (VacuumReport, error) {
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
// For a transfer whose target pattern contains %w, the images of other
// suites (config.TargetSection.AnySuitePatterns: an any-suite pattern
// matches them, no current pattern does) follow their own rule: for each
// other suite the newest version is kept, so a switch back to that suite can
// link it without a download, and older ones are removed. The image that the
// sysext link points to (compared by file name) and an image that the merged
// state records are always kept. Only regular files count. A transfer
// without %w has no such images.
//
// The vacuum never guesses: when it cannot read the link or the merged-state
// metadata, it returns an error and deletes nothing. A dangling link protects
// nothing, because it names no image.
func VacuumWith(t *config.Transfer, p VacuumPaths) (removed []string, kept []string, err error) {
	r, err := planVacuum(t, "", true, p)
	return r.Removed, r.Kept, err
}

// VacuumWithReport is VacuumWith that also reports the images of other suites
// that it removed and kept.
func VacuumWithReport(t *config.Transfer, p VacuumPaths) (VacuumReport, error) {
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

// otherSuiteProtected returns the file names of images that the vacuum must
// keep whatever their suite: the base name the sysext link points to, the
// target of a legacy CurrentSymlink, and the base names the merged-state
// metadata records. Like protectedVersions it reads everything before the
// vacuum deletes anything, and fails on a read error instead of guessing.
//
// keepAll reports that a hierarchy's metadata names the extension
// t.Component (an origin key, or the bare name in the older extensions list)
// and origin gives that key no path whose base name is an image of the
// transfer: the merged image could be any of them, so the vacuum keeps every
// image of another suite. Another entry never resolves it, even one that
// matches the transfer's pattern.
func otherSuiteProtected(t *config.Transfer, current, anySuite []*version.Pattern, p VacuumPaths) (protected map[string]bool, keepAll bool, err error) {
	protected = make(map[string]bool)
	isImage := func(name string) bool {
		if _, _, ok := version.ExtractVersionParsed(name, current); ok {
			return true
		}
		_, _, ok := otherSuiteName(name, current, anySuite)
		return ok
	}

	// A legacy CurrentSymlink target is kept too, as for the running suite.
	target, err := currentSymlinkTarget(t, p.DefaultDir)
	if err != nil {
		return nil, false, err
	}
	if target != "" {
		protected[target] = true
	}

	if linkName := SysextLinkName(t); p.LinkDir != "" && linkName != "" {
		linkPath := filepath.Join(p.LinkDir, linkName)
		info, err := os.Lstat(linkPath)
		switch {
		case err == nil && info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(linkPath)
			if err != nil {
				return nil, false, fmt.Errorf("read linked version: failed to read sysext link: %w", err)
			}
			protected[filepath.Base(target)] = true
		case err != nil && !os.IsNotExist(err):
			return nil, false, fmt.Errorf("read linked version: failed to stat sysext link: %w", err)
		}
	}

	for _, hierarchy := range p.Hierarchies {
		// named: the metadata names the extension t.Component (origin key or
		// bare extensions entry); resolved: origin gives that same key a path
		// whose base name is an image of the transfer. Only the entry of that
		// extension resolves it: another entry that matches the pattern (such
		// as foo_bar_9.0_14_x86-64 for foo_@v_14_x86-64.raw) is another
		// extension and says nothing about which image foo merged.
		var named, resolved bool
		stateDir := filepath.Join(hierarchy, MergedStateDirName)
		data, err := readLimitedFile(filepath.Join(stateDir, "origin"), 1<<20)
		switch {
		case err == nil:
			var origin mergedExtensionOrigin
			if json.Unmarshal(data, &origin) == nil {
				for key, entry := range origin.Extensions {
					own := t.Component != "" && key == t.Component
					if own {
						named = true
					}
					if entry.Path == "" {
						continue
					}
					name := filepath.Base(entry.Path)
					protected[name] = true
					if own && isImage(name) {
						resolved = true
					}
				}
			}
		case !errors.Is(err, fs.ErrNotExist):
			return nil, false, fmt.Errorf("read merged version: read merged state: %w", err)
		}

		data, err = readLimitedFile(filepath.Join(stateDir, "extensions"), 64<<10)
		switch {
		case err == nil:
			for line := range strings.SplitSeq(string(data), "\n") {
				name := strings.TrimSpace(line)
				if name == "" {
					continue
				}
				if t.Component != "" && name == t.Component {
					named = true
				}
				protected[name+targetExtension(t)] = true
			}
		case !errors.Is(err, fs.ErrNotExist):
			return nil, false, fmt.Errorf("read merged version: read merged state: %w", err)
		}

		if named && !resolved {
			keepAll = true
		}
	}

	return protected, keepAll, nil
}

// planOtherSuites decides which images of other suites the vacuum keeps: for
// each suite the newest version (every file of that version), and every
// image named in protected. It returns the files to remove and to keep, each
// in the order of files.
func planOtherSuites(files []otherSuiteFile, protected map[string]bool, keepAll bool) (remove, keep []otherSuiteFile) {
	if keepAll {
		return nil, files
	}
	newest := make(map[string]string)
	for _, f := range files {
		if cur, ok := newest[f.suite]; !ok || version.Compare(f.version, cur) > 0 {
			newest[f.suite] = f.version
		}
	}
	for _, f := range files {
		if protected[f.filename] || version.Compare(f.version, newest[f.suite]) == 0 {
			keep = append(keep, f)
			continue
		}
		remove = append(remove, f)
	}
	return remove, keep
}

func planVacuum(t *config.Transfer, activeVersionOverride string, remove bool, p VacuumPaths) (report VacuumReport, err error) {
	patterns, err := parseTargetPatterns(t)
	if err != nil {
		return VacuumReport{}, err
	}
	anySuite := parseAnySuitePatterns(t)

	targetDir := targetDirAt(t, p.DefaultDir)

	entries, err := os.ReadDir(targetDir)
	if err != nil {
		if os.IsNotExist(err) {
			return VacuumReport{}, nil
		}
		return VacuumReport{}, fmt.Errorf("failed to read directory: %w", err)
	}

	var installed []versionFile
	var others []otherSuiteFile
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
			continue
		}
		if v, suite, ok := otherSuiteImage(entry, patterns, anySuite); ok {
			others = append(others, otherSuiteFile{version: v, suite: suite, filename: name})
		}
	}

	if activeVersionOverride != "" && !seenVersions[activeVersionOverride] {
		installed = append(installed, versionFile{version: activeVersionOverride})
	}

	if len(installed) == 0 && len(others) == 0 {
		return VacuumReport{}, nil
	}

	// Decide what is protected before anything is removed, so an unreadable
	// link or merged state fails the vacuum with every image still on disk.
	var protected map[string]bool
	if len(installed) > 0 {
		protected, err = protectedVersions(t, patterns, p)
		if err != nil {
			return VacuumReport{}, err
		}
	}
	var removeOthers, keepOthers []otherSuiteFile
	if len(others) > 0 {
		keepNames, keepAll, err := otherSuiteProtected(t, patterns, anySuite, p)
		if err != nil {
			return VacuumReport{}, err
		}
		removeOthers, keepOthers = planOtherSuites(others, keepNames, keepAll)
		report.OtherSuiteMergedUnknown = keepAll
	}
	for _, f := range keepOthers {
		report.OtherSuiteKept = append(report.OtherSuiteKept, f.filename)
	}

	removed, kept, err := vacuumCurrentSuite(t, installed, activeVersionOverride, protected, remove, targetDir, patterns)
	report.Removed, report.Kept = removed, kept
	if err != nil {
		return report, err
	}

	for _, f := range removeOthers {
		if remove {
			if err := os.Remove(filepath.Join(targetDir, f.filename)); err != nil {
				return report, fmt.Errorf("failed to remove %s: %w", f.filename, err)
			}
		}
		report.OtherSuiteRemoved = append(report.OtherSuiteRemoved, f.filename)
	}

	return report, nil
}

// vacuumCurrentSuite applies the retention rule to the images of the running
// suite (installed) and removes the ones it does not keep when remove is set.
func vacuumCurrentSuite(t *config.Transfer, installed []versionFile, activeVersionOverride string, protected map[string]bool, remove bool, targetDir string, patterns []*version.Pattern) (removed []string, kept []string, err error) {
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
// directory for transfers that omit Target.Path. For a transfer whose target
// pattern contains %w it also removes the images of every other suite
// (config.TargetSection.AnySuitePatterns), so a disabled feature leaves no
// image of any suite; such an image is removed only when it is a regular
// file.
func RemoveAllVersionsAt(t *config.Transfer, defaultDir string) ([]string, error) {
	patterns, err := parseTargetPatterns(t)
	if err != nil {
		return nil, err
	}
	anySuite := parseAnySuitePatterns(t)

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

		// Check if this file matches any of the patterns, or is an image of
		// another suite.
		_, _, ok := version.ExtractVersionParsed(name, patterns)
		if !ok {
			_, _, ok = otherSuiteImage(entry, patterns, anySuite)
		}
		if ok {
			filePath := filepath.Join(targetDir, name)
			if err := os.Remove(filePath); err != nil {
				return removed, fmt.Errorf("failed to remove %s: %w", filePath, err)
			}
			removed = append(removed, filePath)
		}
	}

	return removed, nil
}
