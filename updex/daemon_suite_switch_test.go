package updex

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/frostyard/updex/v2/systemd"
)

// TestSuiteSwitchUnitText pins updex-suite-switch.service (ADR-0021).
func TestSuiteSwitchUnitText(t *testing.T) {
	unit := SuiteSwitchUnit()
	if unit.Name != suiteSwitchUnitName {
		t.Errorf("Name = %q, want %q", unit.Name, suiteSwitchUnitName)
	}
	text := systemd.GenerateService(unit)
	for _, want := range []string{
		"Wants=network-online.target\n",
		"After=sysinit.target basic.target network-online.target\n",
		"Before=updex-update.service shutdown.target\n",
		"DefaultDependencies=no\n",
		"Conflicts=shutdown.target\n",
		"Type=oneshot\n",
		"ExecStart=/usr/bin/updex features update --suite-switch\n",
		"[Install]\nWantedBy=multi-user.target\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("unit has no %q:\n%s", want, text)
		}
	}
	assertSuiteSwitchRetry(t, text)
	assertBootDoesNotWaitForSuiteSwitch(t, map[string]string{"updex-suite-switch.service": text})
	// Every call returns a new value: a caller cannot change the next one,
	// nor the shared RefreshSandboxDirectives.
	unit.Directives[0] = "changed"
	unit.Directives[len(unit.Directives)-1] = "changed"
	if SuiteSwitchUnit().Directives[0] == "changed" {
		t.Error("SuiteSwitchUnit shares its Directives slice")
	}
	if slices.Contains(systemd.RefreshSandboxDirectives, "changed") {
		t.Error("SuiteSwitchUnit shares systemd.RefreshSandboxDirectives")
	}
}

// TestSuiteSwitchUnitRetryIsBounded checks that the start limit of
// updex-suite-switch.service bounds its retries also when every run hangs
// until TimeoutStartSec= (ADR-0022): StartLimitIntervalSec must be longer than
// StartLimitBurst × (TimeoutStartSec + RestartSec). It also checks the checker:
// the 6h window, which lets hung runs restart for ever, and a burst or timeout
// that a later change could raise past the window must fail it.
func TestSuiteSwitchUnitRetryIsBounded(t *testing.T) {
	text := systemd.GenerateService(SuiteSwitchUnit())
	if err := suiteSwitchRetryBound(text); err != nil {
		t.Fatalf("suiteSwitchRetryBound() = %v:\n%s", err, text)
	}
	for name, change := range map[string][2]string{
		"window of 6h":       {"StartLimitIntervalSec=12h\n", "StartLimitIntervalSec=6h\n"},
		"window of 8h 20min": {"StartLimitIntervalSec=12h\n", "StartLimitIntervalSec=8h 20min\n"},
		"burst of 6":         {"StartLimitBurst=4\n", "StartLimitBurst=6\n"},
		"timeout of 3h":      {"TimeoutStartSec=2h\n", "TimeoutStartSec=3h\n"},
		"no window":          {"StartLimitIntervalSec=12h\n", ""},
	} {
		changed := strings.Replace(text, change[0], change[1], 1)
		if changed == text {
			t.Fatalf("%s: the unit has no %q", name, change[0])
		}
		if err := suiteSwitchRetryBound(changed); err == nil {
			t.Errorf("%s: suiteSwitchRetryBound() = nil, want an error", name)
		}
	}
}

// TestSuiteSwitchUnitRetryText pins the exact generated text of the lines that
// ADR-0022 adds to updex-suite-switch.service, in their place: the start
// limit after the [Unit] directives of ADR-0021, and the restart after
// TimeoutStartSec= and before the refresh sandbox.
func TestSuiteSwitchUnitRetryText(t *testing.T) {
	text := systemd.GenerateService(SuiteSwitchUnit())
	for _, want := range []string{
		"DefaultDependencies=no\nConflicts=shutdown.target\nStartLimitIntervalSec=12h\nStartLimitBurst=4\n\n[Service]\n",
		"TimeoutStartSec=2h\nRestart=on-failure\nRestartSec=5min\n" + systemd.RefreshSandboxDirectives[0] + "\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("unit has no %q:\n%s", want, text)
		}
	}
}

