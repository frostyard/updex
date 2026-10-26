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

	// held reports that a rollback record holds the feature: either another
	// feature's record names one of its transfers (only that feature advances
	// it), or no version newer than the rejected one is published. Nothing
	// was downloaded or linked and every old link stays. It is not an error.
	// outcome.Version is the version the link names.
	held bool

	// rejected is the highest version a rollback record that applies to this
	// transfer rejects, whether or not the feature is held; the dry-run
	// vacuum plan keeps it.
	rejected string

	// heldVersion is the version the applicable records reject for this
	// transfer. It is empty when no record names this component and the
	// transfer is held only because another transfer of its feature, or a
	// record of another feature, holds the feature.
	heldVersion string

	// heldReason says why the feature is held when this transfer's own
	// rejected version is not the reason: for its own record, for example
	// "grp: beta has no version newer than 2.0.0", and for another
	// feature's record, "grp: it advances only when grp updates (or run
	// 'updex features rollback --clear grp')". Empty when heldVersion is
	// the reason, and when the transfer is not held.
	heldReason string
}

// heldMessage renders why the rollback holds this transfer: the reason that
// holds the whole feature, else the transfer's own rejected version.
func (in transferInstall) heldMessage() string {
	if in.heldReason != "" {
		return "held by rollback of " + in.heldReason
	}
	return "held by rollback: rejected " + in.heldVersion
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

	// Domain is every transfer the calling operation loaded. A rollback
	// record can name a component that this feature's transfers do not
	// cover: the record holds the feature only while that component has no
	// newer version, so its transfer and available versions are needed to
	// decide the hold, and its link decides whether the record may be
	// deleted.
	Domain []*config.Transfer
}

// installTransfers installs the transfers of one feature so that they link
// together (ADR-0016): it stages every transfer first, then links them all.
// transfers holds only the feature's active transfers (ADR-0017): a transfer
// that waits for another feature is not passed, so it is never downloaded,
// linked or vacuumed, and its rollback records do not apply. The rollback
// holds and the deletion of finished records are computed over this set.
//
//   - A rollback record of another feature that names one of the transfers
//     holds the whole feature: only the feature that wrote the record
//     advances it, and nothing is downloaded or linked here. The hold lasts
//     while the record exists, also when its feature is disabled or masked.
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

	// A rollback record holds the transfers at an older version (ADR-0016).
	// A transfer can belong to several features, so the records of every
	// feature that names one of these transfers apply, whether that feature
	// is enabled or not: a rollback of one feature must not be undone by an
	// update through another that shares the transfer. When an applicable
	// record cannot be read, nothing is downloaded or linked for the
	// feature: the update cannot tell what was rejected.
	holds := c.collectRollbackHolds(transfers)
	if len(holds.errs) > 0 {
		for i, transfer := range transfers {
			if err := holds.errs[transfer.Component]; err != nil {
				results[i].err = fmt.Errorf("not linking feature %s: %w", feature, err)
			}
		}
		c.markNotLinked(feature, results)
		return results
	}
	for i, transfer := range transfers {
		results[i].rejected = holds.rejected[transfer.Component]
	}
	// A record written by another feature applies to one of these transfers,
	// so only the feature that wrote it advances that transfer: a record and
	// its feature's links move together only when that feature itself updates
	// (ADR-0016). Holding the whole feature here keeps the rolled-back
	// feature whole whatever happens to the owner's downloads or links, and
	// in whatever order the features are processed.
	if owners := holds.ownersOtherThan(feature); len(owners) > 0 {
		c.holdFeatureOwnedBy(transfers, holds, owners[0], results)
		return results
	}
	if rec := holds.records[feature]; rec != nil {
		// The feature's own record holds only while some component it names
		// has no newer version. It covers every component it names, also one
		// outside this feature's transfers, so the feature stays whole
		// whatever the other component's newest version is.
		reason, err := c.rollbackHoldReason(ctx, rec, transfers, opts.Domain, manifests)
		if err != nil {
			// The versions that decide whether the record still holds
			// cannot be read: link nothing, as for an unreadable record.
			for i := range results {
				results[i].err = fmt.Errorf("not linking feature %s: %w", feature, err)
			}
			return results
		}
		if reason != nil {
			c.holdFeature(transfers, holds, reason, results)
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
			if _, _, err := sysext.VacuumWith(transfer, c.vacuumPaths(holds.rejected[transfer.Component])); err != nil {
				c.warn("vacuum failed for %s: %v", transfer.Component, err)
			}
		}
	}

	// A newer version than the one rejected is linked for every component of
	// the feature's own record: that rollback is over.
	c.deleteFinishedRollbackRecords(feature, transfers, opts.Domain, holds)

	return results
}

