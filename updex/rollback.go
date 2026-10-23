package updex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/frostyard/updex/v2/catalog"
	"github.com/frostyard/updex/v2/config"
	"github.com/frostyard/updex/v2/manifest"
	"github.com/frostyard/updex/v2/sysext"
	"github.com/frostyard/updex/v2/version"
)

// DefaultRollbackDir is the directory that holds the rollback records, unless
// RuntimePaths.RollbackDir names another one. See ADR-0016, "Rollback".
const DefaultRollbackDir = "/var/lib/updex/rollback"

// rollbackDirDefault is the value resolveRuntimePaths uses for a zero
// RuntimePaths.RollbackDir. Production code never assigns it: it exists so the
// package's own tests can point every default-constructed client at a
// temporary directory in TestMain, and no test touches /var/lib/updex.
var rollbackDirDefault = DefaultRollbackDir

// rollbackRecordFormat is the only record format this updex reads. A record
// with another format is not understood, so it fails closed.
const rollbackRecordFormat = 1

// maxRollbackRecordSize bounds the bytes read from a record file.
const maxRollbackRecordSize = 64 << 10

// rollbackRecord is the content of <RollbackDir>/<feature>, as JSON:
//
//	{"format":1,"feature":"incus","created":"2026-10-23T15:04:05Z","rejected":{"incus":"r5"}}
//
// Rejected holds, per transfer component, the version that a rollback
// rejected. features update links no version at or below it for that
// transfer; it resumes when a newer version is published, and then deletes
// the record.
type rollbackRecord struct {
	Format   int               `json:"format"`
	Feature  string            `json:"feature"`
	Created  string            `json:"created"`
	Rejected map[string]string `json:"rejected"`
}

// rollbackRecordPath returns the record path for a feature. The name is
// validated first, so a traversal-shaped value never reaches filepath.Join.
func (c *Client) rollbackRecordPath(feature string) (string, error) {
	if err := catalog.ValidateSysextName(feature); err != nil {
		return "", fmt.Errorf("invalid feature name %q for a rollback record: %w", feature, err)
	}
	return filepath.Join(c.paths.rollbackDir, feature), nil
}

// checkRollbackDir Lstats the record directory. It reports whether it
// exists, and refuses anything that is not a real directory (a symlink would
// move the record wherever it points, as root).
func (c *Client) checkRollbackDir() (bool, error) {
	info, err := os.Lstat(c.paths.rollbackDir)
	switch {
	case err == nil && info.IsDir():
		return true, nil
	case err == nil:
		return false, fmt.Errorf("rollback directory %s is not a directory (mode %s); remove it manually", c.paths.rollbackDir, info.Mode().Type())
	case os.IsNotExist(err):
		return false, nil
	default:
		return false, fmt.Errorf("check rollback directory: %w", err)
	}
}

// readRollbackRecordBytes returns the raw record, or nil when there is none.
// An entry that is not a regular file, or that cannot be read, is an error.
func (c *Client) readRollbackRecordBytes(path string) ([]byte, error) {
	exists, err := c.checkRollbackDir()
	if err != nil || !exists {
		return nil, err
	}
	switch info, err := os.Lstat(path); {
	case err == nil && !info.Mode().IsRegular():
		return nil, fmt.Errorf("rollback record %s is not a regular file (mode %s)", path, info.Mode().Type())
	case os.IsNotExist(err):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("check rollback record: %w", err)
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open rollback record: %w", err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxRollbackRecordSize+1))
	if err != nil {
		return nil, fmt.Errorf("read rollback record: %w", err)
	}
	if len(data) > maxRollbackRecordSize {
		return nil, fmt.Errorf("rollback record %s is larger than %d bytes", path, maxRollbackRecordSize)
	}
	return data, nil
}

// readRollbackRecord returns the feature's rollback record, or nil with a nil
// error when it has none. An unreadable, malformed or unsupported record is an
// error: a caller that links must then not link, because it cannot tell what
// was rejected. A feature name that cannot have a record has none.
func (c *Client) readRollbackRecord(feature string) (*rollbackRecord, error) {
	path, err := c.rollbackRecordPath(feature)
	if err != nil {
		return nil, nil
	}
	data, err := c.readRollbackRecordBytes(path)
	if err != nil || data == nil {
		return nil, err
	}
	var rec rollbackRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("rollback record %s is malformed: %w", path, err)
	}
	if rec.Format != rollbackRecordFormat {
		return nil, fmt.Errorf("rollback record %s has unsupported format %d", path, rec.Format)
	}
	if rec.Feature != feature {
		return nil, fmt.Errorf("rollback record %s names feature %q", path, rec.Feature)
	}
	if len(rec.Rejected) == 0 {
		return nil, fmt.Errorf("rollback record %s rejects no version", path)
	}
	for component, v := range rec.Rejected {
		if component == "" || v == "" {
			return nil, fmt.Errorf("rollback record %s has an empty component or version", path)
		}
	}
	return &rec, nil
}

