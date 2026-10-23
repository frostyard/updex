package updex

import (
	"fmt"
	"slices"
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

	// Error is set when the merged state or the link could not be read. The
	// version that could be read is still reported.
	Error string `json:"error,omitempty"`
}

// PendingActivation names a sysext whose merged state a systemd-sysext
// refresh (or the next boot) changes: it is linked to a version the host does
// not run (an activation), or, with Deactivate, it is merged but has no link
// (a deactivation: the refresh unmerges it under any workload that uses it).
type PendingActivation struct {
	// Component is the transfer's component name.
	Component string `json:"component"`
	// RunningVersion is the version merged now; empty when not merged.
	RunningVersion string `json:"running_version"`
	// LinkedVersion is the version the refresh would merge; empty for a
	// deactivation.
	LinkedVersion string `json:"linked_version"`
	// Deactivate is true when the sysext is merged but not linked, so the
	// refresh unmerges it. An image that another systemd-sysext search
	// directory (/etc/extensions, /run/extensions, /usr/lib/extensions)
	// provides stays merged; updex cannot tell, so it lists it anyway.
	Deactivate bool `json:"deactivate,omitempty"`
}

// String renders an activation as "component: running -> linked" and a
// deactivation as "component: running -> not linked".
func (p PendingActivation) String() string {
	running := p.RunningVersion
	if running == "" {
		running = "not merged"
	}
	if p.Deactivate {
		return fmt.Sprintf("%s: %s -> not linked", p.Component, running)
	}
	return fmt.Sprintf("%s: %s -> %s", p.Component, running, p.LinkedVersion)
}

// deactivationPending reports whether a refresh unmerges a sysext: it is
// merged (any running version, "unknown" included) and has no link.
func deactivationPending(running, linked string) bool {
	return running != "" && linked == ""
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

// pendingActivations lists the transfers, outside own, whose merged state a
// systemd-sysext refresh changes in addition to what the calling operation
// changed: a link that names a version the host does not run (activation),
// and a merged sysext with no link (deactivation). own is the operation's own
// transfers; they are matched by link name, because the link is the thing a
// refresh activates. scoped is the domain the caller loaded.
//
// A refresh does not honour any scope, so the host's normal definitions are
// always read and unioned with scoped, whether scoped is one component or a
// definitions directory (ClientConfig.Definitions). A run limited that way
// still lists every host sysext its refresh changes, for example a transfer
// whose definitions live in a normal sysupdate.d root while the run works in
// another directory. Transfers are matched by link name and the first one
// seen wins (scoped before the host's own), so a transfer that both describe
// is listed once. Only the warning reads this wide; the operation's update
// scope stays scoped. When the host definitions cannot be read, the failure is
// a warning and scoped is used alone.
//
// Only links of transfers that updex's definitions describe are visible: a
// link made by hand for an image with no transfer is not listed. A transfer
// whose state cannot be read is reported as a warning and skipped, and the
// refresh is not prevented.
func (c *Client) pendingActivations(scoped, own []*config.Transfer) []PendingActivation {
	domain := scoped
	// Name-collision warnings of the full union are dropped: this is a read
	// for the warning only, not the domain the caller operates on.
	host, _, err := config.LoadAllTransfersIn("", c.paths.definitionRoots, c.paths.osReleasePaths)
	if err != nil {
		c.warn("could not list the sysexts outside the requested scope that a refresh would activate: %v", err)
	} else {
		domain = slices.Concat(scoped, host)
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
		switch {
		case status.PendingReboot:
			pending = append(pending, PendingActivation{
				Component:      t.Component,
				RunningVersion: status.RunningVersion,
				LinkedVersion:  status.LinkedVersion,
			})
		case status.Error == "" && deactivationPending(status.RunningVersion, status.LinkedVersion):
			pending = append(pending, PendingActivation{
				Component:      t.Component,
				RunningVersion: status.RunningVersion,
				Deactivate:     true,
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
	var activate, deactivate []string
	for _, p := range pending {
		if p.Deactivate {
			deactivate = append(deactivate, p.String())
		} else {
			activate = append(activate, p.String())
		}
	}
	will := "will"
	if dryRun {
		will = "would"
	}
	if len(activate) > 0 {
		c.warn("systemd-sysext refresh %s also activate %d sysext(s) that are linked but not merged: %s",
			will, len(activate), strings.Join(activate, ", "))
	}
	if len(deactivate) > 0 {
		c.warn("systemd-sysext refresh %s also deactivate %d sysext(s) that are merged but not linked: %s",
			will, len(deactivate), strings.Join(deactivate, ", "))
	}
}
