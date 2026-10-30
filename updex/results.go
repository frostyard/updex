package updex

import "strings"

// DaemonActionResult represents the result of enabling or disabling the
// automatic update daemon.
type DaemonActionResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	// DryRun reports that the daemon units and their systemd state were not
	// changed; Message describes the actions that a real run would take.
	DryRun bool `json:"dry_run,omitempty"`
	// UnitFiles holds the absolute paths of the unit files the operation
	// wrote, removed, or would write or remove (timer first), or, when
	// Packaged is set on an enable, the packaged files in use. Disable lists
	// only the files updex removed; a unit that the OS package ships is not
	// listed because disable never removes it.
	UnitFiles []string `json:"unit_files,omitzero"`
	// Packaged reports that the OS package ships the timer, so enable wrote
	// nothing and disable left the packaged units in place.
	Packaged bool `json:"packaged,omitempty"`
	// KeptFiles holds the unit files in /etc/systemd/system that disable
	// left in place because updex cannot prove that it wrote them.
	KeptFiles []string `json:"kept_files,omitzero"`
	// MaskedFiles holds the unit files in /etc/systemd/system that disable
	// left in place because they are masked: a symlink to /dev/null, the
	// shape `systemctl mask` writes. Only `systemctl unmask` removes a mask.
	MaskedFiles []string `json:"masked_files,omitzero"`
}

// DaemonStatusResult represents the automatic update daemon's current state.
type DaemonStatusResult struct {
	// Installed reports that a timer unit file exists, packaged or in /etc.
	Installed bool `json:"installed"`
	Enabled   bool `json:"enabled"`
	Active    bool `json:"active"`
	// Schedule is the timer's effective OnCalendar schedule, a comma-separated
	// list of its calendar triggers. It comes from the TimersCalendar property
	// systemd reports, which includes administrator drop-ins, or, when systemd
	// cannot report it, from the unit file in use and its drop-ins.
	Schedule string `json:"schedule,omitempty"`
	// Source is "packaged" when the timer file in use is the package's in
	// /usr/lib/systemd/system, or "etc" when it is in /etc/systemd/system
	// (where it overrides a packaged one).
	Source string `json:"source,omitempty"`
	// UnitFiles holds the paths of the timer and service files in use.
	UnitFiles []string `json:"unit_files,omitzero"`
	// NextRun is the timer's NextElapseUSecRealtime as systemd prints it;
	// empty when no run is scheduled.
	NextRun string `json:"next_run,omitempty"`
	// LastRun is the service's ExecMainExitTimestamp as systemd prints it;
	// empty when the service has not run since boot.
	LastRun string `json:"last_run,omitempty"`
	// LastResult is the service's Result for the last run ("success",
	// "exit-code", ...); empty when LastRun is empty.
	LastResult string `json:"last_result,omitempty"`
	// Masked reports that the timer file in use is a systemd mask: a symlink
	// to /dev/null. A masked timer never fires, so Schedule is empty.
	Masked bool `json:"masked,omitempty"`
}

