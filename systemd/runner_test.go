package systemd

import (
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

			got, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatalf("read command log: %v", err)
			}
			if strings.TrimSpace(string(got)) != tt.want {
				t.Errorf("command arguments = %q, want %q", strings.TrimSpace(string(got)), tt.want)
			}
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
	installFakeSystemctl(t)
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
		})
	}
}

func TestDefaultSystemctlRunnerIsEnabled(t *testing.T) {
	installFakeSystemctl(t)
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
		})
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
