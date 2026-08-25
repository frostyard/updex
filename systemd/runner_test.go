package systemd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultSystemctlRunnerCommands(t *testing.T) {
	logPath := installFakeSystemctl(t)
	runner := &DefaultSystemctlRunner{}

	tests := []struct {
		name string
		run  func() error
		want string
	}{
		{name: "daemon reload", run: runner.DaemonReload, want: "daemon-reload"},
		{name: "enable", run: func() error { return runner.Enable("updex.timer") }, want: "enable\nupdex.timer"},
		{name: "disable", run: func() error { return runner.Disable("updex.timer") }, want: "disable\nupdex.timer"},
		{name: "start", run: func() error { return runner.Start("updex.timer") }, want: "start\nupdex.timer"},
		{name: "stop", run: func() error { return runner.Stop("updex.timer") }, want: "stop\nupdex.timer"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.run(); err != nil {
				t.Fatalf("command failed: %v", err)
			}
			assertSystemctlArgs(t, logPath, tt.want)
		})
	}
}

func TestDefaultSystemctlRunnerCommandError(t *testing.T) {
	installFakeSystemctl(t)
	t.Setenv("UPDEX_SYSTEMCTL_EXIT", "17")

	err := (&DefaultSystemctlRunner{}).Enable("updex.timer")
	if err == nil {
		t.Fatal("Enable() error = nil, want command failure")
	}
	if !strings.Contains(err.Error(), "systemctl enable failed: exit status 17") {
		t.Errorf("Enable() error = %q, want contextual systemctl failure", err)
	}
}

func TestDefaultSystemctlRunnerIsActive(t *testing.T) {
	logPath := installFakeSystemctl(t)
	runner := &DefaultSystemctlRunner{}

	tests := []struct {
		name     string
		exitCode string
		want     bool
		wantErr  bool
	}{
		{name: "active", exitCode: "0", want: true},
		{name: "inactive", exitCode: "3", want: false},
		{name: "command failure", exitCode: "4", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("UPDEX_SYSTEMCTL_EXIT", tt.exitCode)
			got, err := runner.IsActive("updex.timer")
			if (err != nil) != tt.wantErr {
				t.Fatalf("IsActive() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("IsActive() = %v, want %v", got, tt.want)
			}
			assertSystemctlArgs(t, logPath, "is-active\nupdex.timer")
		})
	}
}

func TestDefaultSystemctlRunnerIsEnabled(t *testing.T) {
	logPath := installFakeSystemctl(t)
	runner := &DefaultSystemctlRunner{}

	tests := []struct {
		name     string
		exitCode string
		want     bool
		wantErr  bool
	}{
		{name: "enabled", exitCode: "0", want: true},
		{name: "disabled", exitCode: "1", want: false},
		{name: "command failure", exitCode: "2", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("UPDEX_SYSTEMCTL_EXIT", tt.exitCode)
			got, err := runner.IsEnabled("updex.timer")
			if (err != nil) != tt.wantErr {
				t.Fatalf("IsEnabled() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("IsEnabled() = %v, want %v", got, tt.want)
			}
			assertSystemctlArgs(t, logPath, "is-enabled\nupdex.timer")
		})
	}
}

// TestDefaultSystemctlRunnerContextCommands pins that the Context-suffixed
// methods invoke the same real systemctl commands as their legacy
// counterparts.
func TestDefaultSystemctlRunnerContextCommands(t *testing.T) {
	logPath := installFakeSystemctl(t)
	runner := &DefaultSystemctlRunner{}
	ctx := t.Context()

	tests := []struct {
		name string
		run  func() error
		want string
	}{
		{name: "daemon reload", run: func() error { return runner.DaemonReloadContext(ctx) }, want: "daemon-reload"},
		{name: "enable", run: func() error { return runner.EnableContext(ctx, "updex.timer") }, want: "enable\nupdex.timer"},
		{name: "disable", run: func() error { return runner.DisableContext(ctx, "updex.timer") }, want: "disable\nupdex.timer"},
		{name: "start", run: func() error { return runner.StartContext(ctx, "updex.timer") }, want: "start\nupdex.timer"},
		{name: "stop", run: func() error { return runner.StopContext(ctx, "updex.timer") }, want: "stop\nupdex.timer"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.run(); err != nil {
				t.Fatalf("command failed: %v", err)
			}
			assertSystemctlArgs(t, logPath, tt.want)
		})
	}

	t.Setenv("UPDEX_SYSTEMCTL_EXIT", "0")
	if active, err := runner.IsActiveContext(ctx, "updex.timer"); err != nil || !active {
		t.Fatalf("IsActiveContext() = (%v, %v), want (true, nil)", active, err)
	}
	assertSystemctlArgs(t, logPath, "is-active\nupdex.timer")

	if enabled, err := runner.IsEnabledContext(ctx, "updex.timer"); err != nil || !enabled {
		t.Fatalf("IsEnabledContext() = (%v, %v), want (true, nil)", enabled, err)
	}
	assertSystemctlArgs(t, logPath, "is-enabled\nupdex.timer")
}

// TestDefaultSystemctlRunnerAlreadyCanceledContext pins that every Context
// method refuses to run systemctl at all when the context is already
// canceled before the command starts, reporting an error matching
// context.Canceled rather than the raw process-kill error.
func TestDefaultSystemctlRunnerAlreadyCanceledContext(t *testing.T) {
	installFakeSystemctl(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runner := &DefaultSystemctlRunner{}

	if err := runner.DaemonReloadContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("DaemonReloadContext() error = %v, want context.Canceled", err)
	}
	if err := runner.EnableContext(ctx, "updex.timer"); !errors.Is(err, context.Canceled) {
		t.Fatalf("EnableContext() error = %v, want context.Canceled", err)
	}
	if _, err := runner.IsActiveContext(ctx, "updex.timer"); !errors.Is(err, context.Canceled) {
		t.Fatalf("IsActiveContext() error = %v, want context.Canceled", err)
	}
	if _, err := runner.IsEnabledContext(ctx, "updex.timer"); !errors.Is(err, context.Canceled) {
		t.Fatalf("IsEnabledContext() error = %v, want context.Canceled", err)
	}
}

func assertSystemctlArgs(t *testing.T, logPath, want string) {
	t.Helper()

	got, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read command log: %v", err)
	}
	if strings.TrimSpace(string(got)) != want {
		t.Errorf("command arguments = %q, want %q", strings.TrimSpace(string(got)), want)
	}
}

func installFakeSystemctl(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	logPath := filepath.Join(dir, "arguments")
	scriptPath := filepath.Join(dir, "systemctl")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$UPDEX_SYSTEMCTL_LOG\"\nexit \"${UPDEX_SYSTEMCTL_EXIT:-0}\"\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake systemctl: %v", err)
	}

	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("UPDEX_SYSTEMCTL_LOG", logPath)
	return logPath
}