// writeRollbackRecordBytes writes the record through a temp file plus rename
// after Lstat checks of the directory and the file (ADR-0005).
func (c *Client) writeRollbackRecordBytes(path string, data []byte) error {
	exists, err := c.checkRollbackDir()
	if err != nil {
		return err
	}
	if !exists {
		if err := os.MkdirAll(c.paths.rollbackDir, 0o755); err != nil {
			return fmt.Errorf("create rollback directory: %w", err)
		}
	}
	if _, err := managedFileExists(path); err != nil {
		return fmt.Errorf("check rollback record: %w", err)
	}
	if err := writeManagedFileWithMode(path, data, 0o644); err != nil {
		return fmt.Errorf("write rollback record: %w", err)
	}
	return nil
}

// deleteRollbackRecord removes the feature's record and reports whether one
// existed. An entry that is not a regular file is refused.
func (c *Client) deleteRollbackRecord(feature string) (bool, error) {
	path, err := c.rollbackRecordPath(feature)
	if err != nil {
		return false, nil
	}
	exists, err := c.checkRollbackDir()
	if err != nil || !exists {
		return false, err
	}
	found, err := managedFileExists(path)
	if err != nil || !found {
		return false, err
	}
	if err := os.Remove(path); err != nil {
		return false, fmt.Errorf("remove rollback record: %w", err)
	}
	return true, nil
}

// rejectedVersions returns the record's rejected version per component, or
// nil for no record.
func (r *rollbackRecord) rejectedVersions() map[string]string {
	if r == nil {
		return nil
	}
	return r.Rejected
}

// rollbackHolds is what every rollback record that applies to a set of
// transfers says (ADR-0016). A record is keyed by the feature that was rolled
// back, and a transfer can belong to several features (Features= and
// RequisiteFeatures=), so a rollback of one feature must hold the transfer for
// every other feature that shares it; otherwise updating through a sharer
// would link the rejected version again.
type rollbackHolds struct {
	// rejected maps a transfer component to the highest version any
	// applicable record rejects for it. Several features can reject
	// different versions of one component; the highest wins, so an older
	// rejection never lifts a newer one.
	rejected map[string]string

	// errs maps a transfer component to the failure of an applicable record
	// that cannot be read. A caller that links must then link nothing for
	// the feature, because it cannot tell what was rejected.
	errs map[string]error

	// records holds the applicable records that could be read, by the
	// feature that wrote them. A rollback is over, and its record may be
	// deleted, once every version it rejects is behind a link (see
	// deleteFinishedRollbackRecords).
	records map[string]*rollbackRecord
}

