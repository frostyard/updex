package updex

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/frostyard/updex/v2/systemd"
)

func newDaemonTestClient(t *testing.T, runner systemd.SystemctlRunner) (*Client, string) {
	t.Helper()
	unitPath := t.TempDir()
	client := NewClient(ClientConfig{
		SystemdManager: systemd.NewTestManager(unitPath, runner),
	})
	return client, unitPath
}

// markedStubUnit is the content that seedDaemonUnits writes: a unit file that
// carries the updex ownership marker, as `daemon enable` writes it.
const markedStubUnit = "# " + daemonMarker + "\nstub\n"

func seedDaemonUnits(t *testing.T, unitPath string) {
	t.Helper()
	for _, suffix := range []string{".timer", ".service"} {
		if err := os.WriteFile(filepath.Join(unitPath, daemonUnitName+suffix), []byte(markedStubUnit), 0644); err != nil {
			t.Fatalf("seed unit %s: %v", suffix, err)
		}
	}
}

func TestEnableDaemon(t *testing.T) {
	runner := &systemd.MockSystemctlRunner{}
	client, unitPath := newDaemonTestClient(t, runner)

	result, err := client.EnableDaemon(t.Context(), EnableDaemonOptions{})
	if err != nil {
		t.Fatalf("EnableDaemon() error = %v", err)
	}
	if !result.Success || result.Message != "Auto-update daemon enabled" || result.DryRun {
		t.Fatalf("EnableDaemon() result = %+v", result)
	}
	if want := daemonTestUnitFiles(unitPath); !slices.Equal(result.UnitFiles, want) {
		t.Errorf("EnableDaemon() UnitFiles = %v, want %v", result.UnitFiles, want)
	}
	if !runner.DaemonReloadCalled {
		t.Error("EnableDaemon() did not reload systemd after installing units")
	}
	if !runner.EnableCalled || runner.EnableUnit != daemonUnitName+".timer" {
		t.Errorf("EnableDaemon() enable = (%v, %q)", runner.EnableCalled, runner.EnableUnit)
	}
	if !runner.StartCalled || runner.StartUnit != daemonUnitName+".timer" {
		t.Errorf("EnableDaemon() start = (%v, %q)", runner.StartCalled, runner.StartUnit)
	}

	service, err := os.ReadFile(filepath.Join(unitPath, daemonUnitName+".service"))
	if err != nil {
		t.Fatalf("read service unit: %v", err)
	}
	for _, required := range []string{
		"# " + daemonMarker + "\n",
		"Wants=network-online.target\n",
		"After=network-online.target\n",
		"ConditionACPower=true\n",
		"Type=oneshot\n",
		"ExecStart=/usr/bin/updex features update --no-refresh\n",
		"NoNewPrivileges=yes\n",
		"ProtectSystem=full\n",
	} {
		if !strings.Contains(string(service), required) {
			t.Errorf("service unit missing %q:\n%s", required, service)
		}
	}

	timer, err := os.ReadFile(filepath.Join(unitPath, daemonUnitName+".timer"))
	if err != nil {
		t.Fatalf("read timer unit: %v", err)
	}
	for _, required := range []string{
		"# " + daemonMarker + "\n",
		"OnCalendar=*-*-* 00/6:00:00\n",
		"[Install]\nWantedBy=timers.target\n",
		"Persistent=true\n",
		"RandomizedDelaySec=3600s\n",
	} {
		if !strings.Contains(string(timer), required) {
			t.Errorf("timer unit missing %q:\n%s", required, timer)
		}
	}
}

