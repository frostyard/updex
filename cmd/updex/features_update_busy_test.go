package updex

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/frostyard/clix"
	"github.com/frostyard/updex/v2/updex"
	"github.com/spf13/cobra"
)

// holdLock takes the exclusive flock on the CLI tests' lock file, as another
// updex process would, and releases it when the test ends.
func holdLock(t *testing.T) {
	t.Helper()
	path := runtimePaths.LockPath
	if path == "" || path == updex.DefaultLockPath {
		t.Fatalf("runtimePaths.LockPath = %q: handler tests must use a temporary lock", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		t.Fatalf("hold lock: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })
}

// busyUpdateFixture prepares the globals runFeaturesUpdate reads: root, no
// dry run, and an empty definitions directory.
func busyUpdateFixture(t *testing.T, json bool) {
	t.Helper()
	oldDefinitions, oldNoRefresh, oldDryRun, oldJSON, oldEUID := definitions, noRefresh, clix.DryRun, clix.JSONOutput, getEUID
	t.Cleanup(func() {
		definitions, noRefresh, clix.DryRun, clix.JSONOutput, getEUID = oldDefinitions, oldNoRefresh, oldDryRun, oldJSON, oldEUID
	})
	definitions = t.TempDir()
	noRefresh = true
	clix.DryRun = false
	clix.JSONOutput = json
	getEUID = func() int { return 0 }
}

func runUpdateCapturing(t *testing.T) (stdout, stderr string, err error) {
	t.Helper()
	var errBuf bytes.Buffer
	stdout, err = captureStdout(t, func() error {
		cmd := &cobra.Command{}
		cmd.SetContext(t.Context())
		cmd.SetErr(&errBuf)
		return runFeaturesUpdate(cmd, nil)
	})
	return stdout, errBuf.String(), err
}

// Under systemd (INVOCATION_ID is set), a scheduled run that finds the lock
// taken exits 0 with one log line and prints no table.
func TestRunFeaturesUpdate_LockHeldUnderSystemdExitsZero(t *testing.T) {
	busyUpdateFixture(t, false)
	t.Setenv("INVOCATION_ID", "0123456789abcdef0123456789abcdef")
	holdLock(t)

	stdout, stderr, err := runUpdateCapturing(t)
	if err != nil {
		t.Fatalf("runFeaturesUpdate under systemd with the lock held: %v, want nil", err)
	}
	if strings.Count(stderr, "\n") != 1 || !strings.Contains(stderr, "another updex operation is running") || !strings.Contains(stderr, "skipping this run") {
		t.Errorf("stderr = %q, want one log line saying the run was skipped", stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing", stdout)
	}
}

// Under systemd with --json, stdout stays a valid JSON array.
func TestRunFeaturesUpdate_LockHeldUnderSystemdJSONEmitsArray(t *testing.T) {
	busyUpdateFixture(t, true)
	t.Setenv("INVOCATION_ID", "0123456789abcdef0123456789abcdef")
	holdLock(t)

	stdout, _, err := runUpdateCapturing(t)
	if err != nil {
		t.Fatalf("runFeaturesUpdate: %v, want nil", err)
	}
	if strings.TrimSpace(stdout) != "[]" {
		t.Errorf("stdout = %q, want []", stdout)
	}
}

// In an interactive shell (no INVOCATION_ID) the same condition is an error
// and the command exits non-zero.
func TestRunFeaturesUpdate_LockHeldInteractiveIsAnError(t *testing.T) {
	busyUpdateFixture(t, false)
	t.Setenv("INVOCATION_ID", "")
	holdLock(t)

	stdout, stderr, err := runUpdateCapturing(t)
	if !errors.Is(err, updex.ErrBusy) {
		t.Fatalf("runFeaturesUpdate interactive with the lock held: %v, want ErrBusy", err)
	}
	if strings.Contains(stderr, "skipping this run") || stdout != "" {
		t.Errorf("interactive run must not log a skip: stdout=%q stderr=%q", stdout, stderr)
	}
}

// The exit-0 rule belongs to the scheduled update only: every other mutating
// command still fails when the lock is taken, also under systemd.
func TestRunFeaturesEnable_LockHeldUnderSystemdStillFails(t *testing.T) {
	busyUpdateFixture(t, false)
	t.Setenv("INVOCATION_ID", "0123456789abcdef0123456789abcdef")
	holdLock(t)

	_, err := captureStdout(t, func() error {
		cmd := &cobra.Command{}
		cmd.SetContext(t.Context())
		return runFeaturesEnable(cmd, []string{"anything"})
	})
	if !errors.Is(err, updex.ErrBusy) {
		t.Fatalf("runFeaturesEnable with the lock held: %v, want ErrBusy", err)
	}
}

// A dry run takes no lock, so it works while another operation runs.
func TestRunFeaturesUpdate_DryRunIgnoresLock(t *testing.T) {
	busyUpdateFixture(t, false)
	clix.DryRun = true
	t.Setenv("INVOCATION_ID", "")
	holdLock(t)

	if _, _, err := runUpdateCapturing(t); err != nil {
		t.Fatalf("dry run with the lock held: %v, want nil", err)
	}
}

func TestRunningUnderSystemd(t *testing.T) {
	t.Setenv("INVOCATION_ID", "")
	if runningUnderSystemd() {
		t.Error("runningUnderSystemd() = true with an empty INVOCATION_ID")
	}
	t.Setenv("INVOCATION_ID", "abc")
	if !runningUnderSystemd() {
		t.Error("runningUnderSystemd() = false with INVOCATION_ID set")
	}
}