// Without packaged units, enable writes the suite-switch unit with the
// marker, enables it (not started) after the timer, and disable disables and
// removes it.
func TestDaemonEnableDisableSuiteSwitchUnit(t *testing.T) {
	runner := &systemd.MockSystemctlRunner{}
	client, unitPath := newDaemonTestClient(t, runner)

	if _, err := client.EnableDaemon(t.Context(), EnableDaemonOptions{}); err != nil {
		t.Fatalf("EnableDaemon() error = %v", err)
	}
	path := filepath.Join(unitPath, suiteSwitchUnitName+".service")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read suite-switch unit: %v", err)
	}
	unit := SuiteSwitchUnit()
	unit.Comment = daemonMarker
	if string(data) != systemd.GenerateService(unit) {
		t.Errorf("suite-switch unit =\n%s\nwant the generator text with the marker", data)
	}
	if want := []string{daemonUnitName + ".timer", suiteSwitchUnitName + ".service"}; !slices.Equal(runner.EnableUnits, want) {
		t.Errorf("enabled %v, want %v", runner.EnableUnits, want)
	}
	if runner.StartUnit != daemonUnitName+".timer" {
		t.Errorf("started %q, want only the timer", runner.StartUnit)
	}

	// A second enable is refused, as for the timer.
	if err := os.Remove(filepath.Join(unitPath, daemonUnitName+".timer")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(unitPath, daemonUnitName+".service")); err != nil {
		t.Fatal(err)
	}
	if _, err := client.EnableDaemon(t.Context(), EnableDaemonOptions{}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("EnableDaemon() over an existing suite-switch unit: %v", err)
	}
	seedDaemonUnits(t, unitPath)

	result, err := client.DisableDaemon(t.Context(), DisableDaemonOptions{})
	if err != nil {
		t.Fatalf("DisableDaemon() error = %v", err)
	}
	if !slices.Contains(result.UnitFiles, path) {
		t.Errorf("DisableDaemon() UnitFiles = %v, want %s", result.UnitFiles, path)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Errorf("suite-switch unit remains: %v", err)
	}
	if !slices.Contains(runner.DisableUnits, suiteSwitchUnitName+".service") {
		t.Errorf("disabled %v, want the suite-switch unit too", runner.DisableUnits)
	}
}