func TestEnableDaemonFailures(t *testing.T) {
	t.Run("already installed", func(t *testing.T) {
		runner := &systemd.MockSystemctlRunner{}
		client, unitPath := newDaemonTestClient(t, runner)
		seedDaemonUnits(t, unitPath)

		_, err := client.EnableDaemon(t.Context(), EnableDaemonOptions{})
		if err == nil || !strings.Contains(err.Error(), "timer already installed") {
			t.Fatalf("EnableDaemon() error = %v", err)
		}
		if runner.DaemonReloadCalled || runner.EnableCalled || runner.StartCalled {
			t.Fatal("EnableDaemon() touched systemd for an existing installation")
		}
	})

	t.Run("install", func(t *testing.T) {
		runner := &systemd.MockSystemctlRunner{}
		manager := systemd.NewTestManager(filepath.Join(t.TempDir(), "missing", "units"), runner)
		client := NewClient(ClientConfig{SystemdManager: manager})

		_, err := client.EnableDaemon(t.Context(), EnableDaemonOptions{})
		if err == nil || !strings.Contains(err.Error(), "failed to install timer") {
			t.Fatalf("EnableDaemon() error = %v", err)
		}
		if runner.EnableCalled || runner.StartCalled {
			t.Fatal("EnableDaemon() continued after install failure")
		}
	})

	t.Run("enable", func(t *testing.T) {
		runner := &systemd.MockSystemctlRunner{EnableErr: errors.New("boom")}
		client, _ := newDaemonTestClient(t, runner)

		_, err := client.EnableDaemon(t.Context(), EnableDaemonOptions{})
		if err == nil || !strings.Contains(err.Error(), "failed to enable timer: boom") {
			t.Fatalf("EnableDaemon() error = %v", err)
		}
		if runner.StartCalled {
			t.Fatal("EnableDaemon() started timer after enable failure")
		}
	})

	t.Run("start", func(t *testing.T) {
		runner := &systemd.MockSystemctlRunner{StartErr: errors.New("boom")}
		client, _ := newDaemonTestClient(t, runner)

		_, err := client.EnableDaemon(t.Context(), EnableDaemonOptions{})
		if err == nil || !strings.Contains(err.Error(), "failed to start timer: boom") {
			t.Fatalf("EnableDaemon() error = %v", err)
		}
	})
}

func TestDisableDaemon(t *testing.T) {
	runner := &systemd.MockSystemctlRunner{}
	client, unitPath := newDaemonTestClient(t, runner)
	seedDaemonUnits(t, unitPath)

	result, err := client.DisableDaemon(t.Context(), DisableDaemonOptions{})
	if err != nil {
		t.Fatalf("DisableDaemon() error = %v", err)
	}
	if !result.Success || result.Message != "Auto-update daemon disabled" || result.DryRun {
		t.Fatalf("DisableDaemon() result = %+v", result)
	}
	if want := daemonTestUnitFiles(unitPath); !slices.Equal(result.UnitFiles, want) {
		t.Errorf("DisableDaemon() UnitFiles = %v, want %v", result.UnitFiles, want)
	}
	if !runner.StopCalled || runner.StopUnit != daemonUnitName+".timer" {
		t.Errorf("DisableDaemon() stop = (%v, %q)", runner.StopCalled, runner.StopUnit)
	}
	if !runner.DisableCalled || runner.DisableUnit != daemonUnitName+".timer" {
		t.Errorf("DisableDaemon() disable = (%v, %q)", runner.DisableCalled, runner.DisableUnit)
	}
	if !runner.DaemonReloadCalled {
		t.Error("DisableDaemon() did not reload systemd after removing units")
	}
	for _, suffix := range []string{".timer", ".service"} {
		if _, err := os.Lstat(filepath.Join(unitPath, daemonUnitName+suffix)); !os.IsNotExist(err) {
			t.Errorf("unit %s remains after DisableDaemon(): %v", suffix, err)
		}
	}
}