// rollbackFeaturesOf returns the features that name a transfer, deduplicated
// and sorted: Features= is OR logic and RequisiteFeatures= is AND logic, and
// either way the transfer belongs to that feature, with or without the
// feature being enabled.
func rollbackFeaturesOf(t *config.Transfer) []string {
	names := make(map[string]struct{}, len(t.Transfer.Features)+len(t.Transfer.RequisiteFeatures))
	for _, name := range t.Transfer.Features {
		names[name] = struct{}{}
	}
	for _, name := range t.Transfer.RequisiteFeatures {
		names[name] = struct{}{}
	}
	return slices.Sorted(maps.Keys(names))
}

// collectRollbackHolds reads the rollback record of every feature that names
// one of transfers, and combines them. Each record is read at most once per
// call. A record that cannot be read is an error for every transfer it names,
// which names the feature whose record failed.
func (c *Client) collectRollbackHolds(transfers []*config.Transfer) rollbackHolds {
	holds := rollbackHolds{
		rejected: make(map[string]string),
		errs:     make(map[string]error),
		records:  make(map[string]*rollbackRecord),
	}
	read := make(map[string]*rollbackRecord)
	readErr := make(map[string]error)
	problems := make(map[string][]error)

	for _, t := range transfers {
		for _, name := range rollbackFeaturesOf(t) {
			rec, ok := read[name]
			if !ok {
				rec, readErr[name] = c.readRollbackRecord(name)
				read[name] = rec
			}
			if err := readErr[name]; err != nil {
				problems[t.Component] = append(problems[t.Component],
					fmt.Errorf("rollback record of feature %s cannot be read: %w", name, err))
				continue
			}
			if rec == nil {
				continue
			}
			holds.records[name] = rec
			rejected := rec.Rejected[t.Component]
			if rejected == "" {
				continue
			}
			if current := holds.rejected[t.Component]; current == "" || version.Compare(rejected, current) > 0 {
				holds.rejected[t.Component] = rejected
			}
		}
	}
	for component, errs := range problems {
		holds.errs[component] = errors.Join(errs...)
	}
	return holds
}

// rollbackHoldReason is one reason an applicable rollback record still holds a
// feature: a component the record rejects has no available version newer than
// the rejected one, so linking the newest version would undo the rollback.
// The component can be one outside the feature's own transfers: a feature's
// transfers link together, so a record that still holds any of them holds the
// whole feature.
type rollbackHoldReason struct {
	// feature is the feature whose record holds.
	feature string

	// component is the record component that keeps the record in force.
	component string

	// rejected is the version the record rejects for component.
	rejected string

	// newest is the newest available version of component. It is empty when
	// the component's source lists no version.
	newest string

	// noTransfer reports that component has no transfer in the loaded
	// domain, so the operation cannot tell whether a newer version exists.
	noTransfer bool
}

// String renders the reason for a hold message:
// "grp: beta has no version newer than 2.0.0", or, when the record names a
// component the operation has no transfer for, "grp: beta has no transfer in
// the loaded definitions".
func (h *rollbackHoldReason) String() string {
	if h.noTransfer {
		return fmt.Sprintf("%s: %s has no transfer in the loaded definitions", h.feature, h.component)
	}
	return fmt.Sprintf("%s: %s has no version newer than %s", h.feature, h.component, h.rejected)
}

// rollbackHoldLookupError reports that the newest available version of a
// component an applicable rollback record names could not be read. The
// operation cannot tell whether the record still holds, so it links nothing
// for the feature (fail closed), as it does for a record it cannot read.
type rollbackHoldLookupError struct {
	// feature is the feature whose record names component.
	feature string

	// component is the record component whose versions could not be read.
	component string

	// err is the failure to read them.
	err error
}

// Error implements error.
func (e *rollbackHoldLookupError) Error() string {
	return fmt.Sprintf("rollback record of feature %s needs the newest version of %s: %v", e.feature, e.component, e.err)
}

// Unwrap returns the failure to read the versions.
func (e *rollbackHoldLookupError) Unwrap() error { return e.err }

