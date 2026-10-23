package updex

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/frostyard/updex/v2/systemd"
)

// newPackagedDaemonTestClient returns a client whose manager has a separate
// temporary packaged unit directory (standing in for /usr/lib/systemd/system).
func newPackagedDaemonTestClient(t *testing.T, runner systemd.SystemctlRunner) (client *Client, etcPath, packagedPath string) {
	t.Helper()
	etcPath = t.TempDir()
	packagedPath = t.TempDir()
	manager := systemd.NewTestManager(etcPath, runner)
	manager.PackagedUnitPath = packagedPath
	return NewClient(ClientConfig{SystemdManager: manager}), etcPath, packagedPath
}

func writeUnit(t *testing.T, dir, suffix, content string) string {
	t.Helper()
	path := filepath.Join(dir, daemonUnitName+suffix)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func seedPackagedUnits(t *testing.T, dir string) {
	t.Helper()
	timer, service := DaemonUnits()
	writeUnit(t, dir, ".timer", systemd.GenerateTimer(timer))
	writeUnit(t, dir, ".service", systemd.GenerateService(service))
}

func dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func TestDaemonUnitsText(t *testing.T) {
	timer, service := DaemonUnits()

	timerText := systemd.GenerateTimer(timer)
	for _, want := range []string{
		"OnCalendar=*-*-* 00/6:00:00\n",
		"RandomizedDelaySec=3600s\n",
		"Persistent=true\n",
		"[Install]\nWantedBy=timers.target\n",
	} {
		if !strings.Contains(timerText, want) {
			t.Errorf("timer unit missing %q:\n%s", want, timerText)
		}
	}
	if strings.HasPrefix(timerText, "#") {
		t.Errorf("DaemonUnits() timer has a comment; the marker belongs to `daemon enable` only:\n%s", timerText)
	}

	serviceText := systemd.GenerateService(service)
	wants := []string{
		"Wants=network-online.target\n",
		"After=network-online.target\n",
		"ConditionACPower=true\n",
		"Type=oneshot\n",
		"ExecStart=/usr/bin/updex features update --no-refresh\n",
	}
	for _, directive := range systemd.SandboxDirectives {
		wants = append(wants, directive+"\n")
	}
	for _, want := range wants {
		if !strings.Contains(serviceText, want) {
			t.Errorf("service unit missing %q:\n%s", want, serviceText)
		}
	}
	if strings.HasPrefix(serviceText, "#") {
		t.Errorf("DaemonUnits() service has a comment:\n%s", serviceText)
	}

	// Each call returns independent values.
	timer.OnCalendar = "changed"
	service.After[0] = "changed"
	timer2, service2 := DaemonUnits()
	if timer2.OnCalendar == "changed" || service2.After[0] == "changed" {
		t.Error("DaemonUnits() shares state between calls")
	}
}

func TestEnableDaemonWithPackagedUnits(t *testing.T) {
	t.Run("enables without writing", func(t *testing.T) {
		runner := &systemd.MockSystemctlRunner{}
		client, etcPath, packagedPath := newPackagedDaemonTestClient(t, runner)
		seedPackagedUnits(t, packagedPath)

		result, err := client.EnableDaemon(t.Context(), EnableDaemonOptions{})
		if err != nil {
			t.Fatalf("EnableDaemon() error = %v", err)
		}
		if !result.Success || !result.Packaged || result.DryRun || result.Message != "Auto-update daemon enabled" {
			t.Errorf("EnableDaemon() result = %+v", result)
		}
		if want := daemonTestUnitFiles(packagedPath); !slices.Equal(result.UnitFiles, want) {
			t.Errorf("EnableDaemon() UnitFiles = %v, want %v", result.UnitFiles, want)
		}
		if got := dirEntries(t, etcPath); len(got) != 0 {
			t.Errorf("EnableDaemon() wrote %v to the /etc unit directory", got)
		}
		if !runner.EnableCalled || runner.EnableUnit != daemonUnitName+".timer" ||
			!runner.StartCalled || runner.StartUnit != daemonUnitName+".timer" {
			t.Errorf("enable/start = (%v %q) (%v %q), want the timer enabled and started",
				runner.EnableCalled, runner.EnableUnit, runner.StartCalled, runner.StartUnit)
		}
		if runner.DaemonReloadCalled {
			t.Error("EnableDaemon() reloaded systemd although it wrote no unit file")
		}
	})

	t.Run("enable again is idempotent", func(t *testing.T) {
		runner := &systemd.MockSystemctlRunner{}
		client, _, packagedPath := newPackagedDaemonTestClient(t, runner)
		seedPackagedUnits(t, packagedPath)
		for range 2 {
			if _, err := client.EnableDaemon(t.Context(), EnableDaemonOptions{}); err != nil {
				t.Fatalf("EnableDaemon() error = %v", err)
			}
		}
	})

	t.Run("etc units override: note", func(t *testing.T) {
		runner := &systemd.MockSystemctlRunner{}
		client, etcPath, packagedPath := newPackagedDaemonTestClient(t, runner)
		seedPackagedUnits(t, packagedPath)
		seedDaemonUnits(t, etcPath)

		result, err := client.EnableDaemon(t.Context(), EnableDaemonOptions{})
		if err != nil {
			t.Fatalf("EnableDaemon() error = %v", err)
		}
		if !strings.Contains(result.Message, "override the packaged units") {
			t.Errorf("EnableDaemon() message = %q, want a note about the /etc override", result.Message)
		}
		if !runner.EnableCalled {
			t.Error("EnableDaemon() did not enable the timer")
		}
	})

	t.Run("dry run writes and calls nothing", func(t *testing.T) {
		runner := &systemd.MockSystemctlRunner{}
		client, etcPath, packagedPath := newPackagedDaemonTestClient(t, runner)
		seedPackagedUnits(t, packagedPath)

		result, err := client.EnableDaemon(t.Context(), EnableDaemonOptions{DryRun: true})
		if err != nil {
			t.Fatalf("EnableDaemon() error = %v", err)
		}
		if !result.DryRun || !result.Packaged || !strings.Contains(result.Message, "nothing is written") {
			t.Errorf("EnableDaemon() dry-run result = %+v", result)
		}
		if got := dirEntries(t, etcPath); len(got) != 0 {
			t.Errorf("dry run wrote %v", got)
		}
		if runner.EnableCalled || runner.StartCalled || runner.DaemonReloadCalled {
			t.Error("dry run touched systemd")
		}
	})

	t.Run("enable failure", func(t *testing.T) {
		runner := &systemd.MockSystemctlRunner{EnableErr: errors.New("masked")}
		client, _, packagedPath := newPackagedDaemonTestClient(t, runner)
		seedPackagedUnits(t, packagedPath)

		_, err := client.EnableDaemon(t.Context(), EnableDaemonOptions{})
		if err == nil || !strings.Contains(err.Error(), "failed to enable timer: masked") {
			t.Fatalf("EnableDaemon() error = %v", err)
		}
		if runner.StartCalled {
			t.Error("EnableDaemon() started the timer after enable failed")
		}
	})
}

func TestEnableDaemonWithoutPackagedUnitsWritesMarker(t *testing.T) {
	runner := &systemd.MockSystemctlRunner{}
	client, etcPath, packagedPath := newPackagedDaemonTestClient(t, runner)
	// The packaged directory exists but does not ship the timer.

	result, err := client.EnableDaemon(t.Context(), EnableDaemonOptions{})
	if err != nil {
		t.Fatalf("EnableDaemon() error = %v", err)
	}
	if result.Packaged {
		t.Error("result.Packaged is set although no package ships the units")
	}
	if got := dirEntries(t, packagedPath); len(got) != 0 {
		t.Errorf("EnableDaemon() wrote %v to the packaged directory", got)
	}
	for _, path := range daemonTestUnitFiles(etcPath) {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if first, _, _ := strings.Cut(string(content), "\n"); first != "# "+daemonMarker {
			t.Errorf("%s first line = %q, want the ownership marker", path, first)
		}
	}
	if !runner.DaemonReloadCalled || !runner.EnableCalled || !runner.StartCalled {
		t.Error("EnableDaemon() did not reload, enable and start")
	}
}

func TestDisableDaemonOwnership(t *testing.T) {
	const marker = "# " + daemonMarker + "\n"
	foreign := "[Unit]\nDescription=shipped by the OS image\n"
	editedLegacy := legacyTimerUnits[0] + "# edited by an administrator\n"

	tests := []struct {
		name         string
		timer        *string // content of the /etc timer, nil for none
		service      *string
		packaged     bool
		wantRemoved  []string // suffixes
		wantKept     []string
		wantKeptNote bool
	}{
		{name: "both marked", timer: ptr(marker + "a"), service: ptr(marker + "b"), wantRemoved: []string{".timer", ".service"}},
		{name: "marked timer, foreign service", timer: ptr(marker + "a"), service: ptr(foreign), wantRemoved: []string{".timer"}, wantKept: []string{".service"}, wantKeptNote: true},
		{name: "foreign timer, marked service", timer: ptr(foreign), service: ptr(marker + "b"), wantRemoved: []string{".service"}, wantKept: []string{".timer"}, wantKeptNote: true},
		{name: "both foreign", timer: ptr(foreign), service: ptr(foreign), wantKept: []string{".timer", ".service"}, wantKeptNote: true},
		{name: "marker not on the first line", timer: ptr("[Unit]\n" + marker), service: ptr("[Unit]\n"), wantKept: []string{".timer", ".service"}, wantKeptNote: true},
		{name: "legacy ADR-0007 units", timer: ptr(legacyTimerUnits[0]), service: ptr(legacyServiceUnits[0]), wantRemoved: []string{".timer", ".service"}},
		{name: "legacy unsandboxed service", timer: ptr(legacyTimerUnits[0]), service: ptr(legacyServiceUnits[1]), wantRemoved: []string{".timer", ".service"}},
		{name: "edited legacy timer is kept", timer: ptr(editedLegacy), service: ptr(legacyServiceUnits[0]), wantRemoved: []string{".service"}, wantKept: []string{".timer"}, wantKeptNote: true},
		{name: "only a marked timer", timer: ptr(marker + "a"), wantRemoved: []string{".timer"}},
		{name: "packaged only", packaged: true},
		{name: "packaged and marked etc", packaged: true, timer: ptr(marker + "a"), service: ptr(marker + "b"), wantRemoved: []string{".timer", ".service"}},
		{name: "packaged and foreign etc", packaged: true, timer: ptr(foreign), service: ptr(foreign), wantKept: []string{".timer", ".service"}, wantKeptNote: true},
	}
	for _, test := range tests {
		for _, dryRun := range []bool{false, true} {
			name := test.name
			if dryRun {
				name += " (dry run)"
			}
			t.Run(name, func(t *testing.T) {
				runner := &systemd.MockSystemctlRunner{}
				client, etcPath, packagedPath := newPackagedDaemonTestClient(t, runner)
				if test.packaged {
					seedPackagedUnits(t, packagedPath)
				}
				before := map[string]string{}
				for suffix, content := range map[string]*string{".timer": test.timer, ".service": test.service} {
					if content != nil {
						writeUnit(t, etcPath, suffix, *content)
						before[suffix] = *content
					}
				}

				result, err := client.DisableDaemon(t.Context(), DisableDaemonOptions{DryRun: dryRun})
				if err != nil {
					t.Fatalf("DisableDaemon() error = %v", err)
				}
				var wantRemoved, wantKept []string
				for _, suffix := range test.wantRemoved {
					wantRemoved = append(wantRemoved, filepath.Join(etcPath, daemonUnitName+suffix))
				}
				for _, suffix := range test.wantKept {
					wantKept = append(wantKept, filepath.Join(etcPath, daemonUnitName+suffix))
				}
				if !slices.Equal(result.UnitFiles, wantRemoved) {
					t.Errorf("UnitFiles = %v, want %v", result.UnitFiles, wantRemoved)
				}
				if !slices.Equal(result.KeptFiles, wantKept) {
					t.Errorf("KeptFiles = %v, want %v", result.KeptFiles, wantKept)
				}
				if got := strings.Contains(result.Message, "kept "); got != test.wantKeptNote {
					t.Errorf("Message = %q, kept note = %v, want %v", result.Message, got, test.wantKeptNote)
				}
				if result.Packaged != test.packaged || result.DryRun != dryRun {
					t.Errorf("result = %+v, want Packaged=%v DryRun=%v", result, test.packaged, dryRun)
				}

				for suffix, content := range before {
					path := filepath.Join(etcPath, daemonUnitName+suffix)
					got, readErr := os.ReadFile(path)
					removed := slices.Contains(test.wantRemoved, suffix)
					switch {
					case dryRun || !removed:
						if readErr != nil || string(got) != content {
							t.Errorf("%s changed: err=%v content=%q, want it kept as %q", path, readErr, got, content)
						}
					case !os.IsNotExist(readErr):
						t.Errorf("%s still exists after disable: %v", path, readErr)
					}
				}
				if test.packaged {
					for _, path := range daemonTestUnitFiles(packagedPath) {
						if _, statErr := os.Lstat(path); statErr != nil {
							t.Errorf("packaged unit %s is gone: %v", path, statErr)
						}
					}
				}

				if dryRun {
					if runner.StopCalled || runner.DisableCalled || runner.DaemonReloadCalled {
						t.Error("dry run touched systemd")
					}
					return
				}
				if !runner.StopCalled || !runner.DisableCalled || !runner.DaemonReloadCalled ||
					runner.StopUnit != daemonUnitName+".timer" || runner.DisableUnit != daemonUnitName+".timer" {
					t.Errorf("stop/disable/reload = %v/%v/%v on %q/%q, want all on the timer",
						runner.StopCalled, runner.DisableCalled, runner.DaemonReloadCalled, runner.StopUnit, runner.DisableUnit)
				}
			})
		}
	}
}

func ptr[T any](v T) *T { return &v }

func TestDisableDaemonRefusesNonRegularUnitFiles(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		t.Run(map[bool]string{false: "real run", true: "dry run"}[dryRun], func(t *testing.T) {
			runner := &systemd.MockSystemctlRunner{}
			client, etcPath, _ := newPackagedDaemonTestClient(t, runner)
			// A symlink whose target carries the marker: it must be neither
			// read through nor removed.
			target := filepath.Join(t.TempDir(), "target")
			if err := os.WriteFile(target, []byte(markedStubUnit), 0o644); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(etcPath, daemonUnitName+".timer")
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}
			serviceDir := filepath.Join(etcPath, daemonUnitName+".service")
			if err := os.Mkdir(serviceDir, 0o755); err != nil {
				t.Fatal(err)
			}

			_, err := client.DisableDaemon(t.Context(), DisableDaemonOptions{DryRun: dryRun})
			if err == nil || !strings.Contains(err.Error(), "not a regular file") || !strings.Contains(err.Error(), "failed to remove timer") {
				t.Fatalf("DisableDaemon() error = %v, want a refusal for non-regular files", err)
			}
			if info, statErr := os.Lstat(link); statErr != nil || info.Mode()&os.ModeSymlink == 0 {
				t.Errorf("symlink %s was removed or replaced: %v", link, statErr)
			}
			if _, statErr := os.Lstat(serviceDir); statErr != nil {
				t.Errorf("directory %s was removed: %v", serviceDir, statErr)
			}
			if _, statErr := os.Stat(target); statErr != nil {
				t.Errorf("symlink target was removed: %v", statErr)
			}
			if dryRun && (runner.StopCalled || runner.DisableCalled) {
				t.Error("dry run touched systemd")
			}
		})
	}
}

