package updex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostyard/updex/v2/updex"
	"github.com/spf13/cobra"
)

// suiteSwitchFixture sets the globals of `features update --suite-switch`
// and a definitions directory with feature "grp" whose transfer "foo" selects
// the suite with %w. os-release says VERSION_ID=14; the sysext link points to
// linkTarget in the staging directory ("" = no link).
func suiteSwitchFixture(t *testing.T, linkTarget string) {
	t.Helper()
	busyUpdateFixture(t, false)
	noRefresh = false
	old := featureUpdateSuiteSwitch
	t.Cleanup(func() { featureUpdateSuiteSwitch = old })
	featureUpdateSuiteSwitch = true

	targetDir := t.TempDir()
	writeFeatureFile(t, definitions, "grp", true)
	transfer := "[Transfer]\nFeatures=grp\nVerify=false\n\n[Source]\nType=url-file\nPath=http://127.0.0.1:1\n" +
		"MatchPattern=foo_@v_%w.raw\n\n[Target]\nPath=" + targetDir + "\nMatchPattern=foo_@v_%w.raw\n"
	if err := os.WriteFile(filepath.Join(definitions, "foo.transfer"), []byte(transfer), 0o644); err != nil {
		t.Fatal(err)
	}
	osRelease := filepath.Join(t.TempDir(), "os-release")
	if err := os.WriteFile(osRelease, []byte("ID=debian\nVERSION_ID=14\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	linkDir := t.TempDir()
	if linkTarget != "" {
		if err := os.WriteFile(filepath.Join(targetDir, linkTarget), nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(targetDir, linkTarget), filepath.Join(linkDir, "foo.raw")); err != nil {
			t.Fatal(err)
		}
	}
	setRuntimePaths(t, updex.RuntimePaths{OSReleasePaths: []string{osRelease}, SysextLinkDir: linkDir})
}

func runSuiteSwitch(t *testing.T, args []string) (string, error) {
	t.Helper()
	return captureStdout(t, func() error {
		cmd := &cobra.Command{}
		cmd.SetContext(t.Context())
		return runFeaturesUpdate(cmd, args)
	})
}

// --suite-switch selects its own features and exists to refresh.
func TestRunFeaturesUpdate_SuiteSwitchRefusesNamesAndNoRefresh(t *testing.T) {
	suiteSwitchFixture(t, "foo_1.0_13.raw")
	if _, err := runSuiteSwitch(t, []string{"grp"}); err == nil || !strings.Contains(err.Error(), "--suite-switch takes no feature names") {
		t.Errorf("with a name: error = %v", err)
	}
	noRefresh = true
	if _, err := runSuiteSwitch(t, nil); err == nil || !strings.Contains(err.Error(), "cannot be combined with --no-refresh") {
		t.Errorf("with --no-refresh: error = %v", err)
	}
}

// A boot without a suite switch prints one line and succeeds.
func TestRunFeaturesUpdate_SuiteSwitchNothingToDo(t *testing.T) {
	suiteSwitchFixture(t, "foo_1.0_14.raw")
	out, err := runSuiteSwitch(t, nil)
	if err != nil {
		t.Fatalf("runFeaturesUpdate(--suite-switch) error = %v", err)
	}
	if strings.TrimSpace(out) != "No sysext link points to an image of another suite; nothing to do." {
		t.Errorf("stdout = %q", out)
	}
}

// Under systemd, a suite-switch run that finds the lock taken fails: the
// failure makes updex-suite-switch.service restart (bounded, ADR-0022) and
// shows it to the operator.
func TestRunFeaturesUpdate_SuiteSwitchLockHeldUnderSystemdFails(t *testing.T) {
	suiteSwitchFixture(t, "foo_1.0_13.raw")
	t.Setenv("INVOCATION_ID", "0123456789abcdef0123456789abcdef")
	holdLock(t)

	if _, err := runSuiteSwitch(t, nil); err == nil || !strings.Contains(err.Error(), "another updex operation is running") {
		t.Errorf("runFeaturesUpdate(--suite-switch) error = %v, want the busy error", err)
	}
}

// The flag is registered on `features update`.
func TestFeaturesUpdateCmdHasSuiteSwitchFlag(t *testing.T) {
	flag := newFeaturesUpdateCmd().Flags().Lookup("suite-switch")
	if flag == nil || flag.DefValue != "false" {
		t.Fatalf("--suite-switch flag = %+v, want a boolean flag defaulting to false", flag)
	}
}
