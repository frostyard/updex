package updex

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/frostyard/updex/v2/config"
	"github.com/frostyard/updex/v2/download"
	"github.com/frostyard/updex/v2/manifest"
	"github.com/frostyard/updex/v2/sysext"
	"github.com/frostyard/updex/v2/version"
)

// installOutcome reports what installTransfers did for one transfer. It is
// unexported: callers inside the SDK translate it into their own result types.
type installOutcome struct {
	// Version is the version that was selected.
	Version string

	// Manifest is the manifest that lists Version, when one was read.
	Manifest *manifest.Manifest

	// Downloaded reports that a new image was fetched and installed, or in a
	// dry run that it would be.
	Downloaded bool

	// Relinked reports that nothing was downloaded because the image was
	// already staged and current, but the systemd-sysext link had to be
	// changed to point at it (it was missing, dangling, or pointed at another
	// image). It is always false in a dry run, which never links.
	Relinked bool
}

// stagedTransfer is the result of stageTransfer: what the stage step found
// or downloaded, and whether the link step has work to do.
type stagedTransfer struct {
	// outcome is what stageTransfer reports so far. Relinked stays false
	// until linkStaged moves the link.
	outcome installOutcome

	// link reports that the systemd-sysext link must be (re)pointed at the
	// newest staged image: a new image was downloaded, or the image was
	// already staged but the link was missing, dangling or elsewhere. It is
	// always false in a dry run.
	link bool

	// relink reports that link is needed although nothing was downloaded.
	relink bool
}

// stageTransfer is the stage step of an install: it selects the newest
// available version of a single transfer, downloads and verifies it into the
// staging directory, and changes no systemd-sysext link. A failure leaves the
// current link and every image already on disk untouched.
// If opts.CachedManifest is non-nil, it is used instead of fetching the
// manifest over HTTP.
func (c *Client) stageTransfer(ctx context.Context, transfer *config.Transfer, opts installTransferOptions) (stagedTransfer, error) {
	fail := func(err error) (stagedTransfer, error) { return stagedTransfer{}, err }

	// Get available versions (applies MinVersion filter)
	available, m, patterns, err := c.getAvailableVersions(ctx, transfer, opts.CachedManifest)
	if err != nil {
		return fail(fmt.Errorf("failed to get available versions: %w", err))
	}

	if len(available) == 0 {
		return fail(fmt.Errorf("no versions available"))
	}

	// Sort and get newest
	version.Sort(available)
	versionToInstall := available[0]
	c.debug("selected version %s (from %d available)", versionToInstall, len(available))

	// Check if already installed and current
	installed, current, err := sysext.GetInstalledVersionsAt(transfer, c.paths.sysextLinkDir)
	if err != nil {
		return fail(fmt.Errorf("failed to inspect installed versions: %w", err))
	}
	if !opts.DryRun && transfer.Target.CurrentSymlink != "" {
		if err := sysext.RemoveLegacyCurrentSymlinkAt(transfer, c.paths.sysextLinkDir); err != nil {
			c.warn("failed to remove legacy symlink for %s: %v", transfer.Component, err)
		}
	}
	for _, v := range installed {
		if v == versionToInstall && v == current {
			// The image is staged and current, but the systemd-sysext link
			// may be missing, dangling, or pointing at another image (a
			// crashed earlier run, a failed run that downloaded this image
			// but linked nothing, a hand-edited link dir). The link step
			// restores it without re-downloading; a correct link is left
			// untouched.
			staged := stagedTransfer{outcome: installOutcome{Version: versionToInstall, Manifest: m}}
			if !opts.DryRun {
				linked, err := sysext.LinkIsCurrentAt(transfer, c.paths.sysextLinkDir)
				if err != nil {
					return fail(fmt.Errorf("failed to inspect sysext link: %w", err))
				}
				staged.link = !linked
				staged.relink = !linked
			}
			return staged, nil
		}
	}

	// Find the file for this version using patterns already parsed by getAvailableVersions
	var sourceFile string
	var expectedHash string
	for filename, hash := range m.Files {
		if v, _, ok := version.ExtractVersionParsed(filename, patterns); ok && v == versionToInstall {
			sourceFile = filename
			expectedHash = hash
			break
		}
	}

	if sourceFile == "" {
		return fail(fmt.Errorf("no file found for version %s", versionToInstall))
	}

	targetFile, err := buildTargetFilename(transfer.Target.Patterns(), versionToInstall)
	if err != nil {
		return fail(err)
	}
	targetPath := filepath.Join(transfer.Target.Path, targetFile)

	// Download
	downloadURL := transfer.Source.Path + "/" + sourceFile
	if opts.DryRun {
		c.debug("would download %s → %s", downloadURL, targetPath)
		return stagedTransfer{outcome: installOutcome{Version: versionToInstall, Manifest: m, Downloaded: true}}, nil
	}

	c.debug("downloading %s → %s", downloadURL, targetPath)
	err = download.Download(ctx, c.httpClient, downloadURL, targetPath, expectedHash, transfer.Target.Mode, c.config.OnDownloadProgress, download.WithRetryNotify(c.retryNotify("download")))
	if err != nil {
		return fail(fmt.Errorf("download failed: %w", err))
	}

	return stagedTransfer{
		outcome: installOutcome{Version: versionToInstall, Manifest: m, Downloaded: true},
		link:    true,
	}, nil
}

