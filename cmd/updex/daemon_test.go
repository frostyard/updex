package updex

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostyard/clix"
	"github.com/frostyard/updex/systemd"
	sdk "github.com/frostyard/updex/updex"
	"github.com/spf13/cobra"
)

const daemonTestUnitName = "updex-update"

func daemonTestCommand(t *testing.T) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.SetContext(t.Context())
	return cmd
}

func TestDaemonHandlersStayBehindSDKBoundary(t *testing.T) {
	source, err := os.ReadFile("daemon.go")
	if err != nil {
		t.Fatalf("read daemon command source: %v", err)
	}
	contents := string(source)
	if strings.Contains(contents, `"github.com/frostyard/updex/systemd"`) {
		t.Fatal("daemon command must not import systemd directly")
	}
	for _, method := range []string{".EnableDaemon(", ".DisableDaemon(", ".DaemonStatus("} {
		if !strings.Contains(contents, method) {
			t.Errorf("daemon command does not call SDK method %s", method)
		}
	}
}

// daemonTestEnv installs injectable seams for the daemon command tests: an
// in-memory systemd Manager rooted at a temp dir and a single MockSystemctlRunner
// shared by the manager and the command runner, plus a root-privileged getEUID.
// It never writes real unit files or invokes systemctl.
func daemonTestEnv(t *testing.T, mock *systemd.MockSystemctlRunner) (unitDir string) {
	t.Helper()
	unitDir = t.TempDir()

	oldManager := systemdManager
	oldGetEUID, oldJSON := getEUID, clix.JSONOutput
	t.Cleanup(func() {
		systemdManager = oldManager
		getEUID = oldGetEUID
		clix.JSONOutput = oldJSON
	})

	systemdManager = systemd.NewTestManager(unitDir, mock)
	getEUID = func() int { return 0 }
	clix.JSONOutput = false
	return unitDir
}

func seedUnits(t *testing.T, dir string) {
	t.Helper()
	for _, ext := range []string{".timer", ".service"} {
		if err := os.WriteFile(filepath.Join(dir, daemonTestUnitName+ext), []byte("stub"), 0644); err != nil {
			t.Fatalf("failed to seed unit file: %v", err)
		}
	}
}

func TestRunDaemonEnable_RejectsNonRoot(t *testing.T) {
	daemonTestEnv(t, &systemd.MockSystemctlRunner{})
	getEUID = func() int { return 1000 }

	err := runDaemonEnable(daemonTestCommand(t), nil)
	if err == nil || !strings.Contains(err.Error(), "root privileges") {
		t.Fatalf("expected root-privileges error, got: %v", err)
	}
}

func TestRunDaemonDisable_RejectsNonRoot(t *testing.T) {
	daemonTestEnv(t, &systemd.MockSystemctlRunner{})
	getEUID = func() int { return 1000 }

	err := runDaemonDisable(daemonTestCommand(t), nil)
	if err == nil || !strings.Contains(err.Error(), "root privileges") {
		t.Fatalf("expected root-privileges error, got: %v", err)
	}
}

func TestRunDaemonEnable_AlreadyInstalled(t *testing.T) {
	mock := &systemd.MockSystemctlRunner{}
	dir := daemonTestEnv(t, mock)
	seedUnits(t, dir)

	err := runDaemonEnable(daemonTestCommand(t), nil)
	if err == nil || !strings.Contains(err.Error(), "already installed") {
		t.Fatalf("expected already-installed error, got: %v", err)
	}
	if mock.EnableCalled || mock.StartCalled {
		t.Error("enable must not touch systemctl when the timer is already installed")
	}
}

