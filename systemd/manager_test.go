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

func testUnitConfigs(name string) (*TimerConfig, *ServiceConfig) {
	return &TimerConfig{
			Name:        name,
			Description: "Test timer",
			OnCalendar:  "daily",
		}, &ServiceConfig{
			Name:        name,
			Description: "Test service",
			Type:        "oneshot",
			ExecStart:   "/usr/bin/updex update",
		}
}

func TestInstall_CleanupOnPartialFailure(t *testing.T) {
	// Test that the timer file is removed if the service write fails after
	// the timer was written. Both unit paths pass the pre-write check (they
	// do not exist), but the service path's parent directory is missing, so
	// its write fails after the timer is already on disk.
	tmpDir := t.TempDir()
	mockRunner := &MockSystemctlRunner{}
	mgr := NewTestManager(tmpDir, mockRunner)

	timer, _ := testUnitConfigs("updex-update")
	_, service := testUnitConfigs("missing-dir/updex-update")

	err := mgr.Install(timer, service)
	if err == nil {
		t.Fatal("expected error for service write failure")
	}
	if !strings.Contains(err.Error(), "failed to write service") {
		t.Errorf("unexpected error: %v", err)
	}

	// Verify timer file was cleaned up
	timerPath := filepath.Join(tmpDir, "updex-update.timer")
	if _, err := os.Lstat(timerPath); !os.IsNotExist(err) {
		t.Error("timer file should have been removed after service write failure")
	}
	if mockRunner.DaemonReloadCalled {
		t.Error("daemon-reload must not run after a failed install")
	}
}