// transferInstall is the result of installing one transfer of a feature with
// installTransfers.
type transferInstall struct {
	transfer *config.Transfer

	// outcome reports what was staged. It is set whenever staging succeeded,
	// also when err is set because the link step did not run or was undone.
	outcome installOutcome

	// err is why the transfer is not installed: its own stage or link
	// failure, or errNotLinked when another transfer of its feature failed.
	err error

	// notLinked reports that err is errNotLinked: this transfer did nothing
	// wrong, but its feature stayed on the old links.
	notLinked bool

	// needsImage is "needs image >= <value>" when this transfer's staged image
	// needs a newer base image than the one that boots next (ADR-0016). Nothing
	// of the feature is linked, the old links stay and the image stays staged.
	// It is not an error.
	needsImage string

	// imageBlocked is set on the other transfers of a feature that needsImage
	// kept unlinked: it says which transfer needs what. It is not an error.
	imageBlocked string

	// nextBootImage is the next-boot IMAGE_VERSION the check compared; empty
	// when the image has no key.
	nextBootImage string

	// held reports that a rollback record holds the feature: nothing newer
	// than the rejected version is published, so nothing was downloaded or
	// linked and every old link stays. It is not an error. outcome.Version is
	// the version the link names.
	held bool

	// rejected is the version a rollback record rejected for this transfer,
	// whether or not the feature is held; the dry-run vacuum plan keeps it.
	rejected string

	// heldVersion is the version the record rejected for this transfer. It
	// is empty when the record names no version for this component and the
	// transfer is held only because another transfer of its feature is.
	heldVersion string
}

// errNotLinked builds the error that a transfer reports when another
// transfer of its feature failed, so the feature kept its old links.
func errNotLinked(feature string) error {
	return fmt.Errorf("not linked: another transfer of feature %s failed", feature)
}

// installFeatureOptions configures installTransfers.
type installFeatureOptions struct {
	// DryRun stages nothing and links nothing; it only reports what would
	// be downloaded.
	DryRun bool

	// NoVacuum skips removing old versions after the links succeeded.
	NoVacuum bool

	// Manifests caches manifests by source URL across the transfers of one
	// call and, when the caller keeps the map, across features. It may be
	// nil.
	Manifests map[string]*manifest.Manifest

	// NextBoot caches the next-boot image version across the features of one
	// operation, so bootc runs at most once and only when an image has the
	// minimum-image-version key. It may be nil, then the call resolves it
	// itself.
	NextBoot *nextBootImage
}