// CheckResult represents the result of a check operation for a single component.
type CheckResult struct {
	Component string `json:"component"`
	// CurrentVersion is the version the systemd-sysext link names, or the
	// staged version when no usable link exists. It is kept for
	// compatibility; RunningVersion and LinkedVersion are the precise values.
	// With a usable link it equals LinkedVersion.
	CurrentVersion string `json:"current_version,omitempty"`
	NewestVersion  string `json:"newest_version"`
	// RunningVersion is the version merged now (see TransferStatus). Empty
	// when the image is not merged; "unknown" when merged without a recorded
	// version. Always present in JSON.
	RunningVersion string `json:"running_version"`
	// LinkedVersion is the version the systemd-sysext link names, which the
	// next boot merges. Empty when there is no usable link. Always present in
	// JSON.
	LinkedVersion string `json:"linked_version"`
	// PendingReboot is true when LinkedVersion differs from RunningVersion
	// (see TransferStatus.PendingReboot).
	PendingReboot bool `json:"pending_reboot,omitempty"`
	// RollbackRejected is the version a rollback record rejected for this
	// component (see TransferStatus.RollbackRejected). While NewestVersion is
	// not newer than it, UpdateAvailable is false: `features update` holds.
	RollbackRejected string `json:"rollback_rejected,omitempty"`
	// RollbackHeldBy names why a record holds the feature although this
	// component could advance (see TransferStatus.RollbackHeldBy), for
	// example "grp: beta has no version newer than 2.0.0" or "grp: it
	// advances only when grp updates (or run 'updex features rollback --clear
	// grp')". While it is set, UpdateAvailable is false.
	RollbackHeldBy  string `json:"rollback_held_by,omitempty"`
	UpdateAvailable bool   `json:"update_available"`
	// NeedsImage is "needs image >= <value>" when the newest staged image of
	// this component is not linked and `features update` would not link it
	// because its UPDEX_MIN_IMAGE_VERSION is higher than the next-boot image
	// version (ADR-0016). It is set only for an image that is already staged:
	// a check never downloads, so it cannot tell for a newer published image.
	// It is not an error.
	NeedsImage string `json:"needs_image,omitempty"`
	// NextBootImageVersion is the IMAGE_VERSION of the image that boots next,
	// when the check had to read it (a staged unlinked image sets the key).
	NextBootImageVersion string `json:"next_boot_image_version,omitempty"`
	// Error is set when the component could not be checked (manifest fetch,
	// signature verification, pattern failure, or installed-version listing).
	// UpdateAvailable is always false in that case; other fields may be empty,
	// except NewestVersion may be set if the failure happens after reading the manifest.
	Error string `json:"error,omitempty"`
}

// UpdateResult represents the result of an update operation for a single component.
type UpdateResult struct {
	Component         string   `json:"component"`
	Version           string   `json:"version"`
	Downloaded        bool     `json:"downloaded"`
	Installed         bool     `json:"installed"`
	DryRun            bool     `json:"dry_run,omitempty"`
	Error             string   `json:"error,omitempty"`
	NextActionMessage string   `json:"next_action_message,omitempty"`
	RemovedVersions   []string `json:"removed_versions,omitzero"`
	// Held is true when a rollback record holds the feature, so nothing was
	// downloaded or linked and every old link stays. It is not an error. A
	// record of any feature that shares a transfer counts, not only the
	// feature being updated; a record of another feature holds the feature
	// while only that feature advances the transfers its record names, and
	// the hold lasts while its record exists, also when that feature is
	// disabled or masked. The feature's own record holds while a component
	// it names, inside this feature or not, has no version newer than the
	// one it rejects. Version is the version the link names;
	// NextActionMessage says what holds the feature.
	Held bool `json:"held_by_rollback,omitempty"`
	// RejectedVersion is the highest version the applicable rollback
	// records reject for this transfer, when Held. Empty when the transfer
	// is held only because another transfer of its feature, and no record
	// component, is the reason.
	RejectedVersion string `json:"rejected_version,omitempty"`
	// NeedsImage is "needs image >= <value>" when this transfer's staged image
	// has an UPDEX_MIN_IMAGE_VERSION higher than the IMAGE_VERSION of the image
	// that boots next (ADR-0016). Nothing of the feature was linked: the old
	// links stay and the image stays staged until a later run finds the host
	// image new enough. It is not an error; Installed is false. In a dry run
	// it is set only for an image that is already staged.
	NeedsImage string `json:"needs_image,omitempty"`
	// NotLinked is true on the other transfers of a feature that NeedsImage
	// kept unlinked, because a feature's transfers link together.
	// NextActionMessage names the transfer that needs the newer image.
	NotLinked bool `json:"not_linked,omitempty"`
	// NextBootImageVersion is the next-boot IMAGE_VERSION that the check
	// compared, when it was read.
	NextBootImageVersion string `json:"next_boot_image_version,omitempty"`
	// Linked is true when this run moved the systemd-sysext link to Version:
	// on the download path and on the relink path (an image that was already
	// staged but whose link was missing, dangling, or pointed elsewhere). It
	// is always false in a dry run. Downloaded=false with Linked=true is an
	// image that needed no download but that the run linked.
	Linked bool `json:"linked,omitempty"`
}