// TestInstall_RefusesNonRegularUnitPaths pins the ADR-0005 rule for the
// unit installer: a dangling symlink, a directory, or a live symlink at
// either unit path is refused before anything is written, the symlink
// target is never created, and no partial state is left behind.
func TestInstall_RefusesNonRegularUnitPaths(t *testing.T) {
	tests := []struct {
		name  string
		plant func(t *testing.T, timerPath, servicePath, target string)
	}{
		{
			name: "dangling symlink at timer path",
			plant: func(t *testing.T, timerPath, _, target string) {
				if err := os.Symlink(target, timerPath); err != nil {
					t.Fatalf("symlink: %v", err)
				}
			},
		},
		{
			name: "dangling symlink at service path",
			plant: func(t *testing.T, _, servicePath, target string) {
				if err := os.Symlink(target, servicePath); err != nil {
					t.Fatalf("symlink: %v", err)
				}
			},
		},
		{
			name: "directory at timer path",
			plant: func(t *testing.T, timerPath, _, _ string) {
				if err := os.Mkdir(timerPath, 0755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
			},
		},
		{
			name: "directory at service path",
			plant: func(t *testing.T, _, servicePath, _ string) {
				if err := os.Mkdir(servicePath, 0755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
			},
		},
		{
			name: "live symlink at timer path",
			plant: func(t *testing.T, timerPath, _, target string) {
				if err := os.WriteFile(target, []byte("real"), 0644); err != nil {
					t.Fatalf("write target: %v", err)
				}
				if err := os.Symlink(target, timerPath); err != nil {
					t.Fatalf("symlink: %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unitDir := t.TempDir()
			outside := t.TempDir()
			target := filepath.Join(outside, "escaped")
			timerPath := filepath.Join(unitDir, "updex-update.timer")
			servicePath := filepath.Join(unitDir, "updex-update.service")
			tt.plant(t, timerPath, servicePath, target)
			targetBefore, targetExisted := "", false
			if data, err := os.ReadFile(target); err == nil {
				targetBefore, targetExisted = string(data), true
			}

			mockRunner := &MockSystemctlRunner{}
			mgr := NewTestManager(unitDir, mockRunner)
			timer, service := testUnitConfigs("updex-update")

			err := mgr.Install(timer, service)
			if err == nil {
				t.Fatal("expected Install to refuse a non-regular unit path")
			}
			if !strings.Contains(err.Error(), "not a regular file") {
				t.Errorf("expected a not-a-regular-file error, got: %v", err)
			}

			// The symlink target must be untouched: not created for a
			// dangling link, not rewritten for a live one.
			data, readErr := os.ReadFile(target)
			switch {
			case targetExisted && (readErr != nil || string(data) != targetBefore):
				t.Errorf("symlink target was modified: %q, %v", data, readErr)
			case !targetExisted && !os.IsNotExist(readErr):
				t.Errorf("symlink target must not be created, got err=%v", readErr)
			}

			// No unit content written anywhere and no temp files left.
			entries, _ := os.ReadDir(unitDir)
			for _, e := range entries {
				if strings.Contains(e.Name(), ".tmp-") {
					t.Errorf("temp file left behind: %s", e.Name())
				}
				if e.Type().IsRegular() && (e.Name() == "updex-update.timer" || e.Name() == "updex-update.service") {
					t.Errorf("unit file %s must not be written", e.Name())
				}
			}
			if mockRunner.DaemonReloadCalled {
				t.Error("daemon-reload must not run after a refused install")
			}
		})
	}
}

// TestInstall_WritesRegularFiles verifies the temp+rename write leaves
// exactly the two 0644 regular unit files and no temp files.
func TestInstall_WritesRegularFiles(t *testing.T) {
	unitDir := t.TempDir()
	mgr := NewTestManager(unitDir, &MockSystemctlRunner{})
	timer, service := testUnitConfigs("updex-update")
	if err := mgr.Install(timer, service); err != nil {
		t.Fatalf("Install: %v", err)
	}
	entries, err := os.ReadDir(unitDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected exactly 2 entries, got %d", len(entries))
	}
	for _, e := range entries {
		info, err := os.Lstat(filepath.Join(unitDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if !info.Mode().IsRegular() {
			t.Errorf("%s is not a regular file: %v", e.Name(), info.Mode())
		}
		if info.Mode().Perm() != 0644 {
			t.Errorf("%s has mode %o, want 0644", e.Name(), info.Mode().Perm())
		}
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
			wantErr:           true,
			wantStopCalled:    true,
			wantDisableCalled: true,
			wantDaemonReload:  true,
		},
		{
			name:              "disable fails",
			timerExists:       true,
			serviceExists:     true,
			disableErr:        errors.New("disable failed"),
			wantErr:           true,
			wantStopCalled:    true,
			wantDisableCalled: true,
			wantDaemonReload:  true,
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
		{
			name:              "stop disable and reload fail",
			timerExists:       true,
			serviceExists:     true,
			stopErr:           errors.New("stop failed"),
			disableErr:        errors.New("disable failed"),
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
			for operation, cause := range map[string]error{
				"stop timer":    tt.stopErr,
				"disable timer": tt.disableErr,
				"daemon-reload": tt.daemonReloadErr,
			} {
				if cause == nil {
					continue
				}
				if !errors.Is(err, cause) {
					t.Errorf("Remove() error = %v, want cause %v", err, cause)
				}
				if !strings.Contains(err.Error(), operation) {
					t.Errorf("Remove() error = %v, want context %q", err, operation)
				}
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

			// Stop, disable, and reload failures must not prevent file cleanup.
			timerPath := filepath.Join(tmpDir, "updex-update.timer")
			if _, err := os.Stat(timerPath); !os.IsNotExist(err) {
				t.Error("timer file should be removed")
			}
			servicePath := filepath.Join(tmpDir, "updex-update.service")
			if _, err := os.Stat(servicePath); !os.IsNotExist(err) {
				t.Error("service file should be removed")
			}
		})
	}
}

func TestExists(t *testing.T) {
	tests := []struct {
		name            string
		timerExists     bool
		serviceExists   bool
		timerDangling   bool
		serviceDangling bool
		want            bool
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
		{
			// A dangling symlink occupies the unit path: Install would
			// refuse to write through it, so Exists reports it as present
			// (os.Stat would have reported it absent).
			name:          "dangling symlink at timer path counts as present",
			timerDangling: true,
			want:          true,
		},
		{
			name:            "dangling symlink at service path counts as present",
			serviceDangling: true,
			want:            true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			mgr := NewTestManager(tmpDir, &MockSystemctlRunner{})

			if tt.timerDangling {
				if err := os.Symlink(filepath.Join(tmpDir, "nope"), filepath.Join(tmpDir, "updex-update.timer")); err != nil {
					t.Fatalf("symlink: %v", err)
				}
			}
			if tt.serviceDangling {
				if err := os.Symlink(filepath.Join(tmpDir, "nope"), filepath.Join(tmpDir, "updex-update.service")); err != nil {
					t.Fatalf("symlink: %v", err)
				}
			}
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

func TestManagerSystemctlPrimitives(t *testing.T) {
	enableErr := errors.New("enable failed")
	startErr := errors.New("start failed")
	enabledErr := errors.New("enabled query failed")
	activeErr := errors.New("active query failed")
	runner := &MockSystemctlRunner{
		EnableErr:       enableErr,
		StartErr:        startErr,
		IsEnabledResult: true,
		IsEnabledErr:    enabledErr,
		IsActiveResult:  true,
		IsActiveErr:     activeErr,
	}
	manager := NewTestManager(t.TempDir(), runner)

	if err := manager.Enable("example.timer"); !errors.Is(err, enableErr) {
		t.Fatalf("Enable() error = %v, want %v", err, enableErr)
	}
	if !runner.EnableCalled || runner.EnableUnit != "example.timer" {
		t.Fatalf("Enable() call = (%v, %q)", runner.EnableCalled, runner.EnableUnit)
	}

	if err := manager.Start("example.timer"); !errors.Is(err, startErr) {
		t.Fatalf("Start() error = %v, want %v", err, startErr)
	}
	if !runner.StartCalled || runner.StartUnit != "example.timer" {
		t.Fatalf("Start() call = (%v, %q)", runner.StartCalled, runner.StartUnit)
	}

	enabled, err := manager.IsEnabled("example.timer")
	if !enabled || !errors.Is(err, enabledErr) {
		t.Fatalf("IsEnabled() = (%v, %v), want (true, %v)", enabled, err, enabledErr)
	}
	if !runner.IsEnabledCalled || runner.IsEnabledUnit != "example.timer" {
		t.Fatalf("IsEnabled() call = (%v, %q)", runner.IsEnabledCalled, runner.IsEnabledUnit)
	}

	active, err := manager.IsActive("example.timer")
	if !active || !errors.Is(err, activeErr) {
		t.Fatalf("IsActive() = (%v, %v), want (true, %v)", active, err, activeErr)
	}
	if !runner.IsActiveCalled || runner.IsActiveUnit != "example.timer" {
		t.Fatalf("IsActive() call = (%v, %q)", runner.IsActiveCalled, runner.IsActiveUnit)
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