func TestRunDaemonEnable_Success(t *testing.T) {
	mock := &systemd.MockSystemctlRunner{}
	dir := daemonTestEnv(t, mock)
	clix.JSONOutput = true

	out, err := captureStdout(t, func() error { return runDaemonEnable(daemonTestCommand(t), nil) })
	if err != nil {
		t.Fatalf("enable failed: %v", err)
	}

	for _, ext := range []string{".timer", ".service"} {
		if _, statErr := os.Stat(filepath.Join(dir, daemonTestUnitName+ext)); statErr != nil {
			t.Errorf("expected %s unit file to be written: %v", ext, statErr)
		}
	}
	if !mock.DaemonReloadCalled {
		t.Error("expected daemon-reload after install")
	}
	serviceContent, readErr := os.ReadFile(filepath.Join(dir, daemonTestUnitName+".service"))
	if readErr != nil {
		t.Fatalf("read written service unit: %v", readErr)
	}
	for _, expected := range []string{
		"Type=oneshot",
		"ExecStart=/usr/bin/updex features update --no-refresh",
		"NoNewPrivileges=yes",
		"ProtectSystem=full",
	} {
		if !strings.Contains(string(serviceContent), expected+"\n") {
			t.Errorf("written service unit missing %q\nGot:\n%s", expected, serviceContent)
		}
	}
	if mock.EnableUnit != daemonTestUnitName+".timer" || !mock.EnableCalled {
		t.Errorf("expected timer to be enabled, got called=%v unit=%q", mock.EnableCalled, mock.EnableUnit)
	}
	if mock.StartUnit != daemonTestUnitName+".timer" || !mock.StartCalled {
		t.Errorf("expected timer to be started, got called=%v unit=%q", mock.StartCalled, mock.StartUnit)
	}

	var payload map[string]any
	if jsonErr := json.Unmarshal([]byte(out), &payload); jsonErr != nil {
		t.Fatalf("enable JSON output invalid: %v\n%s", jsonErr, out)
	}
	if payload["success"] != true {
		t.Errorf("expected success=true in JSON output, got: %v", out)
	}
}

func TestRunDaemonEnable_InstallFailure(t *testing.T) {
	mock := &systemd.MockSystemctlRunner{}
	daemonTestEnv(t, mock)
	// Point the manager at a path that cannot be written so Install fails.
	systemdManager = systemd.NewTestManager(filepath.Join(t.TempDir(), "does", "not", "exist"), mock)

	err := runDaemonEnable(daemonTestCommand(t), nil)
	if err == nil || !strings.Contains(err.Error(), "failed to install timer") {
		t.Fatalf("expected install failure, got: %v", err)
	}
	if mock.EnableCalled {
		t.Error("enable must not proceed to systemctl when install fails")
	}
}

func TestRunDaemonEnable_EnableFailure(t *testing.T) {
	mock := &systemd.MockSystemctlRunner{EnableErr: errors.New("boom")}
	daemonTestEnv(t, mock)

	err := runDaemonEnable(daemonTestCommand(t), nil)
	if err == nil || !strings.Contains(err.Error(), "failed to enable timer") {
		t.Fatalf("expected enable failure, got: %v", err)
	}
	if mock.StartCalled {
		t.Error("start must not run after enable fails")
	}
}

func TestRunDaemonEnable_StartFailure(t *testing.T) {
	mock := &systemd.MockSystemctlRunner{StartErr: errors.New("boom")}
	daemonTestEnv(t, mock)

	err := runDaemonEnable(daemonTestCommand(t), nil)
	if err == nil || !strings.Contains(err.Error(), "failed to start timer") {
		t.Fatalf("expected start failure, got: %v", err)
	}
}

func TestRunDaemonDisable_NotInstalled(t *testing.T) {
	mock := &systemd.MockSystemctlRunner{}
	daemonTestEnv(t, mock)

	err := runDaemonDisable(daemonTestCommand(t), nil)
	if err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("expected not-installed error, got: %v", err)
	}
}