func TestDisableDaemonFailures(t *testing.T) {
	t.Run("not installed", func(t *testing.T) {
		runner := &systemd.MockSystemctlRunner{}
		client, _ := newDaemonTestClient(t, runner)

		_, err := client.DisableDaemon(t.Context(), DisableDaemonOptions{})
		if err == nil || !strings.Contains(err.Error(), "timer not installed") {
			t.Fatalf("DisableDaemon() error = %v", err)
		}
		if runner.StopCalled || runner.DisableCalled || runner.DaemonReloadCalled {
			t.Fatal("DisableDaemon() touched systemd without an installation")
		}
	})

	t.Run("remove", func(t *testing.T) {
		runner := &systemd.MockSystemctlRunner{}
		client, unitPath := newDaemonTestClient(t, runner)
		timerPath := filepath.Join(unitPath, daemonUnitName+".timer")
		if err := os.Mkdir(timerPath, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(timerPath, "keep"), nil, 0644); err != nil {
			t.Fatal(err)
		}

		_, err := client.DisableDaemon(t.Context(), DisableDaemonOptions{})
		if err == nil || !strings.Contains(err.Error(), "failed to remove timer") {
			t.Fatalf("DisableDaemon() error = %v", err)
		}
	})

	for _, test := range []struct {
		name        string
		configure   func(*systemd.MockSystemctlRunner, error)
		wantContext string
	}{
		{
			name: "stop",
			configure: func(runner *systemd.MockSystemctlRunner, cause error) {
				runner.StopErr = cause
			},
			wantContext: "stop timer",
		},
		{
			name: "disable",
			configure: func(runner *systemd.MockSystemctlRunner, cause error) {
				runner.DisableErr = cause
			},
			wantContext: "disable timer",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			cause := errors.New(test.name + " failed")
			runner := &systemd.MockSystemctlRunner{}
			test.configure(runner, cause)
			client, unitPath := newDaemonTestClient(t, runner)
			seedDaemonUnits(t, unitPath)

			result, err := client.DisableDaemon(t.Context(), DisableDaemonOptions{})

			if result != nil {
				t.Fatalf("DisableDaemon() result = %+v, want nil", result)
			}
			if !errors.Is(err, cause) || !strings.Contains(err.Error(), test.wantContext) {
				t.Fatalf("DisableDaemon() error = %v, want contextualized cause %v", err, cause)
			}
			if !runner.StopCalled || !runner.DisableCalled || !runner.DaemonReloadCalled {
				t.Fatalf(
					"DisableDaemon() calls = stop:%v disable:%v reload:%v, want all true",
					runner.StopCalled,
					runner.DisableCalled,
					runner.DaemonReloadCalled,
				)
			}
			for _, suffix := range []string{".timer", ".service"} {
				if _, statErr := os.Lstat(filepath.Join(unitPath, daemonUnitName+suffix)); !os.IsNotExist(statErr) {
					t.Errorf("unit %s remains after failed DisableDaemon(): %v", suffix, statErr)
				}
			}
		})
	}
}

func TestDaemonStatus(t *testing.T) {
	t.Run("not installed", func(t *testing.T) {
		runner := &systemd.MockSystemctlRunner{}
		client, _ := newDaemonTestClient(t, runner)

		status, err := client.DaemonStatus(t.Context(), DaemonStatusOptions{})
		if err != nil {
			t.Fatalf("DaemonStatus() error = %v", err)
		}
		if !reflect.DeepEqual(*status, DaemonStatusResult{}) {
			t.Fatalf("DaemonStatus() = %+v", status)
		}
		if runner.IsEnabledCalled || runner.IsActiveCalled {
			t.Fatal("DaemonStatus() queried systemctl without an installation")
		}
	})

	t.Run("installed", func(t *testing.T) {
		runner := &systemd.MockSystemctlRunner{
			IsEnabledResult: true,
			IsActiveResult:  true,
		}
		client, unitPath := newDaemonTestClient(t, runner)
		seedDaemonUnits(t, unitPath)

		status, err := client.DaemonStatus(t.Context(), DaemonStatusOptions{})
		if err != nil {
			t.Fatalf("DaemonStatus() error = %v", err)
		}
		want := DaemonStatusResult{
			Installed: true, Enabled: true, Active: true, Source: DaemonSourceEtc,
			UnitFiles: daemonTestUnitFiles(unitPath),
		}
		if !reflect.DeepEqual(*status, want) {
			t.Fatalf("DaemonStatus() = %+v, want %+v", status, want)
		}
		if !runner.IsEnabledCalled || !runner.IsActiveCalled {
			t.Fatal("DaemonStatus() did not query enabled and active state")
		}
	})

	t.Run("enabled query error", func(t *testing.T) {
		queryErr := errors.New("query failed")
		runner := &systemd.MockSystemctlRunner{IsEnabledErr: queryErr}
		client, unitPath := newDaemonTestClient(t, runner)
		seedDaemonUnits(t, unitPath)

		status, err := client.DaemonStatus(t.Context(), DaemonStatusOptions{})
		if status != nil {
			t.Fatalf("DaemonStatus() status = %+v, want nil", status)
		}
		if !errors.Is(err, queryErr) || !strings.Contains(err.Error(), "query enabled state") {
			t.Fatalf("DaemonStatus() error = %v, want contextual enabled-state error", err)
		}
		if runner.IsActiveCalled {
			t.Fatal("DaemonStatus() queried active state after enabled-state failure")
		}
	})

	t.Run("active query error", func(t *testing.T) {
		queryErr := errors.New("query failed")
		runner := &systemd.MockSystemctlRunner{
			IsEnabledResult: true,
			IsActiveErr:     queryErr,
		}
		client, unitPath := newDaemonTestClient(t, runner)
		seedDaemonUnits(t, unitPath)

		status, err := client.DaemonStatus(t.Context(), DaemonStatusOptions{})
		if status != nil {
			t.Fatalf("DaemonStatus() status = %+v, want nil", status)
		}
		if !errors.Is(err, queryErr) || !strings.Contains(err.Error(), "query active state") {
			t.Fatalf("DaemonStatus() error = %v, want contextual active-state error", err)
		}
	})
}

func TestDaemonMethodsRespectCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	runner := &systemd.MockSystemctlRunner{}
	client, _ := newDaemonTestClient(t, runner)
	tests := []struct {
		name string
		call func() error
	}{
		{name: "enable", call: func() error { _, err := client.EnableDaemon(ctx, EnableDaemonOptions{}); return err }},
		{name: "disable", call: func() error { _, err := client.DisableDaemon(ctx, DisableDaemonOptions{}); return err }},
		{name: "status", call: func() error { _, err := client.DaemonStatus(ctx, DaemonStatusOptions{}); return err }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.call(); !errors.Is(err, context.Canceled) {
				t.Fatalf("error = %v, want context canceled", err)
			}
		})
	}
	if runner.DaemonReloadCalled || runner.EnableCalled || runner.StartCalled ||
		runner.StopCalled || runner.DisableCalled || runner.IsEnabledCalled || runner.IsActiveCalled {
		t.Fatal("canceled daemon operation touched systemd")
	}
}

// TestEnableDaemonCancelsInFlightSystemctl exercises in-flight cancellation
// end to end through the public SDK: EnableDaemon runs against a real
// DefaultSystemctlRunner backed by a fake blocking systemctl script on PATH.
// The SDK context is canceled only after that fake systemctl child process
// has actually started (during Install's daemon-reload call), and
// EnableDaemon must return promptly with an error matching context.Canceled
// instead of waiting for the blocking command to finish on its own.
func TestEnableDaemonCancelsInFlightSystemctl(t *testing.T) {
	dir := t.TempDir()
	startedPath := filepath.Join(dir, "started")
	scriptPath := filepath.Join(dir, "systemctl")
	script := "#!/bin/sh\n: > \"$UPDEX_SYSTEMCTL_STARTED\"\nsleep 30\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake systemctl: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("UPDEX_SYSTEMCTL_STARTED", startedPath)

	client, _ := newDaemonTestClient(t, &systemd.DefaultSystemctlRunner{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		_, err := client.EnableDaemon(ctx, EnableDaemonOptions{})
		errCh <- err
	}()

	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, statErr := os.Stat(startedPath); statErr == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fake systemctl did not start in time")
		}
		time.Sleep(10 * time.Millisecond)
	}

	start := time.Now()
	cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("EnableDaemon() error = %v, want context.Canceled", err)
		}
		if elapsed := time.Since(start); elapsed > 3*time.Second {
			t.Fatalf("EnableDaemon() took %v to return after cancellation, want prompt", elapsed)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("EnableDaemon() did not return after context cancellation")
	}
}

