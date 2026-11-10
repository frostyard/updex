package updex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/frostyard/updex/v2/systemd"
)

func statusRecord() LastRunRecord {
	return LastRunRecord{
		Format: lastRunFormat, Started: "2026-11-05T10:00:00Z", Finished: "2026-11-05T10:00:42Z",
		Outcome: runFailed, Refresh: refreshNotRequested,
		Features: []LastRunFeature{{Feature: "incus", Origin: "image", Outcome: outcomeFailed,
			ErrorClass: "network", Version: "1.2.3", At: "2026-11-05T10:00:42Z",
			Transfers: []LastRunTransfer{{Component: "incus", Outcome: outcomeFailed, ErrorClass: "network", Version: "1.2.3"}}}},
	}
}

func writeStatusRecord(t *testing.T, path string, rec LastRunRecord) []byte {
	t.Helper()
	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return data
}

func TestReadLastRunStatus(t *testing.T) {
	valid, err := json.Marshal(statusRecord())
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name       string
		data       string
		setup      func(*testing.T, string)
		wantError  string
		wantRecord bool
	}{
		{name: "absent"},
		{name: "valid", data: string(valid), wantRecord: true},
		{name: "unknown fields", data: strings.Replace(string(valid), `"format":1`, `"future":true,"format":1`, 1), wantRecord: true},
		{name: "unknown format", data: `{"format":2,"features":false}`},
		{name: "missing format", data: `{}`},
		{name: "zero format", data: `{"format":0}`},
		{name: "malformed", data: `{"format":`, wantError: "invalid JSON"},
		{name: "trailing JSON", data: string(valid) + `{}`, wantError: "invalid JSON"},
		{name: "wrong field type", data: strings.Replace(string(valid), `"features":[`, `"features":"bad","unused":[`, 1), wantError: "invalid field type"},
		{name: "missing started", data: strings.Replace(string(valid), `"started":"2026-11-05T10:00:00Z",`, "", 1), wantError: "timestamps"},
		{name: "invalid finished", data: strings.Replace(string(valid), `"finished":"2026-11-05T10:00:42Z"`, `"finished":"bad"`, 1), wantError: "timestamps"},
		{name: "missing outcome", data: strings.Replace(string(valid), `"outcome":"failed",`, "", 1), wantError: "outcome, refresh and features"},
		{name: "empty refresh", data: strings.Replace(string(valid), `"refresh":"not_requested"`, `"refresh":""`, 1), wantError: "outcome, refresh and features"},
		{name: "null features", data: `{"format":1,"started":"2026-11-05T10:00:00Z","finished":"2026-11-05T10:00:42Z","outcome":"failed","refresh":"not_run","features":null}`, wantError: "features are required"},
		{name: "missing features", data: `{"format":1,"started":"2026-11-05T10:00:00Z","finished":"2026-11-05T10:00:42Z","outcome":"failed","refresh":"not_run"}`, wantError: "features are required"},
		{name: "empty feature", data: strings.Replace(string(valid), `"feature":"incus"`, `"feature":""`, 1), wantError: "feature name"},
		{name: "empty feature outcome", data: strings.Replace(string(valid), `"origin":"image","outcome":"failed"`, `"origin":"image","outcome":""`, 1), wantError: "feature name"},
		{name: "null transfers", data: `{"format":1,"started":"2026-11-05T10:00:00Z","finished":"2026-11-05T10:00:42Z","outcome":"failed","refresh":"not_run","features":[{"feature":"incus","outcome":"failed","transfers":null}]}`, wantError: "transfers are required"},
		{name: "symlink", setup: func(t *testing.T, path string) {
			writeStatusRecord(t, path+"-target", statusRecord())
			if err := os.Symlink(path+"-target", path); err != nil {
				t.Fatal(err)
			}
		}, wantError: "not a regular file"},
		{name: "fifo", setup: func(t *testing.T, path string) {
			if err := syscall.Mkfifo(path, 0o600); err != nil {
				t.Fatal(err)
			}
		}, wantError: "not a regular file"},
		{name: "directory", setup: func(t *testing.T, path string) {
			if err := os.Mkdir(path, 0o755); err != nil {
				t.Fatal(err)
			}
		}, wantError: "not a regular file"},
		{name: "parent not directory", setup: func(t *testing.T, path string) {
			if err := os.WriteFile(filepath.Dir(path), nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}, wantError: "check last-run record"},
		{name: "oversize", data: strings.Repeat(" ", maxLastRunRecordSize+1), wantError: "larger than 1 MiB"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "record")
			if tt.name == "parent not directory" {
				path = filepath.Join(path, "record")
			}
			if tt.setup != nil {
				tt.setup(t, path)
			}
			if tt.data != "" {
				if err := os.WriteFile(path, []byte(tt.data), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			got, err := readLastRunStatus(path)
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) || got != nil {
					t.Fatalf("read = %+v, %v, want error containing %q", got, err, tt.wantError)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tt.wantRecord {
				want := statusRecord()
				if !reflect.DeepEqual(got, &want) {
					t.Fatalf("got %+v, want %+v", got, want)
				}
			} else if got != nil {
				t.Fatalf("got %+v, want no record", got)
			}
		})
	}
}

