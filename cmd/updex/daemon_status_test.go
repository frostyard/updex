package updex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

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
				want := "  Last update: 2026-11-05T10:00:42Z failed (class config, refresh not_requested, named features)\n" +
					"    incus: failed (class network, version 1.2.3)\n" +
					"    busy: skipped_busy\n" +
					"    held: held_by_rollback (version 2.0)\n" +
					"    \"unsafe\\x1b[31m\\n\": failed (version \"1\\r\\t\\u202e\")\n"
				if !strings.HasSuffix(out, want) || strings.Contains(out, "-hidden") || strings.ContainsAny(out, "\x1b\r\t\u202e") {
					t.Fatalf("unexpected output: %q", out)
				}
			case "bad":
				if !strings.Contains(out, "  Last update: unreadable: malformed last-run record: invalid JSON\n") {
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
	out, err := captureStdout(t, func() error {
		printLastUpdate(&sdk.DaemonStatusResult{LastUpdate: &sdk.LastRunRecord{
			Finished: "2026-11-05T10:00:42Z", Outcome: "failed", Refresh: "not_requested",
		}})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if out != "  Last update: 2026-11-05T10:00:42Z failed (refresh not_requested)\n" {
		t.Fatalf("got %q", out)
	}
}