// holdFeature reports that the feature's own rollback record holds it, so
// nothing is downloaded or linked for any of its transfers: the feature never
// ends split between a new and an old revision. The transfer whose own
// rejected version is the reason reports that version; the others report the
// record component that keeps the whole feature from advancing.
func (c *Client) holdFeature(transfers []*config.Transfer, holds rollbackHolds, reason *rollbackHoldReason, results []transferInstall) {
	for i, transfer := range transfers {
		linked, err := sysext.LinkedVersionAt(transfer, c.paths.sysextLinkDir)
		if err != nil {
			linked = ""
		}
		results[i].held = true
		results[i].heldVersion = holds.rejected[transfer.Component]
		if reason.component != transfer.Component {
			results[i].heldReason = reason.String()
		}
		results[i].outcome = installOutcome{Version: linked}
	}
}

// holdFeatureOwnedBy reports that a rollback record of another feature
// applies to one of the transfers, so only that feature advances it: nothing
// is downloaded or linked for this feature, and every transfer says why. The
// hold lasts while the record exists, also when the feature that wrote it is
// disabled or masked; an update through that feature, or `updex features
// rollback --clear <feature>`, ends it.
func (c *Client) holdFeatureOwnedBy(transfers []*config.Transfer, holds rollbackHolds, owner string, results []transferInstall) {
	reason := rollbackOwnerHold{feature: owner}.String()
	for i, transfer := range transfers {
		linked, err := sysext.LinkedVersionAt(transfer, c.paths.sysextLinkDir)
		if err != nil {
			linked = ""
		}
		results[i].held = true
		results[i].heldVersion = holds.rejected[transfer.Component]
		results[i].heldReason = reason
		results[i].outcome = installOutcome{Version: linked}
	}
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

// backupLink and restoreRename are os.Link and os.Rename, replaceable by
// tests that inject a backup or a restore failure for a link path that is a
// regular file.
var (
	backupLink    = os.Link
	restoreRename = os.Rename
)

// linkSnapshot records what a sysext link was before the link step moved
// it, so a later failure can put it back.
type linkSnapshot struct {
	path    string
	symlink bool
	target  string

	// backup is the path of a hard link to the regular file that was at
	// path, made during the snapshot pass. The link step replaces a regular
	// file (rename replaces it), so restoring the record needs the file's
	// data as well as its name. It is empty for a symlink or an absent path.
	backup string

	// keepBackup reports that a restore could not put the backup back, so
	// the backup holds the only copy of the replaced file and must stay.
	keepBackup bool
}

// snapshotLink records the entry at path with Lstat, so a symlink is seen as
// itself and a dangling one is not mistaken for absent. A regular file is
// backed up as a hard link beside it (see backupLinkPath), so a later failed
// link step can put the file itself back; any other entry is refused here,
// before a link moves.
func snapshotLink(path string) (linkSnapshot, error) {
	snap := linkSnapshot{path: path}
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return snap, nil
		}
		return snap, fmt.Errorf("failed to inspect sysext link: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		snap.symlink = true
		snap.target, err = os.Readlink(path)
		if err != nil {
			return snap, fmt.Errorf("failed to read sysext link: %w", err)
		}
		return snap, nil
	}
	if !info.Mode().IsRegular() {
		return snap, fmt.Errorf("%s is not a symlink or a regular file; updex will not replace it", path)
	}
	backup := backupLinkPath(path)
	if err := backupLink(path, backup); err != nil {
		return snap, fmt.Errorf("%s is a regular file that updex cannot back up: %w", path, err)
	}
	snap.backup = backup
	return snap, nil
}

// backupLinkPath is the hard link snapshotLink makes to a regular file at the
// link path. The name starts with a dot and does not end in the image
// extension, so systemd-sysext never reads it as an image.
func backupLinkPath(path string) string {
	return filepath.Join(filepath.Dir(path), fmt.Sprintf(".%s.updex-backup-%d-%d", filepath.Base(path), os.Getpid(), time.Now().UnixNano()))
}

