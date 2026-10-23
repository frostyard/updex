package updex

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostyard/clix"
	"github.com/frostyard/updex/v2/sysext"
	"github.com/frostyard/updex/v2/updex"
	"github.com/spf13/cobra"
)

// rollbackFlags are the inputs of runFeaturesRollback.
type rollbackFlags struct {
	to        string
	now       bool
	clear     bool
	dryRun    bool
	json      bool
	root      bool
	noRefresh bool
}

// runRollback runs `features rollback <name>` against the host with the given
// flags, restoring every global afterwards.
func (h *statusHost) runRollback(t *testing.T, name string, fl rollbackFlags) (string, error) {
	t.Helper()
	oldDefinitions, oldComponent := definitions, featureComponent
	oldTo, oldNow, oldClear := featureRollbackTo, featureRollbackNow, featureRollbackClear
	oldJSON, oldSilent, oldDry, oldNoRefresh, oldEUID := clix.JSONOutput, clix.Silent, clix.DryRun, noRefresh, getEUID
	t.Cleanup(func() {
		definitions, featureComponent = oldDefinitions, oldComponent
		featureRollbackTo, featureRollbackNow, featureRollbackClear = oldTo, oldNow, oldClear
		clix.JSONOutput, clix.Silent, clix.DryRun, noRefresh, getEUID = oldJSON, oldSilent, oldDry, oldNoRefresh, oldEUID
	})
	definitions, featureComponent = h.configDir, ""
	featureRollbackTo, featureRollbackNow, featureRollbackClear = fl.to, fl.now, fl.clear
	clix.JSONOutput, clix.Silent, clix.DryRun, noRefresh = fl.json, true, fl.dryRun, fl.noRefresh
	getEUID = func() int {
		if fl.root {
			return 0
		}
		return 1000
	}

	return captureStdout(t, func() error {
		cmd := &cobra.Command{}
		cmd.SetContext(t.Context())
		return runFeaturesRollback(cmd, []string{name})
	})
}

func (h *statusHost) record(component string) string { return filepath.Join(h.rollbackDir, component) }