// rollbackHoldReason reports whether an applicable rollback record still holds
// transfers, and why (ADR-0016).
//
// A record still holds when any component it rejects has no available version
// newer than the rejected one (version.Compare), because the update would then
// link at or below the version the rollback left. It covers every component
// the record names, also one outside transfers: that component's transfer is
// looked up in domain, and its versions are read through manifests, the
// per-call cache. A component with no transfer in domain holds the record too
// (fail closed): without its newest version the operation cannot tell whether
// the rollback is over, and it must never link a rejected version by default.
// A failure to read a component's versions is a *rollbackHoldLookupError: the
// caller then links nothing for the feature, rather than guess. Only one
// reason is returned, from the record and component that sort first, so the
// message is stable.
func (c *Client) rollbackHoldReason(ctx context.Context, transfers, domain []*config.Transfer, holds rollbackHolds, manifests map[string]*manifest.Manifest) (*rollbackHoldReason, error) {
	if len(holds.records) == 0 {
		return nil, nil
	}
	if manifests == nil {
		manifests = make(map[string]*manifest.Manifest)
	}
	for _, name := range slices.Sorted(maps.Keys(holds.records)) {
		rejectedVersions := holds.records[name].Rejected
		for _, component := range slices.Sorted(maps.Keys(rejectedVersions)) {
			rejected := rejectedVersions[component]
			t := transferForComponent(component, transfers)
			if t == nil {
				t = transferForComponent(component, domain)
			}
			if t == nil {
				return &rollbackHoldReason{feature: name, component: component, rejected: rejected, noTransfer: true}, nil
			}
			available, m, _, err := c.getAvailableVersions(ctx, t, manifests[t.Source.Path])
			if m != nil {
				manifests[t.Source.Path] = m
			}
			if err != nil {
				return nil, &rollbackHoldLookupError{feature: name, component: component, err: err}
			}
			newest := ""
			if len(available) > 0 {
				version.Sort(available)
				newest = available[0]
			}
			if newest == "" || version.Compare(newest, rejected) <= 0 {
				return &rollbackHoldReason{feature: name, component: component, rejected: rejected, newest: newest}, nil
			}
		}
	}
	return nil, nil
}

// deleteFinishedRollbackRecords deletes every applicable record that a link
// has overtaken: for each component the record rejects, the version linked now
// is newer than the rejected one (ADR-0016). A component of the record that is
// not one of transfers is looked up in domain, the transfers the operation
// loaded. A component whose transfer cannot be found, whose link cannot be
// read, or whose link is not newer keeps its record: the rollback may still be
// in force for it.
func (c *Client) deleteFinishedRollbackRecords(transfers, domain []*config.Transfer, holds rollbackHolds) {
	for _, name := range slices.Sorted(maps.Keys(holds.records)) {
		if !c.rollbackOvertaken(holds.records[name], transfers, domain) {
			continue
		}
		if _, err := c.deleteRollbackRecord(name); err != nil {
			c.warn("failed to delete the rollback record of %s: %v", name, err)
			continue
		}
		c.msg("Linked a version newer than the one rolled back from; deleted the rollback record of %s", name)
	}
}

// rollbackOvertaken reports whether every version rec rejects is behind the
// link in place now.
func (c *Client) rollbackOvertaken(rec *rollbackRecord, transfers, domain []*config.Transfer) bool {
	for component, rejected := range rec.Rejected {
		t := transferForComponent(component, transfers)
		if t == nil {
			t = transferForComponent(component, domain)
		}
		if t == nil {
			return false
		}
		linked, err := sysext.LinkedVersionAt(t, c.paths.sysextLinkDir)
		if err != nil || linked == "" {
			return false
		}
		if version.Compare(linked, rejected) <= 0 {
			return false
		}
	}
	return true
}

// transferForComponent returns the transfer of the named component, or nil
// when the list has none.
func transferForComponent(component string, transfers []*config.Transfer) *config.Transfer {
	for _, t := range transfers {
		if t.Component == component {
			return t
		}
	}
	return nil
}