// restore puts the link back as snapshotLink saw it. A backed-up regular file
// is renamed back over the link, which atomically replaces it; a symlink is
// recreated beside the link and renamed over it, so systemd-sysext never sees
// a moment without a link (the way sysext.LinkToSysextAt replaces one); a link
// that did not exist is removed.
func (s *linkSnapshot) restore() error {
	if s.backup != "" {
		if err := restoreRename(s.backup, s.path); err != nil {
			// The data is still in the backup: keep it so an operator can
			// put the file back by hand.
			s.keepBackup = true
			return err
		}
		s.backup = ""
		return nil
	}
	if s.symlink {
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
	// Nothing was recorded: remove the link this run made.
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
}

// linkAllOrNone moves the link of every transfer with link(i, transfers[i]),
// in order, all or none. It snapshots every old link first (see
// snapshotLink: a regular file at a link path is backed up as a hard link,
// any other non-symlink entry is refused), so a refusal changes nothing.
// When a link fails, it restores the links already moved, including the
// failing one, and returns the index of the failing transfer and the link
// error joined with every restore failure. restoreFailed maps the indexes
// (up to and including failed) whose restore failed to that failure: their link may still point at the new
// version. Backups are removed at the end, except one whose restore failed,
// which holds the only copy of the file. On success failed is -1.
func (c *Client) linkAllOrNone(transfers []*config.Transfer, link func(i int, t *config.Transfer) error) (failed int, restoreFailed map[int]error, err error) {
	snapshots := make([]linkSnapshot, 0, len(transfers))
	defer func() { c.removeLinkBackups(snapshots) }()
	for i, transfer := range transfers {
		snap, err := snapshotLink(filepath.Join(c.paths.sysextLinkDir, sysext.SysextLinkName(transfer)))
		if err != nil {
			// Nothing is linked yet, so there is nothing to undo. The
			// backups taken so far are redundant: the files they stand for
			// were never replaced.
			return i, nil, err
		}
		snapshots = append(snapshots, snap)
	}

	for i, transfer := range transfers {
		if err := link(i, transfer); err != nil {
			errs := []error{err}
			for j := 0; j <= i; j++ {
				rerr := snapshots[j].restore()
				if rerr == nil {
					continue
				}
				rerr = fmt.Errorf("failed to restore sysext link for %s: %w", transfers[j].Component, rerr)
				c.warn("%s", rerr)
				errs = append(errs, rerr)
				// The failing transfer's link may have moved before link
				// returned the error, so it is recorded too.
				if restoreFailed == nil {
					restoreFailed = make(map[int]error)
				}
				restoreFailed[j] = rerr
			}
			return i, restoreFailed, errors.Join(errs...)
		}
	}
	return -1, nil, nil
}

// linkStaged is the link step: it points the systemd-sysext link of every
// staged transfer that needs one at its newest image. When a link fails, it
// restores the links this call already moved, records the failure on the
// failing transfer and errNotLinked on the others, and returns the error
// joined with every restore failure.
//
// Everything that could refuse a link is checked in the snapshot pass, before
// any link moves: a regular file at a link path is backed up as a hard link
// and any other entry is refused. So a refused feature keeps every link (and
// every regular file) exactly as it was.
func (c *Client) linkStaged(feature string, transfers []*config.Transfer, staged []stagedTransfer, results []transferInstall) error {
	var idx []int
	var todo []*config.Transfer
	for i, transfer := range transfers {
		if staged[i].link {
			idx = append(idx, i)
			todo = append(todo, transfer)
		}
	}

	failed, restoreFailed, err := c.linkAllOrNone(todo, func(_ int, transfer *config.Transfer) error {
		return c.linkToSysext(transfer)
	})
	if err != nil {
		results[idx[failed]].err = err
		c.markNotLinked(feature, results)
		for j, rerr := range restoreFailed {
			// This transfer's link may have moved and could not be put
			// back: it may still point at the new version, so it must not
			// read as not linked. The failing transfer keeps its link error
			// too.
			rerr = fmt.Errorf("%w; the link may point at the new version", rerr)
			if j == failed {
				results[idx[j]].err = errors.Join(err, rerr)
			} else {
				results[idx[j]].err = rerr
			}
			results[idx[j]].notLinked = false
		}
		return err
	}
	return nil
}

// removeLinkBackups removes the backup files the snapshot pass made, except a
// backup that a failed restore still needs (see linkSnapshot.keepBackup). A
// backup that cannot be removed is only reported: the feature is linked, and
// a stray dot file in the link directory does not break it.
func (c *Client) removeLinkBackups(snapshots []linkSnapshot) {
	for _, snap := range snapshots {
		if snap.backup == "" || snap.keepBackup {
			continue
		}
		if err := os.Remove(snap.backup); err != nil && !os.IsNotExist(err) {
			c.warn("failed to remove sysext link backup %s: %v", snap.backup, err)
		}
	}
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
