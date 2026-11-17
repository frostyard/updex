package updex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/frostyard/clix"
	"github.com/frostyard/updex/v2/systemd"
	sdk "github.com/frostyard/updex/v2/updex"
)

func TestRunDaemonStatusLastUpdate(t *testing.T) {
	rec := sdk.LastRunRecord{
		Format: 1, Started: "2026-11-05T10:00:00Z", Finished: "2026-11-05T10:00:42Z",
		Outcome: "failed", ErrorClass: "config", Refresh: "not_requested", Selected: true,
		Features: []sdk.LastRunFeature{
			{Feature: "incus", Outcome: "failed", ErrorClass: "network", Version: "1.2.3", Transfers: []sdk.LastRunTransfer{}},
			{Feature: "busy", Outcome: "skipped_busy", Transfers: []sdk.LastRunTransfer{}},
			{Feature: "held", Outcome: "held_by_rollback", Version: "2.0", Transfers: []sdk.LastRunTransfer{}},
			{Feature: "updated-hidden", Outcome: "updated", Transfers: []sdk.LastRunTransfer{}},
			{Feature: "current-hidden", Outcome: "current", Transfers: []sdk.LastRunTransfer{}},
			{Feature: "unsafe\x1b[31m\n", Outcome: "failed", Version: "1\r\t\u202e", Transfers: []sdk.LastRunTransfer{}},
			{Feature: "same-run", Outcome: "needs_image", At: "2026-11-05T10:00:42Z", Transfers: []sdk.LastRunTransfer{}},
			{Feature: "carried", Outcome: "failed", ErrorClass: "checksum", At: "2026-11-01T08:00:00Z", Transfers: []sdk.LastRunTransfer{}},
		},
	}
	for _, tt := range []struct {
		name            string
		installed, json bool
		record          string
	}{
		{name: "text installed", installed: true, record: "good"},
		{name: "text missing timer", record: "good"},
		{name: "text unreadable", installed: true, record: "bad"},
		{name: "text unreadable missing timer", record: "bad"},
		{name: "text no record", installed: true},
		{name: "text no timer no record"},
		{name: "json good", installed: true, json: true, record: "good"},
		{name: "json unreadable", json: true, record: "bad"},
		{name: "json no record", json: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := daemonTestEnv(t, &systemd.MockSystemctlRunner{})
			if tt.installed {
				seedUnits(t, dir)
			}
			path := filepath.Join(t.TempDir(), "last-run.json")
			setRuntimePaths(t, sdk.RuntimePaths{LastRunPath: path})
			if tt.record != "" {
				data := []byte("{")
				if tt.record == "good" {
					var err error
					data, err = json.Marshal(rec)
					if err != nil {
						t.Fatal(err)
					}
				}
				if err := os.WriteFile(path, data, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			clix.JSONOutput = tt.json
			out, err := captureStdout(t, func() error { return runDaemonStatus(daemonTestCommand(t), nil) })
			if err != nil {
				t.Fatal(err)
			}
			if tt.json {
				var got sdk.DaemonStatusResult
				if err := json.Unmarshal([]byte(out), &got); err != nil {
					t.Fatal(err)
				}
				switch tt.record {
				case "good":
					if !strings.Contains(out, `"last_update":`) || !reflect.DeepEqual(got.LastUpdate, &rec) || got.LastUpdateError != "" {
						t.Fatalf("got %s", out)
					}
				case "bad":
					if !strings.Contains(out, `"last_update_error":`) || got.LastUpdate != nil || got.LastUpdateError == "" {
						t.Fatalf("got %s", out)
					}
				default:
					if strings.Contains(out, `"last_update`) {
						t.Fatalf("got %s", out)
					}
				}
				return
			}
			switch tt.record {
			case "good":
				ind := ""
				if tt.installed {
					ind = "  "
				}
				want := ind + "Last update: 2026-11-05T10:00:42Z failed (class config, refresh not_requested, named features)\n" +
					ind + "  incus: failed (class network, version 1.2.3, never ok)\n" +
					ind + "  busy: skipped_busy\n" +
					ind + "  held: held_by_rollback (version 2.0)\n" +
					ind + "  \"unsafe\\x1b[31m\\n\": failed (version \"1\\r\\t\\u202e\", never ok)\n" +
					ind + "  same-run: needs_image\n" +
					ind + "  carried: failed (class checksum, at 2026-11-01T08:00:00Z, never ok)\n"
				if !tt.installed && !strings.HasPrefix(out, "Auto-update daemon: not installed\nRun 'updex daemon enable' to enable automatic updates.\nLast update: ") {
					t.Fatalf("unexpected output without a timer: %q", out)
				}
				if !strings.HasSuffix(out, want) || strings.Contains(out, "-hidden") || strings.ContainsAny(out, "\x1b\r\t\u202e") {
					t.Fatalf("unexpected output: %q", out)
				}
			case "bad":
				want := "Last update: unreadable: malformed last-run record: invalid JSON\n"
				if tt.installed {
					want = "  " + want
				}
				if !strings.HasSuffix(out, "\n"+want) {
					t.Fatalf("got %q", out)
				}
			default:
				if tt.installed {
					if !strings.HasSuffix(out, "  Last update: none recorded\n") {
						t.Fatalf("got %q", out)
					}
				} else if out != "Auto-update daemon: not installed\nRun 'updex daemon enable' to enable automatic updates.\n" {
					t.Fatalf("got %q", out)
				}
			}
		})
	}
}

func TestPrintLastUpdateWithoutOptionalDetails(t *testing.T) {
	for _, installed := range []bool{true, false} {
		out, err := captureStdout(t, func() error {
			printLastUpdate(&sdk.DaemonStatusResult{Installed: installed, LastUpdate: &sdk.LastRunRecord{
				Finished: "2026-11-05T10:00:42Z", Outcome: "failed", Refresh: "not_requested",
			}})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		want := "Last update: 2026-11-05T10:00:42Z failed (refresh not_requested)\n"
		if installed {
			want = "  " + want
		}
		if out != want {
			t.Fatalf("installed=%v: got %q, want %q", installed, out, want)
		}
	}
}

// A feature that keeps failing shows when it last succeeded, or that it never
// did, and each stale feature gets one warning line; JSON carries last_ok and
// stale_features (ADR-0018: a source that is wrong for ever looks offline, so
// the record must show it).
func TestRunDaemonStatusStaleFeatures(t *testing.T) {
	daemonTestEnv(t, &systemd.MockSystemctlRunner{})
	defs := t.TempDir()
	legacy := filepath.Join(defs, "sysupdate.d")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	// off is disabled in the domain, so it is never reported, old as it is.
	for name, enabled := range map[string]string{"docker": "true", "incus": "true", "never": "true", "off": "false"} {
		content := "[Feature]\nDescription=Test feature\nEnabled=" + enabled + "\n"
		if err := os.WriteFile(filepath.Join(legacy, name+".feature"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(t.TempDir(), "last-run.json")
	setRuntimePaths(t, sdk.RuntimePaths{LastRunPath: path, DefinitionRoots: []string{defs}})

	old := "2000-01-01T00:00:00Z"
	fresh := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	rec := sdk.LastRunRecord{
		Format: 1, Started: "2026-11-05T10:00:00Z", Finished: "2026-11-05T10:00:42Z",
		Outcome: "failed", Refresh: "not_requested",
		Features: []sdk.LastRunFeature{
			{Feature: "docker", Outcome: "failed", ErrorClass: "network", LastOK: old, Transfers: []sdk.LastRunTransfer{}},
			{Feature: "incus", Outcome: "failed", ErrorClass: "network", LastOK: fresh, Transfers: []sdk.LastRunTransfer{}},
			{Feature: "never", Outcome: "skipped_unreachable", ErrorClass: "network", Transfers: []sdk.LastRunTransfer{}},
			{Feature: "off", Outcome: "failed", ErrorClass: "network", LastOK: old, Transfers: []sdk.LastRunTransfer{}},
		},
	}
	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := captureStdout(t, func() error { return runDaemonStatus(daemonTestCommand(t), nil) })
	if err != nil {
		t.Fatal(err)
	}
	want := "Last update: 2026-11-05T10:00:42Z failed (refresh not_requested)\n" +
		"  docker: failed (class network, last ok " + old + ")\n" +
		"  incus: failed (class network, last ok " + fresh + ")\n" +
		"  never: skipped_unreachable (class network, never ok)\n" +
		"  off: failed (class network, last ok " + old + ")\n" +
		"  Warning: docker: no successful update since " + old + " (more than 7 days); check its source with 'updex features check docker'\n" +
		"  Warning: never: no successful update recorded; check its source with 'updex features check never'\n"
	if !strings.HasSuffix(out, want) {
		t.Fatalf("got %q, want the tail %q", out, want)
	}
	for _, name := range []string{"incus", "off"} {
		if strings.Contains(out, "Warning: "+name+":") {
			t.Fatalf("got %q, want no warning for a fresh or a disabled feature", out)
		}
	}

	clix.JSONOutput = true
	t.Cleanup(func() { clix.JSONOutput = false })
	jsonOut, err := captureStdout(t, func() error { return runDaemonStatus(daemonTestCommand(t), nil) })
	if err != nil {
		t.Fatal(err)
	}
	var got sdk.DaemonStatusResult
	if err := json.Unmarshal([]byte(jsonOut), &got); err != nil {
		t.Fatal(err)
	}
	wantStale := []sdk.StaleFeature{
		{Feature: "docker", LastOK: old, Outcome: "failed"},
		{Feature: "never", Outcome: "skipped_unreachable"},
	}
	if !reflect.DeepEqual(got.StaleFeatures, wantStale) {
		t.Fatalf("stale features = %+v, want %+v", got.StaleFeatures, wantStale)
	}
	if got.LastUpdate == nil || len(got.LastUpdate.Features) != 4 || got.LastUpdate.Features[0].LastOK != old || got.LastUpdate.Features[2].LastOK != "" {
		t.Fatalf("last update = %+v, want last_ok per feature", got.LastUpdate)
	}
	if !strings.Contains(jsonOut, `"stale_features":`) {
		t.Fatalf("got %s, want stale_features in JSON", jsonOut)
	}
}