// versionRunner returns the client's runner as a sysext.VersionSysextRunner,
// or an error wrapping ErrLegacySysextRunner when it cannot link a given
// version.
func (c *Client) versionRunner() (sysext.VersionSysextRunner, error) {
	runner, ok := c.runner.(sysext.VersionSysextRunner)
	if !ok {
		return nil, fmt.Errorf("%w: %T must add LinkVersionAt(*config.Transfer, string, string) error", ErrLegacySysextRunner, c.runner)
	}
	return runner, nil
}

// rollbackPlan is the decision for one transfer.
type rollbackPlan struct {
	transfer *config.Transfer
	entry    RollbackTransfer
}

// RollbackFeature links every transfer of a feature to an older staged
// version and records the version it rolled back from, so that
// `features update` does not link it again (ADR-0016, "Rollback").
//
// The record names only the rolled-back feature, but its hold covers every
// feature that shares one of its transfers (Features= or
// RequisiteFeatures=), enabled or not: the records of all of them are read by
// UpdateFeatures, EnableFeature, Features and CheckFeatures, so a second
// feature cannot link the rejected version again.
//
// By default each transfer goes to its own newest staged version older than
// the version its link names. With opts.To every transfer goes to that
// version. All transfers must have their target on disk and a link; when any
// does not, RollbackFeature changes nothing and returns an error. The record,
// /var/lib/updex/rollback/<feature> (RuntimePaths.RollbackDir), keeps one
// rejected version per transfer: the version rolled back from, or a newer one
// that an earlier rollback rejected. The links move all or none: when one
// fails, the others are restored and the previous record is put back.
//
// The rollback takes effect at the next boot, or with opts.Now through a
// systemd-sysext refresh. opts.Clear deletes the record and changes no link.
// Except in a dry run, RollbackFeature holds the operation lock and returns an
// error wrapping ErrBusy when another operation holds it.
func (c *Client) RollbackFeature(ctx context.Context, name string, opts RollbackFeatureOptions) (*RollbackFeatureResult, error) {
	result := &RollbackFeatureResult{Feature: name, Action: "rollback", DryRun: opts.DryRun}
	if opts.Clear {
		result.Action = "rollback-clear"
	}
	fail := func(err error) (*RollbackFeatureResult, error) {
		result.Error = err.Error()
		c.warn("%s", result.Error)
		return result, err
	}

	recordPath, err := c.rollbackRecordPath(name)
	if err != nil {
		return fail(err)
	}
	result.Record = recordPath
	if opts.Clear && (opts.To != "" || opts.Now) {
		return fail(errors.New("--clear cannot be combined with --to or --now"))
	}

	release, err := c.lockMutation(opts.DryRun)
	if err != nil {
		return fail(err)
	}
	defer release()

	if opts.Clear {
		return c.clearRollback(name, recordPath, opts, result, fail)
	}

	// A rollback ends in links to a given version: refuse a runner that
	// cannot do that before changing anything.
	runner, runnerErr := c.versionRunner()
	if runnerErr != nil && !opts.DryRun {
		return fail(runnerErr)
	}

	features, transfers, err := c.loadDomain(opts.Component)
	if err != nil {
		return fail(err)
	}
	if _, err := lookupFeature(features, name, "rolled back"); err != nil {
		return fail(err)
	}
	featureTransfers := config.GetTransfersForFeature(transfers, name)
	if len(featureTransfers) == 0 {
		return fail(fmt.Errorf("feature '%s' has no transfers to roll back", name))
	}

	// What an earlier rollback rejected stays rejected. A record that cannot
	// be read is not overwritten: it says what was rejected, and guessing
	// could let a rejected version through.
	existing, err := c.readRollbackRecord(name)
	if err != nil {
		return fail(fmt.Errorf("%w; run 'updex features rollback --clear %s' to delete it first", err, name))
	}

	plans, err := c.planRollback(featureTransfers, existing.rejectedVersions(), opts.To)
	if err != nil {
		return fail(err)
	}
	for _, p := range plans {
		result.Transfers = append(result.Transfers, p.entry)
	}

	if opts.DryRun {
		result.Success = true
		result.NextActionMessage = fmt.Sprintf("Dry run complete. Would roll back feature '%s'", name)
		if opts.Now && !opts.NoRefresh {
			result.PendingActivations = c.pendingActivations(transfers, featureTransfers)
			c.warnPendingActivations(result.PendingActivations, true)
		}
		return result, nil
	}

	// Write the record first. A crash between the record and the links then
	// leaves a hold on the current link, which is harmless, and never a
	// rolled-back link that the next update would undo.
	previous, err := c.readRollbackRecordBytes(recordPath)
	if err != nil {
		return fail(err)
	}
	rec := rollbackRecord{
		Format:   rollbackRecordFormat,
		Feature:  name,
		Created:  time.Now().UTC().Format(time.RFC3339),
		Rejected: make(map[string]string, len(plans)),
	}
	for _, p := range plans {
		rec.Rejected[p.transfer.Component] = p.entry.RejectedVersion
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return fail(fmt.Errorf("encode rollback record: %w", err))
	}
	if err := c.writeRollbackRecordBytes(recordPath, append(data, '\n')); err != nil {
		return fail(err)
	}
	restoreRecord := func() {
		var rerr error
		if previous != nil {
			rerr = c.writeRollbackRecordBytes(recordPath, previous)
		} else {
			_, rerr = c.deleteRollbackRecord(name)
		}
		if rerr != nil {
			c.warn("failed to restore the rollback record of %s: %v", name, rerr)
		}
	}

	targets := make([]*config.Transfer, len(plans))
	for i, p := range plans {
		targets[i] = p.transfer
	}
	failed, _, err := c.linkAllOrNone(targets, func(i int, t *config.Transfer) error {
		if err := runner.LinkVersionAt(t, c.paths.sysextLinkDir, plans[i].entry.ToVersion); err != nil {
			return fmt.Errorf("failed to link %s to %s: %w", t.Component, plans[i].entry.ToVersion, err)
		}
		return nil
	})
	if err != nil {
		restoreRecord()
		if failed >= 0 && len(plans) > 1 {
			err = fmt.Errorf("%w (no link was changed)", err)
		}
		return fail(err)
	}
	c.msg("Rolled back %s", name)

	if opts.Now && !opts.NoRefresh {
		result.PendingActivations = c.pendingActivations(transfers, featureTransfers)
		c.warnPendingActivations(result.PendingActivations, false)
		c.msg("Refreshing sysext")
		if err := c.runner.Refresh(); err != nil {
			err = fmt.Errorf("sysext refresh failed: %w", err)
			result.RefreshError = err.Error()
			result.NextActionMessage = fmt.Sprintf("Feature '%s' rolled back and recorded, but systemd-sysext refresh failed; run 'systemd-sysext refresh' (or reboot) to activate it", name)
			return fail(err)
		}
		result.Refreshed = true
	}

	result.Success = true
	holdText := fmt.Sprintf("'updex features update' will not link %s or older; it resumes when a newer version is published, or after 'updex features rollback --clear %s'.",
		rejectedSummary(result.Transfers), name)
	if result.Refreshed {
		result.NextActionMessage = fmt.Sprintf("Feature '%s' rolled back and refreshed. %s", name, holdText)
	} else {
		result.NextActionMessage = fmt.Sprintf("Feature '%s' rolled back. It takes effect at the next boot (or run 'systemd-sysext refresh', or use --now). %s", name, holdText)
	}
	return result, nil
}

