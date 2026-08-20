package updex

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostyard/updex/systemd"
)

func newDaemonTestClient(t *testing.T, runner systemd.SystemctlRunner) (*Client, string) {
	t.Helper()
	unitPath := t.TempDir()
	client := NewClient(ClientConfig{
		SystemdManager: systemd.NewTestManager(unitPath, runner),
	})
	return client, unitPath
}

func seedDaemonUnits(t *testing.T, unitPath string) {
	t.Helper()
	for _, suffix := range []string{".timer", ".service"} {
		if err := os.WriteFile(filepath.Join(unitPath, daemonUnitName+suffix), []byte("stub"), 0644); err != nil {
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
	if !result.Success || result.Message != "Auto-update daemon enabled" {
		t.Fatalf("EnableDaemon() result = %+v", result)
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
		"OnCalendar=daily\n",
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
	if !result.Success || result.Message != "Auto-update daemon disabled" {
		t.Fatalf("DisableDaemon() result = %+v", result)
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
		if *status != (DaemonStatusResult{}) {
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
		want := DaemonStatusResult{Installed: true, Enabled: true, Active: true, Schedule: "daily"}
		if *status != want {
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