// installTransfers installs the transfers of one feature so that they link
// together (ADR-0016): it stages every transfer first, then links them all.
//
//   - If any transfer fails to stage (version lookup, download or
//     verification), no transfer is linked. Every old link stays, and the
//     images that were downloaded stay on disk for the next run, which
//     relinks them without downloading again. The failing transfer reports
//     its own error; the others report errNotLinked.
//   - If a link fails part-way, the links that this call already moved are
//     put back, and no transfer is reported as installed.
//   - Before the links, every image that would move a link is checked against
//     the next-boot image version (ADR-0016, "Minimum image version"; see
//     gateMinImage). An image that needs a newer base blocks the whole
//     feature without an error: no link moves, no vacuum runs, the downloaded
//     images stay staged. A read or bootc error is an error and also links
//     nothing. The check covers the relink path, because an image that was
//     blocked earlier stays staged and the next run reaches it there.
//   - The vacuum runs only after every link succeeded.
//
// The result has one entry per transfer, in order. A dry run stages and
// links nothing; it checks the images that are already staged and would be
// relinked, and says nothing about an image it would download.
func (c *Client) installTransfers(ctx context.Context, feature string, transfers []*config.Transfer, opts installFeatureOptions) []transferInstall {
	results := make([]transferInstall, len(transfers))
	for i, transfer := range transfers {
		results[i].transfer = transfer
	}

	// A real install ends in a link. Refuse a runner that cannot be told
	// where to link before removing a legacy symlink or downloading
	// anything, so the failure leaves no half-installed state behind. A dry
	// run never links, so it stays available.
	if !opts.DryRun {
		if err := c.requireLinkableRunner(); err != nil {
			for i := range results {
				results[i].err = err
			}
			return results
		}
	}

	manifests := opts.Manifests
	if manifests == nil {
		manifests = make(map[string]*manifest.Manifest)
	}

	// A rollback record holds the feature at an older version (ADR-0016).
	// When it cannot be read, nothing is linked: the update cannot tell what
	// was rejected, and linking the newest version could undo the rollback.
	rec, recErr := c.readRollbackRecord(feature)
	if recErr != nil {
		err := fmt.Errorf("not linking feature %s: %w", feature, recErr)
		for i := range results {
			results[i].err = err
		}
		return results
	}
	rejected := rec.rejectedVersions()
	for i, transfer := range transfers {
		results[i].rejected = rejected[transfer.Component]
	}
	if rec != nil {
		if c.holdByRollback(ctx, feature, transfers, rejected, manifests, results) {
			return results
		}
	}

	// Stage step.
	staged := make([]stagedTransfer, len(transfers))
	failed := false
	for i, transfer := range transfers {
		c.msg("Processing %s/%s", feature, transfer.Component)

		st, err := c.stageTransfer(ctx, transfer, installTransferOptions{
			DryRun:         opts.DryRun,
			CachedManifest: manifests[transfer.Source.Path],
		})
		if st.outcome.Manifest != nil {
			manifests[transfer.Source.Path] = st.outcome.Manifest
		}
		staged[i] = st
		results[i].outcome = st.outcome
		if err != nil {
			results[i].err = err
			failed = true
		}
	}

	if failed {
		c.markNotLinked(feature, results)
		return results
	}

	// Minimum image version: after the stage step, before any link moves.
	nextBoot := opts.NextBoot
	if nextBoot == nil {
		nextBoot = &nextBootImage{}
	}
	var move []bool
	if opts.DryRun {
		move = c.dryRunMoves(transfers, results)
	} else {
		move = make([]bool, len(staged))
		for i := range staged {
			move[i] = staged[i].link
		}
	}
	if c.gateMinImage(ctx, feature, transfers, move, results, nextBoot) {
		return results
	}
	if opts.DryRun {
		return results
	}

	// Link step: all or none.
	if err := c.linkStaged(feature, transfers, staged, results); err != nil {
		return results
	}
	for i := range results {
		results[i].outcome.Relinked = staged[i].relink
	}

	// Vacuum only after the feature's links succeeded. While the record
	// exists it keeps the rejected version.
	if !opts.NoVacuum {
		for _, transfer := range transfers {
			if _, _, err := sysext.VacuumWith(transfer, c.vacuumPaths(rejected[transfer.Component])); err != nil {
				c.warn("vacuum failed for %s: %v", transfer.Component, err)
			}
		}
	}

	// A newer version than the rejected one is linked: the rollback is over.
	if rec != nil {
		if _, err := c.deleteRollbackRecord(feature); err != nil {
			c.warn("failed to delete the rollback record of %s: %v", feature, err)
		} else {
			c.msg("Linked a version newer than the one rolled back from; deleted the rollback record of %s", feature)
		}
	}

	return results
}

