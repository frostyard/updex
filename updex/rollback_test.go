package updex

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/frostyard/updex/v2/config"
	"github.com/frostyard/updex/v2/sysext"
)

// recordingRunner links through the real runner and counts refreshes.
type recordingRunner struct {
	*sysext.DefaultRunner
	refreshes  int
	refreshErr error
}

func (r *recordingRunner) Refresh() error {
	r.refreshes++
	return r.refreshErr
}

// failingVersionRunner fails LinkVersionAt for one component.
type failingVersionRunner struct {
	*recordingRunner
	failComponent string
}

func (r failingVersionRunner) LinkVersionAt(t *config.Transfer, dir, version string) error {
	if t.Component == r.failComponent {
		return errors.New("injected link failure")
	}
	return r.recordingRunner.LinkVersionAt(t, dir, version)
}

// failingUpdateLinkRunner fails LinkToSysextAt (the update link step) for one
// component. LinkVersionAt, which a rollback uses, still works.
type failingUpdateLinkRunner struct {
	*recordingRunner
	failComponent string
}

func (r failingUpdateLinkRunner) LinkToSysextAt(t *config.Transfer, dir string) error {
	if t.Component == r.failComponent {
		return errors.New("injected link failure")
	}
	return r.recordingRunner.LinkToSysextAt(t, dir)
}

// rollbackFixture is a featureLinkFixture with a recording runner and helpers
// to set the staged and linked versions of each component.
type rollbackFixture struct {
	*featureLinkFixture
	runner *recordingRunner
}

func newRollbackFixture(t *testing.T, components ...string) *rollbackFixture {
	t.Helper()
	return newRollbackFixtureWithRunner(t, nil, components...)
}

// newRollbackFixtureWithRunner is newRollbackFixture with the recording
// runner wrapped by wrap, so a test can inject a failure into one path (a link
// or a version link) while the other still works.
func newRollbackFixtureWithRunner(t *testing.T, wrap func(*recordingRunner) sysext.SysextRunner, components ...string) *rollbackFixture {
	t.Helper()
	recording := &recordingRunner{DefaultRunner: &sysext.DefaultRunner{}}
	var runner sysext.SysextRunner = recording
	if wrap != nil {
		runner = wrap(recording)
	}
	return &rollbackFixture{featureLinkFixture: newFeatureLinkFixtureWithRunner(t, runner, components...), runner: recording}
}

// setup makes exactly the given versions staged for component, and links the
// one named linked ("" removes the link).
func (f *rollbackFixture) setup(component, linked string, staged ...string) {
	f.t.Helper()
	old, _ := filepath.Glob(filepath.Join(f.targetDir, component+"_*.raw"))
	for _, p := range old {
		if err := os.Remove(p); err != nil {
			f.t.Fatal(err)
		}
	}
	for _, v := range staged {
		if err := os.WriteFile(filepath.Join(f.targetDir, component+"_"+v+".raw"), []byte(component+" image "+v), 0o644); err != nil {
			f.t.Fatal(err)
		}
	}
	link := filepath.Join(f.linkDir, component+".raw")
	_ = os.Remove(link)
	if linked != "" {
		if err := os.Symlink(filepath.Join(f.targetDir, component+"_"+linked+".raw"), link); err != nil {
			f.t.Fatal(err)
		}
	}
}

func (f *rollbackFixture) stagedVersions(component string) []string {
	f.t.Helper()
	var got []string
	for _, v := range []string{"0.5.0", "1.0.0", "2.0.0", "3.0.0", "4.0.0", "5.0.0", "6.0.0"} {
		if f.staged(component, v) {
			got = append(got, v)
		}
	}
	return got
}

func (f *rollbackFixture) recordPath() string { return filepath.Join(f.rollbackDir, "grp") }

func (f *rollbackFixture) readRecord() rollbackRecord {
	f.t.Helper()
	data, err := os.ReadFile(f.recordPath())
	if err != nil {
		f.t.Fatalf("read rollback record: %v", err)
	}
	var rec rollbackRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		f.t.Fatalf("decode rollback record %q: %v", data, err)
	}
	return rec
}

func (f *rollbackFixture) noRecord() {
	f.t.Helper()
	if _, err := os.Lstat(f.recordPath()); !os.IsNotExist(err) {
		f.t.Errorf("rollback record exists (err = %v); none may be written", err)
	}
}

func (f *rollbackFixture) rollback(opts RollbackFeatureOptions) (*RollbackFeatureResult, error) {
	f.t.Helper()
	return f.client.RollbackFeature(f.t.Context(), "grp", opts)
}

func (f *rollbackFixture) wantLinks(want map[string]string) {
	f.t.Helper()
	for component, v := range want {
		if got := f.linkTarget(component); got != component+"_"+v+".raw" {
			f.t.Errorf("%s link = %s, want version %s", component, got, v)
		}
	}
}

// By default each transfer goes to its newest staged version older than the
// linked one, and the version rolled back from is recorded.
func TestRollbackFeature_DefaultTarget(t *testing.T) {
	f := newRollbackFixture(t, "alpha")
	f.setup("alpha", "3.0.0", "1.0.0", "2.0.0", "3.0.0")

	result, err := f.rollback(RollbackFeatureOptions{})
	if err != nil {
		t.Fatalf("RollbackFeature: %v", err)
	}
	f.wantLinks(map[string]string{"alpha": "2.0.0"})
	if !result.Success || result.Refreshed || result.Action != "rollback" {
		t.Errorf("result = %+v", result)
	}
	want := RollbackTransfer{Component: "alpha", FromVersion: "3.0.0", ToVersion: "2.0.0", RejectedVersion: "3.0.0"}
	if len(result.Transfers) != 1 || result.Transfers[0] != want {
		t.Errorf("transfers = %+v, want [%+v]", result.Transfers, want)
	}
	if result.Record != f.recordPath() {
		t.Errorf("record = %q, want %q", result.Record, f.recordPath())
	}
	if !strings.Contains(result.NextActionMessage, "next boot") {
		t.Errorf("message must say it takes effect at the next boot: %q", result.NextActionMessage)
	}
	if f.runner.refreshes != 0 {
		t.Error("a rollback without --now must not refresh")
	}

	rec := f.readRecord()
	if rec.Format != 1 || rec.Feature != "grp" || rec.Created == "" || !maps_equal(rec.Rejected, map[string]string{"alpha": "3.0.0"}) {
		t.Errorf("record = %+v", rec)
	}
	// Nothing is deleted by a rollback.
	if got := f.stagedVersions("alpha"); !slices.Equal(got, []string{"1.0.0", "2.0.0", "3.0.0"}) {
		t.Errorf("staged = %v", got)
	}
}

