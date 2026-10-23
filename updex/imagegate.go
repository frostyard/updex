package updex

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/frostyard/updex/v2/config"
	"github.com/frostyard/updex/v2/imagecompat"
	"github.com/frostyard/updex/v2/sysext"
	"github.com/frostyard/updex/v2/version"
)

// This file is the minimum image version gate (ADR-0016, "Minimum image
// version"): before a link moves to a staged image, updex reads
// UPDEX_MIN_IMAGE_VERSION from the image's extension-release file and
// compares it with the IMAGE_VERSION of the image that boots next. The
// reading and comparing live in package imagecompat; this file decides what
// each outcome means for the install.
//
// Every path that moves a link to a newer image goes through
// installTransfers, which calls the gate: UpdateFeatures and EnableFeature
// with Now. RollbackFeature links an older staged image on purpose and does
// not call it: the gate protects an update from a revision that needs a
// newer base, and an older revision needs at most the same base as the one
// the host already runs.
//
// The link gate answers "may the next boot run this image". A
// systemd-sysext refresh is a different question: it merges into the base the
// host runs now, not into the one that boots next. A staged deployment that is
// newer than the booted one can therefore allow a link the running base cannot
// run, so every refresh updex runs is gated separately against the running
// image version (see refreshGate).

// nextBootImage resolves the IMAGE_VERSION of the image that boots next once
// per operation and keeps the answer, so an operation that meets no image
// with the key never runs bootc, and one that meets several runs it once.
// The zero value is ready to use. It is not safe for concurrent use; an
// operation handles its features one after another.
type nextBootImage struct {
	resolved bool
	version  string
	source   imagecompat.Source
	err      error
}

// get returns the next-boot image version, running imagecompat's lookup on
// the first call only. A failure is kept too, so every feature whose image
// has the key fails the same way (fail closed) without a second bootc run.
func (n *nextBootImage) get(ctx context.Context, c *Client) (string, imagecompat.Source, error) {
	if !n.resolved {
		n.version, n.source, n.err = imagecompat.NextBootImageVersion(ctx, imagecompat.Options{
			Runner:         c.config.BootcRunner,
			OSReleasePaths: c.paths.osReleasePaths,
		})
		n.resolved = true
	}
	return n.version, n.source, n.err
}

// imageGate is the outcome of the gate for one transfer.
type imageGate struct {
	// needs is "needs image >= <value>" when the image must not be linked;
	// empty when it may be.
	needs string
	// nextBoot is the next-boot IMAGE_VERSION that was compared; empty when
	// the image has no key, so bootc and os-release were not consulted.
	nextBoot string
}

// minImageGate reads the newest staged image of the transfer (the one the
// link step links, sysext.LinkTargetAt) and decides whether it may be linked.
//
//   - No key in the extension-release file: it may.
//   - imagecompat.ErrUnsupportedImage (no EROFS filesystem at all: not GPT+EROFS
//     and not a bare EROFS image, for example a squashfs sysext from another
//     catalog): it may. This fails open on purpose. Only snosi sets the key,
//     and snosi ships EROFS, so an image with no EROFS filesystem has no key
//     that updex could honour; refusing it would stop every non-snosi sysext
//     from updating.
//   - Any other read error (an EROFS image whose release file cannot be read,
//     no extension-release file, several without a match, too large): an
//     error. This fails closed, because such an image has an EROFS filesystem
//     and may carry a key updex cannot honour.
//   - No staged image that the link step could link (sysext.LinkTargetAt
//     fails, for example on a target pattern with a compression suffix): an
//     error. This fails closed too: the gate never depends on the link step
//     failing later.
//   - Key set: the next-boot version is resolved (once per operation), and
//     the two are compared with imagecompat.CheckMinImageVersion. When bootc
//     is installed but fails, that is an error (fail closed): the booted
//     os-release could be older than the staged image.
func (c *Client) minImageGate(ctx context.Context, feature string, t *config.Transfer, nb *nextBootImage) (imageGate, error) {
	path, err := sysext.LinkTargetAt(t, c.paths.sysextLinkDir)
	if err != nil {
		// Fail closed: the link step asks the same question with the same
		// function, so a link this gate cannot see is a link the run cannot
		// check.
		return imageGate{}, fmt.Errorf("cannot check the minimum image version of %s: no staged image: %w", t.Component, err)
	}

	// The transfer's component names the sysext in snosi's catalog; the
	// feature name is the second guess when an image holds several
	// extension-release files.
	release, err := imagecompat.ReadExtensionRelease(path, t.Component)
	if errors.Is(err, imagecompat.ErrAmbiguousExtensionRelease) && feature != t.Component {
		release, err = imagecompat.ReadExtensionRelease(path, feature)
	}
	switch {
	case errors.Is(err, imagecompat.ErrUnsupportedImage):
		c.debug("%s: image format not readable, minimum image version not checked: %v", t.Component, err)
		return imageGate{}, nil
	case err != nil:
		return imageGate{}, fmt.Errorf("cannot read the extension-release file of %s: %w", t.Component, err)
	}

	if _, set := imagecompat.MinImageVersion(release); !set {
		return imageGate{}, nil
	}

	nextBoot, source, err := nb.get(ctx, c)
	if err != nil {
		return imageGate{}, fmt.Errorf("cannot check the minimum image version of %s: next-boot image version: %w", t.Component, err)
	}
	c.debug("%s: next-boot image version %q (%s)", t.Component, nextBoot, source)
	ok, reason := imagecompat.CheckMinImageVersion(release, nextBoot)
	if ok {
		return imageGate{nextBoot: nextBoot}, nil
	}
	return imageGate{needs: reason, nextBoot: nextBoot}, nil
}

