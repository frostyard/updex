package updex

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/frostyard/std/reporter"
	"github.com/frostyard/updex/v2/config"
	"github.com/frostyard/updex/v2/internal/fault"
)

// This file writes and reads the last-run record (ADR-0020,
// docs/specs/last-run-record.md): a machine-readable summary of the last
// UpdateFeatures run that was not a dry run. snosi's health collector reads
// only this file. It carries classes and enums, never error text.

// DefaultLastRunPath is the last-run record, unless RuntimePaths.LastRunPath
// names another file. See ADR-0020.
const DefaultLastRunPath = "/var/lib/updex/last-run.json"

// lastRunPathDefault is the value resolveRuntimePaths uses for a zero
// RuntimePaths.LastRunPath. Production code never assigns it: it exists so the
// package's own tests can point every default-constructed client at a
// temporary file in TestMain, and no test touches /var/lib/updex.
var lastRunPathDefault = DefaultLastRunPath

// lastRunFormat is the only record format this updex writes and reads.
const lastRunFormat = 1

// maxLastRunRecordSize bounds the bytes read from a previous record.
const maxLastRunRecordSize = 1 << 20

// lastRunBeforeWrite is a test seam: when it is not nil, storeLastRun calls
// it after every check and just before it renames the new record into place.
// Production never sets it.
var lastRunBeforeWrite func()

// lastRunLockWait and lastRunLockRetry bound the wait for the record
// directory's flock: the write is skipped with a warning when another writer
// holds it for longer. They are variables so a test can shorten the wait.
var (
	lastRunLockWait  = 2 * time.Second
	lastRunLockRetry = 10 * time.Millisecond
)

// Run outcomes.
const (
	runCompleted          = "completed"
	runFailed             = "failed"
	runSkippedBusy        = "skipped_busy"
	runSkippedUnreachable = "skipped_unreachable"
)

// Refresh values.
const (
	refreshDone         = "done"
	refreshFailed       = "failed"
	refreshSkippedGate  = "skipped"
	refreshNotRequested = "not_requested"
	refreshNotRun       = "not_run"
)

// Feature and transfer outcomes. outcomeNotLinked is for a transfer only.
const (
	outcomeUpdated            = "updated"
	outcomeCurrent            = "current"
	outcomeHeldByRollback     = "held_by_rollback"
	outcomeNeedsImage         = "needs_image"
	outcomeFailed             = "failed"
	outcomeSkippedUnreachable = "skipped_unreachable"
	outcomeSkippedBusy        = "skipped_busy"
	outcomeNotLinked          = "not_linked"
)

// LastRunRecord is the format 1 last-run record defined in
// docs/specs/last-run-record.md.
type LastRunRecord struct {
	Format     int              `json:"format"`
	Started    string           `json:"started"`
	Finished   string           `json:"finished"`
	Outcome    string           `json:"outcome"`
	ErrorClass string           `json:"error_class,omitempty"`
	Refresh    string           `json:"refresh"`
	Selected   bool             `json:"selected"`
	Features   []LastRunFeature `json:"features"`
}

// LastRunFeature records one feature, as defined in docs/specs/last-run-record.md.
type LastRunFeature struct {
	Feature    string            `json:"feature"`
	Origin     string            `json:"origin"`
	Outcome    string            `json:"outcome"`
	ErrorClass string            `json:"error_class,omitempty"`
	Version    string            `json:"version,omitempty"`
	At         string            `json:"at"`
	Transfers  []LastRunTransfer `json:"transfers"`
}

// LastRunTransfer records one active transfer of a feature, as defined in
// docs/specs/last-run-record.md.
type LastRunTransfer struct {
	Component  string `json:"component"`
	Outcome    string `json:"outcome"`
	ErrorClass string `json:"error_class,omitempty"`
	Version    string `json:"version,omitempty"`
}

// lastRun collects what one UpdateFeatures run did, for the record. A dry run
// has none.
type lastRun struct {
	started  time.Time
	selected bool

	// skip reports that the run must write nothing: selectFeatures refused
	// it, so nothing ran.
	skip bool

	// written reports that writeLastRun ran for this run, so the record is
	// written at most once.
	written bool

	// busy reports that another operation held the lock.
	busy bool

	// runClass is the class of a failure outside every feature: config for a
	// domain that could not be loaded, other for a lock error that is not
	// ErrBusy.
	runClass fault.Class

	refresh  string
	features []LastRunFeature

	// carry holds, for a run limited to named features, every enabled and
	// unmasked feature of the loaded domain, in domain order: their entries
	// of the previous record are kept when this run did not act on them. It
	// is nil for an unnamed run, which carries nothing.
	carry []string
}