func maps_equal(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func TestRollbackFeature_ToVersion(t *testing.T) {
	f := newRollbackFixture(t, "alpha")
	f.setup("alpha", "3.0.0", "1.0.0", "2.0.0", "3.0.0")

	result, err := f.rollback(RollbackFeatureOptions{To: "1.0.0"})
	if err != nil {
		t.Fatalf("RollbackFeature: %v", err)
	}
	f.wantLinks(map[string]string{"alpha": "1.0.0"})
	if got := result.Transfers[0]; got.ToVersion != "1.0.0" || got.RejectedVersion != "3.0.0" {
		t.Errorf("transfer = %+v", got)
	}
}

// Several transfers: by default each goes to its own newest older staged
// version, and the record keeps one rejected version per transfer.
func TestRollbackFeature_MultiTransferDefaultIsPerTransfer(t *testing.T) {
	f := newRollbackFixture(t, "alpha", "beta")
	f.setup("alpha", "3.0.0", "1.0.0", "2.0.0", "3.0.0")
	f.setup("beta", "3.0.0", "1.0.0", "3.0.0")

	if _, err := f.rollback(RollbackFeatureOptions{}); err != nil {
		t.Fatalf("RollbackFeature: %v", err)
	}
	f.wantLinks(map[string]string{"alpha": "2.0.0", "beta": "1.0.0"})
	if got := f.readRecord().Rejected; !maps_equal(got, map[string]string{"alpha": "3.0.0", "beta": "3.0.0"}) {
		t.Errorf("rejected = %v", got)
	}
}

// Every problem blocks the rollback and changes nothing: no link, no record.
func TestRollbackFeature_RefusalsChangeNothing(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(f *rollbackFixture)
		opts    RollbackFeatureOptions
		wantErr string
	}{
		{
			name:    "target not on disk",
			setup:   func(f *rollbackFixture) { f.setup("alpha", "3.0.0", "2.0.0", "3.0.0") },
			opts:    RollbackFeatureOptions{To: "1.0.0"},
			wantErr: "version 1.0.0 of alpha is not on disk",
		},
		{
			name: "one transfer lacks the target",
			setup: func(f *rollbackFixture) {
				f.setup("alpha", "3.0.0", "1.0.0", "2.0.0", "3.0.0")
				f.setup("beta", "3.0.0", "1.0.0", "3.0.0")
			},
			opts:    RollbackFeatureOptions{To: "2.0.0"},
			wantErr: "version 2.0.0 of beta is not on disk",
		},
		{
			name: "one transfer has no older staged version",
			setup: func(f *rollbackFixture) {
				f.setup("alpha", "3.0.0", "1.0.0", "2.0.0", "3.0.0")
				f.setup("beta", "3.0.0", "3.0.0")
			},
			wantErr: "no staged version of beta is older than the linked version 3.0.0",
		},
		{
			name: "one transfer has no link",
			setup: func(f *rollbackFixture) {
				f.setup("alpha", "3.0.0", "1.0.0", "2.0.0", "3.0.0")
				f.setup("beta", "", "1.0.0", "3.0.0")
			},
			wantErr: "beta has no usable sysext link",
		},
		{
			name: "to the linked version",
			setup: func(f *rollbackFixture) {
				f.setup("alpha", "3.0.0", "1.0.0", "2.0.0", "3.0.0")
				f.setup("beta", "3.0.0", "1.0.0", "2.0.0", "3.0.0")
			},
			opts:    RollbackFeatureOptions{To: "3.0.0"},
			wantErr: "is already linked",
		},
		{
			name: "to a newer version than the linked one",
			setup: func(f *rollbackFixture) {
				f.setup("alpha", "2.0.0", "1.0.0", "2.0.0", "3.0.0")
				f.setup("beta", "2.0.0", "1.0.0", "2.0.0", "3.0.0")
			},
			opts:    RollbackFeatureOptions{To: "3.0.0"},
			wantErr: "is not older than the version rolled back from",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newRollbackFixture(t, "alpha", "beta")
			tt.setup(f)
			before := map[string]string{"alpha": f.linkTarget("alpha")}
			if _, err := os.Lstat(filepath.Join(f.linkDir, "beta.raw")); err == nil {
				before["beta"] = f.linkTarget("beta")
			}

			result, err := f.rollback(tt.opts)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want %q", err, tt.wantErr)
			}
			if result == nil || result.Success || result.Error == "" {
				t.Errorf("result = %+v, want a failure", result)
			}
			for c, target := range before {
				if got := f.linkTarget(c); got != target {
					t.Errorf("%s link moved: %s -> %s", c, target, got)
				}
			}
			f.noRecord()
			if _, err := os.Lstat(f.rollbackDir); !os.IsNotExist(err) {
				t.Errorf("rollback directory created (err = %v); nothing may be written", err)
			}
		})
	}
}

// A link that fails on the second transfer restores the first link and
// writes no record.
func TestRollbackFeature_LinkFailureRestoresFirstLink(t *testing.T) {
	r := &recordingRunner{DefaultRunner: &sysext.DefaultRunner{}}
	f := &rollbackFixture{
		featureLinkFixture: newFeatureLinkFixtureWithRunner(t, failingVersionRunner{recordingRunner: r, failComponent: "beta"}, "alpha", "beta"),
		runner:             r,
	}
	f.setup("alpha", "3.0.0", "1.0.0", "2.0.0", "3.0.0")
	f.setup("beta", "3.0.0", "1.0.0", "2.0.0", "3.0.0")

	_, err := f.rollback(RollbackFeatureOptions{})
	if err == nil || !strings.Contains(err.Error(), "injected link failure") {
		t.Fatalf("error = %v, want the link failure", err)
	}
	f.wantLinks(map[string]string{"alpha": "3.0.0", "beta": "3.0.0"})
	f.noRecord()
	entries, _ := os.ReadDir(f.linkDir)
	if len(entries) != 2 {
		t.Errorf("link dir holds %d entries, want 2 (no temp symlink left)", len(entries))
	}
}

// A failed rollback puts back the record that an earlier rollback wrote.
func TestRollbackFeature_LinkFailureRestoresPreviousRecord(t *testing.T) {
	f := newRollbackFixture(t, "alpha")
	f.setup("alpha", "3.0.0", "1.0.0", "2.0.0", "3.0.0")
	if _, err := f.rollback(RollbackFeatureOptions{}); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(f.recordPath())
	if err != nil {
		t.Fatal(err)
	}

	// The staged image of the target vanishes before the second rollback
	// links: the link fails after the record was rewritten.
	r := &recordingRunner{DefaultRunner: &sysext.DefaultRunner{}}
	f.client.runner = failingVersionRunner{recordingRunner: r, failComponent: "alpha"}
	if _, err := f.rollback(RollbackFeatureOptions{}); err == nil {
		t.Fatal("expected a link failure")
	}
	after, err := os.ReadFile(f.recordPath())
	if err != nil || string(after) != string(first) {
		t.Errorf("record = %q (err %v), want the previous record %q", after, err, first)
	}
	f.wantLinks(map[string]string{"alpha": "2.0.0"})
}

// A second rollback keeps the higher rejected version.
func TestRollbackFeature_SecondRollbackKeepsHigherRejected(t *testing.T) {
	f := newRollbackFixture(t, "alpha")
	f.setup("alpha", "3.0.0", "1.0.0", "2.0.0", "3.0.0")
	if _, err := f.rollback(RollbackFeatureOptions{}); err != nil {
		t.Fatal(err)
	}
	result, err := f.rollback(RollbackFeatureOptions{})
	if err != nil {
		t.Fatalf("second rollback: %v", err)
	}
	f.wantLinks(map[string]string{"alpha": "1.0.0"})
	if got := result.Transfers[0]; got.FromVersion != "2.0.0" || got.RejectedVersion != "3.0.0" {
		t.Errorf("transfer = %+v, want rejected 3.0.0 kept", got)
	}
	if got := f.readRecord().Rejected["alpha"]; got != "3.0.0" {
		t.Errorf("rejected = %s, want 3.0.0", got)
	}
}

func TestRollbackFeature_DryRunWritesNothing(t *testing.T) {
	f := newRollbackFixture(t, "alpha")
	f.setup("alpha", "3.0.0", "1.0.0", "2.0.0", "3.0.0")

	result, err := f.rollback(RollbackFeatureOptions{DryRun: true, Now: true})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if !result.DryRun || !result.Success || len(result.Transfers) != 1 || result.Transfers[0].ToVersion != "2.0.0" {
		t.Errorf("result = %+v", result)
	}
	f.wantLinks(map[string]string{"alpha": "3.0.0"})
	f.noRecord()
	if _, err := os.Lstat(f.rollbackDir); !os.IsNotExist(err) {
		t.Errorf("dry run created the rollback directory (err = %v)", err)
	}
	if f.runner.refreshes != 0 {
		t.Error("dry run refreshed")
	}
}

func TestRollbackFeature_NowRefreshes(t *testing.T) {
	f := newRollbackFixture(t, "alpha")
	f.setup("alpha", "3.0.0", "1.0.0", "2.0.0", "3.0.0")

	result, err := f.rollback(RollbackFeatureOptions{Now: true})
	if err != nil {
		t.Fatalf("RollbackFeature --now: %v", err)
	}
	if f.runner.refreshes != 1 || !result.Refreshed {
		t.Errorf("refreshes = %d, refreshed = %v; want one refresh", f.runner.refreshes, result.Refreshed)
	}
	if strings.Contains(result.NextActionMessage, "next boot") {
		t.Errorf("message after a refresh must not say next boot: %q", result.NextActionMessage)
	}
}

func TestRollbackFeature_NowWithNoRefreshDoesNotRefresh(t *testing.T) {
	f := newRollbackFixture(t, "alpha")
	f.setup("alpha", "3.0.0", "1.0.0", "2.0.0", "3.0.0")

	result, err := f.rollback(RollbackFeatureOptions{Now: true, NoRefresh: true})
	if err != nil || f.runner.refreshes != 0 || result.Refreshed {
		t.Fatalf("err = %v, refreshes = %d, refreshed = %v", err, f.runner.refreshes, result.Refreshed)
	}
}

// A failed refresh keeps the links and the record and reports the error.
func TestRollbackFeature_NowRefreshFailure(t *testing.T) {
	f := newRollbackFixture(t, "alpha")
	f.setup("alpha", "3.0.0", "1.0.0", "2.0.0", "3.0.0")
	f.runner.refreshErr = errors.New("boom")

	result, err := f.rollback(RollbackFeatureOptions{Now: true})
	if err == nil || !strings.Contains(err.Error(), "sysext refresh failed") {
		t.Fatalf("error = %v", err)
	}
	if result.Success || result.RefreshError == "" || result.Refreshed {
		t.Errorf("result = %+v", result)
	}
	f.wantLinks(map[string]string{"alpha": "2.0.0"})
	f.readRecord()
}