// gateMinImage applies minImageGate to the transfers of one feature whose
// link would move (move[i]), before the link step. It reports whether the
// feature must stop here: nothing of it is linked.
//
//   - A read or bootc error on a transfer is an error on it and errNotLinked
//     on the others (as a failed stage): the run exits non-zero.
//   - An image that is too old is not an error. The transfer gets needsImage,
//     the others of the feature get imageBlocked, and nothing is linked, so
//     the feature never ends split between a new and an old revision. The
//     downloaded images stay staged; the next run reaches the relink path and
//     checks again.
func (c *Client) gateMinImage(ctx context.Context, feature string, transfers []*config.Transfer, move []bool, results []transferInstall, nb *nextBootImage) (stop bool) {
	failed := false
	blockedBy := ""
	for i, transfer := range transfers {
		if !move[i] {
			continue
		}
		gate, err := c.minImageGate(ctx, feature, transfer, nb)
		results[i].nextBootImage = gate.nextBoot
		if err != nil {
			results[i].err = err
			failed = true
			continue
		}
		if gate.needs != "" {
			results[i].needsImage = gate.needs
			if blockedBy == "" {
				blockedBy = fmt.Sprintf("%s of feature %s %s", transfer.Component, feature, gate.needs)
			}
		}
	}
	if failed {
		c.markNotLinked(feature, results)
		return true
	}
	if blockedBy == "" {
		return false
	}
	for i := range results {
		if results[i].needsImage == "" {
			results[i].imageBlocked = blockedBy
		}
	}
	return true
}

// dryRunMoves reports, for a dry run, which transfers a real run would link
// now: those whose newest image is already staged and current but whose link
// is not (the relink path). A transfer that a real run would download has no
// staged image to read, so a dry run cannot check it and says nothing.
func (c *Client) dryRunMoves(transfers []*config.Transfer, results []transferInstall) []bool {
	move := make([]bool, len(transfers))
	for i, transfer := range transfers {
		if results[i].outcome.Downloaded {
			continue
		}
		linked, err := sysext.LinkIsCurrentAt(transfer, c.paths.sysextLinkDir)
		move[i] = err == nil && !linked
	}
	return move
}