func TestRunDaemonDisable_Success(t *testing.T) {
	mock := &systemd.MockSystemctlRunner{}
	dir := daemonTestEnv(t, mock)
	seedUnits(t, dir)
	clix.JSONOutput = true

	out, err := captureStdout(t, func() error { return runDaemonDisable(daemonTestCommand(t), nil) })
	if err != nil {
		t.Fatalf("disable failed: %v", err)
	}

	for _, ext := range []string{".timer", ".service"} {
		if _, statErr := os.Stat(filepath.Join(dir, daemonTestUnitName+ext)); !os.IsNotExist(statErr) {
			t.Errorf("expected %s unit file to be removed", ext)
		}
	}
	if !mock.StopCalled || !mock.DisableCalled {
		t.Error("expected timer to be stopped and disabled before removal")
	}

	var payload map[string]any
	if jsonErr := json.Unmarshal([]byte(out), &payload); jsonErr != nil {
		t.Fatalf("disable JSON output invalid: %v\n%s", jsonErr, out)
	}
	if payload["success"] != true {
		t.Errorf("expected success=true in JSON output, got: %v", out)
	}
}

func TestRunDaemonStatus_NotInstalled(t *testing.T) {
	mock := &systemd.MockSystemctlRunner{}
	daemonTestEnv(t, mock)
	clix.JSONOutput = true

	out, err := captureStdout(t, func() error { return runDaemonStatus(daemonTestCommand(t), nil) })
	if err != nil {
		t.Fatalf("status failed: %v", err)
	}

	var status sdk.DaemonStatusResult
	if jsonErr := json.Unmarshal([]byte(out), &status); jsonErr != nil {
		t.Fatalf("status JSON output invalid: %v\n%s", jsonErr, out)
	}
	if status.Installed {
		t.Errorf("expected installed=false, got: %+v", status)
	}
	if mock.IsEnabledCalled || mock.IsActiveCalled {
		t.Error("status must not query systemctl when the timer is not installed")
	}
}

func TestRunDaemonStatus_Installed(t *testing.T) {
	mock := &systemd.MockSystemctlRunner{IsEnabledResult: true, IsActiveResult: true}
	dir := daemonTestEnv(t, mock)
	seedUnits(t, dir)
	clix.JSONOutput = true

	out, err := captureStdout(t, func() error { return runDaemonStatus(daemonTestCommand(t), nil) })
	if err != nil {
		t.Fatalf("status failed: %v", err)
	}

	var status sdk.DaemonStatusResult
	if jsonErr := json.Unmarshal([]byte(out), &status); jsonErr != nil {
		t.Fatalf("status JSON output invalid: %v\n%s", jsonErr, out)
	}
	if !status.Installed || !status.Enabled || !status.Active || status.Schedule != "daily" {
		t.Errorf("unexpected status: %+v", status)
	}
}

func TestRunDaemonStatus_ReturnsQueryErrors(t *testing.T) {
	queryErr := errors.New("query failed")
	tests := []struct {
		name    string
		mock    *systemd.MockSystemctlRunner
		context string
	}{
		{name: "enabled", mock: &systemd.MockSystemctlRunner{IsEnabledErr: queryErr}, context: "query enabled state"},
		{name: "active", mock: &systemd.MockSystemctlRunner{IsEnabledResult: true, IsActiveErr: queryErr}, context: "query active state"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := daemonTestEnv(t, test.mock)
			seedUnits(t, dir)
			clix.JSONOutput = true

			cmd := newDaemonCmd()
			cmd.SetArgs([]string{"status"})
			out, err := captureStdout(t, cmd.Execute)
			if !errors.Is(err, queryErr) || !strings.Contains(err.Error(), test.context) {
				t.Fatalf("daemon status error = %v, want contextual query failure", err)
			}
			if out != "" {
				t.Fatalf("daemon status rendered successful output after query failure: %q", out)
			}
		})
	}
}

func TestRunDaemonStatus_TextOutput(t *testing.T) {
	mock := &systemd.MockSystemctlRunner{IsEnabledResult: true, IsActiveResult: false}
	dir := daemonTestEnv(t, mock)
	seedUnits(t, dir)

	out, err := captureStdout(t, func() error { return runDaemonStatus(daemonTestCommand(t), nil) })
	if err != nil {
		t.Fatalf("status failed: %v", err)
	}
	if !strings.Contains(out, "installed") || !strings.Contains(out, "Enabled: true") ||
		!strings.Contains(out, "Active: false") || !strings.Contains(out, "Schedule: daily") {
		t.Errorf("unexpected text output:\n%s", out)
	}
}
