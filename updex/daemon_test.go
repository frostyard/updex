package updex

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/frostyard/updex/systemd"
)

type recordingSystemctlRunner struct {
	calls      []string
	enableErr  error
	startErr   error
	reloadErr  error
	enabled    bool
	enabledErr error
	active     bool
	activeErr  error
}

func (r *recordingSystemctlRunner) DaemonReload() error {
	r.calls = append(r.calls, "daemon-reload")
	return r.reloadErr
}

func (r *recordingSystemctlRunner) Enable(unit string) error {
	r.calls = append(r.calls, "enable "+unit)
	return r.enableErr
}

func (r *recordingSystemctlRunner) Disable(unit string) error {
	r.calls = append(r.calls, "disable "+unit)
	return nil
}

func (r *recordingSystemctlRunner) Start(unit string) error {
	r.calls = append(r.calls, "start "+unit)
	return r.startErr
}

func (r *recordingSystemctlRunner) Stop(unit string) error {
	r.calls = append(r.calls, "stop "+unit)
	return nil
}

func (r *recordingSystemctlRunner) IsActive(unit string) (bool, error) {
	r.calls = append(r.calls, "is-active "+unit)
	return r.active, r.activeErr
}

func (r *recordingSystemctlRunner) IsEnabled(unit string) (bool, error) {
	r.calls = append(r.calls, "is-enabled "+unit)
	return r.enabled, r.enabledErr
}

func newDaemonTestClient(t *testing.T, runner systemd.SystemctlRunner) (*Client, string) {
	t.Helper()
	unitPath := t.TempDir()
	return NewClient(ClientConfig{
		SystemdManager: systemd.NewTestManager(unitPath, runner),
	}), unitPath
}

func TestEnableDaemon(t *testing.T) {
	runner := &recordingSystemctlRunner{}
	client, unitPath := newDaemonTestClient(t, runner)

	result, err := client.EnableDaemon(t.Context(), EnableDaemonOptions{})
	if err != nil {
		t.Fatalf("EnableDaemon() error = %v", err)
	}
	if !result.Success || result.Message != "Auto-update daemon enabled" {
		t.Fatalf("EnableDaemon() result = %+v", result)
	}
	wantCalls := []string{
		"daemon-reload",
		"enable updex-update.timer",
		"start updex-update.timer",
	}
	if !slices.Equal(runner.calls, wantCalls) {
		t.Fatalf("calls = %v, want %v", runner.calls, wantCalls)
	}

	timer, err := os.ReadFile(filepath.Join(unitPath, "updex-update.timer"))
	if err != nil {
		t.Fatalf("read timer: %v", err)
	}
	wantTimer := `[Unit]
Description=Automatic sysext updates

[Timer]
OnCalendar=daily
Persistent=true
RandomizedDelaySec=3600s

[Install]
WantedBy=timers.target
`
	if string(timer) != wantTimer {
		t.Errorf("timer content = %q, want %q", timer, wantTimer)
	}

	service, err := os.ReadFile(filepath.Join(unitPath, "updex-update.service"))
	if err != nil {
		t.Fatalf("read service: %v", err)
	}
	wantService := `[Unit]
Description=Automatic sysext update service

[Service]
Type=oneshot
ExecStart=/usr/bin/updex features update --no-refresh
`
	if string(service) != wantService {
		t.Errorf("service content = %q, want %q", service, wantService)
	}
}