// newLastRun starts the record of a run.
func newLastRun(started time.Time, opts UpdateFeaturesOptions) *lastRun {
	return &lastRun{
		started:  started,
		selected: len(opts.Features) > 0,
		refresh:  refreshNotRun,
	}
}

// setDomain records the features a named run may carry from the previous
// record.
func (r *lastRun) setDomain(features []*config.Feature) {
	if r == nil || !r.selected {
		return
	}
	r.carry = []string{}
	for _, f := range features {
		if f.Enabled && !f.Masked {
			r.carry = append(r.carry, f.Name)
		}
	}
}

// addFeature records one feature this run acted on.
func (r *lastRun) addFeature(name, origin string, installs []transferInstall) {
	if r == nil {
		return
	}
	transfers := make([]LastRunTransfer, 0, len(installs))
	for _, in := range installs {
		transfers = append(transfers, lastRunTransferFor(in))
	}
	r.features = append(r.features, lastRunFeatureFor(name, origin, transfers))
}

// lastRunTransferFor maps the install result of a transfer to its record.
func lastRunTransferFor(in transferInstall) LastRunTransfer {
	t := LastRunTransfer{Component: in.transfer.Component}
	switch {
	case in.err != nil && in.notLinked:
		t.Outcome = outcomeNotLinked
		t.Version = in.outcome.Version
	case in.err != nil:
		t.Outcome = outcomeFailed
		t.ErrorClass = string(classifyError(in.err))
		t.Version = in.attempted
	case in.needsImage != "":
		t.Outcome = outcomeNeedsImage
		t.Version = in.outcome.Version
	case in.imageBlocked != "":
		t.Outcome = outcomeNotLinked
		t.Version = in.outcome.Version
	case in.held:
		t.Outcome = outcomeHeldByRollback
		t.Version = in.outcome.Version
	case in.outcome.Downloaded || in.outcome.Relinked:
		t.Outcome = outcomeUpdated
		t.Version = in.outcome.Version
	default:
		t.Outcome = outcomeCurrent
		t.Version = in.outcome.Version
	}
	return t
}

// featureOutcomeRank orders the transfer outcomes that decide a feature's
// outcome: failed > needs_image > held_by_rollback > updated > current.
// not_linked never decides it: its feature has a failed or a needs_image
// transfer.
var featureOutcomeRank = map[string]int{
	outcomeCurrent:        1,
	outcomeUpdated:        2,
	outcomeHeldByRollback: 3,
	outcomeNeedsImage:     4,
	outcomeFailed:         5,
}

// lastRunFeatureFor builds a feature's record from its transfers. A feature
// with no active transfer is current. The class is the class of its first
// failed transfer (not a not_linked one); the version is the single distinct
// non-empty transfer version, else empty.
func lastRunFeatureFor(name, origin string, transfers []LastRunTransfer) LastRunFeature {
	f := LastRunFeature{Feature: name, Origin: origin, Outcome: outcomeCurrent, Transfers: transfers}
	versions := map[string]bool{}
	for _, t := range transfers {
		if featureOutcomeRank[t.Outcome] > featureOutcomeRank[f.Outcome] {
			f.Outcome = t.Outcome
		}
		if t.Outcome == outcomeFailed && f.ErrorClass == "" {
			f.ErrorClass = t.ErrorClass
		}
		if t.Version != "" {
			versions[t.Version] = true
		}
	}
	if len(versions) == 1 {
		for v := range versions {
			f.Version = v
		}
	}
	return f
}

// classifyError gives the class of a transfer error. The order matters: a
// cancelled run is other, a full disk is disk_space whatever wraps it, a
// network failure is network (also a TLS or x509 verification error, which a
// captive portal gives), then the outermost fault mark decides, else other.
func classifyError(err error) fault.Class {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.Canceled):
		return fault.Other
	case errors.Is(err, syscall.ENOSPC), errors.Is(err, syscall.EDQUOT):
		return fault.DiskSpace
	case isNetworkFailure(err):
		return fault.Network
	}
	if class, ok := fault.ClassOf(err); ok {
		return class
	}
	return fault.Other
}

