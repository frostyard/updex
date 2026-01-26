package systemd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstall(t *testing.T) {
	tests := []struct {
		name                string
		timerExists         bool
		serviceExists       bool
		daemonReloadErr     error
		wantErr             bool
		wantErrContains     string
		wantDaemonReload    bool
		wantTimerContains   []string
		wantServiceContains []string
	}{
		{
			name:             "successful install",
			wantErr:          false,
			wantDaemonReload: true,
			wantTimerContains: []string{
				"[Timer]",
				"OnCalendar=daily",
				"Persistent=true",
			},
			wantServiceContains: []string{
				"[Service]",
				"Type=oneshot",
				"ExecStart=/usr/bin/updex update",
			},
		},
		{
			name:            "timer already exists",
			timerExists:     true,
			wantErr:         true,
			wantErrContains: "timer file already exists",
		},
		{
			name:            "service already exists",
			serviceExists:   true,
			wantErr:         true,
			wantErrContains: "service file already exists",
		},
		{
			name:             "daemon-reload error",
			daemonReloadErr:  errors.New("reload failed"),
			wantErr:          true,
			wantErrContains:  "daemon-reload failed",
			wantDaemonReload: true, // Should still be called
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			mockRunner := &MockSystemctlRunner{
				DaemonReloadErr: tt.daemonReloadErr,
			}

			// Create pre-existing files if needed
			if tt.timerExists {
				_ = os.WriteFile(filepath.Join(tmpDir, "updex-update.timer"), []byte("existing"), 0644)
			}
			if tt.serviceExists {
				_ = os.WriteFile(filepath.Join(tmpDir, "updex-update.service"), []byte("existing"), 0644)
			}

			mgr := NewTestManager(tmpDir, mockRunner)

			timer := &TimerConfig{
				Name:        "updex-update",
				Description: "Test timer",
				OnCalendar:  "daily",
				Persistent:  true,
			}
			service := &ServiceConfig{
				Name:        "updex-update",
				Description: "Test service",
				Type:        "oneshot",
				ExecStart:   "/usr/bin/updex update",
			}

			err := mgr.Install(timer, service)

			// Check error
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.wantErrContains != "" && !strings.Contains(err.Error(), tt.wantErrContains) {
					t.Errorf("error = %v, want containing %q", err, tt.wantErrContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Check DaemonReload was called
			if tt.wantDaemonReload && !mockRunner.DaemonReloadCalled {
				t.Error("DaemonReload() not called")
			}

			// Verify timer file exists and contains expected content
			timerPath := filepath.Join(tmpDir, "updex-update.timer")
			timerContent, err := os.ReadFile(timerPath)
			if err != nil {
				t.Fatalf("timer file not created: %v", err)
			}
			for _, s := range tt.wantTimerContains {
				if !strings.Contains(string(timerContent), s) {
					t.Errorf("timer content missing %q", s)
				}
			}

			// Verify service file exists and contains expected content
			servicePath := filepath.Join(tmpDir, "updex-update.service")
			serviceContent, err := os.ReadFile(servicePath)
			if err != nil {
				t.Fatalf("service file not created: %v", err)
			}
			for _, s := range tt.wantServiceContains {
				if !strings.Contains(string(serviceContent), s) {
					t.Errorf("service content missing %q", s)
				}
			}
		})
	}
}

func TestInstall_CleanupOnPartialFailure(t *testing.T) {
	// Test that timer file is removed if service write fails
	tmpDir := t.TempDir()
	mockRunner := &MockSystemctlRunner{}
	mgr := NewTestManager(tmpDir, mockRunner)

	// Create a directory with service name to cause write failure
	servicePath := filepath.Join(tmpDir, "updex-update.service")
	if err := os.Mkdir(servicePath, 0755); err != nil {
		t.Fatalf("failed to create blocking directory: %v", err)
	}

	timer := &TimerConfig{
		Name:        "updex-update",
		Description: "Test timer",
		OnCalendar:  "daily",
	}
	service := &ServiceConfig{
		Name:        "updex-update",
		Description: "Test service",
		Type:        "oneshot",
		ExecStart:   "/usr/bin/updex update",
	}

	err := mgr.Install(timer, service)
	if err == nil {
		t.Fatal("expected error for service write failure")
	}

	// Verify timer file was cleaned up
	timerPath := filepath.Join(tmpDir, "updex-update.timer")
	if _, err := os.Stat(timerPath); !os.IsNotExist(err) {
		t.Error("timer file should have been removed after service write failure")
	}
}

func TestRemove(t *testing.T) {
	tests := []struct {
		name              string
		timerExists       bool
		serviceExists     bool
		stopErr           error
		disableErr        error
		daemonReloadErr   error
		wantErr           bool
		wantStopCalled    bool
		wantDisableCalled bool
		wantDaemonReload  bool
	}{
		{
			name:              "successful remove",
			timerExists:       true,
			serviceExists:     true,
			wantStopCalled:    true,
			wantDisableCalled: true,
			wantDaemonReload:  true,
		},
		{
			name:              "files don't exist",
			timerExists:       false,
			serviceExists:     false,
			wantStopCalled:    true,
			wantDisableCalled: true,
			wantDaemonReload:  true,
			// Should succeed silently (idempotent)
		},
		{
			name:              "stop fails",
			timerExists:       true,
			serviceExists:     true,
			stopErr:           errors.New("stop failed"),
			wantStopCalled:    true,
			wantDisableCalled: true,
			wantDaemonReload:  true,
			// Should continue with removal anyway
		},
		{
			name:              "disable fails",
			timerExists:       true,
			serviceExists:     true,
			disableErr:        errors.New("disable failed"),
			wantStopCalled:    true,
			wantDisableCalled: true,
			wantDaemonReload:  true,
			// Should continue with removal anyway
		},
		{
			name:              "daemon-reload fails",
			timerExists:       true,
			serviceExists:     true,
			daemonReloadErr:   errors.New("reload failed"),
			wantErr:           true,
			wantStopCalled:    true,
			wantDisableCalled: true,
			wantDaemonReload:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			mockRunner := &MockSystemctlRunner{
				StopErr:         tt.stopErr,
				DisableErr:      tt.disableErr,
				DaemonReloadErr: tt.daemonReloadErr,
			}

			// Create files if they should exist
			if tt.timerExists {
				_ = os.WriteFile(filepath.Join(tmpDir, "updex-update.timer"), []byte("timer"), 0644)
			}
			if tt.serviceExists {
				_ = os.WriteFile(filepath.Join(tmpDir, "updex-update.service"), []byte("service"), 0644)
			}

			mgr := NewTestManager(tmpDir, mockRunner)

			err := mgr.Remove("updex-update")

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Check Stop was called
			if tt.wantStopCalled {
				if !mockRunner.StopCalled {
					t.Error("Stop() not called")
				}
				if mockRunner.StopUnit != "updex-update.timer" {
					t.Errorf("Stop unit = %q, want %q", mockRunner.StopUnit, "updex-update.timer")
				}
			}

			// Check Disable was called
			if tt.wantDisableCalled {
				if !mockRunner.DisableCalled {
					t.Error("Disable() not called")
				}
				if mockRunner.DisableUnit != "updex-update.timer" {
					t.Errorf("Disable unit = %q, want %q", mockRunner.DisableUnit, "updex-update.timer")
				}
			}

			// Check DaemonReload was called
			if tt.wantDaemonReload && !mockRunner.DaemonReloadCalled {
				t.Error("DaemonReload() not called")
			}

			// Verify files are removed (for successful cases)
			if !tt.wantErr {
				timerPath := filepath.Join(tmpDir, "updex-update.timer")
				if _, err := os.Stat(timerPath); !os.IsNotExist(err) {
					t.Error("timer file should be removed")
				}
				servicePath := filepath.Join(tmpDir, "updex-update.service")
				if _, err := os.Stat(servicePath); !os.IsNotExist(err) {
					t.Error("service file should be removed")
				}
			}
		})
	}
}