// plainRunner implements only the legacy SystemctlRunner, with no ShowRunner
// method, to pin that status leaves next/last run empty instead of failing.
type plainRunner struct{ inner *systemd.MockSystemctlRunner }

func (r plainRunner) DaemonReload() error                 { return r.inner.DaemonReload() }
func (r plainRunner) Enable(unit string) error            { return r.inner.Enable(unit) }
func (r plainRunner) Disable(unit string) error           { return r.inner.Disable(unit) }
func (r plainRunner) Start(unit string) error             { return r.inner.Start(unit) }
func (r plainRunner) Stop(unit string) error              { return r.inner.Stop(unit) }
func (r plainRunner) IsActive(unit string) (bool, error)  { return r.inner.IsActive(unit) }
func (r plainRunner) IsEnabled(unit string) (bool, error) { return r.inner.IsEnabled(unit) }

func TestDaemonStatusRunsAndSources(t *testing.T) {
	const nextRun = "Fri 2026-10-23 18:34:12 EDT"
	const lastRun = "Fri 2026-10-23 12:41:03 EDT"

	tests := []struct {
		name     string
		show     map[string]string
		showErr  error
		etc      bool
		packaged bool
		want     DaemonStatusResult
	}{
		{
			name:     "packaged, ran successfully",
			packaged: true,
			show: map[string]string{
				"NextElapseUSecRealtime": nextRun, "ExecMainExitTimestamp": lastRun, "Result": "success",
			},
			want: DaemonStatusResult{
				Source: DaemonSourcePackaged, Schedule: "*-*-* 00/6:00:00",
				NextRun: nextRun, LastRun: lastRun, LastResult: "success",
			},
		},
		{
			name:     "packaged, last run failed",
			packaged: true,
			show: map[string]string{
				"NextElapseUSecRealtime": nextRun, "ExecMainExitTimestamp": lastRun, "Result": "exit-code",
			},
			want: DaemonStatusResult{
				Source: DaemonSourcePackaged, Schedule: "*-*-* 00/6:00:00",
				NextRun: nextRun, LastRun: lastRun, LastResult: "exit-code",
			},
		},
		{
			name:     "never ran (empty timestamp; Result is the default success and is not reported)",
			packaged: true,
			show: map[string]string{
				"NextElapseUSecRealtime": nextRun, "ExecMainExitTimestamp": "", "Result": "success",
			},
			want: DaemonStatusResult{Source: DaemonSourcePackaged, Schedule: "*-*-* 00/6:00:00", NextRun: nextRun},
		},
		{
			name:     "never ran (n/a), nothing scheduled (0)",
			packaged: true,
			show: map[string]string{
				"NextElapseUSecRealtime": "0", "ExecMainExitTimestamp": "n/a", "Result": "success",
			},
			want: DaemonStatusResult{Source: DaemonSourcePackaged, Schedule: "*-*-* 00/6:00:00"},
		},
		{
			name: "etc units override packaged ones",
			etc:  true, packaged: true,
			want: DaemonStatusResult{Source: DaemonSourceEtc},
		},
		{
			name: "etc units only",
			etc:  true,
			want: DaemonStatusResult{Source: DaemonSourceEtc},
		},
		{
			name:     "show error",
			packaged: true,
			showErr:  errors.New("bus down"),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &systemd.MockSystemctlRunner{
				IsEnabledResult: true, IsActiveResult: true,
				ShowValues: test.show, ShowErr: test.showErr,
			}
			client, etcPath, packagedPath := newPackagedDaemonTestClient(t, runner)
			if test.packaged {
				seedPackagedUnits(t, packagedPath)
			}
			if test.etc {
				seedDaemonUnits(t, etcPath)
			}

			got, err := client.DaemonStatus(t.Context(), DaemonStatusOptions{})
			if test.showErr != nil {
				if !errors.Is(err, test.showErr) || !strings.Contains(err.Error(), "query next run") {
					t.Fatalf("DaemonStatus() error = %v, want the contextual show error", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("DaemonStatus() error = %v", err)
			}
			want := test.want
			want.Installed, want.Enabled, want.Active = true, true, true
			dir := packagedPath
			if test.etc {
				dir = etcPath
			}
			want.UnitFiles = daemonTestUnitFiles(dir)
			if !reflect.DeepEqual(*got, want) {
				t.Errorf("DaemonStatus() = %+v, want %+v", *got, want)
			}
			wantCalls := []string{
				daemonUnitName + ".timer:NextElapseUSecRealtime",
				daemonUnitName + ".service:ExecMainExitTimestamp",
			}
			if test.want.LastRun != "" {
				wantCalls = append(wantCalls, daemonUnitName+".service:Result")
			}
			if !slices.Equal(runner.ShowCalls, wantCalls) {
				t.Errorf("show calls = %v, want %v", runner.ShowCalls, wantCalls)
			}
		})
	}

	t.Run("not installed anywhere", func(t *testing.T) {
		runner := &systemd.MockSystemctlRunner{}
		client, _, _ := newPackagedDaemonTestClient(t, runner)
		got, err := client.DaemonStatus(t.Context(), DaemonStatusOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(*got, DaemonStatusResult{}) {
			t.Errorf("DaemonStatus() = %+v, want the zero result", *got)
		}
		if runner.IsEnabledCalled || len(runner.ShowCalls) != 0 {
			t.Error("DaemonStatus() queried systemctl without a unit file")
		}
	})

	t.Run("runner without show support", func(t *testing.T) {
		inner := &systemd.MockSystemctlRunner{IsEnabledResult: true}
		client, _, packagedPath := newPackagedDaemonTestClient(t, plainRunner{inner})
		seedPackagedUnits(t, packagedPath)
		got, err := client.DaemonStatus(t.Context(), DaemonStatusOptions{})
		if err != nil {
			t.Fatalf("DaemonStatus() error = %v", err)
		}
		if !got.Installed || !got.Enabled || got.NextRun != "" || got.LastRun != "" || got.LastResult != "" {
			t.Errorf("DaemonStatus() = %+v, want installed and enabled with no run times", *got)
		}
	})
}

func TestDaemonStatusSchedule(t *testing.T) {
	tests := []struct {
		name, content, want string
	}{
		{name: "one schedule", content: "[Timer]\nOnCalendar=daily\nPersistent=true\n", want: "daily"},
		{name: "two schedules", content: "[Timer]\nOnCalendar=daily\nOnCalendar=weekly\n", want: "daily, weekly"},
		{name: "none", content: "[Timer]\nPersistent=true\n", want: ""},
		{name: "empty value", content: "[Timer]\nOnCalendar=\n", want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "t.timer")
			if err := os.WriteFile(path, []byte(test.content), 0o644); err != nil {
				t.Fatal(err)
			}
			if got := readOnCalendar(path); got != test.want {
				t.Errorf("readOnCalendar() = %q, want %q", got, test.want)
			}
		})
	}
	if got := readOnCalendar(filepath.Join(t.TempDir(), "missing.timer")); got != "" {
		t.Errorf("readOnCalendar(missing) = %q, want empty", got)
	}
}