func TestEnableDaemonFailures(t *testing.T) {
	t.Run("already installed", func(t *testing.T) {
		runner := &recordingSystemctlRunner{}
		client, unitPath := newDaemonTestClient(t, runner)
		if err := os.WriteFile(filepath.Join(unitPath, "updex-update.service"), nil, 0644); err != nil {
			t.Fatal(err)
		}

		_, err := client.EnableDaemon(t.Context(), EnableDaemonOptions{})
		if err == nil || err.Error() != "timer already installed; run 'updex daemon disable' first to reinstall" {
			t.Fatalf("EnableDaemon() error = %v", err)
		}
		if len(runner.calls) != 0 {
			t.Fatalf("calls = %v, want none", runner.calls)
		}
	})

	t.Run("install", func(t *testing.T) {
		runner := &recordingSystemctlRunner{}
		manager := systemd.NewTestManager(filepath.Join(t.TempDir(), "missing"), runner)
		client := NewClient(ClientConfig{SystemdManager: manager})

		_, err := client.EnableDaemon(t.Context(), EnableDaemonOptions{})
		if err == nil || !strings.Contains(err.Error(), "failed to install timer: failed to write timer:") {
			t.Fatalf("EnableDaemon() error = %v", err)
		}
	})

	t.Run("daemon reload", func(t *testing.T) {
		runner := &recordingSystemctlRunner{reloadErr: errors.New("reload failed")}
		client, _ := newDaemonTestClient(t, runner)

		_, err := client.EnableDaemon(t.Context(), EnableDaemonOptions{})
		if err == nil || err.Error() != "failed to install timer: daemon-reload failed: reload failed" {
			t.Fatalf("EnableDaemon() error = %v", err)
		}
		if !slices.Equal(runner.calls, []string{"daemon-reload"}) {
			t.Fatalf("calls = %v, want daemon-reload only", runner.calls)
		}
	})

	t.Run("enable", func(t *testing.T) {
		runner := &recordingSystemctlRunner{enableErr: errors.New("enable failed")}
		client, _ := newDaemonTestClient(t, runner)

		_, err := client.EnableDaemon(t.Context(), EnableDaemonOptions{})
		if err == nil || err.Error() != "failed to enable timer: enable failed" {
			t.Fatalf("EnableDaemon() error = %v", err)
		}
		wantCalls := []string{"daemon-reload", "enable updex-update.timer"}
		if !slices.Equal(runner.calls, wantCalls) {
			t.Fatalf("calls = %v, want %v", runner.calls, wantCalls)
		}
	})

	t.Run("start", func(t *testing.T) {
		runner := &recordingSystemctlRunner{startErr: errors.New("start failed")}
		client, _ := newDaemonTestClient(t, runner)

		_, err := client.EnableDaemon(t.Context(), EnableDaemonOptions{})
		if err == nil || err.Error() != "failed to start timer: start failed" {
			t.Fatalf("EnableDaemon() error = %v", err)
		}
		wantCalls := []string{
			"daemon-reload",
			"enable updex-update.timer",
			"start updex-update.timer",
		}
		if !slices.Equal(runner.calls, wantCalls) {
			t.Fatalf("calls = %v, want %v", runner.calls, wantCalls)
		}
	})
}

func TestDisableDaemon(t *testing.T) {
	runner := &recordingSystemctlRunner{}
	client, unitPath := newDaemonTestClient(t, runner)
	for _, name := range []string{"updex-update.timer", "updex-update.service"} {
		if err := os.WriteFile(filepath.Join(unitPath, name), nil, 0644); err != nil {
			t.Fatal(err)
		}
	}

	result, err := client.DisableDaemon(t.Context(), DisableDaemonOptions{})
	if err != nil {
		t.Fatalf("DisableDaemon() error = %v", err)
	}
	if !result.Success || result.Message != "Auto-update daemon disabled" {
		t.Fatalf("DisableDaemon() result = %+v", result)
	}
	wantCalls := []string{
		"stop updex-update.timer",
		"disable updex-update.timer",
		"daemon-reload",
	}
	if !slices.Equal(runner.calls, wantCalls) {
		t.Fatalf("calls = %v, want %v", runner.calls, wantCalls)
	}
	for _, name := range []string{"updex-update.timer", "updex-update.service"} {
		if _, err := os.Stat(filepath.Join(unitPath, name)); !os.IsNotExist(err) {
			t.Errorf("%s still exists or stat failed: %v", name, err)
		}
	}
}