// InactiveTransfer reports a transfer that a feature command did not act on
// because it is not active (sysupdate.d(5)): a transfer is active when its
// Features= is empty or at least one listed feature is enabled (OR), and every
// feature in its RequisiteFeatures= is enabled (AND). Nothing is downloaded,
// linked or vacuumed for an inactive transfer. See ADR-0017.
type InactiveTransfer struct {
	// Component is the transfer's component name.
	Component string `json:"component"`
	// MissingRequisites lists the RequisiteFeatures= entries that are not
	// enabled (a feature that does not exist is not enabled). The transfer
	// waits until all of them are enabled.
	MissingRequisites []string `json:"missing_requisites,omitzero"`
	// NeedsOneOf lists the Features= entries when none of them is enabled.
	// The transfer waits until at least one of them is enabled.
	NeedsOneOf []string `json:"needs_one_of,omitzero"`
}

// Reason renders why the transfer is inactive, for a message: "waits for
// feature(s) lodge", "needs one of features: maild", or both joined by "and".
func (i InactiveTransfer) Reason() string {
	var parts []string
	if len(i.MissingRequisites) > 0 {
		parts = append(parts, "waits for feature(s) "+strings.Join(i.MissingRequisites, ", "))
	}
	if len(i.NeedsOneOf) > 0 {
		parts = append(parts, "needs one of features: "+strings.Join(i.NeedsOneOf, ", "))
	}
	return strings.Join(parts, " and ")
}

// KeptTransfer reports a transfer that a disable with Now did not remove
// because another enabled feature still activates it: it is associated with
// the disabled feature through Features=, and stays active without it (ADR-0017).
type KeptTransfer struct {
	// Component is the transfer's component name.
	Component string `json:"component"`
	// ActiveThrough lists the other enabled features in the transfer's
	// Features= that keep it active.
	ActiveThrough []string `json:"active_through"`
}

// UpdateFeaturesResult represents the result of updating all enabled features.
type UpdateFeaturesResult struct {
	Feature string         `json:"feature"`
	Results []UpdateResult `json:"results"`
	// Inactive lists the transfers associated with the feature that were not
	// downloaded or linked because they are not active: a requisite feature is
	// disabled, or none of their Features= is enabled. See InactiveTransfer.
	Inactive []InactiveTransfer `json:"inactive,omitzero"`
	// PendingActivations lists the sysexts outside this run's own features
	// that are linked to a version the host does not run, and that the
	// systemd-sysext refresh at the end of the run activates as well (a
	// dry run: would activate). Empty with NoRefresh. The list is host-wide:
	// every element of the result slice carries the same list.
	PendingActivations []PendingActivation `json:"pending_activations,omitzero"`
	// RefreshSkipped says why the systemd-sysext refresh did not run (a dry
	// run: why it would not run): the refresh merges into the running image,
	// and a sysext it would activate needs a newer image version than the
	// running one (ADR-0016, "Minimum image version"). The links are correct
	// for the next boot and stay, so the change takes effect at the next
	// boot instead. It is not an error. Empty when the refresh ran, was not
	// requested (NoRefresh), or nothing blocked it. The text is host-wide:
	// every element of the result slice carries the same text.
	RefreshSkipped string `json:"refresh_skipped,omitempty"`
}

// CheckFeaturesResult represents the result of checking all enabled features.
type CheckFeaturesResult struct {
	Feature string        `json:"feature"`
	Results []CheckResult `json:"results"`
	// Inactive lists the transfers associated with the feature that were not
	// checked because they are not active. See InactiveTransfer.
	Inactive []InactiveTransfer `json:"inactive,omitzero"`
}

