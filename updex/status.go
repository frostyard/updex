package updex

import (
	"fmt"
	"strings"

	"github.com/frostyard/updex/v2/config"
	"github.com/frostyard/updex/v2/sysext"
)

// TransferStatus reports, for one transfer, the sysext version the host runs
// and the version its next boot merges (ADR-0016, "Status and logs").
type TransferStatus struct {
	// Component is the transfer's component name.
	Component string `json:"component"`

	// RunningVersion is the version merged now, read from the
	// .systemd-sysext metadata under the client's sysext hierarchies
	// (ADR-0015). sysext.UnknownVersion ("unknown") means the image is merged
	// but the metadata records no version. Empty when the image is not
	// merged, or when the merged state could not be read (see Error).
	RunningVersion string `json:"running_version"`

	// LinkedVersion is the version that <SysextLinkDir>/<component>.<ext>
	// names: the image the next boot, or the next systemd-sysext refresh,
	// merges. Empty when the link is absent, not a symlink, dangling, or
	// names an image the transfer's target patterns do not match, and when
	// the link could not be inspected (see Error).
	LinkedVersion string `json:"linked_version"`

	// PendingReboot is true when LinkedVersion names a version other than the
	// running one, so the next boot (or refresh) changes what the host runs.
	// It is false when no version is linked, when RunningVersion is
	// "unknown" (the two cannot be compared), and when Error is set.
	PendingReboot bool `json:"pending_reboot,omitempty"`

	// RollbackRejected is the version a rollback record rejected for this
	// transfer: `features update` links nothing at or below it until a newer
	// version is published. Empty when the feature has no record. It is set
	// by the callers that know the feature (Features, CheckFeatures).
	RollbackRejected string `json:"rollback_rejected,omitempty"`

	// NeedsImage is "needs image >= <value>" when the newest staged image of
	// this transfer is not linked and its UPDEX_MIN_IMAGE_VERSION is higher
	// than the next-boot image version, so `features update` leaves the old
	// link (ADR-0016). Set by Features and CheckFeatures; empty when the image
	// has no key, is old enough, or no unlinked image is staged.
	NeedsImage string `json:"needs_image,omitempty"`

	// Error is set when the merged state or the link could not be read, or
	// when the feature's rollback record could not be read. The version that
	// could be read is still reported.
	Error string `json:"error,omitempty"`
}

// featureTransferStatus is transferStatus plus what the feature's rollback
// record (read once by the caller) says about the transfer. A record that
// cannot be read is an Error on every transfer of the feature, because the
// update path then links nothing for it.
func (c *Client) featureTransferStatus(t *config.Transfer, rec *rollbackRecord, recErr error) TransferStatus {
	status := c.transferStatus(t)
	if recErr != nil {
		if status.Error != "" {
			status.Error += "; "
		}
		status.Error += recErr.Error()
		status.PendingReboot = false
		return status
	}
	status.RollbackRejected = rec.rejectedVersions()[t.Component]
	return status
}

// PendingActivation names a sysext that is linked to a version the host does
// not run, so that a systemd-sysext refresh (or the next boot) activates it.
type PendingActivation struct {
	// Component is the transfer's component name.
	Component string `json:"component"`
	// RunningVersion is the version merged now; empty when not merged.
	RunningVersion string `json:"running_version"`
	// LinkedVersion is the version the refresh would merge.
	LinkedVersion string `json:"linked_version"`
}

// String renders the activation as "component: running -> linked".
func (p PendingActivation) String() string {
	running := p.RunningVersion
	if running == "" {
		running = "not merged"
	}
	return fmt.Sprintf("%s: %s -> %s", p.Component, running, p.LinkedVersion)
}

// activationPending reports whether a link to linked changes what the host
// runs, given the merged version running. An "unknown" merged version cannot
// be compared, so it is not reported as pending.
func activationPending(running, linked string) bool {
	return linked != "" && linked != running && running != sysext.UnknownVersion
}

// transferStatus reads the merged and linked versions of one transfer. A read
// failure goes to the Error field and never aborts: the other value is kept.
func (c *Client) transferStatus(t *config.Transfer) TransferStatus {
	status := TransferStatus{Component: t.Component}
	var problems []string

	running, err := sysext.MergedVersionIn(t, c.paths.sysextHierarchies)
	if err != nil {
		problems = append(problems, fmt.Sprintf("failed to read merged state: %v", err))
	} else {
		status.RunningVersion = running
	}

	linked, err := sysext.LinkedVersionAt(t, c.paths.sysextLinkDir)
	if err != nil {
		problems = append(problems, fmt.Sprintf("failed to inspect sysext link: %v", err))
	} else {
		status.LinkedVersion = linked
	}

	// With either value unread the comparison would be a guess.
	status.Error = strings.Join(problems, "; ")
	status.PendingReboot = status.Error == "" && activationPending(status.RunningVersion, status.LinkedVersion)
	return status
}

// pendingActivations lists the transfers, outside own, whose link names a
// version the host does not run: what a systemd-sysext refresh activates in
// addition to what the calling operation changed. own is the operation's own
// transfers; they are matched by link name, because the link is the thing a
// refresh activates. scoped is the domain the caller loaded. A caller scoped
// to one component (ClientConfig.Definitions unset) still gets the whole host
// checked, because a refresh does not honour that scope.
//
// Only links of transfers that updex's definitions describe are visible: a
// link made by hand for an image with no transfer is not listed. A transfer
// whose state cannot be read is reported as a warning and skipped, and the
// refresh is not prevented.
func (c *Client) pendingActivations(component string, scoped, own []*config.Transfer) []PendingActivation {
	domain := scoped
	if component != "" && c.config.Definitions == "" {
		// Name-collision warnings of the full union are dropped: this is a
		// read for the warning only, not the domain the caller operates on.
		all, _, err := config.LoadAllTransfersIn("", c.paths.definitionRoots, c.paths.osReleasePaths)
		if err != nil {
			c.warn("could not list sysexts outside component %q that a refresh would activate: %v", component, err)
		} else {
			domain = all
		}
	}

	ownLinks := make(map[string]bool, len(own))
	for _, t := range own {
		if name := sysext.SysextLinkName(t); name != "" {
			ownLinks[name] = true
		}
	}

	var pending []PendingActivation
	seen := make(map[string]bool)
	for _, t := range domain {
		name := sysext.SysextLinkName(t)
		if name == "" || ownLinks[name] || seen[name] {
			continue
		}
		seen[name] = true

		status := c.transferStatus(t)
		if status.Error != "" {
			c.warn("could not determine whether a refresh activates %s: %s", t.Component, status.Error)
		}
		if status.PendingReboot {
			pending = append(pending, PendingActivation{
				Component:      t.Component,
				RunningVersion: status.RunningVersion,
				LinkedVersion:  status.LinkedVersion,
			})
		}
	}
	return pending
}

// warnPendingActivations reports pending through the reporter as one warning,
// before the refresh runs. With dryRun it says what the refresh would do.
func (c *Client) warnPendingActivations(pending []PendingActivation, dryRun bool) {
	if len(pending) == 0 {
		return
	}
	items := make([]string, len(pending))
	for i, p := range pending {
		items[i] = p.String()
	}
	verb := "will also activate"
	if dryRun {
		verb = "would also activate"
	}
	c.warn("systemd-sysext refresh %s %d sysext(s) that are linked but not merged: %s",
		verb, len(pending), strings.Join(items, ", "))
}