func TestDisableDaemonFailures(t *testing.T) {
	t.Run("not installed", func(t *testing.T) {
		runner := &recordingSystemctlRunner{}
		client, _ := newDaemonTestClient(t, runner)

		_, err := client.DisableDaemon(t.Context(), DisableDaemonOptions{})
		if err == nil || err.Error() != "timer not installed; nothing to disable" {
			t.Fatalf("DisableDaemon() error = %v", err)
		}
		if len(runner.calls) != 0 {
			t.Fatalf("calls = %v, want none", runner.calls)
		}
	})

	t.Run("remove", func(t *testing.T) {
		runner := &recordingSystemctlRunner{}
		client, unitPath := newDaemonTestClient(t, runner)
		timerPath := filepath.Join(unitPath, "updex-update.timer")
		if err := os.Mkdir(timerPath, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(timerPath, "keep"), nil, 0644); err != nil {
			t.Fatal(err)
		}

		_, err := client.DisableDaemon(t.Context(), DisableDaemonOptions{})
		if err == nil || !strings.Contains(err.Error(), "failed to remove timer: remove timer:") {
			t.Fatalf("DisableDaemon() error = %v", err)
		}
		wantCalls := []string{
			"stop updex-update.timer",
			"disable updex-update.timer",
			"daemon-reload",
		}
		if !slices.Equal(runner.calls, wantCalls) {
			t.Fatalf("calls = %v, want %v", runner.calls, wantCalls)
		}
	})
}

func TestDaemonStatus(t *testing.T) {
	t.Run("not installed", func(t *testing.T) {
		runner := &recordingSystemctlRunner{}
		client, _ := newDaemonTestClient(t, runner)

		status, err := client.DaemonStatus(t.Context(), DaemonStatusOptions{})
		if err != nil {
			t.Fatalf("DaemonStatus() error = %v", err)
		}
		if *status != (DaemonStatusResult{}) {
			t.Fatalf("DaemonStatus() = %+v", status)
		}
		if len(runner.calls) != 0 {
			t.Fatalf("calls = %v, want none", runner.calls)
		}
	})

	t.Run("installed", func(t *testing.T) {
		runner := &recordingSystemctlRunner{
			enabled:    true,
			enabledErr: errors.New("ignored"),
			active:     true,
			activeErr:  errors.New("ignored"),
		}
		client, unitPath := newDaemonTestClient(t, runner)
		if err := os.WriteFile(filepath.Join(unitPath, "updex-update.timer"), nil, 0644); err != nil {
			t.Fatal(err)
		}

		status, err := client.DaemonStatus(t.Context(), DaemonStatusOptions{})
		if err != nil {
			t.Fatalf("DaemonStatus() error = %v", err)
		}
		want := DaemonStatusResult{
			Installed: true,
			Enabled:   true,
			Active:    true,
			Schedule:  "daily",
		}
		if *status != want {
			t.Fatalf("DaemonStatus() = %+v, want %+v", status, want)
		}
		wantCalls := []string{
			"is-enabled updex-update.timer",
			"is-active updex-update.timer",
		}
		if !slices.Equal(runner.calls, wantCalls) {
			t.Fatalf("calls = %v, want %v", runner.calls, wantCalls)
		}
	})
}

func TestDaemonMethodsRespectCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	runner := &recordingSystemctlRunner{}
	client, _ := newDaemonTestClient(t, runner)
	tests := []struct {
		name string
		call func() error
		want string
	}{
		{
			name: "enable",
			call: func() error {
				_, err := client.EnableDaemon(ctx, EnableDaemonOptions{})
				return err
			},
			want: "enable daemon: context canceled",
		},
		{
			name: "disable",
			call: func() error {
				_, err := client.DisableDaemon(ctx, DisableDaemonOptions{})
				return err
			},
			want: "disable daemon: context canceled",
		},
		{
			name: "status",
			call: func() error {
				_, err := client.DaemonStatus(ctx, DaemonStatusOptions{})
				return err
			},
			want: "get daemon status: context canceled",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); err == nil || err.Error() != tt.want {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
	if len(runner.calls) != 0 {
		t.Fatalf("calls = %v, want none", runner.calls)
	}
}

func TestDaemonResultJSON(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{
			name: "action",
			value: DaemonActionResult{
				Success: true,
				Message: "Auto-update daemon enabled",
			},
			want: `{"success":true,"message":"Auto-update daemon enabled"}`,
		},
		{
			name:  "not installed",
			value: DaemonStatusResult{},
			want:  `{"installed":false,"enabled":false,"active":false}`,
		},
		{
			name: "installed",
			value: DaemonStatusResult{
				Installed: true,
				Enabled:   true,
				Active:    true,
				Schedule:  "daily",
			},
			want: `{"installed":true,"enabled":true,"active":true,"schedule":"daily"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.value)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			if string(got) != tt.want {
				t.Fatalf("Marshal() = %s, want %s", got, tt.want)
			}
		})
	}
}