// Feature origin kinds reported in FeatureInfo.Origin. Kind and name are
// kept separate so consumers can match on the kind without having to
// disambiguate a catalog legitimately named "image" or "local".
const (
	// FeatureOriginCatalog: added by 'updex catalog add'. OriginName is
	// the catalog repo, e.g. "fedora".
	FeatureOriginCatalog = "catalog"
	// FeatureOriginImage: shipped in /usr/lib by the OS image. OriginName
	// is the image identifier (see config.ImageName), e.g. "ucore".
	FeatureOriginImage = "image"
	// FeatureOriginLocal: administered on this machine. OriginName is the
	// search root: "etc", "usr" (/usr/local/lib), or "run".
	FeatureOriginLocal = "local"
	// FeatureOriginUnknown: outside every search root, i.e. loaded from a
	// --definitions override directory. OriginName is empty.
	FeatureOriginUnknown = "unknown"
)

// FeatureInfo represents feature information.
type FeatureInfo struct {
	Name          string   `json:"name"`
	Description   string   `json:"description,omitempty"`
	Documentation string   `json:"documentation,omitempty"`
	Enabled       bool     `json:"enabled"`
	Masked        bool     `json:"masked,omitempty"`
	Source        string   `json:"source"`
	Origin        string   `json:"origin"`
	OriginName    string   `json:"origin_name,omitempty"`
	Transfers     []string `json:"transfers,omitzero"`
	// TransferStatus holds the running and linked version of each transfer,
	// in the same order as Transfers.
	TransferStatus []TransferStatus `json:"transfer_status,omitzero"`
}

// CatalogEntry represents one sysext available from a configured catalog repo.
type CatalogEntry struct {
	Name      string `json:"name"`
	Repo      string `json:"repo"`
	Installed bool   `json:"installed"`
	Enabled   bool   `json:"enabled"`
}

// CatalogAddResult represents the result of adding a sysext from a catalog.
type CatalogAddResult struct {
	Name         string               `json:"name"`
	Repo         string               `json:"repo"`
	Component    string               `json:"component"`
	TransferFile string               `json:"transfer_file"`
	FeatureFile  string               `json:"feature_file"`
	DryRun       bool                 `json:"dry_run,omitempty"`
	Enable       *FeatureActionResult `json:"enable,omitempty"`
}

// CatalogRemoveResult represents the result of removing a catalog-managed sysext.
type CatalogRemoveResult struct {
	Name         string               `json:"name"`
	Repo         string               `json:"repo"`
	Component    string               `json:"component"`
	RemovedFiles []string             `json:"removed_files,omitzero"`
	DryRun       bool                 `json:"dry_run,omitempty"`
	Disable      *FeatureActionResult `json:"disable,omitempty"`
}