func TestReadLastRunStatusNormalization(t *testing.T) {
	rec := statusRecord()
	rec.Outcome, rec.Refresh, rec.ErrorClass = "future", "future", "future"
	rec.Features[0].Outcome, rec.Features[0].ErrorClass = "future", "future"
	rec.Features[0].Transfers[0].Outcome, rec.Features[0].Transfers[0].ErrorClass = "future", "future"
	path := filepath.Join(t.TempDir(), "record")
	writeStatusRecord(t, path, rec)
	got, err := readLastRunStatus(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != "other" || got.Refresh != "other" || got.ErrorClass != "other" ||
		got.Features[0].Outcome != "other" || got.Features[0].ErrorClass != "other" ||
		got.Features[0].Transfers[0].Outcome != "other" || got.Features[0].Transfers[0].ErrorClass != "other" {
		t.Fatalf("not normalized: %+v, %+v", got, got.Features[0])
	}
	// Status normalization must not affect the writer's carry reader or disk.
	if previous := readLastRunRecord(path); !reflect.DeepEqual(previous, &rec) {
		t.Fatalf("writer reader changed: %+v", previous)
	}
	for _, outcome := range []string{outcomeUpdated, outcomeCurrent, outcomeHeldByRollback, outcomeNeedsImage,
		outcomeFailed, outcomeSkippedUnreachable, outcomeSkippedBusy, outcomeNotLinked} {
		t.Run(outcome, func(t *testing.T) {
			if got := normalizeLastRunOutcome(outcome, true); got != outcome {
				t.Fatalf("got %q", got)
			}
			if outcome == outcomeNotLinked && normalizeLastRunOutcome(outcome, false) != "other" {
				t.Fatal("not_linked is not a feature outcome")
			}
		})
	}
}

func TestReadLastRunStatusWriterRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "record")
	client := NewClient(ClientConfig{Paths: RuntimePaths{LastRunPath: path}})
	run := newLastRun(time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC), UpdateFeaturesOptions{})
	finished := run.started.Add(42 * time.Second)
	client.writeLastRun(run, finished, nil)
	got, err := readLastRunStatus(path)
	want := run.record(finished, nil)
	if err != nil || !reflect.DeepEqual(got, &want) {
		t.Fatalf("got %+v, %v, want %+v", got, err, want)
	}
}

func TestDaemonStatusLastUpdate(t *testing.T) {
	for _, installed := range []bool{false, true} {
		for _, kind := range []string{"valid", "invalid", "absent", "unknown"} {
			t.Run(kind+map[bool]string{false: " without timer", true: " with timer"}[installed], func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "record")
				unitDir := t.TempDir()
				if installed {
					seedDaemonUnits(t, unitDir)
				}
				var before []byte
				switch kind {
				case "valid":
					before = writeStatusRecord(t, path, statusRecord())
				case "invalid", "unknown":
					before = []byte(`{"format":`)
					if kind == "unknown" {
						before = []byte(`{"format":2}`)
					}
					if err := os.WriteFile(path, before, 0o644); err != nil {
						t.Fatal(err)
					}
				}
				lockPath := filepath.Join(t.TempDir(), "unused-lock")
				client := NewClient(ClientConfig{SystemdManager: systemd.NewTestManager(unitDir, &systemd.MockSystemctlRunner{}),
					Paths: RuntimePaths{LastRunPath: path, LockPath: lockPath}})
				got, err := client.DaemonStatus(t.Context(), DaemonStatusOptions{})
				if err != nil {
					t.Fatal(err)
				}
				if got.Installed != installed {
					t.Fatalf("installed = %v", got.Installed)
				}
				if kind == "valid" {
					want := statusRecord()
					if !reflect.DeepEqual(got.LastUpdate, &want) || got.LastUpdateError != "" {
						t.Fatalf("got %+v", got)
					}
				} else if got.LastUpdate != nil || (got.LastUpdateError != "") != (kind == "invalid") {
					t.Fatalf("got %+v", got)
				}
				if _, err := os.Lstat(lockPath); !os.IsNotExist(err) {
					t.Fatalf("status touched lock: %v", err)
				}
				after, err := os.ReadFile(path)
				if kind == "absent" {
					if !os.IsNotExist(err) {
						t.Fatal("status created a record")
					}
				} else if err != nil || string(before) != string(after) {
					t.Fatalf("status changed record: %v", err)
				}
			})
		}
	}
}