// With packaged units, enable enables the packaged suite-switch unit and
// writes nothing; an older package without it, and a masked one, are left
// alone. Disable disables the packaged unit and removes nothing.
func TestDaemonPackagedSuiteSwitchUnit(t *testing.T) {
	t.Run("packaged", func(t *testing.T) {
		runner := &systemd.MockSystemctlRunner{}
		client, etcPath, packagedPath := newPackagedDaemonTestClient(t, runner)
		seedPackagedUnits(t, packagedPath)
		suitePath := filepath.Join(packagedPath, suiteSwitchUnitName+".service")
		if err := os.WriteFile(suitePath, []byte(systemd.GenerateService(SuiteSwitchUnit())), 0o644); err != nil {
			t.Fatal(err)
		}

		result, err := client.EnableDaemon(t.Context(), EnableDaemonOptions{})
		if err != nil {
			t.Fatalf("EnableDaemon() error = %v", err)
		}
		if !slices.Contains(result.UnitFiles, suitePath) {
			t.Errorf("UnitFiles = %v, want %s", result.UnitFiles, suitePath)
		}
		if want := []string{daemonUnitName + ".timer", suiteSwitchUnitName + ".service"}; !slices.Equal(runner.EnableUnits, want) {
			t.Errorf("enabled %v, want %v", runner.EnableUnits, want)
		}
		if entries := dirEntries(t, etcPath); len(entries) != 0 {
			t.Errorf("enable wrote %v", entries)
		}

		if _, err := client.DisableDaemon(t.Context(), DisableDaemonOptions{}); err != nil {
			t.Fatalf("DisableDaemon() error = %v", err)
		}
		if !slices.Contains(runner.DisableUnits, suiteSwitchUnitName+".service") {
			t.Errorf("disabled %v, want the suite-switch unit too", runner.DisableUnits)
		}
		if _, err := os.Stat(suitePath); err != nil {
			t.Errorf("packaged unit removed: %v", err)
		}
	})

	t.Run("older package", func(t *testing.T) {
		runner := &systemd.MockSystemctlRunner{}
		client, _, packagedPath := newPackagedDaemonTestClient(t, runner)
		seedPackagedUnits(t, packagedPath)

		result, err := client.EnableDaemon(t.Context(), EnableDaemonOptions{})
		if err != nil {
			t.Fatalf("EnableDaemon() error = %v", err)
		}
		if result.Message != "Auto-update daemon enabled" {
			t.Errorf("Message = %q", result.Message)
		}
		if want := []string{daemonUnitName + ".timer"}; !slices.Equal(runner.EnableUnits, want) {
			t.Errorf("enabled %v, want %v", runner.EnableUnits, want)
		}
	})

	t.Run("masked", func(t *testing.T) {
		runner := &systemd.MockSystemctlRunner{}
		client, etcPath, packagedPath := newPackagedDaemonTestClient(t, runner)
		seedPackagedUnits(t, packagedPath)
		if err := os.WriteFile(filepath.Join(packagedPath, suiteSwitchUnitName+".service"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(os.DevNull, filepath.Join(etcPath, suiteSwitchUnitName+".service")); err != nil {
			t.Fatal(err)
		}

		result, err := client.EnableDaemon(t.Context(), EnableDaemonOptions{})
		if err != nil {
			t.Fatalf("EnableDaemon() error = %v", err)
		}
		if !strings.Contains(result.Message, "updex-suite-switch.service is masked and stays disabled") {
			t.Errorf("Message = %q", result.Message)
		}
		if slices.Contains(runner.EnableUnits, suiteSwitchUnitName+".service") {
			t.Errorf("enabled a masked unit: %v", runner.EnableUnits)
		}
		if _, err := client.DisableDaemon(t.Context(), DisableDaemonOptions{}); err != nil {
			t.Fatalf("DisableDaemon() error = %v", err)
		}
		if slices.Contains(runner.DisableUnits, suiteSwitchUnitName+".service") {
			t.Errorf("disabled a masked unit: %v", runner.DisableUnits)
		}
	})
}

// Status reports the suite-switch unit's file and enabled state.
func TestDaemonStatusSuiteSwitchUnit(t *testing.T) {
	runner := &systemd.MockSystemctlRunner{IsEnabledResult: true}
	client, unitPath := newDaemonTestClient(t, runner)
	seedDaemonUnits(t, unitPath)

	status, err := client.DaemonStatus(t.Context(), DaemonStatusOptions{})
	if err != nil {
		t.Fatalf("DaemonStatus() error = %v", err)
	}
	if status.SuiteSwitchInstalled || status.SuiteSwitchEnabled {
		t.Errorf("status = %+v, want no suite-switch unit", status)
	}

	path := filepath.Join(unitPath, suiteSwitchUnitName+".service")
	if err := os.WriteFile(path, []byte(markedStubUnit), 0o644); err != nil {
		t.Fatal(err)
	}
	status, err = client.DaemonStatus(t.Context(), DaemonStatusOptions{})
	if err != nil {
		t.Fatalf("DaemonStatus() error = %v", err)
	}
	if !status.SuiteSwitchInstalled || !status.SuiteSwitchEnabled || !slices.Contains(status.UnitFiles, path) {
		t.Errorf("status = %+v, want the suite-switch unit installed and enabled", status)
	}
	if !slices.Contains(runner.IsEnabledUnits, suiteSwitchUnitName+".service") {
		t.Errorf("IsEnabled calls = %v", runner.IsEnabledUnits)
	}
}