// holdByRollback reports whether the rollback record holds the feature, and
// fills results when it does. The feature is held when, for any transfer, the
// newest available version is not newer than the version the record rejected
// (compared with version.Compare). It then downloads and links nothing for
// any transfer, so the feature never ends split between a new and an old
// revision. A transfer whose versions cannot be listed gets its error and the
// others errNotLinked, as in the stage step.
func (c *Client) holdByRollback(ctx context.Context, feature string, transfers []*config.Transfer, rejected map[string]string, manifests map[string]*manifest.Manifest, results []transferInstall) bool {
	held := false
	failed := false
	for i, transfer := range transfers {
		available, m, _, err := c.getAvailableVersions(ctx, transfer, manifests[transfer.Source.Path])
		if m != nil {
			manifests[transfer.Source.Path] = m
		}
		if err == nil && len(available) == 0 {
			err = fmt.Errorf("no versions available")
		}
		if err != nil {
			results[i].err = fmt.Errorf("failed to get available versions: %w", err)
			failed = true
			continue
		}
		version.Sort(available)
		if rej := rejected[transfer.Component]; rej != "" && version.Compare(available[0], rej) <= 0 {
			held = true
		}
	}
	if failed {
		c.markNotLinked(feature, results)
		return true
	}
	if !held {
		return false
	}
	for i, transfer := range transfers {
		linked, err := sysext.LinkedVersionAt(transfer, c.paths.sysextLinkDir)
		if err != nil {
			linked = ""
		}
		results[i].held = true
		results[i].heldVersion = rejected[transfer.Component]
		results[i].outcome = installOutcome{Version: linked}
	}
	return true
}

// markNotLinked gives every transfer that has no error of its own the
// errNotLinked error.
func (c *Client) markNotLinked(feature string, results []transferInstall) {
	for i := range results {
		if results[i].err == nil {
			results[i].err = errNotLinked(feature)
			results[i].notLinked = true
		}
	}
}

// vacuumPaths returns the paths the vacuum reads for a transfer: the staging
// fallback and the link directory the client captured at construction, and
// its merged-state hierarchies. protect lists versions to keep besides, such
// as the version a rollback record rejected for the transfer; empty entries
// are ignored.
func (c *Client) vacuumPaths(protect ...string) sysext.VacuumPaths {
	return sysext.VacuumPaths{
		DefaultDir:      c.paths.sysextLinkDir,
		LinkDir:         c.paths.sysextLinkDir,
		Hierarchies:     c.paths.sysextHierarchies,
		ProtectVersions: protect,
	}
}

// linkSnapshot records what a sysext link was before the link step moved
// it, so a later failure can put it back.
type linkSnapshot struct {
	path    string
	exists  bool
	symlink bool
	target  string
}