// rejectedSummary renders the rejected versions of a rollback, one per
// transfer, for a message.
func rejectedSummary(transfers []RollbackTransfer) string {
	if len(transfers) == 1 {
		return transfers[0].RejectedVersion
	}
	parts := make([]string, len(transfers))
	for i, t := range transfers {
		parts[i] = t.Component + " " + t.RejectedVersion
	}
	return strings.Join(parts, ", ")
}

// clearRollback implements RollbackFeatureOptions.Clear: it deletes the
// record and changes no link. A missing record is reported, not an error; an
// unreadable regular file is deleted like any other, which is what --clear is
// for.
func (c *Client) clearRollback(name, recordPath string, opts RollbackFeatureOptions, result *RollbackFeatureResult, fail func(error) (*RollbackFeatureResult, error)) (*RollbackFeatureResult, error) {
	found, err := c.rollbackRecordExists(recordPath)
	if err != nil {
		return fail(err)
	}
	switch {
	case !found:
		result.NoRecord = true
		result.NextActionMessage = fmt.Sprintf("Feature '%s' has no rollback record; nothing to clear.", name)
	case opts.DryRun:
		result.Cleared = true
		result.NextActionMessage = fmt.Sprintf("Dry run complete. Would delete the rollback record of feature '%s'.", name)
	default:
		if _, err := c.deleteRollbackRecord(name); err != nil {
			return fail(err)
		}
		result.Cleared = true
		result.NextActionMessage = fmt.Sprintf("Rollback record of feature '%s' deleted. 'updex features update' links the newest version again; no link was changed.", name)
	}
	result.Success = true
	return result, nil
}