// stagedImageGate is the read-only form of the gate for `features check` and
// `features list`: when the newest staged image of the transfer is not the
// linked one, and no rollback record holds it, it reports whether the next
// `features update` may link it. It links and writes nothing, and runs bootc
// only when that image has the key. status carries the linked version and the
// rollback-rejected version.
func (c *Client) stagedImageGate(ctx context.Context, feature string, t *config.Transfer, status TransferStatus, nb *nextBootImage) (imageGate, error) {
	staged, err := sysext.StagedVersionsAt(t, c.paths.sysextLinkDir)
	if err != nil || len(staged) == 0 {
		return imageGate{}, nil // other code reports an unreadable staging directory
	}
	newest := staged[0]
	if newest == status.LinkedVersion {
		return imageGate{}, nil
	}
	if rej := status.RollbackRejected; rej != "" && version.Compare(newest, rej) <= 0 {
		return imageGate{}, nil // held by a rollback: update links nothing
	}
	return c.minImageGate(ctx, feature, t, nb)
}

// runningImage resolves the IMAGE_VERSION of the running image once per
// operation and keeps it, so an operation that meets no sysext with the key
// never reads os-release, and one that meets several reads it once. The zero
// value is ready to use. It is not safe for concurrent use; an operation
// handles its sysexts one after another.
type runningImage struct {
	resolved bool
	version  string
}

// get returns the running image version, reading the booted os-release
// files on the first call only. It runs no command: on a bootc host the
// booted os-release describes the running deployment, and bootc is needed
// only for the next-boot question (nextBootImage).
func (r *runningImage) get(c *Client) string {
	if !r.resolved {
		r.version = imagecompat.BootedImageVersion(c.paths.osReleasePaths)
		r.resolved = true
	}
	return r.version
}

// refreshLink is one sysext that a systemd-sysext refresh merges with a
// version other than the running one, or one whose merged state updex cannot
// read: a link the operation moved, or a pending link elsewhere on the host
// that the host-wide refresh activates too.
type refreshLink struct {
	// component names the sysext, for the report.
	component string
	// transfer describes the sysext, so its staged image can be found.
	transfer *config.Transfer
	// version is the linked version the refresh would merge.
	version string
	// stateErr is set when the merged state or the link could not be read.
	// The gate then fails closed without reading the image.
	stateErr error
}

// refreshBlocker names the sysext that keeps the refresh from running: the
// running base image cannot run it.
type refreshBlocker struct {
	component string
	// needs is "needs image >= <value>" when the linked image sets the key.
	needs string
	// err is the failure that kept the image from being checked. Like the
	// link gate, the refresh check fails closed on it.
	err error
	// running is the running IMAGE_VERSION that was compared. Empty when the
	// image has no key or could not be read.
	running string
}

// reason renders the blocker for an error message, without the advice that
// differs per command.
func (b refreshBlocker) reason() string {
	if b.err != nil {
		return fmt.Sprintf("cannot check %s: %v", b.component, b.err)
	}
	running := b.running
	if running == "" {
		running = "unknown"
	}
	return fmt.Sprintf("%s %s (running image %s)", b.component, b.needs, running)
}

// skipped renders the blocker as the RefreshSkipped text of the operations
// that keep their links and leave activation to the next boot.
func (b refreshBlocker) skipped() string {
	return "refresh skipped: " + b.reason() + "; it takes effect at the next boot"
}

// refreshGate decides whether a systemd-sysext refresh may run now, and
// returns why it may not, or nil when it may.
//
// Every link the refresh would merge, except one whose merged version is known
// to equal it, is read like the link gate reads a staged image, but compared
// with the running image version instead of the next-boot one. A link whose
// merged state or link cannot be read is blocked without reading its image:
// the gate fails closed when it cannot establish the state.
//
//   - No key in the extension-release file: it may be merged.
//   - imagecompat.ErrUnsupportedImage: it may be merged, for the same reason
//     the link gate fails open on it (only snosi sets the key, and snosi
//     ships EROFS).
//   - Any other read error: blocked. Such an image has an EROFS filesystem
//     and may carry a key that cannot be honoured, so it fails closed.
//   - Key set: blocked when imagecompat.CheckMinImageVersion against the
//     running version is not ok, which includes a running version that is
//     missing or not numeric.
//
// The links are only read. Keeping them is always correct for the next boot;
// this gate decides about activating them now.
func (c *Client) refreshGate(links []refreshLink, ri *runningImage) *refreshBlocker {
	for _, link := range links {
		if b := c.refreshLinkBlocker(link, ri); b != nil {
			return b
		}
	}
	return nil
}