// snapshotLink records the entry at path with Lstat, so a symlink is seen as
// itself and a dangling one is not mistaken for absent.
func snapshotLink(path string) (linkSnapshot, error) {
	snap := linkSnapshot{path: path}
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return snap, nil
		}
		return snap, fmt.Errorf("failed to inspect sysext link: %w", err)
	}
	snap.exists = true
	if info.Mode()&os.ModeSymlink != 0 {
		snap.symlink = true
		snap.target, err = os.Readlink(path)
		if err != nil {
			return snap, fmt.Errorf("failed to read sysext link: %w", err)
		}
	}
	return snap, nil
}

// restore puts the link back as snapshotLink saw it. A symlink is recreated
// beside the link and renamed over it, so systemd-sysext never sees a moment
// without a link (the way sysext.LinkToSysextAt replaces one); a link that
// did not exist is removed. An entry that was not a symlink cannot be
// restored from a snapshot and is reported.
func (s linkSnapshot) restore() error {
	switch {
	case !s.exists:
		info, err := os.Lstat(s.path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info.Mode()&os.ModeSymlink == 0 {
			return nil // not a link this run made
		}
		return os.Remove(s.path)
	case !s.symlink:
		return fmt.Errorf("%s was not a symlink and cannot be restored", s.path)
	}
	tmp := fmt.Sprintf("%s.tmp-%d-%d", s.path, os.Getpid(), time.Now().UnixNano())
	if err := os.Symlink(s.target, tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// linkAllOrNone moves the link of every transfer with link(i, transfers[i]),
// in order, all or none. It snapshots every old link first (see
// snapshotLink); when a snapshot or a link fails, it restores the links
// already moved, including the failing one, and returns the index of the
// failing transfer and the error. On success it returns failed = -1 and an
// undo function that restores every link, for a caller whose later step
// fails.
func (c *Client) linkAllOrNone(transfers []*config.Transfer, link func(i int, t *config.Transfer) error) (undo func(), failed int, err error) {
	snapshots := make([]linkSnapshot, len(transfers))
	for i, transfer := range transfers {
		snap, err := snapshotLink(filepath.Join(c.paths.sysextLinkDir, sysext.SysextLinkName(transfer)))
		if err != nil {
			// Nothing is linked yet, so there is nothing to undo.
			return func() {}, i, err
		}
		snapshots[i] = snap
	}

	restore := func(n int) {
		for j := 0; j < n; j++ {
			if rerr := snapshots[j].restore(); rerr != nil {
				c.warn("failed to restore sysext link for %s: %v", transfers[j].Component, rerr)
			}
		}
	}
	for i, transfer := range transfers {
		if err := link(i, transfer); err != nil {
			restore(i + 1)
			return func() {}, i, err
		}
	}
	return func() { restore(len(transfers)) }, -1, nil
}

// linkStaged is the link step: it points the systemd-sysext link of every
// staged transfer that needs one at its newest image. When a link fails, it
// restores the links this call already moved, records the failure on the
// failing transfer and errNotLinked on the others, and returns the error.
func (c *Client) linkStaged(feature string, transfers []*config.Transfer, staged []stagedTransfer, results []transferInstall) error {
	var idx []int
	var todo []*config.Transfer
	for i, transfer := range transfers {
		if staged[i].link {
			idx = append(idx, i)
			todo = append(todo, transfer)
		}
	}

	_, failed, err := c.linkAllOrNone(todo, func(_ int, transfer *config.Transfer) error {
		return c.linkToSysext(transfer)
	})
	if err != nil {
		results[idx[failed]].err = err
		c.markNotLinked(feature, results)
		return err
	}
	for _, i := range idx {
		if staged[i].relink {
			c.msg("restored sysext link for %s", transfers[i].Component)
		}
	}
	return nil
}

// ErrLegacySysextRunner reports a ClientConfig.SysextRunner that implements
// only the original [sysext.SysextRunner] interface and not
// [sysext.PathSysextRunner]. Such a runner cannot be told which directory to
// link into, so it can only link into the mutable package-global
// sysext.SysextDir — which would silently ignore
// RuntimePaths.SysextLinkDir and break the ADR-0011 capture-at-construction
// invariant. Operations that may link refuse with this error before touching
// the filesystem instead. Callers can test for it with errors.Is.
var ErrLegacySysextRunner = errors.New("sysext runner does not implement sysext.PathSysextRunner, so it cannot honor the client's SysextLinkDir")

// pathRunner returns the client's runner as a sysext.PathSysextRunner, or
// ErrLegacySysextRunner when the injected runner predates that interface.
func (c *Client) pathRunner() (sysext.PathSysextRunner, error) {
	runner, ok := c.runner.(sysext.PathSysextRunner)
	if !ok {
		return nil, fmt.Errorf("%w: %T must add LinkToSysextAt(*config.Transfer, string) error", ErrLegacySysextRunner, c.runner)
	}
	return runner, nil
}

// requireLinkableRunner is the precondition every non-dry-run operation that
// may end up linking checks before it mutates anything, so a legacy runner
// fails cleanly rather than part-way through an install.
func (c *Client) requireLinkableRunner() error {
	_, err := c.pathRunner()
	return err
}

// linkToSysext points the systemd-sysext link for transfer at its newest
// staged image through the client's runner, in the directory the client
// captured at construction.
func (c *Client) linkToSysext(transfer *config.Transfer) error {
	runner, err := c.pathRunner()
	if err != nil {
		return fmt.Errorf("failed to link to sysext: %w", err)
	}
	if err := runner.LinkToSysextAt(transfer, c.paths.sysextLinkDir); err != nil {
		return fmt.Errorf("failed to link to sysext: %w", err)
	}
	return nil
}

// buildTargetFilename derives the installed filename for a version from the
// target match patterns. Downloads are always stored decompressed, so it
// prefers the first pattern whose name carries no compression suffix; if every
// pattern is a compressed variant, it strips the suffix from the first one so
// the on-disk name matches the actual content.
func buildTargetFilename(targetPatterns []string, ver string) (string, error) {
	var fallback string
	var firstErr error
	keepErr := func(err error) {
		if firstErr == nil {
			firstErr = err
		}
	}
	for _, patternStr := range targetPatterns {
		p, err := version.ParsePattern(patternStr)
		if err != nil {
			keepErr(err)
			continue
		}
		name := p.BuildFilename(ver)
		if err := validateTargetFilename(name); err != nil {
			keepErr(err)
			continue
		}
		stripped := download.StripCompressionSuffix(name)
		if stripped == name {
			return name, nil
		}
		// The stripped name, not `name`, is what this pattern would
		// contribute, so it has to clear the same check: stripping a
		// compression suffix can turn an acceptable name into a traversal
		// segment (e.g. "...gz" -> ".."). A stripped value that fails
		// validation is rejected outright rather than kept as a silent
		// fallback, so every value returned below has been validated.
		if err := validateTargetFilename(stripped); err != nil {
			keepErr(err)
			continue
		}
		if fallback == "" {
			fallback = stripped
		}
	}
	if fallback != "" {
		return fallback, nil
	}
	if firstErr != nil {
		return "", fmt.Errorf("invalid target pattern: %w", firstErr)
	}
	return "", fmt.Errorf("no target pattern configured")
}

// validateTargetFilename rejects a filename built from a runtime-substituted
// version (config.Transfer.Target.Patterns' @v placeholder) that would escape
// Target.Path when joined with filepath.Join in stageTransfer: "." or "..",
// any path separator, or a name whose Base differs from itself (e.g. an
// absolute path). This mirrors catalog/catalog.go's validateCatalogPatterns,
// which validates pattern *literals* configured by trusted operators; this
// check instead covers the version string substituted in at runtime from an
// untrusted manifest.
func validateTargetFilename(name string) error {
	if name == "." || name == ".." || strings.ContainsAny(name, "/\\") || filepath.Base(name) != name {
		return fmt.Errorf("invalid target filename %q: must not contain path separators or traverse directories", name)
	}
	return nil
}