func TestExists(t *testing.T) {
	tests := []struct {
		name          string
		timerExists   bool
		serviceExists bool
		want          bool
	}{
		{
			name:          "timer exists",
			timerExists:   true,
			serviceExists: false,
			want:          true,
		},
		{
			name:          "service exists",
			timerExists:   false,
			serviceExists: true,
			want:          true,
		},
		{
			name:          "both exist",
			timerExists:   true,
			serviceExists: true,
			want:          true,
		},
		{
			name:          "neither exists",
			timerExists:   false,
			serviceExists: false,
			want:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			mgr := NewTestManager(tmpDir, &MockSystemctlRunner{})

			if tt.timerExists {
				_ = os.WriteFile(filepath.Join(tmpDir, "updex-update.timer"), []byte("timer"), 0644)
			}
			if tt.serviceExists {
				_ = os.WriteFile(filepath.Join(tmpDir, "updex-update.service"), []byte("service"), 0644)
			}

			got := mgr.Exists("updex-update")
			if got != tt.want {
				t.Errorf("Exists() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewManager(t *testing.T) {
	mgr := NewManager()

	if mgr.UnitPath != "/etc/systemd/system" {
		t.Errorf("UnitPath = %q, want %q", mgr.UnitPath, "/etc/systemd/system")
	}

	if mgr.runner == nil {
		t.Error("runner should not be nil")
	}

	// Verify it's a DefaultSystemctlRunner
	if _, ok := mgr.runner.(*DefaultSystemctlRunner); !ok {
		t.Error("runner should be *DefaultSystemctlRunner")
	}
}

func TestNewTestManager(t *testing.T) {
	customPath := "/custom/path"
	mockRunner := &MockSystemctlRunner{}

	mgr := NewTestManager(customPath, mockRunner)

	if mgr.UnitPath != customPath {
		t.Errorf("UnitPath = %q, want %q", mgr.UnitPath, customPath)
	}

	if mgr.runner != mockRunner {
		t.Error("runner should be the provided mock")
	}
}