// isNetworkFailure reports a failure of the connection to a source: every
// unreachable cause (isUnreachable), a reset or aborted connection, and a TLS
// certificate that does not verify.
func isNetworkFailure(err error) bool {
	if isUnreachable(err) {
		return true
	}
	if errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNABORTED) {
		return true
	}
	var certErr *tls.CertificateVerificationError
	var authorityErr x509.UnknownAuthorityError
	var hostnameErr x509.HostnameError
	var invalidErr x509.CertificateInvalidError
	return errors.As(err, &certErr) || errors.As(err, &authorityErr) ||
		errors.As(err, &hostnameErr) || errors.As(err, &invalidErr)
}

// recordTime formats t for the record: RFC 3339, UTC, seconds.
func recordTime(t time.Time) string {
	return t.UTC().Truncate(time.Second).Format(time.RFC3339)
}

// record builds the record of the run, which ended at finished with runErr.
func (r *lastRun) record(finished time.Time, runErr error) LastRunRecord {
	rec := LastRunRecord{
		Format:     lastRunFormat,
		Started:    recordTime(r.started),
		Finished:   recordTime(finished),
		ErrorClass: string(r.runClass),
		Refresh:    r.refresh,
		Selected:   r.selected,
		Features:   make([]LastRunFeature, 0, len(r.features)),
	}
	unreachable := errors.Is(runErr, ErrSourcesUnreachable)
	switch {
	case r.busy:
		rec.Outcome = runSkippedBusy
	case runErr == nil:
		rec.Outcome = runCompleted
	case unreachable:
		rec.Outcome = runSkippedUnreachable
	default:
		rec.Outcome = runFailed
	}
	at := recordTime(finished)
	for _, f := range r.features {
		f.At = at
		if unreachable {
			f = skipUnreachable(f)
		}
		rec.Features = append(rec.Features, f)
	}
	return rec
}

// skipUnreachable turns the failed feature and transfers of a run that no
// source answered into skipped_unreachable, class network.
func skipUnreachable(f LastRunFeature) LastRunFeature {
	transfers := make([]LastRunTransfer, len(f.Transfers))
	for i, t := range f.Transfers {
		if t.Outcome == outcomeFailed {
			t.Outcome = outcomeSkippedUnreachable
			t.ErrorClass = string(fault.Network)
		}
		transfers[i] = t
	}
	f.Transfers = transfers
	if f.Outcome == outcomeFailed {
		f.Outcome = outcomeSkippedUnreachable
		f.ErrorClass = string(fault.Network)
	}
	return f
}

// busyFeatures records the features a busy run would have acted on: every
// enabled, unmasked, selected feature with an associated transfer, each
// skipped_busy. It loads the domain read-only and without warnings (the busy
// run's output is unchanged); when the domain cannot be loaded it lists no
// feature. Names that selectFeatures refuses make the busy run write nothing,
// as the same names do for a run that gets the lock. A busy run that names
// features carries the previous entries of the other enabled features, as a
// named run does.
func (c *Client) busyFeatures(r *lastRun, opts UpdateFeaturesOptions) {
	quiet := *c
	quiet.reporter = reporter.NoopReporter{}
	features, transfers, err := quiet.loadDomain(opts.Component)
	if err != nil {
		return
	}
	selected, err := selectFeatures(features, opts.Features)
	if err != nil {
		r.skip = true
		return
	}
	r.setDomain(features)
	imageName := config.ImageNameFrom(c.paths.osReleasePaths)
	for _, f := range features {
		if !f.Enabled || f.Masked || (selected != nil && !selected[f.Name]) {
			continue
		}
		split := splitFeatureTransfers(features, transfers, f.Name, true)
		if split.associated() == 0 {
			continue
		}
		origin, _ := featureOrigin(f.FilePath, imageName, c.paths.definitionRoots, c.config.Definitions != "")
		entry := LastRunFeature{Feature: f.Name, Origin: origin, Outcome: outcomeSkippedBusy, Transfers: make([]LastRunTransfer, 0, len(split.active))}
		for _, t := range split.active {
			entry.Transfers = append(entry.Transfers, LastRunTransfer{Component: t.Component, Outcome: outcomeSkippedBusy})
		}
		r.features = append(r.features, entry)
	}
}

