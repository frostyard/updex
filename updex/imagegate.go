package updex

import (
	"context"
	"errors"
	"fmt"

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