// The refresh also activates other pending links; the result lists them.
func TestRollbackFeature_NowListsOtherPendingActivations(t *testing.T) {
	f := newRollbackFixture(t, "alpha")
	f.setup("alpha", "3.0.0", "1.0.0", "2.0.0", "3.0.0")

	// A second feature, outside grp, linked to 1.0.0 while 0.9.0 is merged.
	server := newRevisionServer(t, "gamma")
	server.publish("1.0.0")
	createFeatureFile(t, f.defDir, "other", true)
	createFeatureTransferFileWithoutCurrentSymlink(t, f.defDir, "gamma", "other", server.URL, f.targetDir)
	if err := os.WriteFile(filepath.Join(f.targetDir, "gamma_1.0.0.raw"), []byte("gamma"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(f.targetDir, "gamma_1.0.0.raw"), filepath.Join(f.linkDir, "gamma.raw")); err != nil {
		t.Fatal(err)
	}
	writeMergedState(t, f.hierarchy, `{"extensions":{"gamma":{"path":"/x/gamma_0.9.0.raw"}}}`)

	result, err := f.rollback(RollbackFeatureOptions{Now: true})
	if err != nil {
		t.Fatalf("RollbackFeature: %v", err)
	}
	if len(result.PendingActivations) != 1 || result.PendingActivations[0].Component != "gamma" {
		t.Errorf("pending activations = %+v, want gamma", result.PendingActivations)
	}
	if !strings.Contains(f.out.String(), "gamma") {
		t.Errorf("the refresh warning must name gamma; output:\n%s", f.out.String())
	}
}

func TestRollbackFeature_ClearDeletesRecordAndChangesNoLink(t *testing.T) {
	f := newRollbackFixture(t, "alpha")
	f.setup("alpha", "3.0.0", "1.0.0", "2.0.0", "3.0.0")
	if _, err := f.rollback(RollbackFeatureOptions{}); err != nil {
		t.Fatal(err)
	}

	dry, err := f.rollback(RollbackFeatureOptions{Clear: true, DryRun: true})
	if err != nil || !dry.Cleared || !dry.DryRun {
		t.Fatalf("dry-run clear = %+v, %v", dry, err)
	}
	f.readRecord() // still there

	result, err := f.rollback(RollbackFeatureOptions{Clear: true})
	if err != nil || !result.Cleared || result.NoRecord || result.Action != "rollback-clear" {
		t.Fatalf("clear = %+v, %v", result, err)
	}
	f.noRecord()
	f.wantLinks(map[string]string{"alpha": "2.0.0"})

	// A missing record is not an error.
	again, err := f.rollback(RollbackFeatureOptions{Clear: true})
	if err != nil || !again.NoRecord || again.Cleared || !again.Success {
		t.Fatalf("second clear = %+v, %v", again, err)
	}
}

func TestRollbackFeature_ClearDoesNotNeedTheFeature(t *testing.T) {
	f := newRollbackFixture(t, "alpha")
	if err := os.MkdirAll(f.rollbackDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.rollbackDir, "gone"), []byte("garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := f.client.RollbackFeature(t.Context(), "gone", RollbackFeatureOptions{Clear: true})
	if err != nil || !result.Cleared {
		t.Fatalf("clear of an unreadable record of a removed feature = %+v, %v", result, err)
	}
}

func TestRollbackFeature_ClearConflictsWithToAndNow(t *testing.T) {
	f := newRollbackFixture(t, "alpha")
	for _, opts := range []RollbackFeatureOptions{{Clear: true, To: "1.0.0"}, {Clear: true, Now: true}} {
		if _, err := f.rollback(opts); err == nil {
			t.Errorf("RollbackFeature(%+v) must fail", opts)
		}
	}
}

// Record path safety: names, symlinks and directories.
func TestRollbackFeature_RecordPathSafety(t *testing.T) {
	t.Run("traversal-shaped names are refused", func(t *testing.T) {
		f := newRollbackFixture(t, "alpha")
		for _, name := range []string{"../etc/passwd", "a/b", "..", ".hidden", "", "x y"} {
			if _, err := f.client.RollbackFeature(t.Context(), name, RollbackFeatureOptions{}); err == nil {
				t.Errorf("name %q was accepted", name)
			}
			if _, err := f.client.RollbackFeature(t.Context(), name, RollbackFeatureOptions{Clear: true}); err == nil {
				t.Errorf("name %q was accepted by --clear", name)
			}
		}
		if _, err := os.Lstat(f.rollbackDir); !os.IsNotExist(err) {
			t.Errorf("rollback directory created (err = %v)", err)
		}
	})

	t.Run("a symlink at the record path is not followed", func(t *testing.T) {
		f := newRollbackFixture(t, "alpha")
		f.setup("alpha", "3.0.0", "1.0.0", "2.0.0", "3.0.0")
		victim := filepath.Join(t.TempDir(), "victim")
		if err := os.MkdirAll(f.rollbackDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(victim, f.recordPath()); err != nil {
			t.Fatal(err)
		}

		if _, err := f.rollback(RollbackFeatureOptions{}); err == nil {
			t.Fatal("rollback through a symlinked record must be refused")
		}
		if _, err := f.rollback(RollbackFeatureOptions{Clear: true}); err == nil {
			t.Fatal("--clear through a symlinked record must be refused")
		}
		if _, err := os.Lstat(victim); !os.IsNotExist(err) {
			t.Errorf("the link target was created (err = %v)", err)
		}
		f.wantLinks(map[string]string{"alpha": "3.0.0"})
	})

	t.Run("a symlinked directory is refused", func(t *testing.T) {
		f := newRollbackFixture(t, "alpha")
		f.setup("alpha", "3.0.0", "1.0.0", "2.0.0", "3.0.0")
		real := t.TempDir()
		if err := os.Symlink(real, f.rollbackDir); err != nil {
			t.Fatal(err)
		}
		if _, err := f.rollback(RollbackFeatureOptions{}); err == nil {
			t.Fatal("a symlinked rollback directory must be refused")
		}
		if entries, _ := os.ReadDir(real); len(entries) != 0 {
			t.Errorf("a record was written through the symlinked directory: %v", entries)
		}
		f.wantLinks(map[string]string{"alpha": "3.0.0"})
	})

	t.Run("directory and file modes", func(t *testing.T) {
		f := newRollbackFixture(t, "alpha")
		f.setup("alpha", "3.0.0", "1.0.0", "2.0.0", "3.0.0")
		if _, err := f.rollback(RollbackFeatureOptions{}); err != nil {
			t.Fatal(err)
		}
		if info, err := os.Stat(f.rollbackDir); err != nil || info.Mode().Perm() != 0o755 {
			t.Errorf("rollback dir = %v, %v", info, err)
		}
		if info, err := os.Stat(f.recordPath()); err != nil || !info.Mode().IsRegular() {
			t.Errorf("record = %v, %v", info, err)
		}
		// No temp file is left beside the record.
		entries, _ := os.ReadDir(f.rollbackDir)
		if len(entries) != 1 {
			t.Errorf("rollback dir holds %d entries, want only the record", len(entries))
		}
	})
}

// The operation lock covers a rollback; a dry run does not take it.
func TestRollbackFeature_LockHeld(t *testing.T) {
	f := newRollbackFixture(t, "alpha")
	f.setup("alpha", "3.0.0", "1.0.0", "2.0.0", "3.0.0")
	holder := newLockTestClient(t, f.lockPath)
	release, err := holder.lockMutation(false)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := f.rollback(RollbackFeatureOptions{}); !errors.Is(err, ErrBusy) {
		t.Fatalf("error = %v, want ErrBusy", err)
	}
	if _, err := f.rollback(RollbackFeatureOptions{Clear: true}); !errors.Is(err, ErrBusy) {
		t.Fatalf("--clear error = %v, want ErrBusy", err)
	}
	f.wantLinks(map[string]string{"alpha": "3.0.0"})
	f.noRecord()
	if _, err := f.rollback(RollbackFeatureOptions{DryRun: true}); err != nil {
		t.Errorf("dry run while the lock is held: %v", err)
	}

	release()
	if _, err := f.rollback(RollbackFeatureOptions{}); err != nil {
		t.Fatalf("after the release: %v", err)
	}
}

// A runner that cannot link a given version is refused cleanly, before any
// change.
func TestRollbackFeature_RunnerWithoutLinkVersionIsRefused(t *testing.T) {
	runner := failingLinkRunner{PathSysextRunner: &sysext.DefaultRunner{}}
	f := &rollbackFixture{featureLinkFixture: newFeatureLinkFixtureWithRunner(t, runner, "alpha")}
	f.setup("alpha", "3.0.0", "1.0.0", "2.0.0", "3.0.0")

	if _, err := f.rollback(RollbackFeatureOptions{}); !errors.Is(err, ErrLegacySysextRunner) {
		t.Fatalf("error = %v, want ErrLegacySysextRunner", err)
	}
	f.wantLinks(map[string]string{"alpha": "3.0.0"})
	f.noRecord()
}

func TestRollbackFeature_FeatureErrors(t *testing.T) {
	f := newRollbackFixture(t, "alpha")
	if _, err := f.client.RollbackFeature(t.Context(), "nope", RollbackFeatureOptions{}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("unknown feature: %v", err)
	}
}

// --- UpdateFeatures honours the record ---

// heldFixture: alpha staged 1, 2, 3 and linked 3, the server publishes up to
// 3; the feature is rolled back to 2 (rejected 3).
func newHeldFixture(t *testing.T, components ...string) *rollbackFixture {
	t.Helper()
	f := newRollbackFixture(t, components...)
	for _, c := range components {
		f.servers[c].publish("3.0.0")
		f.setup(c, "3.0.0", "1.0.0", "2.0.0", "3.0.0")
	}
	if _, err := f.rollback(RollbackFeatureOptions{}); err != nil {
		t.Fatalf("RollbackFeature: %v", err)
	}
	return f
}

func (f *rollbackFixture) gets() int32 {
	var n int32
	for _, s := range f.servers {
		n += s.fileGets.Load()
	}
	return n
}

// A timer run with the record and no newer version: no relink, nothing
// downloaded, exit 0 (no error), and a clear report.
func TestUpdateFeatures_HeldByRollbackKeepsLinkAndDownloadsNothing(t *testing.T) {
	f := newHeldFixture(t, "alpha")
	f.wantLinks(map[string]string{"alpha": "2.0.0"})

	results, err := f.update()
	if err != nil {
		t.Fatalf("a held run must not fail: %v", err)
	}
	r := resultsByComponent(t, results)["alpha"]
	if !r.Held || r.RejectedVersion != "3.0.0" || r.Error != "" || r.Downloaded {
		t.Errorf("result = %+v", r)
	}
	if r.NextActionMessage != "held by rollback: rejected 3.0.0" {
		t.Errorf("message = %q", r.NextActionMessage)
	}
	if r.Version != "2.0.0" {
		t.Errorf("version = %q, want the linked 2.0.0", r.Version)
	}
	f.wantLinks(map[string]string{"alpha": "2.0.0"})
	if got := f.gets(); got != 0 {
		t.Errorf("%d image download(s); a held run downloads nothing", got)
	}
	f.readRecord() // kept

	// The dry run reports the same hold.
	dry, err := f.client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{DryRun: true})
	if err != nil || !resultsByComponent(t, dry)["alpha"].Held {
		t.Errorf("dry run = %+v, %v", dry, err)
	}
}

// The "already staged, relink" path must not move the link back to the
// rejected image, which is the newest staged one.
func TestUpdateFeatures_HeldByRollbackDoesNotRelinkToRejectedImage(t *testing.T) {
	f := newHeldFixture(t, "alpha")
	if !f.staged("alpha", "3.0.0") {
		t.Fatal("the rejected image must still be staged")
	}
	if _, err := f.update(); err != nil {
		t.Fatal(err)
	}
	f.wantLinks(map[string]string{"alpha": "2.0.0"})

	// Control: without the record, the same state relinks to the newest.
	if _, err := f.rollback(RollbackFeatureOptions{Clear: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.update(); err != nil {
		t.Fatal(err)
	}
	f.wantLinks(map[string]string{"alpha": "3.0.0"})
}

// Withdrawing the rejected version does not let an older one through.
func TestUpdateFeatures_HeldWhenNewestIsOlderThanRejected(t *testing.T) {
	f := newHeldFixture(t, "alpha")
	f.servers["alpha"].unpublish("3.0.0")

	results, err := f.update()
	if err != nil || !resultsByComponent(t, results)["alpha"].Held {
		t.Fatalf("results = %+v, err = %v; want held", results, err)
	}
	f.wantLinks(map[string]string{"alpha": "2.0.0"})
}

// A newer revision is downloaded, linked, and the record is deleted.
func TestUpdateFeatures_NewerRevisionResumesAndDeletesRecord(t *testing.T) {
	f := newHeldFixture(t, "alpha", "beta")
	f.servers["alpha"].publish("4.0.0")
	f.servers["beta"].publish("4.0.0")

	results, err := f.update()
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	by := resultsByComponent(t, results)
	for _, c := range []string{"alpha", "beta"} {
		if by[c].Held || !by[c].Downloaded || by[c].Version != "4.0.0" || by[c].Error != "" {
			t.Errorf("%s result = %+v", c, by[c])
		}
	}
	f.wantLinks(map[string]string{"alpha": "4.0.0", "beta": "4.0.0"})
	f.noRecord()
}

// A feature is held as a whole: one transfer with a newer revision is not
// linked while its sibling has none, so the feature never splits.
func TestUpdateFeatures_HeldFeatureStaysTogether(t *testing.T) {
	f := newHeldFixture(t, "alpha", "beta")
	f.servers["beta"].publish("4.0.0")

	results, err := f.update()
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	by := resultsByComponent(t, results)
	if !by["alpha"].Held || !by["beta"].Held {
		t.Errorf("both transfers must be held: %+v", by)
	}
	if f.staged("beta", "4.0.0") {
		t.Error("beta 4.0.0 must not be downloaded while the feature is held")
	}
	f.wantLinks(map[string]string{"alpha": "2.0.0", "beta": "2.0.0"})
	f.readRecord()
}

// While the record exists, the vacuum keeps the rejected version even when it
// is not among the newest InstancesMax; the dry-run plan agrees.
func TestUpdateFeatures_VacuumKeepsRejectedVersion(t *testing.T) {
	f := newHeldFixture(t, "alpha")
	// Staged by hand: 4.0.0 and 5.0.0 (e.g. from a manual download).
	f.setup("alpha", "2.0.0", "1.0.0", "2.0.0", "3.0.0", "4.0.0", "5.0.0")
	f.servers["alpha"].publish("6.0.0")

	dry, err := f.client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	removed := slices.Clone(resultsByComponent(t, dry)["alpha"].RemovedVersions)
	slices.Sort(removed)
	if !slices.Equal(removed, []string{"1.0.0", "2.0.0", "4.0.0"}) {
		t.Errorf("dry run would remove %v, want 1.0.0 2.0.0 4.0.0 (3.0.0 is rejected)", removed)
	}

	if _, err := f.update(); err != nil {
		t.Fatalf("update: %v", err)
	}
	if got := f.stagedVersions("alpha"); !slices.Equal(got, []string{"3.0.0", "5.0.0", "6.0.0"}) {
		t.Errorf("staged = %v, want 3.0.0 (rejected), 5.0.0 and 6.0.0", got)
	}
	f.wantLinks(map[string]string{"alpha": "6.0.0"})
	f.noRecord()
}

// An unreadable or malformed record: nothing links for that feature, and the
// error names the record. Another feature is not affected.
func TestUpdateFeatures_UnreadableRecordFailsClosedForThatFeature(t *testing.T) {
	cases := map[string]func(t *testing.T, path string){
		"malformed json": func(t *testing.T, path string) { writeTestFile(t, path, "{not json") },
		"unsupported format": func(t *testing.T, path string) {
			writeTestFile(t, path, `{"format":2,"feature":"grp","created":"x","rejected":{"alpha":"3.0.0"}}`)
		},
		"other feature": func(t *testing.T, path string) {
			writeTestFile(t, path, `{"format":1,"feature":"other","created":"x","rejected":{"alpha":"3.0.0"}}`)
		},
		"no rejected version": func(t *testing.T, path string) {
			writeTestFile(t, path, `{"format":1,"feature":"grp","created":"x","rejected":{}}`)
		},
		"empty version": func(t *testing.T, path string) {
			writeTestFile(t, path, `{"format":1,"feature":"grp","created":"x","rejected":{"alpha":""}}`)
		},
		"empty file": func(t *testing.T, path string) { writeTestFile(t, path, "") },
		"too large": func(t *testing.T, path string) {
			writeTestFile(t, path, strings.Repeat("x", maxRollbackRecordSize+10))
		},
		"a directory": func(t *testing.T, path string) {
			if err := os.Mkdir(path, 0o755); err != nil {
				t.Fatal(err)
			}
		},
		"a symlink": func(t *testing.T, path string) {
			target := filepath.Join(filepath.Dir(path), "elsewhere")
			writeTestFile(t, target, `{"format":1,"feature":"grp","created":"x","rejected":{"alpha":"3.0.0"}}`)
			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, corrupt := range cases {
		t.Run(name, func(t *testing.T) {
			f := newRollbackFixture(t, "alpha")
			f.servers["alpha"].publish("3.0.0")
			f.setup("alpha", "2.0.0", "1.0.0", "2.0.0", "3.0.0")
			if err := os.MkdirAll(f.rollbackDir, 0o755); err != nil {
				t.Fatal(err)
			}
			corrupt(t, f.recordPath())

			// A second feature that has no record keeps updating.
			other := newRevisionServer(t, "gamma")
			other.publish("1.0.0")
			createFeatureFile(t, f.defDir, "other", true)
			createFeatureTransferFileWithoutCurrentSymlink(t, f.defDir, "gamma", "other", other.URL, f.targetDir)

			results, err := f.update()
			if err == nil {
				t.Fatal("expected an error for the feature with an unreadable record")
			}
			if len(results) != 2 {
				t.Fatalf("results = %+v", results)
			}
			var grp, oth UpdateFeaturesResult
			for _, r := range results {
				if r.Feature == "grp" {
					grp = r
				} else {
					oth = r
				}
			}
			if len(grp.Results) != 1 || !strings.Contains(grp.Results[0].Error, "rollback record") || grp.Results[0].Held {
				t.Errorf("grp result = %+v", grp.Results)
			}
			f.wantLinks(map[string]string{"alpha": "2.0.0"})
			if f.servers["alpha"].fileGets.Load() != 0 {
				t.Error("nothing may be downloaded for the feature with an unreadable record")
			}
			if len(oth.Results) != 1 || oth.Results[0].Error != "" || !oth.Results[0].Downloaded {
				t.Errorf("other feature result = %+v", oth.Results)
			}
			if got, err := os.Readlink(filepath.Join(f.linkDir, "gamma.raw")); err != nil || filepath.Base(got) != "gamma_1.0.0.raw" {
				t.Errorf("gamma link = %q, %v", got, err)
			}
		})
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// --- a shared transfer honours every applicable record ---

// addSharedFeature makes every transfer of the fixture name an extra feature
// (Features=grp <name>), so the two features share them.
func (f *rollbackFixture) addSharedFeature(name string) {
	f.t.Helper()
	createFeatureFile(f.t, f.defDir, name, true)
	for _, component := range f.components {
		f.shareTransfer(name, component)
	}
}

// shareTransfer rewrites one transfer so that it also names the given feature
// (Features=grp <name>).
func (f *rollbackFixture) shareTransfer(name, component string) {
	f.t.Helper()
	createFeatureFile(f.t, f.defDir, name, true)
	content := `[Transfer]
Features=grp ` + name + `
Verify=false

[Source]
Type=url-file
Path=` + f.servers[component].URL + `
MatchPattern=` + component + `_@v.raw

[Target]
Path=` + f.targetDir + `
MatchPattern=` + component + `_@v.raw
`
	writeTestFile(f.t, filepath.Join(f.defDir, component+".transfer"), content)
}

// renameSharedFeature renames the shared feature, so that UpdateFeatures
// processes it in another order (features are sorted by name).
func (f *rollbackFixture) renameSharedFeature(from, to string) {
	f.t.Helper()
	if err := os.Remove(filepath.Join(f.defDir, from+".feature")); err != nil {
		f.t.Fatal(err)
	}
	f.shareTransfer(to, "alpha")
}

// addUnrelatedFeature writes a second, enabled feature with its own component
// and source, publishing 1.0.0 and 2.0.0.
func (f *rollbackFixture) addUnrelatedFeature(name, component string) {
	f.t.Helper()
	server := newRevisionServer(f.t, component)
	server.publish("1.0.0")
	server.publish("2.0.0")
	f.servers[component] = server
	createFeatureFile(f.t, f.defDir, name, true)
	createFeatureTransferFileWithoutCurrentSymlink(f.t, f.defDir, component, name, server.URL, f.targetDir)
}

// newSharedRollbackFixture: alpha has 1.0.0 and 2.0.0 staged and 2.0.0
// linked, grp and other both name it, and grp is rolled back to 1.0.0,
// rejecting 2.0.0. The source still publishes 2.0.0.
func newSharedRollbackFixture(t *testing.T) *rollbackFixture {
	t.Helper()
	f := newRollbackFixture(t, "alpha")
	f.addSharedFeature("other")
	f.setup("alpha", "2.0.0", "1.0.0", "2.0.0")
	if _, err := f.rollback(RollbackFeatureOptions{}); err != nil {
		t.Fatalf("RollbackFeature: %v", err)
	}
	return f
}

// updateResultsByFeature indexes the per-transfer results by feature and
// component.
func updateResultsByFeature(t *testing.T, results []UpdateFeaturesResult) map[string]map[string]UpdateResult {
	t.Helper()
	by := make(map[string]map[string]UpdateResult, len(results))
	for _, feature := range results {
		m := make(map[string]UpdateResult, len(feature.Results))
		for _, r := range feature.Results {
			m[r.Component] = r
		}
		by[feature.Feature] = m
	}
	return by
}

// A rollback of grp must hold alpha for other too: updating through the other
// feature must not link alpha_2.0.0.raw again (the blocking review finding).
func TestUpdateFeatures_SharedTransferHeldByAnotherFeaturesRollback(t *testing.T) {
	f := newSharedRollbackFixture(t)
	f.wantLinks(map[string]string{"alpha": "1.0.0"})
	// A rollback of grp records grp only; the hold reaches other through the
	// shared transfer.
	if rec := f.readRecord(); rec.Feature != "grp" || !maps_equal(rec.Rejected, map[string]string{"alpha": "2.0.0"}) {
		t.Errorf("record = %+v, want grp rejecting alpha 2.0.0", rec)
	}
	if _, err := os.Lstat(filepath.Join(f.rollbackDir, "other")); !os.IsNotExist(err) {
		t.Errorf("a record for other was written (err = %v)", err)
	}

	results, err := f.update()
	if err != nil {
		t.Fatalf("a held run must not fail: %v", err)
	}
	by := updateResultsByFeature(t, results)
	for _, name := range []string{"grp", "other"} {
		r, ok := by[name]["alpha"]
		if !ok {
			t.Fatalf("feature %s has no alpha result: %+v", name, results)
		}
		if !r.Held || r.RejectedVersion != "2.0.0" || r.Error != "" || r.Downloaded {
			t.Errorf("%s result = %+v", name, r)
		}
		if r.Version != "1.0.0" {
			t.Errorf("%s version = %q, want the linked 1.0.0", name, r.Version)
		}
	}
	f.wantLinks(map[string]string{"alpha": "1.0.0"})
	if got := f.gets(); got != 0 {
		t.Errorf("%d image download(s); a held run downloads nothing", got)
	}
	f.readRecord()

	// The record applies whether or not the rolled-back feature is enabled.
	if _, err := f.client.DisableFeature(t.Context(), "grp", DisableFeatureOptions{}); err != nil {
		t.Fatalf("DisableFeature: %v", err)
	}
	results, err = f.update()
	if err != nil {
		t.Fatalf("update with grp disabled: %v", err)
	}
	by = updateResultsByFeature(t, results)
	if _, ok := by["grp"]; ok {
		t.Error("grp is disabled but was updated")
	}
	if r := by["other"]["alpha"]; !r.Held || r.RejectedVersion != "2.0.0" || r.Error != "" {
		t.Errorf("other result with grp disabled = %+v", r)
	}
	f.wantLinks(map[string]string{"alpha": "1.0.0"})
	if got := f.gets(); got != 0 {
		t.Errorf("%d image download(s) after grp was disabled; nothing may be downloaded", got)
	}
}

// EnableFeature --now through a shared feature honours the record too.
func TestEnableFeatureNow_SharedTransferHeldByAnotherFeaturesRollback(t *testing.T) {
	f := newSharedRollbackFixture(t)

	result, err := f.client.EnableFeature(t.Context(), "other", EnableFeatureOptions{Now: true, NoRefresh: true})
	if err != nil || !result.Success {
		t.Fatalf("EnableFeature = %+v, %v", result, err)
	}
	f.wantLinks(map[string]string{"alpha": "1.0.0"})
	if len(result.DownloadedFiles) != 0 {
		t.Errorf("downloaded = %v", result.DownloadedFiles)
	}
	if got := f.gets(); got != 0 {
		t.Errorf("%d image download(s); a held run downloads nothing", got)
	}
	f.readRecord()
}

// A newer revision links once, whichever shared feature is processed first,
// and deletes grp's record.
func TestUpdateFeatures_SharedTransferNewerRevisionDeletesRecord(t *testing.T) {
	f := newSharedRollbackFixture(t)
	f.servers["alpha"].publish("3.0.0")

	results, err := f.update()
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	by := updateResultsByFeature(t, results)
	for _, name := range []string{"grp", "other"} {
		r := by[name]["alpha"]
		if r.Held || r.Error != "" || r.Version != "3.0.0" {
			t.Errorf("%s result = %+v", name, r)
		}
	}
	if by["grp"]["alpha"].Downloaded == by["other"]["alpha"].Downloaded {
		t.Errorf("exactly one feature must download 3.0.0: %+v", by)
	}
	f.wantLinks(map[string]string{"alpha": "3.0.0"})
	f.noRecord()
	if got := f.gets(); got != 1 {
		t.Errorf("%d image download(s), want 1: the new revision links once", got)
	}
}

// A record of a feature that shares the transfer is malformed: the feature
// being processed links nothing and names the feature whose record failed. A
// third, unrelated feature still updates.
func TestUpdateFeatures_MalformedSharedRecordFailsClosed(t *testing.T) {
	f := newSharedRollbackFixture(t)
	f.addUnrelatedFeature("third", "gamma")
	writeTestFile(t, f.recordPath(), "{not json")

	results, err := f.update()
	if err == nil {
		t.Fatal("expected an error for the features that share alpha")
	}
	by := updateResultsByFeature(t, results)
	for _, name := range []string{"grp", "other"} {
		r := by[name]["alpha"]
		if !strings.Contains(r.Error, "rollback record of feature grp") || r.Downloaded || r.Held {
			t.Errorf("%s result = %+v", name, r)
		}
	}
	f.wantLinks(map[string]string{"alpha": "1.0.0"})
	if got := f.servers["alpha"].fileGets.Load(); got != 0 {
		t.Errorf("%d alpha image download(s); nothing may be downloaded", got)
	}
	g := by["third"]["gamma"]
	if g.Error != "" || !g.Downloaded || g.Version != "2.0.0" {
		t.Errorf("third result = %+v; an unrelated feature must still update", g)
	}
	if got := f.linkTarget("gamma"); got != "gamma_2.0.0.raw" {
		t.Errorf("gamma link = %s, want gamma_2.0.0.raw", got)
	}
}

// newSplitRollbackFixture: grp has alpha and beta, other shares only alpha
// (alpha.transfer names both features, beta.transfer only grp). Both
// components have 1.0.0 and 2.0.0 staged and 2.0.0 linked, and grp is rolled
// back to 1.0.0, so its record rejects 2.0.0 for both.
func newSplitRollbackFixture(t *testing.T) *rollbackFixture {
	t.Helper()
	return newSplitRollbackFixtureWithRunner(t, nil)
}

// newSplitRollbackFixtureWithRunner is newSplitRollbackFixture with the
// recording runner wrapped by wrap.
func newSplitRollbackFixtureWithRunner(t *testing.T, wrap func(*recordingRunner) sysext.SysextRunner) *rollbackFixture {
	t.Helper()
	f := newRollbackFixtureWithRunner(t, wrap, "alpha", "beta")
	for _, component := range []string{"alpha", "beta"} {
		f.setup(component, "2.0.0", "1.0.0", "2.0.0")
	}
	f.shareTransfer("other", "alpha")
	if _, err := f.rollback(RollbackFeatureOptions{}); err != nil {
		t.Fatalf("RollbackFeature: %v", err)
	}
	return f
}

// The record of grp also rejects beta, which other does not use. Publishing a
// newer alpha must not let other split the rolled-back feature: alpha stays at
// 1.0.0 because only grp advances the shared transfer, and grp's own hold names
// beta (the review finding).
func TestUpdateFeatures_SharedFeatureCannotSplitAnotherFeaturesRollback(t *testing.T) {
	f := newSplitRollbackFixture(t)
	f.servers["alpha"].publish("3.0.0")

	results, err := f.update()
	if err != nil {
		t.Fatalf("a held run must not fail: %v", err)
	}
	by := updateResultsByFeature(t, results)
	g := by["grp"]["alpha"]
	if !g.Held || g.Error != "" || g.Downloaded || g.Version != "1.0.0" {
		t.Errorf("grp alpha result = %+v", g)
	}
	if !strings.Contains(g.NextActionMessage, "beta") || !strings.Contains(g.NextActionMessage, "2.0.0") {
		t.Errorf("grp alpha message = %q; it must name beta, the component that holds its own record", g.NextActionMessage)
	}
	o := by["other"]["alpha"]
	if !o.Held || o.Error != "" || o.Downloaded || o.Version != "1.0.0" {
		t.Errorf("other alpha result = %+v", o)
	}
	if want := "held by rollback of grp: it advances only when grp updates (or run 'updex features rollback --clear grp')"; o.NextActionMessage != want {
		t.Errorf("other alpha message = %q, want %q", o.NextActionMessage, want)
	}
	if r := by["grp"]["beta"]; !r.Held || r.RejectedVersion != "2.0.0" || r.Error != "" {
		t.Errorf("grp beta result = %+v", r)
	}
	f.wantLinks(map[string]string{"alpha": "1.0.0", "beta": "1.0.0"})
	if got := f.gets(); got != 0 {
		t.Errorf("%d image download(s); a held run downloads nothing", got)
	}
	f.readRecord()
}

// EnableFeature --now through the sharing feature gets the same whole-record
// hold.
func TestEnableFeatureNow_SharedFeatureCannotSplitAnotherFeaturesRollback(t *testing.T) {
	f := newSplitRollbackFixture(t)
	f.servers["alpha"].publish("3.0.0")

	result, err := f.client.EnableFeature(t.Context(), "other", EnableFeatureOptions{Now: true, NoRefresh: true})
	if err != nil || !result.Success {
		t.Fatalf("EnableFeature = %+v, %v", result, err)
	}
	if len(result.DownloadedFiles) != 0 {
		t.Errorf("downloaded = %v", result.DownloadedFiles)
	}
	f.wantLinks(map[string]string{"alpha": "1.0.0", "beta": "1.0.0"})
	if got := f.gets(); got != 0 {
		t.Errorf("%d image download(s); a held run downloads nothing", got)
	}
	if !strings.Contains(f.out.String(), "advances only when grp updates") {
		t.Errorf("the hold message must say that only grp advances the shared transfer; output:\n%s", f.out.String())
	}
	f.readRecord()
}

// Once beta has a newer version too, the whole record can advance: both
// components link 3.0.0 and grp's record is deleted.
func TestUpdateFeatures_SharedFeatureAdvancesWhenEveryRecordComponentAdvances(t *testing.T) {
	f := newSplitRollbackFixture(t)
	f.servers["alpha"].publish("3.0.0")
	f.servers["beta"].publish("3.0.0")

	results, err := f.update()
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	by := updateResultsByFeature(t, results)
	for _, name := range []string{"grp", "other"} {
		r := by[name]["alpha"]
		if r.Held || r.Error != "" || r.Version != "3.0.0" {
			t.Errorf("%s alpha result = %+v", name, r)
		}
	}
	if r := by["grp"]["beta"]; r.Held || r.Error != "" || r.Version != "3.0.0" {
		t.Errorf("grp beta result = %+v", r)
	}
	f.wantLinks(map[string]string{"alpha": "3.0.0", "beta": "3.0.0"})
	f.noRecord()
	if got := f.gets(); got != 2 {
		t.Errorf("%d image download(s), want 2: each new image is downloaded once", got)
	}
}

// A download failure in the feature that owns the record must not let a
// feature that shares one transfer link the image that was staged before the
// failure (blocking review finding): that feature is held, in either
// processing order.
func TestUpdateFeatures_SharedFeatureHeldAfterOwnersDownloadFailure(t *testing.T) {
	for _, tc := range []struct {
		name   string
		shared string
	}{
		{name: "shared feature after the owner", shared: "other"},
		{name: "shared feature before the owner", shared: "aaa"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newSplitRollbackFixture(t)
			if tc.shared != "other" {
				f.renameSharedFeature("other", tc.shared)
			}
			f.servers["alpha"].publish("3.0.0")
			f.servers["beta"].publish("3.0.0")
			f.servers["beta"].failFiles.Store(true)

			results, err := f.update()
			if err == nil {
				t.Fatal("expected an error: beta's image download failed")
			}
			for _, want := range []string{"beta", "download failed"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("UpdateFeatures error = %v, want %q", err, want)
				}
			}
			by := updateResultsByFeature(t, results)

			if r := by["grp"]["beta"]; !strings.Contains(r.Error, "download failed") {
				t.Errorf("grp beta result = %+v", r)
			}
			if r := by["grp"]["alpha"]; !strings.Contains(r.Error, "not linked") || r.Installed {
				t.Errorf("grp alpha result = %+v; alpha was staged but must not be linked", r)
			}
			shared := by[tc.shared]["alpha"]
			if !shared.Held || shared.Error != "" || shared.Downloaded || shared.Version != "1.0.0" {
				t.Errorf("%s alpha result = %+v; the record must hold it", tc.shared, shared)
			}
			if want := "held by rollback of grp: it advances only when grp updates (or run 'updex features rollback --clear grp')"; shared.NextActionMessage != want {
				t.Errorf("%s message = %q, want %q", tc.shared, shared.NextActionMessage, want)
			}
			f.wantLinks(map[string]string{"alpha": "1.0.0", "beta": "1.0.0"})
			if !f.staged("alpha", "3.0.0") {
				t.Error("alpha 3.0.0 must stay staged for the next run")
			}
			if f.staged("beta", "3.0.0") {
				t.Error("beta 3.0.0 must not exist; its download failed")
			}
			f.readRecord()
		})
	}
}

// The same rule for a partial link failure: when grp's beta link fails, alpha
// is restored, and the feature that shares alpha must not link 3.0.0
// afterwards, in either processing order.
func TestUpdateFeatures_SharedFeatureHeldAfterOwnersLinkFailure(t *testing.T) {
	for _, tc := range []struct {
		name   string
		shared string
	}{
		{name: "shared feature after the owner", shared: "other"},
		{name: "shared feature before the owner", shared: "aaa"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newSplitRollbackFixtureWithRunner(t, func(r *recordingRunner) sysext.SysextRunner {
				return failingUpdateLinkRunner{recordingRunner: r, failComponent: "beta"}
			})
			if tc.shared != "other" {
				f.renameSharedFeature("other", tc.shared)
			}
			f.servers["alpha"].publish("3.0.0")
			f.servers["beta"].publish("3.0.0")

			results, err := f.update()
			if err == nil {
				t.Fatal("expected an error: grp's beta link failed")
			}
			if !strings.Contains(err.Error(), "injected link failure") {
				t.Errorf("UpdateFeatures error = %v, want the injected link failure", err)
			}
			by := updateResultsByFeature(t, results)
			if r := by["grp"]["beta"]; !strings.Contains(r.Error, "injected link failure") || r.Installed {
				t.Errorf("grp beta result = %+v", r)
			}
			if r := by["grp"]["alpha"]; !strings.Contains(r.Error, "not linked") || r.Installed {
				t.Errorf("grp alpha result = %+v; its link was moved and must be restored", r)
			}
			shared := by[tc.shared]["alpha"]
			if !shared.Held || shared.Error != "" || shared.Downloaded || shared.Version != "1.0.0" {
				t.Errorf("%s alpha result = %+v; the record must hold it", tc.shared, shared)
			}
			f.wantLinks(map[string]string{"alpha": "1.0.0", "beta": "1.0.0"})
			if got := f.gets(); got != 2 {
				t.Errorf("%d image download(s), want 2 (both new images were staged before the link failed)", got)
			}
			f.readRecord()
		})
	}
}

// The happy path with the sharing feature processed first: it is held while
// the record exists, grp then links both new images and deletes the record,
// and the next run finds alpha current and downloads nothing.
func TestUpdateFeatures_SharedFeatureHeldThenSeesOwnerCurrent(t *testing.T) {
	f := newSplitRollbackFixture(t)
	f.renameSharedFeature("other", "aaa")
	f.servers["alpha"].publish("3.0.0")
	f.servers["beta"].publish("3.0.0")

	results, err := f.update()
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	by := updateResultsByFeature(t, results)
	shared := by["aaa"]["alpha"]
	if !shared.Held || shared.Error != "" || shared.Downloaded || shared.Version != "1.0.0" {
		t.Errorf("aaa alpha result = %+v; the record must hold it", shared)
	}
	for _, component := range []string{"alpha", "beta"} {
		if r := by["grp"][component]; r.Held || r.Error != "" || r.Version != "3.0.0" || !r.Downloaded {
			t.Errorf("grp %s result = %+v", component, r)
		}
	}
	f.wantLinks(map[string]string{"alpha": "3.0.0", "beta": "3.0.0"})
	f.noRecord()
	if got := f.gets(); got != 2 {
		t.Errorf("%d image download(s), want 2: each new image is downloaded once", got)
	}

	results, err = f.update()
	if err != nil {
		t.Fatalf("second update: %v", err)
	}
	by = updateResultsByFeature(t, results)
	shared = by["aaa"]["alpha"]
	if shared.Held || shared.Error != "" || shared.Downloaded || shared.Version != "3.0.0" {
		t.Errorf("aaa alpha result after the record was deleted = %+v", shared)
	}
	if got := f.gets(); got != 2 {
		t.Errorf("%d image download(s) after the record was gone, want 2: alpha is current", got)
	}
}

// Two features that share a transfer can both have a rollback record. Neither
// advances the transfer then, even when a newer version is published: each is
// held by the other's record. Clearing one record lets the other feature
// advance; updex never clears a record by itself.
func TestUpdateFeatures_MutualRollbackRecordsHoldEachOther(t *testing.T) {
	f := newSplitRollbackFixture(t)
	// Give other its own record too: 0.5.0 is staged and older than the linked
	// 1.0.0, so rolling other back records a rejection of 1.0.0.
	f.setup("alpha", "1.0.0", "0.5.0", "1.0.0", "2.0.0")
	if _, err := f.client.RollbackFeature(t.Context(), "other", RollbackFeatureOptions{}); err != nil {
		t.Fatalf("RollbackFeature(other): %v", err)
	}
	f.servers["alpha"].publish("3.0.0")
	f.servers["beta"].publish("3.0.0")

	results, err := f.update()
	if err != nil {
		t.Fatalf("a held run must not fail: %v", err)
	}
	by := updateResultsByFeature(t, results)
	for name, owner := range map[string]string{"grp": "other", "other": "grp"} {
		r := by[name]["alpha"]
		if !r.Held || r.Error != "" || r.Downloaded {
			t.Errorf("%s alpha result = %+v; %s's record must hold it", name, r, owner)
		}
		want := "held by rollback of " + owner + ": it advances only when " + owner + " updates (or run 'updex features rollback --clear " + owner + "')"
		if r.NextActionMessage != want {
			t.Errorf("%s alpha message = %q, want %q", name, r.NextActionMessage, want)
		}
	}
	f.wantLinks(map[string]string{"alpha": "0.5.0", "beta": "1.0.0"})
	if got := f.gets(); got != 0 {
		t.Errorf("%d image download(s); a held run downloads nothing", got)
	}
	f.readRecord()
	if _, err := os.Lstat(filepath.Join(f.rollbackDir, "other")); err != nil {
		t.Errorf("other's record must stay: %v", err)
	}

	// Clearing one record breaks the tie: the other feature advances, links
	// its transfers together, and deletes its own record.
	if _, err := f.client.RollbackFeature(t.Context(), "other", RollbackFeatureOptions{Clear: true}); err != nil {
		t.Fatalf("RollbackFeature(other, Clear): %v", err)
	}
	results, err = f.update()
	if err != nil {
		t.Fatalf("update after clearing other's record: %v", err)
	}
	by = updateResultsByFeature(t, results)
	for _, component := range []string{"alpha", "beta"} {
		if r := by["grp"][component]; r.Held || r.Error != "" || r.Version != "3.0.0" {
			t.Errorf("grp %s result = %+v", component, r)
		}
	}
	f.wantLinks(map[string]string{"alpha": "3.0.0", "beta": "3.0.0"})
	f.noRecord()
	if _, err := os.Lstat(filepath.Join(f.rollbackDir, "other")); !os.IsNotExist(err) {
		t.Errorf("other's record must stay cleared (err = %v)", err)
	}
	if got := f.gets(); got != 2 {
		t.Errorf("%d image download(s), want 2: grp downloads each new image once", got)
	}
}

// A record component whose transfer is not in the loaded definitions cannot be
// checked for a newer version, so the sharing feature stays held (fail
// closed) instead of advancing on the components it can see.
func TestUpdateFeatures_RecordComponentWithNoTransferHoldsTheFeature(t *testing.T) {
	f := newSplitRollbackFixture(t)
	f.servers["alpha"].publish("3.0.0")
	if err := os.Remove(filepath.Join(f.defDir, "beta.transfer")); err != nil {
		t.Fatal(err)
	}

	results, err := f.update()
	if err != nil {
		t.Fatalf("a held run must not fail: %v", err)
	}
	by := updateResultsByFeature(t, results)
	g := by["grp"]["alpha"]
	if !g.Held || g.Error != "" || g.Downloaded {
		t.Errorf("grp alpha result = %+v", g)
	}
	if !strings.Contains(g.NextActionMessage, "beta") || !strings.Contains(g.NextActionMessage, "no transfer") {
		t.Errorf("grp alpha message = %q; it must name the record component without a transfer", g.NextActionMessage)
	}
	o := by["other"]["alpha"]
	if !o.Held || o.Error != "" || o.Downloaded {
		t.Errorf("other alpha result = %+v", o)
	}
	if want := "held by rollback of grp: it advances only when grp updates (or run 'updex features rollback --clear grp')"; o.NextActionMessage != want {
		t.Errorf("other alpha message = %q, want %q", o.NextActionMessage, want)
	}
	f.wantLinks(map[string]string{"alpha": "1.0.0", "beta": "1.0.0"})
	if got := f.gets(); got != 0 {
		t.Errorf("%d image download(s); a held run downloads nothing", got)
	}
	f.readRecord()
}

// A record component whose versions cannot be read fails closed for the
// feature that owns the record: nothing is downloaded or linked for it, and
// the error names the record and the component. A feature that shares the
// transfer is held by the record instead, and never reads the broken
// component. The record stays.
func TestUpdateFeatures_RecordComponentLookupFailureLinksNothing(t *testing.T) {
	f := newSplitRollbackFixture(t)
	f.servers["alpha"].publish("3.0.0")
	// A 404 manifest is non-transient, so the lookup fails at once.
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(broken.Close)
	createFeatureTransferFileWithoutCurrentSymlink(t, f.defDir, "beta", "grp", broken.URL, f.targetDir)

	results, err := f.update()
	if err == nil {
		t.Fatal("expected an error when a record component cannot be read")
	}
	by := updateResultsByFeature(t, results)
	g := by["grp"]["alpha"]
	if g.Held || g.Downloaded || !strings.Contains(g.Error, "rollback record of feature grp") || !strings.Contains(g.Error, "beta") {
		t.Errorf("grp alpha result = %+v", g)
	}
	o := by["other"]["alpha"]
	if !o.Held || o.Error != "" || o.Downloaded {
		t.Errorf("other alpha result = %+v; the other feature is held by the record", o)
	}
	f.wantLinks(map[string]string{"alpha": "1.0.0", "beta": "1.0.0"})
	if got := f.gets(); got != 0 {
		t.Errorf("%d image download(s); nothing may be downloaded", got)
	}
	f.readRecord()
}

// features list and features check report the hold of the sharing feature,
// including the record component that keeps the owner in force and the reason
// the sharing feature itself may not advance.
func TestFeaturesAndCheck_SharedFeatureHeldByAnotherComponent(t *testing.T) {
	f := newSplitRollbackFixture(t)
	f.servers["alpha"].publish("3.0.0")
	const wantOwnerHeldBy = "grp: beta has no version newer than 2.0.0"
	const wantSharedHeldBy = "grp: it advances only when grp updates (or run 'updex features rollback --clear grp')"
	wantHeldBy := map[string]string{"grp": wantOwnerHeldBy, "other": wantSharedHeldBy}

	infos, err := f.client.Features(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 2 {
		t.Fatalf("features = %+v, want grp and other", infos)
	}
	for _, info := range infos {
		var found bool
		for _, status := range info.TransferStatus {
			if status.Component != "alpha" {
				continue
			}
			found = true
			if status.RollbackRejected != "2.0.0" || status.RollbackHeldBy != wantHeldBy[info.Name] {
				t.Errorf("features list %s = %+v", info.Name, status)
			}
		}
		if !found {
			t.Fatalf("%s has no alpha status: %+v", info.Name, info.TransferStatus)
		}
	}

	check, err := f.client.CheckFeatures(t.Context(), CheckFeaturesOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(check) != 2 {
		t.Fatalf("check = %+v, want grp and other", check)
	}
	for _, c := range check {
		var found bool
		for _, r := range c.Results {
			if r.Component != "alpha" {
				continue
			}
			found = true
			if r.UpdateAvailable || r.Error != "" || r.NewestVersion != "3.0.0" || r.LinkedVersion != "1.0.0" {
				t.Errorf("check %s alpha = %+v", c.Feature, r)
			}
			if want := wantHeldBy[c.Feature]; r.RollbackHeldBy != want {
				t.Errorf("check %s alpha rollback_held_by = %q, want %q", c.Feature, r.RollbackHeldBy, want)
			}
		}
		if !found {
			t.Fatalf("%s has no alpha result: %+v", c.Feature, c.Results)
		}
	}
}

// features list and features check report the hold for a shared transfer: the
// feature that owns the record, and the sharing feature that only it can
// advance.
func TestFeaturesAndCheck_SharedTransferShowsRollbackRejected(t *testing.T) {
	f := newSharedRollbackFixture(t)
	wantHeldBy := map[string]string{
		"grp":   "",
		"other": "grp: it advances only when grp updates (or run 'updex features rollback --clear grp')",
	}

	infos, err := f.client.Features(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 2 {
		t.Fatalf("features = %+v, want grp and other", infos)
	}
	for _, info := range infos {
		if len(info.TransferStatus) != 1 || info.TransferStatus[0].Component != "alpha" {
			t.Fatalf("%s statuses = %+v", info.Name, info.TransferStatus)
		}
		if got := info.TransferStatus[0].RollbackRejected; got != "2.0.0" {
			t.Errorf("features list %s: rollback_rejected = %q, want 2.0.0", info.Name, got)
		}
		if got, want := info.TransferStatus[0].RollbackHeldBy, wantHeldBy[info.Name]; got != want {
			t.Errorf("features list %s: rollback_held_by = %q, want %q", info.Name, got, want)
		}
	}

	check, err := f.client.CheckFeatures(t.Context(), CheckFeaturesOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(check) != 2 {
		t.Fatalf("check = %+v, want grp and other", check)
	}
	for _, c := range check {
		r := c.Results[0]
		if r.RollbackRejected != "2.0.0" || r.UpdateAvailable || r.Error != "" {
			t.Errorf("check %s = %+v; a held transfer has no update available", c.Feature, r)
		}
		if r.NewestVersion != "2.0.0" || r.LinkedVersion != "1.0.0" {
			t.Errorf("check %s = %+v", c.Feature, r)
		}
		if got, want := r.RollbackHeldBy, wantHeldBy[c.Feature]; got != want {
			t.Errorf("check %s: rollback_held_by = %q, want %q", c.Feature, got, want)
		}
	}
}

// The vacuum keeps the rejected version of a shared transfer while the record
// exists, when the feature that owns the record updates.
func TestEnableFeatureNow_SharedTransferVacuumKeepsRejectedVersion(t *testing.T) {
	f := newSharedRollbackFixture(t)
	// Staged by hand: 4.0.0 and 5.0.0 (e.g. from a manual download).
	f.setup("alpha", "1.0.0", "1.0.0", "2.0.0", "4.0.0", "5.0.0")
	f.servers["alpha"].publish("6.0.0")

	if _, err := f.client.EnableFeature(t.Context(), "grp", EnableFeatureOptions{Now: true, NoRefresh: true}); err != nil {
		t.Fatalf("EnableFeature: %v", err)
	}
	if got := f.stagedVersions("alpha"); !slices.Equal(got, []string{"2.0.0", "5.0.0", "6.0.0"}) {
		t.Errorf("staged = %v, want 2.0.0 (rejected), 5.0.0 and 6.0.0", got)
	}
	f.wantLinks(map[string]string{"alpha": "6.0.0"})
	f.noRecord()
}

// EnableFeature --now honours the record too.
func TestEnableFeatureNow_HeldByRollbackLinksNothing(t *testing.T) {
	f := newHeldFixture(t, "alpha")
	result, err := f.client.EnableFeature(t.Context(), "grp", EnableFeatureOptions{Now: true, NoRefresh: true})
	if err != nil || !result.Success {
		t.Fatalf("EnableFeature = %+v, %v", result, err)
	}
	f.wantLinks(map[string]string{"alpha": "2.0.0"})
	if len(result.DownloadedFiles) != 0 {
		t.Errorf("downloaded = %v", result.DownloadedFiles)
	}
}

// --- status ---

func TestFeaturesAndCheckReportRollbackHold(t *testing.T) {
	f := newHeldFixture(t, "alpha")

	infos, err := f.client.Features(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 || len(infos[0].TransferStatus) != 1 || infos[0].TransferStatus[0].RollbackRejected != "3.0.0" {
		t.Fatalf("features = %+v", infos)
	}

	check, err := f.client.CheckFeatures(t.Context(), CheckFeaturesOptions{})
	if err != nil {
		t.Fatal(err)
	}
	r := check[0].Results[0]
	if r.RollbackRejected != "3.0.0" || r.UpdateAvailable || r.NewestVersion != "3.0.0" || r.LinkedVersion != "2.0.0" {
		t.Errorf("check = %+v; a held transfer has no update available", r)
	}

	// A newer revision is an update again.
	f.servers["alpha"].publish("4.0.0")
	check, err = f.client.CheckFeatures(t.Context(), CheckFeaturesOptions{})
	if err != nil || !check[0].Results[0].UpdateAvailable {
		t.Errorf("check after a newer revision = %+v, %v", check, err)
	}

	// No record, no field.
	if _, err := f.rollback(RollbackFeatureOptions{Clear: true}); err != nil {
		t.Fatal(err)
	}
	infos, _ = f.client.Features(t.Context())
	if infos[0].TransferStatus[0].RollbackRejected != "" {
		t.Errorf("rollback_rejected = %q after --clear", infos[0].TransferStatus[0].RollbackRejected)
	}
}

func TestCheckFeaturesUnreadableRecordIsAComponentError(t *testing.T) {
	f := newHeldFixture(t, "alpha")
	writeTestFile(t, f.recordPath(), "garbage")

	check, err := f.client.CheckFeatures(t.Context(), CheckFeaturesOptions{})
	if err == nil {
		t.Fatal("expected an error")
	}
	if r := check[0].Results[0]; !strings.Contains(r.Error, "rollback record") || r.UpdateAvailable {
		t.Errorf("check = %+v", r)
	}
}

// --- defaults ---

func TestRollbackDirDefaults(t *testing.T) {
	if DefaultRollbackDir != "/var/lib/updex/rollback" {
		t.Errorf("DefaultRollbackDir = %q", DefaultRollbackDir)
	}
	if got := resolveRuntimePaths(RuntimePaths{}).rollbackDir; got != rollbackDirDefault {
		t.Errorf("zero RollbackDir resolved to %q, want %q", got, rollbackDirDefault)
	}
	if got := resolveRuntimePaths(RuntimePaths{RollbackDir: "/x/r"}).rollbackDir; got != "/x/r" {
		t.Errorf("RollbackDir resolved to %q", got)
	}
	if rollbackDirDefault == DefaultRollbackDir {
		t.Errorf("tests use the production rollback directory %q", DefaultRollbackDir)
	}
}