// refreshLinkBlocker checks one link and returns nil when the running base
// can merge it. A link whose state could not be read is blocked: without the
// merged version, updex cannot tell what the refresh would merge.
func (c *Client) refreshLinkBlocker(link refreshLink, ri *runningImage) *refreshBlocker {
	if link.stateErr != nil {
		return &refreshBlocker{component: link.component, err: fmt.Errorf("cannot read the state of %s: %w", link.component, link.stateErr)}
	}
	path, err := sysext.StagedImageAt(link.transfer, c.paths.sysextLinkDir, link.version)
	if err != nil {
		return &refreshBlocker{component: link.component, err: fmt.Errorf("cannot find the linked image: %w", err)}
	}
	if path == "" {
		// The link names no staged image, so systemd-sysext has nothing to
		// merge and nothing to check. Other code reports a link that moved
		// to a version this function cannot find.
		c.debug("%s: no staged image for version %s, refresh check skipped", link.component, link.version)
		return nil
	}

	// The component is the only name hint available here: a pending link of
	// another feature is found through its transfer, not through the feature
	// name. An image with several extension-release files and no match is a
	// read error, so it fails closed.
	release, err := imagecompat.ReadExtensionRelease(path, link.transfer.Component)
	switch {
	case errors.Is(err, imagecompat.ErrUnsupportedImage):
		c.debug("%s: image format not readable, refresh check skipped: %v", link.component, err)
		return nil
	case err != nil:
		return &refreshBlocker{component: link.component, err: fmt.Errorf("cannot read the extension-release file: %w", err)}
	}

	if _, set := imagecompat.MinImageVersion(release); !set {
		return nil
	}
	running := ri.get(c)
	c.debug("%s: running image version %q", link.component, running)
	if ok, needs := imagecompat.CheckMinImageVersion(release, running); !ok {
		return &refreshBlocker{component: link.component, needs: needs, running: running}
	}
	return nil
}

// ownPendingLinks lists the links of own that the running-base check must
// read: every transfer with a usable link whose merged version is not known to
// equal the linked one, including an "unknown" or empty merged version. It is
// the own half of pendingActivationLinks. A transfer whose state cannot be read
// is a candidate with stateErr, so the gate fails closed.
func (c *Client) ownPendingLinks(own []*config.Transfer) []refreshLink {
	var links []refreshLink
	for _, t := range own {
		status := c.transferStatus(t)
		switch {
		case status.Error != "":
			c.warn("could not determine whether a refresh activates %s: %s", t.Component, status.Error)
			links = append(links, refreshLink{component: t.Component, transfer: t, stateErr: errors.New(status.Error)})
		case refreshPending(status.RunningVersion, status.LinkedVersion):
			links = append(links, refreshLink{component: t.Component, transfer: t, version: status.LinkedVersion})
		}
	}
	return links
}

// activationLinks returns the links a refresh would activate from pending:
// a deactivation merges nothing, so it needs no check. A display-only entry is
// a gate candidate too when its merged version is unknown, and a state error
// is carried into the gate.
func activationLinks(pending []pendingActivationLink) []refreshLink {
	var links []refreshLink
	for _, p := range pending {
		if p.Deactivate {
			continue
		}
		links = append(links, refreshLink{component: p.Component, transfer: p.transfer, version: p.LinkedVersion, stateErr: p.stateErr})
	}
	return links
}

// refreshGateFor is the gate for a command's refresh: the pending links of
// own (the transfers the command worked on), then the other pending
// activations that the host-wide refresh merges as well.
func (c *Client) refreshGateFor(own []*config.Transfer, pending []pendingActivationLink, ri *runningImage) *refreshBlocker {
	links := slices.Concat(c.ownPendingLinks(own), activationLinks(pending))
	return c.refreshGate(links, ri)
}

// activationsOf returns the report form of pending activations, nil when
// there are none. Gate-only entries (an unknown merged version, or a state
// that could not be read) are not shown.
func activationsOf(pending []pendingActivationLink) []PendingActivation {
	var out []PendingActivation
	for _, p := range pending {
		if !p.reported {
			continue
		}
		out = append(out, p.PendingActivation)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