func (h *statusHost) linkVersion(t *testing.T, component string) string {
	t.Helper()
	target, err := os.Readlink(filepath.Join(h.linkDir, component+".raw"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSuffix(strings.TrimPrefix(filepath.Base(target), component+"_"), ".raw")
}

// rolledBackHost has alpha staged at 1.0.0 and 2.0.0 and linked to 2.0.0; the
// server's newest version is 2.0.0.
func rolledBackHost(t *testing.T) *statusHost {
	t.Helper()
	h := newStatusHost(t)
	h.feature(t, "alpha", true, "2.0.0", "1.0.0", "2.0.0")
	return h
}

func TestRunFeaturesRollback_Text(t *testing.T) {
	h := rolledBackHost(t)

	out, err := h.runRollback(t, "alpha", rollbackFlags{root: true})
	if err != nil {
		t.Fatalf("runFeaturesRollback: %v\n%s", err, out)
	}
	fields := rowFields(t, out, "alpha")
	if len(fields) != 5 || fields[1] != "alpha" || fields[2] != "2.0.0" || fields[3] != "1.0.0" || fields[4] != "2.0.0" {
		t.Errorf("table row = %v, want alpha alpha 2.0.0 1.0.0 2.0.0\n%s", fields, out)
	}
	if !strings.Contains(out, "next boot") || !strings.Contains(out, "rollback --clear alpha") {
		t.Errorf("output must say when it takes effect and how to resume:\n%s", out)
	}
	if got := h.linkVersion(t, "alpha"); got != "1.0.0" {
		t.Errorf("link = %s, want 1.0.0", got)
	}
	if _, err := os.Stat(h.record("alpha")); err != nil {
		t.Errorf("record: %v", err)
	}
}

func TestRunFeaturesRollback_JSON(t *testing.T) {
	h := rolledBackHost(t)

	out, err := h.runRollback(t, "alpha", rollbackFlags{root: true, json: true, to: "1.0.0"})
	if err != nil {
		t.Fatalf("runFeaturesRollback: %v", err)
	}
	var result updex.RollbackFeatureResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if !result.Success || result.Action != "rollback" || len(result.Transfers) != 1 ||
		result.Transfers[0].ToVersion != "1.0.0" || result.Transfers[0].RejectedVersion != "2.0.0" || result.Record != h.record("alpha") {
		t.Errorf("result = %+v", result)
	}
	var raw map[string]any
	_ = json.Unmarshal([]byte(out), &raw)
	for _, key := range []string{"feature", "action", "success", "transfers", "record", "next_action_message"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("JSON has no %q key:\n%s", key, out)
		}
	}
}

func TestRunFeaturesRollback_Clear(t *testing.T) {
	h := rolledBackHost(t)
	if _, err := h.runRollback(t, "alpha", rollbackFlags{root: true}); err != nil {
		t.Fatal(err)
	}

	out, err := h.runRollback(t, "alpha", rollbackFlags{root: true, clear: true})
	if err != nil || !strings.Contains(out, "deleted") {
		t.Fatalf("clear = %q, %v", out, err)
	}
	if _, err := os.Lstat(h.record("alpha")); !os.IsNotExist(err) {
		t.Errorf("record still exists (err = %v)", err)
	}
	if got := h.linkVersion(t, "alpha"); got != "1.0.0" {
		t.Errorf("--clear moved the link to %s", got)
	}

	out, err = h.runRollback(t, "alpha", rollbackFlags{root: true, clear: true, json: true})
	if err != nil {
		t.Fatalf("second clear: %v", err)
	}
	var result updex.RollbackFeatureResult
	if err := json.Unmarshal([]byte(out), &result); err != nil || !result.NoRecord || !result.Success || result.Action != "rollback-clear" {
		t.Errorf("second clear = %+v, %v\n%s", result, err, out)
	}
}

// A dry run needs no root and writes nothing; a real run needs root.
func TestRunFeaturesRollback_RootAndDryRun(t *testing.T) {
	h := rolledBackHost(t)

	out, err := h.runRollback(t, "alpha", rollbackFlags{dryRun: true})
	if err != nil {
		t.Fatalf("dry run without root: %v", err)
	}
	if !strings.Contains(out, "[DRY RUN]") || !strings.Contains(out, "1.0.0") {
		t.Errorf("dry-run output:\n%s", out)
	}
	if _, err := os.Lstat(h.rollbackDir); !os.IsNotExist(err) {
		t.Errorf("dry run created the rollback directory (err = %v)", err)
	}
	if got := h.linkVersion(t, "alpha"); got != "2.0.0" {
		t.Errorf("dry run moved the link to %s", got)
	}

	if _, err := h.runRollback(t, "alpha", rollbackFlags{}); err == nil || !strings.Contains(err.Error(), "requires root") {
		t.Errorf("real run without root: %v, want a root error", err)
	}
}

func TestRunFeaturesRollback_ErrorChangesNothing(t *testing.T) {
	h := rolledBackHost(t)
	out, err := h.runRollback(t, "alpha", rollbackFlags{root: true, to: "0.1.0"})
	if err == nil || !strings.Contains(err.Error(), "version 0.1.0 of alpha is not on disk") {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(out, "rolled back") {
		t.Errorf("output must not claim success:\n%s", out)
	}
	if _, err := os.Lstat(h.rollbackDir); !os.IsNotExist(err) {
		t.Errorf("rollback directory created (err = %v)", err)
	}

	// JSON still carries the failed result and the command fails.
	out, err = h.runRollback(t, "alpha", rollbackFlags{root: true, to: "0.1.0", json: true})
	if err == nil {
		t.Fatal("expected an error")
	}
	var result updex.RollbackFeatureResult
	if jerr := json.Unmarshal([]byte(out), &result); jerr != nil || result.Success || result.Error == "" {
		t.Errorf("result = %+v, %v\n%s", result, jerr, out)
	}
}

// --now refreshes through the runner, --no-refresh skips it.
func TestRunFeaturesRollback_Now(t *testing.T) {
	h := rolledBackHost(t)
	mock := &sysext.MockRunner{}
	oldRunner := sysextRunner
	sysextRunner = mock
	t.Cleanup(func() { sysextRunner = oldRunner })

	out, err := h.runRollback(t, "alpha", rollbackFlags{root: true, now: true})
	if err != nil {
		t.Fatalf("--now: %v", err)
	}
	if !mock.RefreshCalled || len(mock.LinkVersionAtCalls) != 1 || mock.LinkVersionAtCalls[0] != "1.0.0" {
		t.Errorf("refresh = %v, versions linked = %v", mock.RefreshCalled, mock.LinkVersionAtCalls)
	}
	if strings.Contains(out, "next boot") {
		t.Errorf("output after a refresh must not say next boot:\n%s", out)
	}

	// The mock links nothing, so the link still names 2.0.0 and a second
	// rollback is possible; --no-refresh skips the refresh.
	mock2 := &sysext.MockRunner{}
	sysextRunner = mock2
	if _, err := h.runRollback(t, "alpha", rollbackFlags{root: true, now: true, noRefresh: true}); err != nil {
		t.Fatalf("--now --no-refresh: %v", err)
	}
	if mock2.RefreshCalled || len(mock2.LinkVersionAtCalls) != 1 {
		t.Errorf("refresh = %v, versions linked = %v; want a link and no refresh", mock2.RefreshCalled, mock2.LinkVersionAtCalls)
	}
}

// The lock applies to rollback, also under systemd.
func TestRunFeaturesRollback_LockHeld(t *testing.T) {
	h := rolledBackHost(t)
	t.Setenv("INVOCATION_ID", "0123456789abcdef0123456789abcdef")
	holdLock(t)

	_, err := h.runRollback(t, "alpha", rollbackFlags{root: true})
	if !errors.Is(err, updex.ErrBusy) {
		t.Fatalf("error = %v, want ErrBusy", err)
	}
}

// After a rollback, update reports the hold and exits 0; check and list mark
// it.
func TestRollbackHoldShowsInUpdateCheckAndList(t *testing.T) {
	h := rolledBackHost(t)
	if _, err := h.runRollback(t, "alpha", rollbackFlags{root: true}); err != nil {
		t.Fatal(err)
	}

	// features update
	oldNoRefresh, oldDry, oldEUID := noRefresh, clix.DryRun, getEUID
	t.Cleanup(func() { noRefresh, clix.DryRun, getEUID = oldNoRefresh, oldDry, oldEUID })
	noRefresh, clix.DryRun, getEUID = true, false, func() int { return 0 }
	out, err := h.run(t, false, runFeaturesUpdate)
	if err != nil {
		t.Fatalf("a held update must exit 0: %v\n%s", err, out)
	}
	if !strings.Contains(out, "held by rollback: rejected 2.0.0") {
		t.Errorf("update output must report the hold:\n%s", out)
	}
	if got := h.linkVersion(t, "alpha"); got != "1.0.0" {
		t.Errorf("update moved the link to %s", got)
	}

	// JSON update carries the fields.
	out, err = h.run(t, true, runFeaturesUpdate)
	if err != nil {
		t.Fatal(err)
	}
	var results []updex.UpdateFeaturesResult
	if err := json.Unmarshal([]byte(out), &results); err != nil || len(results) != 1 {
		t.Fatalf("update JSON: %v\n%s", err, out)
	}
	if r := results[0].Results[0]; !r.Held || r.RejectedVersion != "2.0.0" || r.Error != "" {
		t.Errorf("update result = %+v", r)
	}

	// features check
	out, err = h.run(t, false, runFeaturesCheck)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !strings.Contains(out, "held (rejected 2.0.0)") {
		t.Errorf("check must mark the hold:\n%s", out)
	}
	out, _ = h.run(t, true, runFeaturesCheck)
	var checks []updex.CheckFeaturesResult
	if err := json.Unmarshal([]byte(out), &checks); err != nil || checks[0].Results[0].RollbackRejected != "2.0.0" || checks[0].Results[0].UpdateAvailable {
		t.Errorf("check JSON = %+v, %v\n%s", checks, err, out)
	}

	// features list
	out, err = h.run(t, false, runFeaturesList)
	if err != nil || !strings.Contains(out, "HELD BY ROLLBACK") || !strings.Contains(out, "alpha (rejected 2.0.0)") {
		t.Errorf("list must mark the hold: %v\n%s", err, out)
	}
	out, _ = h.run(t, true, runFeaturesList)
	var infos []updex.FeatureInfo
	if err := json.Unmarshal([]byte(out), &infos); err != nil || infos[0].TransferStatus[0].RollbackRejected != "2.0.0" {
		t.Errorf("list JSON = %+v, %v\n%s", infos, err, out)
	}
}

func TestFeaturesRollbackCommandIsRegistered(t *testing.T) {
	root := NewRootCmd()
	cmd, _, err := root.Find([]string{"features", "rollback"})
	if err != nil || cmd.Name() != "rollback" {
		t.Fatalf("features rollback not found: %v", err)
	}
	for _, flag := range []string{"to", "now", "clear"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("flag --%s is missing", flag)
		}
	}
}