func TestLegacyUnitTextsMatchTheOldGenerator(t *testing.T) {
	// What updex wrote before the marker and the 6-hour schedule existed. The
	// generator no longer produces it, so the config is spelled out here, and
	// the frozen literals must still equal its output.
	oldTimer := &systemd.TimerConfig{
		Name: daemonUnitName, Description: "Automatic sysext updates",
		OnCalendar: "daily", Persistent: true, RandomDelaySec: 3600,
	}
	oldService := &systemd.ServiceConfig{
		Name: daemonUnitName, Description: "Automatic sysext update service",
		ExecStart: "/usr/bin/updex features update --no-refresh", Type: "oneshot",
	}
	if got := systemd.GenerateTimer(oldTimer); got != legacyTimerUnits[0] {
		t.Errorf("legacy timer text drifted:\n%s", got)
	}
	if got := systemd.GenerateService(oldService); got != legacyServiceUnits[1] {
		t.Errorf("legacy unsandboxed service text drifted:\n%s", got)
	}
	// The sandboxed legacy text is not compared with the generator: its
	// SandboxDirectives block can change later, and the frozen literal must
	// not follow it. It is the unsandboxed text plus the 18 directives that
	// releases from #329 on wrote.
	sandboxed := strings.TrimPrefix(legacyServiceUnits[0], legacyServiceUnits[1])
	if got := strings.Count(sandboxed, "\n"); got != 18 {
		t.Errorf("legacy sandboxed service adds %d lines to the unsandboxed text, want 18:\n%s", got, sandboxed)
	}
}