func TestDaemonResultsMarshalToCLIShape(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{
			name:  "action",
			value: DaemonActionResult{Success: true, Message: "Auto-update daemon enabled"},
			want:  `{"success":true,"message":"Auto-update daemon enabled"}`,
		},
		{
			name:  "not installed",
			value: DaemonStatusResult{},
			want:  `{"installed":false,"enabled":false,"active":false}`,
		},
		{
			name:  "installed",
			value: DaemonStatusResult{Installed: true, Enabled: true, Active: true, Schedule: "daily"},
			want:  `{"installed":true,"enabled":true,"active":true,"schedule":"daily"}`,
		},
		{
			name:  "masked timer",
			value: DaemonStatusResult{Installed: true, Masked: true},
			want:  `{"installed":true,"enabled":false,"active":false,"masked":true}`,
		},
		{
			name: "disable kept a mask",
			value: DaemonActionResult{
				Success: true, Message: "Auto-update daemon disabled",
				MaskedFiles: []string{"/etc/systemd/system/updex-update.timer"},
			},
			want: `{"success":true,"message":"Auto-update daemon disabled","masked_files":["/etc/systemd/system/updex-update.timer"]}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := json.Marshal(test.value)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}
			if string(got) != test.want {
				t.Fatalf("json.Marshal() = %s, want %s", got, test.want)
			}
		})
	}
}

// daemonTestUnitFiles returns the expected timer and service unit paths under
// unitPath, in that order.
func daemonTestUnitFiles(unitPath string) []string {
	return []string{
		filepath.Join(unitPath, daemonUnitName+".timer"),
		filepath.Join(unitPath, daemonUnitName+".service"),
	}
}

func TestEnableDaemonDryRun(t *testing.T) {
	t.Run("fresh install", func(t *testing.T) {
		runner := &systemd.MockSystemctlRunner{}
		client, unitPath := newDaemonTestClient(t, runner)

		result, err := client.EnableDaemon(t.Context(), EnableDaemonOptions{DryRun: true})
		if err != nil {
			t.Fatalf("EnableDaemon() error = %v", err)
		}
		if !result.Success || !result.DryRun {
			t.Fatalf("EnableDaemon() result = %+v", result)
		}
		if want := daemonTestUnitFiles(unitPath); !slices.Equal(result.UnitFiles, want) {
			t.Errorf("EnableDaemon() UnitFiles = %v, want %v", result.UnitFiles, want)
		}
		if result.Message == "" {
			t.Error("EnableDaemon() dry run has no message")
		}
		if runner.DaemonReloadCalled || runner.EnableCalled || runner.StartCalled {
			t.Error("EnableDaemon() dry run touched systemd")
		}
		entries, err := os.ReadDir(unitPath)
		if err != nil {
			t.Fatalf("read unit dir: %v", err)
		}
		if len(entries) != 0 {
			t.Errorf("EnableDaemon() dry run wrote %d entries to the unit dir", len(entries))
		}
	})

	t.Run("already installed", func(t *testing.T) {
		runner := &systemd.MockSystemctlRunner{}
		client, unitPath := newDaemonTestClient(t, runner)
		seedDaemonUnits(t, unitPath)

		_, err := client.EnableDaemon(t.Context(), EnableDaemonOptions{DryRun: true})
		if err == nil || !strings.Contains(err.Error(), "timer already installed") {
			t.Fatalf("EnableDaemon() error = %v, want already-installed error", err)
		}
		if runner.DaemonReloadCalled || runner.EnableCalled || runner.StartCalled {
			t.Error("EnableDaemon() dry run touched systemd")
		}
		for _, path := range daemonTestUnitFiles(unitPath) {
			content, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatalf("read seeded unit %s: %v", path, readErr)
			}
			if string(content) != markedStubUnit {
				t.Errorf("seeded unit %s content = %q, want %q", path, content, markedStubUnit)
			}
		}
	})
}

func TestDisableDaemonDryRun(t *testing.T) {
	t.Run("installed", func(t *testing.T) {
		runner := &systemd.MockSystemctlRunner{}
		client, unitPath := newDaemonTestClient(t, runner)
		seedDaemonUnits(t, unitPath)

		result, err := client.DisableDaemon(t.Context(), DisableDaemonOptions{DryRun: true})
		if err != nil {
			t.Fatalf("DisableDaemon() error = %v", err)
		}
		if !result.Success || !result.DryRun {
			t.Fatalf("DisableDaemon() result = %+v", result)
		}
		if want := daemonTestUnitFiles(unitPath); !slices.Equal(result.UnitFiles, want) {
			t.Errorf("DisableDaemon() UnitFiles = %v, want %v", result.UnitFiles, want)
		}
		if result.Message == "" {
			t.Error("DisableDaemon() dry run has no message")
		}
		if runner.StopCalled || runner.DisableCalled || runner.DaemonReloadCalled {
			t.Error("DisableDaemon() dry run touched systemd")
		}
		for _, path := range daemonTestUnitFiles(unitPath) {
			content, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatalf("unit %s removed by dry run: %v", path, readErr)
			}
			if string(content) != markedStubUnit {
				t.Errorf("unit %s content = %q, want %q", path, content, markedStubUnit)
			}
		}
	})

	t.Run("not installed", func(t *testing.T) {
		runner := &systemd.MockSystemctlRunner{}
		client, _ := newDaemonTestClient(t, runner)

		_, err := client.DisableDaemon(t.Context(), DisableDaemonOptions{DryRun: true})
		if err == nil || !strings.Contains(err.Error(), "timer not installed") {
			t.Fatalf("DisableDaemon() error = %v, want not-installed error", err)
		}
		if runner.StopCalled || runner.DisableCalled || runner.DaemonReloadCalled {
			t.Error("DisableDaemon() dry run touched systemd")
		}
	})
}

// maskUnit replaces a unit file with the symlink that `systemctl mask`
// writes: a link to /dev/null.
func maskUnit(t *testing.T, unitPath, suffix string) string {
	t.Helper()
	path := filepath.Join(unitPath, daemonUnitName+suffix)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		t.Fatalf("remove unit %s before masking: %v", suffix, err)
	}
	if err := os.Symlink(os.DevNull, path); err != nil {
		t.Fatalf("mask unit %s: %v", suffix, err)
	}
	return path
}

// TestDisableDaemonMasked verifies that a systemd mask (a symlink to
// /dev/null) no longer makes `daemon disable` fail: the command stops and
// disables the timer, removes the unit files updex wrote, keeps the mask, and
// says how to undo it. Any other symlink stays a refusal.
func TestDisableDaemonMasked(t *testing.T) {
	t.Run("masked timer is kept and reported", func(t *testing.T) {
		runner := &systemd.MockSystemctlRunner{}
		client, unitPath := newDaemonTestClient(t, runner)
		seedDaemonUnits(t, unitPath)
		timerPath := maskUnit(t, unitPath, ".timer")
		servicePath := filepath.Join(unitPath, daemonUnitName+".service")

		result, err := client.DisableDaemon(t.Context(), DisableDaemonOptions{})
		if err != nil {
			t.Fatalf("DisableDaemon() error = %v, want success with a masked unit", err)
		}
		if !result.Success || result.DryRun {
			t.Fatalf("DisableDaemon() result = %+v", result)
		}
		if want := []string{servicePath}; !slices.Equal(result.UnitFiles, want) {
			t.Errorf("DisableDaemon() UnitFiles = %v, want %v", result.UnitFiles, want)
		}
		if want := []string{timerPath}; !slices.Equal(result.MaskedFiles, want) {
			t.Errorf("DisableDaemon() MaskedFiles = %v, want %v", result.MaskedFiles, want)
		}
		wantNote := "; updex-update.timer is masked (run 'systemctl unmask updex-update.timer' to undo)"
		if !strings.Contains(result.Message, wantNote) {
			t.Errorf("DisableDaemon() Message = %q, want it to contain %q", result.Message, wantNote)
		}
		if target, err := os.Readlink(timerPath); err != nil || target != os.DevNull {
			t.Errorf("the mask was removed: target = %q, err = %v", target, err)
		}
		if _, err := os.Lstat(servicePath); !os.IsNotExist(err) {
			t.Errorf("service unit remains after DisableDaemon(): %v", err)
		}
		if !runner.StopCalled || !runner.DisableCalled || !runner.DaemonReloadCalled {
			t.Errorf("systemd calls = stop:%v disable:%v reload:%v, want all true",
				runner.StopCalled, runner.DisableCalled, runner.DaemonReloadCalled)
		}
	})

	t.Run("masked service is kept", func(t *testing.T) {
		runner := &systemd.MockSystemctlRunner{}
		client, unitPath := newDaemonTestClient(t, runner)
		seedDaemonUnits(t, unitPath)
		servicePath := maskUnit(t, unitPath, ".service")
		timerPath := filepath.Join(unitPath, daemonUnitName+".timer")

		result, err := client.DisableDaemon(t.Context(), DisableDaemonOptions{})
		if err != nil {
			t.Fatalf("DisableDaemon() error = %v", err)
		}
		if want := []string{timerPath}; !slices.Equal(result.UnitFiles, want) {
			t.Errorf("DisableDaemon() UnitFiles = %v, want %v", result.UnitFiles, want)
		}
		if want := []string{servicePath}; !slices.Equal(result.MaskedFiles, want) {
			t.Errorf("DisableDaemon() MaskedFiles = %v, want %v", result.MaskedFiles, want)
		}
		if !strings.Contains(result.Message, "updex-update.service is masked") {
			t.Errorf("DisableDaemon() Message = %q", result.Message)
		}
		if _, err := os.Lstat(servicePath); err != nil {
			t.Errorf("the masked service was removed: %v", err)
		}
	})

	t.Run("both units masked: one unmask command", func(t *testing.T) {
		runner := &systemd.MockSystemctlRunner{}
		client, unitPath := newDaemonTestClient(t, runner)
		timerPath := maskUnit(t, unitPath, ".timer")
		servicePath := maskUnit(t, unitPath, ".service")

		result, err := client.DisableDaemon(t.Context(), DisableDaemonOptions{})
		if err != nil {
			t.Fatalf("DisableDaemon() error = %v", err)
		}
		if want := []string{timerPath, servicePath}; !slices.Equal(result.MaskedFiles, want) {
			t.Errorf("DisableDaemon() MaskedFiles = %v, want %v", result.MaskedFiles, want)
		}
		wantNote := "; updex-update.timer and updex-update.service are masked (run 'systemctl unmask updex-update.timer updex-update.service' to undo)"
		if !strings.HasSuffix(result.Message, wantNote) {
			t.Errorf("DisableDaemon() Message = %q, want suffix %q", result.Message, wantNote)
		}
	})

	t.Run("a lone masked timer counts as installed", func(t *testing.T) {
		runner := &systemd.MockSystemctlRunner{}
		client, unitPath := newDaemonTestClient(t, runner)
		timerPath := maskUnit(t, unitPath, ".timer")

		result, err := client.DisableDaemon(t.Context(), DisableDaemonOptions{})
		if err != nil {
			t.Fatalf("DisableDaemon() error = %v, want no not-installed error", err)
		}
		if len(result.UnitFiles) != 0 {
			t.Errorf("DisableDaemon() UnitFiles = %v, want none", result.UnitFiles)
		}
		if want := []string{timerPath}; !slices.Equal(result.MaskedFiles, want) {
			t.Errorf("DisableDaemon() MaskedFiles = %v, want %v", result.MaskedFiles, want)
		}
	})

	t.Run("dry run reports the mask without removing it", func(t *testing.T) {
		runner := &systemd.MockSystemctlRunner{}
		client, unitPath := newDaemonTestClient(t, runner)
		seedDaemonUnits(t, unitPath)
		timerPath := maskUnit(t, unitPath, ".timer")

		result, err := client.DisableDaemon(t.Context(), DisableDaemonOptions{DryRun: true})
		if err != nil {
			t.Fatalf("DisableDaemon() error = %v", err)
		}
		if !result.DryRun || !result.Success {
			t.Fatalf("DisableDaemon() result = %+v", result)
		}
		if want := []string{timerPath}; !slices.Equal(result.MaskedFiles, want) {
			t.Errorf("DisableDaemon() MaskedFiles = %v, want %v", result.MaskedFiles, want)
		}
		if !strings.Contains(result.Message, "is masked") {
			t.Errorf("DisableDaemon() Message = %q, want the mask note", result.Message)
		}
		if _, err := os.Lstat(timerPath); err != nil {
			t.Errorf("the dry run removed the mask: %v", err)
		}
		if runner.StopCalled || runner.DisableCalled || runner.DaemonReloadCalled {
			t.Error("DisableDaemon() dry run touched systemd")
		}
	})

	t.Run("a symlink to another target is still refused", func(t *testing.T) {
		runner := &systemd.MockSystemctlRunner{}
		client, unitPath := newDaemonTestClient(t, runner)
		seedDaemonUnits(t, unitPath)
		timerPath := filepath.Join(unitPath, daemonUnitName+".timer")
		if err := os.Remove(timerPath); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(unitPath, "elsewhere"), timerPath); err != nil {
			t.Fatal(err)
		}

		_, err := client.DisableDaemon(t.Context(), DisableDaemonOptions{})
		if err == nil || !strings.Contains(err.Error(), "not a regular file") {
			t.Fatalf("DisableDaemon() error = %v, want a not-a-regular-file refusal", err)
		}
	})
}

// TestDaemonStatusMasked verifies that a masked timer reports Masked, keeps
// Installed true, and has no schedule: a masked timer never fires, and the
// mask file holds no OnCalendar line.
func TestDaemonStatusMasked(t *testing.T) {
	runner := &systemd.MockSystemctlRunner{
		IsEnabledResult: false,
		IsActiveResult:  false,
		ShowValues: map[string]string{
			"TimersCalendar": "{ OnCalendar=daily ; next_elapse=never }",
		},
	}
	client, unitPath := newDaemonTestClient(t, runner)
	seedDaemonUnits(t, unitPath)
	timerPath := maskUnit(t, unitPath, ".timer")

	status, err := client.DaemonStatus(t.Context(), DaemonStatusOptions{})
	if err != nil {
		t.Fatalf("DaemonStatus() error = %v", err)
	}
	if !status.Installed || !status.Masked {
		t.Fatalf("DaemonStatus() = %+v, want installed and masked", status)
	}
	if status.Schedule != "" {
		t.Errorf("DaemonStatus() Schedule = %q, want empty for a mask", status.Schedule)
	}
	if !strings.Contains(strings.Join(status.UnitFiles, " "), timerPath) {
		t.Errorf("DaemonStatus() UnitFiles = %v, want the masked timer path", status.UnitFiles)
	}
}

func TestDaemonActionResultJSON(t *testing.T) {
	tests := []struct {
		name        string
		result      DaemonActionResult
		wantDryRun  bool
		wantUnitKey bool
	}{
		{
			name: "dry run",
			result: DaemonActionResult{
				Success:   true,
				Message:   "Would install timer",
				DryRun:    true,
				UnitFiles: []string{"/etc/systemd/system/updex-update.timer", "/etc/systemd/system/updex-update.service"},
			},
			wantDryRun:  true,
			wantUnitKey: true,
		},
		{
			name: "real run",
			result: DaemonActionResult{
				Success:   true,
				Message:   "Auto-update daemon enabled",
				UnitFiles: []string{"/etc/systemd/system/updex-update.timer", "/etc/systemd/system/updex-update.service"},
			},
			wantDryRun:  false,
			wantUnitKey: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data, err := json.Marshal(test.result)
			if err != nil {
				t.Fatalf("marshal result: %v", err)
			}
			var payload map[string]any
			if err := json.Unmarshal(data, &payload); err != nil {
				t.Fatalf("unmarshal result: %v", err)
			}
			_, hasDryRun := payload["dry_run"]
			if hasDryRun != test.wantDryRun {
				t.Errorf("JSON has dry_run = %v, want %v: %s", hasDryRun, test.wantDryRun, data)
			}
			if test.wantDryRun && payload["dry_run"] != true {
				t.Errorf("JSON dry_run = %v, want true: %s", payload["dry_run"], data)
			}
			unitFiles, hasUnitFiles := payload["unit_files"]
			if hasUnitFiles != test.wantUnitKey {
				t.Errorf("JSON has unit_files = %v, want %v: %s", hasUnitFiles, test.wantUnitKey, data)
			}
			if hasUnitFiles {
				paths, ok := unitFiles.([]any)
				if !ok || len(paths) != 2 {
					t.Errorf("JSON unit_files = %v, want 2 paths: %s", unitFiles, data)
				}
			}
		})
	}
}