// rollbackRecordExists reports whether a regular record file exists. Anything
// else at the path, or at the directory, is an error.
func (c *Client) rollbackRecordExists(path string) (bool, error) {
	exists, err := c.checkRollbackDir()
	if err != nil || !exists {
		return false, err
	}
	return managedFileExists(path)
}

// planRollback decides, for every transfer of the feature, the version to link
// and the version to reject. It changes nothing. Every problem is reported, so
// that one error lists every transfer that blocks the rollback.
func (c *Client) planRollback(transfers []*config.Transfer, existing map[string]string, to string) ([]rollbackPlan, error) {
	var plans []rollbackPlan
	var problems []error
	for _, t := range transfers {
		linked, err := sysext.LinkedVersionAt(t, c.paths.sysextLinkDir)
		if err != nil {
			problems = append(problems, fmt.Errorf("failed to inspect sysext link of %s: %w", t.Component, err))
			continue
		}
		if linked == "" {
			problems = append(problems, fmt.Errorf("%s has no usable sysext link; there is nothing to roll back from", t.Component))
			continue
		}
		staged, err := sysext.StagedVersionsAt(t, c.paths.sysextLinkDir)
		if err != nil {
			problems = append(problems, fmt.Errorf("failed to list staged versions of %s: %w", t.Component, err))
			continue
		}

		// An earlier rollback's newer rejected version stays rejected.
		rejected := linked
		if prev := existing[t.Component]; prev != "" && version.Compare(prev, rejected) > 0 {
			rejected = prev
		}

		target := to
		if target == "" {
			for _, v := range staged {
				if version.Compare(v, linked) < 0 {
					target = v
					break
				}
			}
			if target == "" {
				problems = append(problems, fmt.Errorf("no staged version of %s is older than the linked version %s", t.Component, linked))
				continue
			}
		} else {
			found := false
			for _, v := range staged {
				if v == target {
					found = true
					break
				}
			}
			switch {
			case !found:
				problems = append(problems, fmt.Errorf("version %s of %s is not on disk", target, t.Component))
				continue
			case version.Compare(target, linked) == 0:
				problems = append(problems, fmt.Errorf("version %s of %s is already linked", target, t.Component))
				continue
			case version.Compare(target, rejected) >= 0:
				problems = append(problems, fmt.Errorf("version %s of %s is not older than the version rolled back from (%s)", target, t.Component, rejected))
				continue
			}
		}

		plans = append(plans, rollbackPlan{
			transfer: t,
			entry: RollbackTransfer{
				Component:       t.Component,
				FromVersion:     linked,
				ToVersion:       target,
				RejectedVersion: rejected,
			},
		})
	}
	if len(problems) > 0 {
		return nil, errors.Join(problems...)
	}
	return plans, nil
}