// writeLastRun writes the record of the run, once. A failure is a warning:
// the record never fails the update. A run that holds the operation lock
// calls it before it releases the lock, so a later run that waited for the
// lock cannot write first and then lose its newer record to this one.
func (c *Client) writeLastRun(r *lastRun, finished time.Time, runErr error) {
	if r == nil || r.written {
		return
	}
	r.written = true
	if r.skip {
		return
	}
	if err := c.storeLastRun(r, r.record(finished, runErr)); err != nil {
		c.warn("last-run record not written: %v", err)
	}
}

// storeLastRun writes rec to the record path under the flock of its
// directory: it reads the previous record, keeps a newer one for a busy run,
// carries the previous entries a named run did not act on, and renames the
// new record into place (ADR-0005: Lstat checks, temp file plus rename).
func (c *Client) storeLastRun(r *lastRun, rec LastRunRecord) error {
	path := c.paths.lastRunPath
	dir := filepath.Dir(path)
	switch info, err := os.Lstat(dir); {
	case err == nil && info.IsDir():
	case err == nil:
		return fmt.Errorf("last-run directory %s is not a directory (mode %s)", dir, info.Mode().Type())
	case os.IsNotExist(err):
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create last-run directory: %w", err)
		}
	default:
		return fmt.Errorf("check last-run directory: %w", err)
	}

	unlock, err := lockLastRunDir(dir)
	if err != nil {
		return err
	}
	defer unlock()

	if _, err := managedFileExists(path); err != nil {
		return fmt.Errorf("check last-run record: %w", err)
	}
	prev := readLastRunRecord(path)

	if r.busy && prev != nil {
		// The lock holder writes its own record when it ends. Keep a record
		// that ended at or after this run started: it is newer.
		finished, err := time.Parse(time.RFC3339, prev.Finished)
		if err == nil && !finished.Before(r.started.Truncate(time.Second)) {
			c.debug("keeping the newer last-run record of %s", prev.Finished)
			return nil
		}
	}
	if r.carry != nil && prev != nil {
		rec.Features = carryFeatures(rec.Features, prev.Features, r.carry)
	}

	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("encode last-run record: %w", err)
	}
	data = append(data, '\n')
	if lastRunBeforeWrite != nil {
		lastRunBeforeWrite()
	}
	if err := writeManagedFileWithMode(path, data, 0o644); err != nil {
		return fmt.Errorf("write last-run record: %w", err)
	}
	return nil
}

// carryFeatures merges the entries of a named run with the previous record:
// a feature in carry (enabled and unmasked now) that this run did not act on
// keeps its previous entry, with its own at. The result is in carry order,
// with any entry of this run that is not in carry at the end.
func carryFeatures(current, previous []LastRunFeature, carry []string) []LastRunFeature {
	byName := make(map[string]LastRunFeature, len(current))
	for _, f := range current {
		byName[f.Feature] = f
	}
	prevByName := make(map[string]LastRunFeature, len(previous))
	for _, f := range previous {
		if _, seen := prevByName[f.Feature]; !seen && f.Feature != "" {
			prevByName[f.Feature] = f
		}
	}
	out := make([]LastRunFeature, 0, len(carry))
	used := make(map[string]bool, len(current))
	for _, name := range carry {
		if f, ok := byName[name]; ok {
			out = append(out, f)
			used[name] = true
			continue
		}
		if f, ok := prevByName[name]; ok {
			if f.Transfers == nil {
				f.Transfers = []LastRunTransfer{}
			}
			out = append(out, f)
		}
	}
	for _, f := range current {
		if !used[f.Feature] {
			out = append(out, f)
		}
	}
	return out
}

// readLastRunRecord returns the previous record, or nil when there is none
// or it cannot be used: unreadable, larger than the bound, malformed, or of
// another format. The open does not follow a symlink.
func readLastRunRecord(path string) *LastRunRecord {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(f, maxLastRunRecordSize+1))
	if err != nil || len(data) > maxLastRunRecordSize {
		return nil
	}
	var rec LastRunRecord
	if err := json.Unmarshal(data, &rec); err != nil || rec.Format != lastRunFormat {
		return nil
	}
	return &rec
}