// FeatureActionResult represents the result of a feature enable/disable action.
type FeatureActionResult struct {
	Feature           string   `json:"feature"`
	Action            string   `json:"action"`
	Success           bool     `json:"success"`
	DropIn            string   `json:"drop_in,omitempty"`
	Error             string   `json:"error,omitempty"`
	NextActionMessage string   `json:"next_action_message,omitempty"`
	RemovedFiles      []string `json:"removed_files,omitzero"`
	DownloadedFiles   []string `json:"downloaded_files,omitzero"`
	DryRun            bool     `json:"dry_run,omitempty"`
	Unmerged          bool     `json:"unmerged,omitempty"`
	// NotLinked lists, for an enable with Now, the images that were
	// downloaded but not linked because they need a newer base image than the
	// one that boots next (ADR-0016): "<component> of feature <name> needs
	// image >= <value>". The feature's old links stay. It is not an error.
	NotLinked []string `json:"not_linked,omitzero"`
	// Inactive lists, for an enable with Now, the transfers associated with
	// the feature that were not downloaded, or in a dry run would not be,
	// because they are not active even with this feature enabled: a requisite
	// feature is disabled, or none of their Features= is enabled. They are
	// downloaded when the missing feature is enabled. See InactiveTransfer.
	Inactive []InactiveTransfer `json:"inactive,omitzero"`
	// Kept lists, for a disable with Now, the transfers that were not removed
	// (in a dry run: would not be) because another enabled feature still
	// activates them. Their links, images and merged state are untouched. See
	// KeptTransfer.
	Kept []KeptTransfer `json:"kept,omitzero"`
	// RefreshError is set when every requested filesystem change succeeded
	// but the final `systemd-sysext refresh` failed. Success is false and
	// Error carries the same message; NextActionMessage says how to finish
	// activation: a reboot after an enable (a plain `systemd-sysext refresh`
	// runs no systemd-sysusers, ADR-0019), a manual refresh or a reboot after
	// a disable. For a disable with Now the preceding unmerge already ran, so
	// the remaining extensions may stay unmerged until that refresh happens.
	RefreshError string `json:"refresh_error,omitempty"`
	// PendingActivations lists the sysexts outside this feature that are
	// linked to a version the host does not run, and that the
	// `systemd-sysext refresh` of this action activates as well (a dry run:
	// would activate). Set only when the action runs, or would run, a refresh.
	PendingActivations []PendingActivation `json:"pending_activations,omitzero"`
	// RefreshSkipped says why the refresh of an enable with Now did not run
	// (a dry run: why it would not run): a sysext the refresh would activate
	// needs a newer image version than the running one (ADR-0016). The
	// feature is enabled and linked for the next boot, so it takes effect at
	// the next boot instead. It is not an error.
	RefreshSkipped string `json:"refresh_skipped,omitempty"`
}

// RollbackTransfer reports what RollbackFeature did, or would do, for one
// transfer of the feature.
type RollbackTransfer struct {
	// Component is the transfer's component name.
	Component string `json:"component"`
	// FromVersion is the version the link named before the rollback.
	FromVersion string `json:"from_version"`
	// ToVersion is the staged version the link names after the rollback.
	ToVersion string `json:"to_version"`
	// RejectedVersion is the version the rollback record holds for this
	// transfer: FromVersion, or a newer version an earlier rollback already
	// rejected. features update links nothing at or below it.
	RejectedVersion string `json:"rejected_version"`
}

// RollbackFeatureResult represents the result of a feature rollback (or of
// clearing its record).
type RollbackFeatureResult struct {
	Feature string `json:"feature"`
	// Action is "rollback", or "rollback-clear" for Clear.
	Action  string `json:"action"`
	Success bool   `json:"success"`
	DryRun  bool   `json:"dry_run,omitempty"`
	// Transfers lists every transfer of the feature, in order.
	Transfers []RollbackTransfer `json:"transfers,omitzero"`
	// Record is the path of the feature's rollback record.
	Record string `json:"record,omitempty"`
	// Cleared is true when Clear deleted a record (in a dry run: would).
	Cleared bool `json:"cleared,omitempty"`
	// NoRecord is true when Clear found no record to delete.
	NoRecord bool `json:"no_record,omitempty"`
	// Refreshed is true when Now ran systemd-sysext refresh successfully.
	Refreshed bool `json:"refreshed,omitempty"`
	// RefreshError is set when the links and the record were written but the
	// refresh failed; Success is false and Error carries the same message.
	RefreshError string `json:"refresh_error,omitempty"`
	// PendingActivations lists the sysexts outside this feature that the
	// refresh activates as well (a dry run: would activate).
	PendingActivations []PendingActivation `json:"pending_activations,omitzero"`
	// RefreshSkipped says why the refresh of a rollback with Now did not run
	// (a dry run: why it would not run): a sysext the refresh would activate
	// needs a newer image version than the running one (ADR-0016). The
	// record and the links stay, so the rollback takes effect at the next
	// boot instead. It is not an error.
	RefreshSkipped    string `json:"refresh_skipped,omitempty"`
	Error             string `json:"error,omitempty"`
	NextActionMessage string `json:"next_action_message,omitempty"`
}