// readLastRunStatus reads a record for status without a lock or a write.
// Unknown formats mean no record; unknown enum values become other.
// The writer's readLastRunRecord deliberately retains its permissive behavior.
func readLastRunStatus(path string) (*LastRunRecord, error) {
	before, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("check last-run record: %w", err)
	}
	if !before.Mode().IsRegular() {
		return nil, fmt.Errorf("check last-run record: not a regular file")
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open last-run record: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat last-run record: %w", err)
	}
	if !info.Mode().IsRegular() || !os.SameFile(before, info) {
		return nil, fmt.Errorf("check last-run record: file changed or is not regular")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxLastRunRecordSize+1))
	if err != nil {
		return nil, fmt.Errorf("read last-run record: %w", err)
	}
	if len(data) > maxLastRunRecordSize {
		return nil, fmt.Errorf("read last-run record: larger than 1 MiB")
	}
	var header struct {
		Format int `json:"format"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return nil, fmt.Errorf("malformed last-run record: invalid JSON")
	}
	if header.Format != lastRunFormat {
		return nil, nil
	}
	var rec LastRunRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("malformed last-run record: invalid field type")
	}
	for _, value := range []string{rec.Started, rec.Finished} {
		if _, err := time.Parse(time.RFC3339, value); err != nil {
			return nil, fmt.Errorf("malformed last-run record: started and finished must be RFC 3339 timestamps")
		}
	}
	if rec.Outcome == "" || rec.Refresh == "" || rec.Features == nil {
		return nil, fmt.Errorf("malformed last-run record: outcome, refresh and features are required")
	}
	rec.Outcome = knownLastRunEnum(rec.Outcome, runCompleted, runFailed, runSkippedBusy, runSkippedUnreachable)
	rec.Refresh = knownLastRunEnum(rec.Refresh, refreshDone, refreshFailed, refreshSkippedGate, refreshNotRequested, refreshNotRun)
	rec.ErrorClass = normalizeLastRunClass(rec.ErrorClass)
	for i := range rec.Features {
		feature := &rec.Features[i]
		if feature.Feature == "" || feature.Outcome == "" || feature.Transfers == nil {
			return nil, fmt.Errorf("malformed last-run record: feature name, outcome and transfers are required")
		}
		feature.Outcome = normalizeLastRunOutcome(feature.Outcome, false)
		feature.ErrorClass = normalizeLastRunClass(feature.ErrorClass)
		for j := range feature.Transfers {
			transfer := &feature.Transfers[j]
			transfer.Outcome = normalizeLastRunOutcome(transfer.Outcome, true)
			transfer.ErrorClass = normalizeLastRunClass(transfer.ErrorClass)
		}
	}
	return &rec, nil
}

func knownLastRunEnum(value string, known ...string) string {
	for _, candidate := range known {
		if value == candidate {
			return value
		}
	}
	return string(fault.Other)
}

func normalizeLastRunClass(value string) string {
	// An absent optional class stays absent, rather than becoming a failure.
	return knownLastRunEnum(value, "", string(fault.Network), string(fault.HTTPStatus),
		string(fault.Signature), string(fault.Checksum), string(fault.DiskSpace),
		string(fault.Link), string(fault.Config), string(fault.Other))
}

func normalizeLastRunOutcome(value string, transfer bool) string {
	if transfer && value == outcomeNotLinked {
		return value
	}
	return knownLastRunEnum(value, outcomeUpdated, outcomeCurrent, outcomeHeldByRollback,
		outcomeNeedsImage, outcomeFailed, outcomeSkippedUnreachable, outcomeSkippedBusy)
}

// lockLastRunDir takes an exclusive flock on the record directory, so two
// writers never interleave the read of the previous record and the rename of
// the new one. It does not block: it retries every lastRunLockRetry for up to
// lastRunLockWait, then gives up with an error.
func lockLastRunDir(dir string) (unlock func(), err error) {
	f, err := os.OpenFile(dir, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open last-run directory: %w", err)
	}
	deadline := time.Now().Add(lastRunLockWait)
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() { _ = f.Close() }, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			_ = f.Close()
			return nil, fmt.Errorf("lock last-run directory: %w", err)
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, fmt.Errorf("last-run directory %s is locked by another writer", dir)
		}
		time.Sleep(lastRunLockRetry)
	}
}
