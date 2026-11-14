package updex

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/frostyard/std/reporter"
	"github.com/frostyard/updex/v2/internal/fault"
	"github.com/frostyard/updex/v2/internal/testutil"
	"github.com/frostyard/updex/v2/sysext"
)

// Tests for the last-run record (ADR-0020, docs/specs/last-run-record.md).

// lastRunFixture is a definitions directory with enabled features
// feature-<component>, one transfer each, and a client whose last-run record
// is lastRun.
type lastRunFixture struct {
	t         *testing.T
	client    *Client
	configDir string
	lastRun   string
	out       *bytes.Buffer
}

func newLastRunFixture(t *testing.T, sources map[string]string, configure func(*ClientConfig)) *lastRunFixture {
	t.Helper()
	f := &lastRunFixture{
		t:         t,
		configDir: t.TempDir(),
		lastRun:   filepath.Join(t.TempDir(), "updex", "last-run.json"),
		out:       &bytes.Buffer{},
	}
	targetDir := t.TempDir()
	for component, source := range sources {
		feature := featureForComponent(component)
		createFeatureTransferFile(t, f.configDir, component, feature, source)
		createFeatureFile(t, f.configDir, feature, true)
	}
	updateTransferTargetPath(t, f.configDir, targetDir)

	cfg := ClientConfig{
		Definitions:  f.configDir,
		SysextRunner: &sysext.MockRunner{},
		Progress:     reporter.NewTextReporter(f.out),
		Paths: RuntimePaths{
			SysextLinkDir:     t.TempDir(),
			SysextHierarchies: []string{t.TempDir()},
			RunExtensionsDir:  t.TempDir(),
			LockPath:          filepath.Join(t.TempDir(), "lock"),
			RollbackDir:       t.TempDir(),
			LastRunPath:       f.lastRun,
		},
	}
	if configure != nil {
		configure(&cfg)
	}
	f.client = NewClient(cfg)
	return f
}

// readLastRun decodes the record at path; it fails the test when there is none.
func readLastRun(t *testing.T, path string) (LastRunRecord, []byte) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read last-run record: %v", err)
	}
	var rec LastRunRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatalf("decode last-run record %q: %v", data, err)
	}
	if rec.Format != 1 {
		t.Errorf("format = %d, want 1", rec.Format)
	}
	return rec, data
}

func noLastRun(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Errorf("last-run record exists (err = %v); none may be written", err)
	}
}

func featureEntry(t *testing.T, rec LastRunRecord, name string) LastRunFeature {
	t.Helper()
	for _, f := range rec.Features {
		if f.Feature == name {
			return f
		}
	}
	t.Fatalf("record has no feature %q: %+v", name, rec.Features)
	return LastRunFeature{}
}

// closedPortURL returns a loopback URL that refuses the connection.
func closedPortURL(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	source := "http://" + listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	return source
}

// goodServer publishes testext/othertex version 1.0.0 with a correct hash.
func goodServer(t *testing.T, component string) string {
	t.Helper()
	content := []byte(component + " image 1.0.0")
	server := testutil.NewTestServer(t, testutil.TestServerFiles{
		Files:   map[string]string{component + "_1.0.0.raw": hashContent(content)},
		Content: map[string][]byte{component + "_1.0.0.raw": content},
	})
	t.Cleanup(server.Close)
	return server.URL
}

// noErrorText fails when the record carries a result's error text or a URL.
func noErrorText(t *testing.T, data []byte, results []UpdateFeaturesResult, urls ...string) {
	t.Helper()
	if bytes.Contains(data, []byte("://")) {
		t.Errorf("record carries a URL:\n%s", data)
	}
	for _, u := range urls {
		host := strings.TrimPrefix(u, "http://")
		if bytes.Contains(data, []byte(host)) {
			t.Errorf("record carries the source %s:\n%s", host, data)
		}
	}
	for _, fr := range results {
		for _, r := range fr.Results {
			if r.Error != "" && bytes.Contains(data, []byte(r.Error)) {
				t.Errorf("record carries the error text %q", r.Error)
			}
			for _, word := range []string{"failed", "mismatch", "status", "signature not found", "not linked"} {
				if r.Error != "" && strings.Contains(r.Error, word) && bytes.Contains(data, []byte(word+" ")) {
					t.Errorf("record carries %q from the error text:\n%s", word, data)
				}
			}
		}
	}
}

// TestLastRun_ErrorClasses runs one failing feature per error class through
// UpdateFeatures and reads the class back from the record.
func TestLastRun_ErrorClasses(t *testing.T) {
	content := []byte("testext image 1.0.0")
	other := []byte("other bytes, so the hash does not match")
	enospc := fmt.Errorf("link: %w", &os.PathError{Op: "symlink", Path: "testext.raw", Err: syscall.ENOSPC})

	tests := []struct {
		name string
		// source returns the testext source; a second feature, othertex,
		// always answers, so no run is unreachable.
		source      func(t *testing.T) string
		configure   func(*ClientConfig)
		patterns    string // target MatchPattern of testext, when set
		wantClass   string
		wantVersion string
	}{
		{
			name:      "network: closed port",
			source:    closedPortURL,
			wantClass: "network",
		},
		{
			name: "http_status: 404 on SHA256SUMS",
			source: func(t *testing.T) string {
				s := testutil.NewErrorServer(t, http.StatusNotFound)
				t.Cleanup(s.Close)
				return s.URL
			},
			wantClass: "http_status",
		},
		{
			name: "http_status: 404 on the image",
			source: func(t *testing.T) string {
				s := testutil.NewTestServer(t, testutil.TestServerFiles{Files: map[string]string{"testext_1.0.0.raw": hashContent(content)}})
				t.Cleanup(s.Close)
				return s.URL
			},
			wantClass:   "http_status",
			wantVersion: "1.0.0",
		},
		{
			name:      "signature: 404 on SHA256SUMS.gpg",
			source:    func(t *testing.T) string { return goodServer(t, "testext") },
			configure: func(cfg *ClientConfig) { cfg.Verify = true },
			wantClass: "signature",
		},
		{
			name: "checksum: wrong hash",
			source: func(t *testing.T) string {
				s := testutil.NewTestServer(t, testutil.TestServerFiles{
					Files:   map[string]string{"testext_1.0.0.raw": hashContent(other)},
					Content: map[string][]byte{"testext_1.0.0.raw": content},
				})
				t.Cleanup(s.Close)
				return s.URL
			},
			wantClass:   "checksum",
			wantVersion: "1.0.0",
		},
		{
			name:        "disk_space: the link fails with ENOSPC",
			source:      func(t *testing.T) string { return goodServer(t, "testext") },
			configure:   func(cfg *ClientConfig) { cfg.SysextRunner = &sysext.MockRunner{LinkToSysextErr: enospc} },
			wantClass:   "disk_space",
			wantVersion: "1.0.0",
		},
		{
			name:   "link: the link fails",
			source: func(t *testing.T) string { return goodServer(t, "testext") },
			configure: func(cfg *ClientConfig) {
				cfg.SysextRunner = &sysext.MockRunner{LinkToSysextErr: errors.New("injected link failure")}
			},
			wantClass:   "link",
			wantVersion: "1.0.0",
		},
		{
			name:        "config: invalid target pattern",
			source:      func(t *testing.T) string { return goodServer(t, "testext") },
			patterns:    "testext.raw",
			wantClass:   "config",
			wantVersion: "1.0.0",
		},
		{
			name: "other: no versions available",
			source: func(t *testing.T) string {
				s := testutil.NewTestServer(t, testutil.TestServerFiles{})
				t.Cleanup(s.Close)
				return s.URL
			},
			wantClass: "other",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := tt.source(t)
			otherURL := goodServer(t, "othertex")
			f := newLastRunFixture(t, map[string]string{"testext": source, "othertex": otherURL}, tt.configure)
			if tt.patterns != "" {
				createTransferFileWithPatterns(t, f.configDir, "testext", featureForComponent("testext"), source, "testext_@v.raw", tt.patterns)
				updateTransferTargetPath(t, f.configDir, t.TempDir())
			}
			// Signature: the other feature must not fail the same way.
			if tt.wantClass == "signature" {
				if err := os.Remove(filepath.Join(f.configDir, "othertex.transfer")); err != nil {
					t.Fatal(err)
				}
			}

			started := time.Now().UTC().Truncate(time.Second)
			results, err := f.client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{NoRefresh: true})
			if err == nil {
				t.Fatal("UpdateFeatures() error = nil, want the failure")
			}
			if errors.Is(err, ErrSourcesUnreachable) {
				t.Fatalf("UpdateFeatures() error = %v: another source answered", err)
			}

			rec, data := readLastRun(t, f.lastRun)
			if rec.Outcome != "failed" || rec.ErrorClass != "" || rec.Refresh != "not_requested" || rec.Selected {
				t.Errorf("run = %+v, want failed, no class, refresh not_requested, not selected", rec)
			}
			if s, err := time.Parse(time.RFC3339, rec.Started); err != nil || s.Before(started) || !strings.HasSuffix(rec.Started, "Z") {
				t.Errorf("started = %q (%v), want RFC 3339 UTC at or after %s", rec.Started, err, started)
			}
			fe := featureEntry(t, rec, "feature-testext")
			if fe.Outcome != "failed" || fe.ErrorClass != tt.wantClass || fe.Origin != "unknown" || fe.At != rec.Finished {
				t.Errorf("feature = %+v, want failed/%s, origin unknown, at %s", fe, tt.wantClass, rec.Finished)
			}
			if len(fe.Transfers) != 1 || fe.Transfers[0].Component != "testext" || fe.Transfers[0].ErrorClass != tt.wantClass || fe.Transfers[0].Version != tt.wantVersion {
				t.Errorf("transfers = %+v, want testext failed/%s version %q", fe.Transfers, tt.wantClass, tt.wantVersion)
			}
			if fe.Version != tt.wantVersion {
				t.Errorf("feature version = %q, want %q", fe.Version, tt.wantVersion)
			}
			if tt.wantClass != "signature" && tt.wantClass != "link" && tt.wantClass != "disk_space" {
				ok := featureEntry(t, rec, "feature-othertex")
				if ok.Outcome != "updated" || ok.ErrorClass != "" || ok.Version != "1.0.0" {
					t.Errorf("othertex = %+v, want updated 1.0.0", ok)
				}
			}
			noErrorText(t, data, results, source, otherURL)
		})
	}
}

// Every source refuses the connection: the run, the feature and the transfer
// are skipped_unreachable with class network.
func TestLastRun_Unreachable(t *testing.T) {
	source := closedPortURL(t)
	f := newLastRunFixture(t, map[string]string{"testext": source}, nil)

	results, err := f.client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{NoRefresh: true})
	if !errors.Is(err, ErrSourcesUnreachable) {
		t.Fatalf("UpdateFeatures() error = %v, want ErrSourcesUnreachable", err)
	}
	rec, data := readLastRun(t, f.lastRun)
	if rec.Outcome != "skipped_unreachable" || rec.ErrorClass != "" {
		t.Errorf("run outcome = %q class %q, want skipped_unreachable and no class", rec.Outcome, rec.ErrorClass)
	}
	fe := featureEntry(t, rec, "feature-testext")
	if fe.Outcome != "skipped_unreachable" || fe.ErrorClass != "network" {
		t.Errorf("feature = %+v, want skipped_unreachable/network", fe)
	}
	if len(fe.Transfers) != 1 || fe.Transfers[0].Outcome != "skipped_unreachable" || fe.Transfers[0].ErrorClass != "network" {
		t.Errorf("transfers = %+v", fe.Transfers)
	}
	noErrorText(t, data, results, source)
}

// A busy run records every feature it would have acted on as skipped_busy,
// but never replaces a record the lock holder wrote after it started.
func TestLastRun_Busy(t *testing.T) {
	t.Run("skipped_busy", func(t *testing.T) {
		f := newLastRunFixture(t, map[string]string{"testext": goodServer(t, "testext"), "othertex": goodServer(t, "othertex")}, nil)
		// A disabled feature is not listed.
		createFeatureTransferFile(t, f.configDir, "offext", "feature-offext", "http://127.0.0.1:1")
		createFeatureFile(t, f.configDir, "feature-offext", false)
		release, err := f.client.lockMutation(false)
		if err != nil {
			t.Fatal(err)
		}
		defer release()

		_, err = f.client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{})
		if !errors.Is(err, ErrBusy) {
			t.Fatalf("UpdateFeatures() error = %v, want ErrBusy", err)
		}
		if f.out.Len() != 0 {
			t.Errorf("a busy run printed %q; its output must not change", f.out.String())
		}
		rec, _ := readLastRun(t, f.lastRun)
		if rec.Outcome != "skipped_busy" || rec.Refresh != "not_run" || rec.ErrorClass != "" {
			t.Errorf("run = %+v, want skipped_busy, refresh not_run", rec)
		}
		if len(rec.Features) != 2 {
			t.Fatalf("features = %+v, want the 2 enabled features", rec.Features)
		}
		for _, fe := range rec.Features {
			if fe.Outcome != "skipped_busy" || fe.ErrorClass != "" || fe.At != rec.Finished || len(fe.Transfers) != 1 || fe.Transfers[0].Outcome != "skipped_busy" {
				t.Errorf("feature = %+v, want skipped_busy", fe)
			}
		}
	})

	t.Run("named busy run lists the named feature", func(t *testing.T) {
		f := newLastRunFixture(t, map[string]string{"testext": goodServer(t, "testext"), "othertex": goodServer(t, "othertex")}, nil)
		release, err := f.client.lockMutation(false)
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		_, err = f.client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{Features: []string{"feature-othertex"}})
		if !errors.Is(err, ErrBusy) {
			t.Fatalf("UpdateFeatures() error = %v, want ErrBusy", err)
		}
		rec, _ := readLastRun(t, f.lastRun)
		if !rec.Selected || len(rec.Features) != 1 || rec.Features[0].Feature != "feature-othertex" {
			t.Errorf("record = %+v, want only feature-othertex, selected", rec)
		}
	})

	t.Run("a newer record is kept", func(t *testing.T) {
		f := newLastRunFixture(t, map[string]string{"testext": goodServer(t, "testext")}, nil)
		newer := LastRunRecord{
			Format: 1, Started: recordTime(time.Now()), Finished: recordTime(time.Now().Add(time.Hour)),
			Outcome: "completed", Refresh: "done", Features: []LastRunFeature{},
		}
		data, _ := json.Marshal(newer)
		if err := os.MkdirAll(filepath.Dir(f.lastRun), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f.lastRun, data, 0o644); err != nil {
			t.Fatal(err)
		}
		release, err := f.client.lockMutation(false)
		if err != nil {
			t.Fatal(err)
		}
		defer release()

		if _, err := f.client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{}); !errors.Is(err, ErrBusy) {
			t.Fatalf("UpdateFeatures() error = %v, want ErrBusy", err)
		}
		got, err := os.ReadFile(f.lastRun)
		if err != nil || !bytes.Equal(got, data) {
			t.Errorf("record = %s (%v), want the newer record kept: %s", got, err, data)
		}
	})

	t.Run("an older record is replaced", func(t *testing.T) {
		f := newLastRunFixture(t, map[string]string{"testext": goodServer(t, "testext")}, nil)
		older := LastRunRecord{
			Format: 1, Started: "2020-01-01T00:00:00Z", Finished: "2020-01-01T00:00:10Z",
			Outcome: "completed", Refresh: "done", Features: []LastRunFeature{},
		}
		data, _ := json.Marshal(older)
		if err := os.MkdirAll(filepath.Dir(f.lastRun), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f.lastRun, data, 0o644); err != nil {
			t.Fatal(err)
		}
		release, err := f.client.lockMutation(false)
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		if _, err := f.client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{}); !errors.Is(err, ErrBusy) {
			t.Fatalf("UpdateFeatures() error = %v, want ErrBusy", err)
		}
		rec, _ := readLastRun(t, f.lastRun)
		if rec.Outcome != "skipped_busy" {
			t.Errorf("outcome = %q, want skipped_busy", rec.Outcome)
		}
	})
}

// updated, current, needs_image and the not_linked sibling, through the real
// link step of a minImageHost.
func TestLastRun_UpdatedCurrentNeedsImage(t *testing.T) {
	h := newMinImageHost(t, bootcNotInstalled(), osRelease20261001,
		minComp{name: "first", feature: "pair", versions: map[string][]byte{newVersion: sysextImage(t, "first", "")}},
		minComp{name: "second", feature: "pair", versions: map[string][]byte{newVersion: sysextImage(t, "second", "20270101000000")}},
		minComp{name: "other", feature: "single", versions: map[string][]byte{newVersion: sysextImage(t, "other", "")}},
	)
	path := filepath.Join(t.TempDir(), "last-run.json")
	h.client.paths.lastRunPath = path
	h.preinstall("first", oldVersion, true)
	h.preinstall("second", oldVersion, true)
	h.preinstall("other", oldVersion, true)

	if _, err := h.update(UpdateFeaturesOptions{}); err != nil {
		t.Fatalf("UpdateFeatures: %v", err)
	}
	rec, _ := readLastRun(t, path)
	if rec.Outcome != "completed" || rec.Refresh != "not_requested" {
		t.Errorf("run = %+v, want completed, refresh not_requested", rec)
	}
	pair := featureEntry(t, rec, "pair")
	if pair.Outcome != "needs_image" || pair.ErrorClass != "" || pair.Version != newVersion || pair.Origin != "local" {
		t.Errorf("pair = %+v, want needs_image %s, origin local", pair, newVersion)
	}
	byComp := map[string]LastRunTransfer{}
	for _, tr := range pair.Transfers {
		byComp[tr.Component] = tr
	}
	if byComp["second"].Outcome != "needs_image" || byComp["second"].Version != newVersion {
		t.Errorf("second = %+v, want needs_image %s", byComp["second"], newVersion)
	}
	if byComp["first"].Outcome != "not_linked" || byComp["first"].ErrorClass != "" {
		t.Errorf("first = %+v, want not_linked", byComp["first"])
	}
	single := featureEntry(t, rec, "single")
	if single.Outcome != "updated" || single.Version != newVersion || single.Transfers[0].Outcome != "updated" {
		t.Errorf("single = %+v, want updated %s", single, newVersion)
	}

	// The next run finds the image linked: current.
	if _, err := h.update(UpdateFeaturesOptions{Features: []string{"single"}}); err != nil {
		t.Fatalf("UpdateFeatures: %v", err)
	}
	rec, _ = readLastRun(t, path)
	single = featureEntry(t, rec, "single")
	if single.Outcome != "current" || single.Version != newVersion {
		t.Errorf("single = %+v, want current %s", single, newVersion)
	}
}

// A rollback record that holds the feature: held_by_rollback with the linked
// version.
func TestLastRun_HeldByRollback(t *testing.T) {
	f := newHeldFixture(t, "alpha")
	path := filepath.Join(t.TempDir(), "last-run.json")
	f.client.paths.lastRunPath = path

	if _, err := f.update(); err != nil {
		t.Fatalf("a held run must not fail: %v", err)
	}
	rec, _ := readLastRun(t, path)
	fe := featureEntry(t, rec, "grp")
	if fe.Outcome != "held_by_rollback" || fe.Version != "2.0.0" || fe.ErrorClass != "" {
		t.Errorf("feature = %+v, want held_by_rollback 2.0.0", fe)
	}
	if len(fe.Transfers) != 1 || fe.Transfers[0].Outcome != "held_by_rollback" || fe.Transfers[0].Version != "2.0.0" {
		t.Errorf("transfers = %+v", fe.Transfers)
	}
	if rec.Outcome != "completed" {
		t.Errorf("run outcome = %q, want completed", rec.Outcome)
	}
}

// The refresh value follows what the run did with the refresh.
func TestLastRun_Refresh(t *testing.T) {
	tests := []struct {
		name    string
		runner  *sysext.MockRunner
		noRefr  bool
		want    string
		outcome string
	}{
		{name: "done", runner: &sysext.MockRunner{}, want: "done", outcome: "completed"},
		{name: "failed", runner: &sysext.MockRunner{RefreshErr: errors.New("boom")}, want: "failed", outcome: "failed"},
		{name: "not requested", runner: &sysext.MockRunner{}, noRefr: true, want: "not_requested", outcome: "completed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newLastRunFixture(t, map[string]string{"testext": goodServer(t, "testext")}, func(cfg *ClientConfig) {
				cfg.SysextRunner = tt.runner
			})
			_, _ = f.client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{NoRefresh: tt.noRefr})
			rec, _ := readLastRun(t, f.lastRun)
			if rec.Refresh != tt.want || rec.Outcome != tt.outcome {
				t.Errorf("refresh = %q outcome = %q, want %q %q", rec.Refresh, rec.Outcome, tt.want, tt.outcome)
			}
		})
	}
}

// A dry run and a run refused by selectFeatures write nothing.
func TestLastRun_NothingWritten(t *testing.T) {
	tests := []struct {
		name string
		opts UpdateFeaturesOptions
	}{
		{name: "dry run", opts: UpdateFeaturesOptions{DryRun: true}},
		{name: "unknown feature name", opts: UpdateFeaturesOptions{Features: []string{"nope"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newLastRunFixture(t, map[string]string{"testext": goodServer(t, "testext")}, nil)
			_, _ = f.client.UpdateFeatures(t.Context(), tt.opts)
			noLastRun(t, f.lastRun)
		})
	}
}

// A domain that cannot be loaded is a failed run of class config, with no
// feature.
func TestLastRun_DomainLoadFailure(t *testing.T) {
	f := newLastRunFixture(t, nil, func(cfg *ClientConfig) {
		cfg.Definitions = filepath.Join(t.TempDir(), "missing")
	})
	if _, err := f.client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{Component: "x"}); err == nil {
		t.Fatal("UpdateFeatures() error = nil, want the domain error")
	}
	rec, data := readLastRun(t, f.lastRun)
	if rec.Outcome != "failed" || rec.ErrorClass != "config" || rec.Refresh != "not_run" || len(rec.Features) != 0 {
		t.Errorf("record = %+v, want failed/config, refresh not_run, no features", rec)
	}
	if !bytes.Contains(data, []byte(`"features": []`)) {
		t.Errorf("features is not an empty array:\n%s", data)
	}
}

// A lock error that is not ErrBusy is a failed run of class other.
func TestLastRun_LockError(t *testing.T) {
	lockDir := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(lockDir, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	f := newLastRunFixture(t, map[string]string{"testext": goodServer(t, "testext")}, func(cfg *ClientConfig) {
		cfg.Paths.LockPath = filepath.Join(lockDir, "lock")
	})
	_, err := f.client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{})
	if err == nil || errors.Is(err, ErrBusy) {
		t.Fatalf("UpdateFeatures() error = %v, want a lock error", err)
	}
	rec, _ := readLastRun(t, f.lastRun)
	if rec.Outcome != "failed" || rec.ErrorClass != "other" || rec.Refresh != "not_run" || len(rec.Features) != 0 {
		t.Errorf("record = %+v, want failed/other", rec)
	}
}

// A named run keeps the previous entries of the enabled features it did not
// act on, with their own at; an unnamed run carries nothing.
func TestLastRun_NamedRunMerges(t *testing.T) {
	previous := LastRunRecord{
		Format: 1, Started: "2020-01-01T00:00:00Z", Finished: "2020-01-01T00:00:10Z",
		Outcome: "completed", Refresh: "done",
		Features: []LastRunFeature{
			{Feature: "feature-othertex", Origin: "unknown", Outcome: "current", Version: "0.9.0", At: "2020-01-01T00:00:10Z", Transfers: []LastRunTransfer{{Component: "othertex", Outcome: "current", Version: "0.9.0"}}},
			{Feature: "feature-offext", Origin: "unknown", Outcome: "current", At: "2020-01-01T00:00:10Z", Transfers: []LastRunTransfer{}},
			{Feature: "gone", Origin: "unknown", Outcome: "current", At: "2020-01-01T00:00:10Z", Transfers: []LastRunTransfer{}},
			{Feature: "feature-testext", Origin: "unknown", Outcome: "failed", ErrorClass: "network", At: "2020-01-01T00:00:10Z", Transfers: []LastRunTransfer{}},
		},
	}
	setup := func(t *testing.T) *lastRunFixture {
		f := newLastRunFixture(t, map[string]string{"testext": goodServer(t, "testext"), "othertex": goodServer(t, "othertex")}, nil)
		createFeatureTransferFile(t, f.configDir, "offext", "feature-offext", "http://127.0.0.1:1")
		createFeatureFile(t, f.configDir, "feature-offext", false)
		data, _ := json.Marshal(previous)
		if err := os.MkdirAll(filepath.Dir(f.lastRun), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f.lastRun, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return f
	}

	t.Run("named", func(t *testing.T) {
		f := setup(t)
		if _, err := f.client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{NoRefresh: true, Features: []string{"feature-testext"}}); err != nil {
			t.Fatalf("UpdateFeatures: %v", err)
		}
		rec, _ := readLastRun(t, f.lastRun)
		if !rec.Selected {
			t.Error("selected = false for a named run")
		}
		names := map[string]LastRunFeature{}
		for _, fe := range rec.Features {
			names[fe.Feature] = fe
		}
		if len(names) != 2 {
			t.Errorf("features = %+v, want feature-testext and the carried feature-othertex", rec.Features)
		}
		if fe := names["feature-testext"]; fe.Outcome != "updated" || fe.At != rec.Finished {
			t.Errorf("feature-testext = %+v, want updated at %s", fe, rec.Finished)
		}
		if fe := names["feature-othertex"]; fe.Outcome != "current" || fe.Version != "0.9.0" || fe.At != "2020-01-01T00:00:10Z" {
			t.Errorf("feature-othertex = %+v, want the previous entry with its own at", fe)
		}
	})

	t.Run("unnamed", func(t *testing.T) {
		f := setup(t)
		if _, err := f.client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{NoRefresh: true}); err != nil {
			t.Fatalf("UpdateFeatures: %v", err)
		}
		rec, _ := readLastRun(t, f.lastRun)
		if rec.Selected || len(rec.Features) != 2 {
			t.Errorf("record = %+v, want the 2 features of this run and nothing carried", rec)
		}
		for _, fe := range rec.Features {
			if fe.At != rec.Finished {
				t.Errorf("feature %s at = %s, want this run's %s", fe.Feature, fe.At, rec.Finished)
			}
		}
	})

	t.Run("malformed previous record is ignored", func(t *testing.T) {
		f := setup(t)
		if err := os.WriteFile(f.lastRun, []byte(`{"format":2,"features":[{"feature":"feature-othertex"}]}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := f.client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{NoRefresh: true, Features: []string{"feature-testext"}}); err != nil {
			t.Fatalf("UpdateFeatures: %v", err)
		}
		rec, _ := readLastRun(t, f.lastRun)
		if len(rec.Features) != 1 || rec.Features[0].Feature != "feature-testext" {
			t.Errorf("features = %+v, want only feature-testext", rec.Features)
		}
	})
}

// A write that cannot happen is one warning; the update result and error stay
// the same.
func TestLastRun_WriteRefusals(t *testing.T) {
	t.Run("symlink at the record path", func(t *testing.T) {
		f := newLastRunFixture(t, map[string]string{"testext": goodServer(t, "testext")}, nil)
		target := filepath.Join(t.TempDir(), "target")
		if err := os.WriteFile(target, []byte("keep"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(f.lastRun), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, f.lastRun); err != nil {
			t.Fatal(err)
		}
		results, err := f.client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{NoRefresh: true})
		if err != nil || len(results) != 1 {
			t.Fatalf("UpdateFeatures() = %+v, %v; the record must not fail the update", results, err)
		}
		if got, _ := os.ReadFile(target); string(got) != "keep" {
			t.Errorf("symlink target = %q, want it untouched", got)
		}
		if info, err := os.Lstat(f.lastRun); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Errorf("record path is no longer the symlink (%v)", err)
		}
		if !strings.Contains(f.out.String(), "last-run record not written") {
			t.Errorf("output %q has no warning", f.out.String())
		}
	})

	t.Run("parent is a file", func(t *testing.T) {
		parent := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(parent, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		f := newLastRunFixture(t, map[string]string{"testext": goodServer(t, "testext")}, func(cfg *ClientConfig) {
			cfg.Paths.LastRunPath = filepath.Join(parent, "last-run.json")
		})
		_, err := f.client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{NoRefresh: true})
		if err != nil {
			t.Fatalf("UpdateFeatures() error = %v; the record must not fail the update", err)
		}
		if !strings.Contains(f.out.String(), "last-run record not written") {
			t.Errorf("output %q has no warning", f.out.String())
		}
	})

	t.Run("failed run keeps its error", func(t *testing.T) {
		parent := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(parent, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		s := testutil.NewErrorServer(t, http.StatusNotFound)
		t.Cleanup(s.Close)
		f := newLastRunFixture(t, map[string]string{"testext": s.URL}, func(cfg *ClientConfig) {
			cfg.Paths.LastRunPath = filepath.Join(parent, "last-run.json")
		})
		_, err := f.client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{NoRefresh: true})
		if err == nil || !strings.Contains(err.Error(), "manifest fetch failed with status: 404") {
			t.Fatalf("UpdateFeatures() error = %v, want the 404 error unchanged", err)
		}
	})

	t.Run("directory lock held", func(t *testing.T) {
		old := lastRunLockWait
		lastRunLockWait = 50 * time.Millisecond
		t.Cleanup(func() { lastRunLockWait = old })

		f := newLastRunFixture(t, map[string]string{"testext": goodServer(t, "testext")}, nil)
		dir := filepath.Dir(f.lastRun)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		unlock, err := lockLastRunDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer unlock()
		if _, err := f.client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{NoRefresh: true}); err != nil {
			t.Fatalf("UpdateFeatures() error = %v", err)
		}
		noLastRun(t, f.lastRun)
		if !strings.Contains(f.out.String(), "locked by another writer") {
			t.Errorf("output %q has no warning", f.out.String())
		}
	})
}

// The record is a regular file with mode 0644.
func TestLastRun_FileMode(t *testing.T) {
	f := newLastRunFixture(t, map[string]string{"testext": goodServer(t, "testext")}, nil)
	if _, err := f.client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{NoRefresh: true}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(f.lastRun)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o644 {
		t.Errorf("record = %v, %v; want a regular 0644 file", info, err)
	}
}

// The record's directory is created with mode 0755 and the record with 0644
// whatever the process umask is (docs/specs/last-run-record.md): a read-only
// command must be able to read the record a root-privileged run wrote
// (issue #524).
func TestLastRun_DirectoryMode(t *testing.T) {
	for _, tc := range []struct {
		name string
		mask int
	}{
		{name: "umask 0022", mask: 0o022},
		{name: "umask 0077", mask: 0o077},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testutil.Umask(t, tc.mask)
			f := newLastRunFixture(t, map[string]string{"testext": goodServer(t, "testext")}, nil)
			if _, err := f.client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{NoRefresh: true}); err != nil {
				t.Fatal(err)
			}
			dir := filepath.Dir(f.lastRun)
			if info, err := os.Lstat(dir); err != nil || !info.IsDir() || info.Mode().Perm() != 0o755 {
				t.Errorf("record directory %s = %v, %v; want a 0755 directory", dir, info, err)
			}
			if info, err := os.Lstat(f.lastRun); err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o644 {
				t.Errorf("record = %v, %v; want a regular 0644 file", info, err)
			}
		})
	}
}

func TestClassifyError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "nil", err: nil, want: ""},
		{name: "cancelled", err: fmt.Errorf("x: %w", errors.Join(syscall.ECONNREFUSED, context.Canceled)), want: "other"},
		{name: "ENOSPC", err: fmt.Errorf("write: %w", syscall.ENOSPC), want: "disk_space"},
		{name: "EDQUOT", err: fmt.Errorf("write: %w", syscall.EDQUOT), want: "disk_space"},
		{name: "refused", err: fmt.Errorf("dial: %w", syscall.ECONNREFUSED), want: "network"},
		{name: "reset", err: fmt.Errorf("read: %w", syscall.ECONNRESET), want: "network"},
		{name: "aborted", err: fmt.Errorf("read: %w", syscall.ECONNABORTED), want: "network"},
		{name: "unknown authority", err: fmt.Errorf("get: %w", x509.UnknownAuthorityError{}), want: "network"},
		{name: "hostname", err: fmt.Errorf("get: %w", x509.HostnameError{}), want: "network"},
		{name: "invalid certificate", err: fmt.Errorf("get: %w", x509.CertificateInvalidError{}), want: "network"},
		{name: "tls verification", err: fmt.Errorf("get: %w", &tls.CertificateVerificationError{Err: errors.New("bad")}), want: "network"},
		{name: "network under a signature wrapper", err: fmt.Errorf("signature verification failed: %w", fmt.Errorf("failed to fetch signature: %w", syscall.ECONNREFUSED)), want: "network"},
		{name: "disk under a link mark", err: fault.Mark(fault.Link, fmt.Errorf("link: %w", syscall.ENOSPC)), want: "disk_space"},
		{name: "outermost mark", err: fault.Mark(fault.Config, fault.Mark(fault.Signature, errors.New("x"))), want: "config"},
		{name: "unmarked", err: errors.New("plain"), want: "other"},
		{name: "no target pattern", err: func() error { _, err := buildTargetFilename(nil, "1.0.0"); return err }(), want: "config"},
		{name: "invalid target filename", err: func() error { _, err := buildTargetFilename([]string{"@v"}, ".."); return err }(), want: "config"},
		{name: "invalid target filename alone", err: validateTargetFilename(".."), want: "config"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(classifyError(tt.err)); got != tt.want {
				t.Errorf("classifyError() = %q, want %q", got, tt.want)
			}
		})
	}
}

// writeOldRecord writes a format 1 record that ended in 2020 with the given
// features, and returns its bytes.
func writeOldRecord(t *testing.T, path string, features ...LastRunFeature) []byte {
	t.Helper()
	rec := LastRunRecord{
		Format: 1, Started: "2020-01-01T00:00:00Z", Finished: "2020-01-01T00:00:10Z",
		Outcome: "completed", Refresh: "done", Features: features,
	}
	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return data
}

// A busy run that names a feature carries the previous entries of the other
// enabled features, as a named run that gets the lock does.
func TestLastRun_NamedBusyRunCarries(t *testing.T) {
	f := newLastRunFixture(t, map[string]string{"testext": goodServer(t, "testext"), "othertex": goodServer(t, "othertex")}, nil)
	writeOldRecord(t, f.lastRun,
		LastRunFeature{Feature: "feature-testext", Origin: "unknown", Outcome: "current", Version: "0.9.0", At: "2020-01-01T00:00:10Z", Transfers: []LastRunTransfer{}},
		LastRunFeature{Feature: "feature-othertex", Origin: "unknown", Outcome: "current", Version: "0.8.0", At: "2020-01-01T00:00:05Z", Transfers: []LastRunTransfer{}},
	)
	release, err := f.client.lockMutation(false)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	if _, err := f.client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{Features: []string{"feature-testext"}}); !errors.Is(err, ErrBusy) {
		t.Fatalf("UpdateFeatures() error = %v, want ErrBusy", err)
	}
	rec, _ := readLastRun(t, f.lastRun)
	if rec.Outcome != "skipped_busy" || !rec.Selected || len(rec.Features) != 2 {
		t.Fatalf("record = %+v, want skipped_busy, selected, 2 features", rec)
	}
	if a := featureEntry(t, rec, "feature-testext"); a.Outcome != "skipped_busy" || a.At != rec.Finished {
		t.Errorf("feature-testext = %+v, want skipped_busy at %s", a, rec.Finished)
	}
	if b := featureEntry(t, rec, "feature-othertex"); b.Outcome != "current" || b.Version != "0.8.0" || b.At != "2020-01-01T00:00:05Z" {
		t.Errorf("feature-othertex = %+v, want the previous entry with its own at", b)
	}
}

// A busy run whose names selectFeatures refuses writes nothing, as the same
// names write nothing for a run that gets the lock; ErrBusy is still returned.
func TestLastRun_BusyRefusedSelectionWritesNothing(t *testing.T) {
	tests := []struct {
		name  string
		names []string
	}{
		{name: "unknown name", names: []string{"nope"}},
		{name: "disabled name", names: []string{"feature-offext"}},
		{name: "masked name", names: []string{"feature-maskext"}},
		{name: "a good and a refused name", names: []string{"feature-testext", "nope"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newLastRunFixture(t, map[string]string{"testext": goodServer(t, "testext")}, nil)
			createFeatureTransferFile(t, f.configDir, "offext", "feature-offext", "http://127.0.0.1:1")
			createFeatureFile(t, f.configDir, "feature-offext", false)
			createFeatureTransferFile(t, f.configDir, "maskext", "feature-maskext", "http://127.0.0.1:1")
			createFeatureFile(t, f.configDir, "feature-maskext", true)
			maskFeature(t, f.configDir, "feature-maskext")
			before := writeOldRecord(t, f.lastRun,
				LastRunFeature{Feature: "feature-testext", Origin: "unknown", Outcome: "current", At: "2020-01-01T00:00:10Z", Transfers: []LastRunTransfer{}},
			)
			release, err := f.client.lockMutation(false)
			if err != nil {
				t.Fatal(err)
			}
			defer release()

			if _, err := f.client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{Features: tt.names}); !errors.Is(err, ErrBusy) {
				t.Fatalf("UpdateFeatures() error = %v, want ErrBusy", err)
			}
			after, err := os.ReadFile(f.lastRun)
			if err != nil || !bytes.Equal(after, before) {
				t.Errorf("record = %s (%v), want it unchanged: %s", after, err, before)
			}
		})
	}
}

// A run that holds the operation lock writes its record before it releases
// the lock, so a later run that waited for the lock cannot write first and
// then lose its newer record to this older one.
func TestLastRun_WrittenUnderOperationLock(t *testing.T) {
	tests := []struct {
		name    string
		sources func(t *testing.T) map[string]string
		opts    UpdateFeaturesOptions
	}{
		{name: "completed run", sources: func(t *testing.T) map[string]string {
			return map[string]string{"testext": goodServer(t, "testext")}
		}, opts: UpdateFeaturesOptions{NoRefresh: true}},
		{name: "failed run", sources: func(t *testing.T) map[string]string {
			s := testutil.NewErrorServer(t, http.StatusNotFound)
			t.Cleanup(s.Close)
			return map[string]string{"testext": s.URL}
		}, opts: UpdateFeaturesOptions{}},
		{name: "named run", sources: func(t *testing.T) map[string]string {
			return map[string]string{"testext": goodServer(t, "testext"), "othertex": goodServer(t, "othertex")}
		}, opts: UpdateFeaturesOptions{NoRefresh: true, Features: []string{"feature-othertex"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newLastRunFixture(t, tt.sources(t), nil)
			calls := 0
			var lockErr error
			lastRunBeforeWrite = func() {
				calls++
				release, err := acquireLock(f.client.paths.lockPath)
				if err == nil {
					release()
				}
				lockErr = err
			}
			t.Cleanup(func() { lastRunBeforeWrite = nil })

			_, _ = f.client.UpdateFeatures(t.Context(), tt.opts)
			if calls != 1 {
				t.Fatalf("the record was written %d times, want 1", calls)
			}
			if !errors.Is(lockErr, ErrBusy) {
				t.Errorf("lock at the record write: %v, want ErrBusy (the run must still hold the operation lock)", lockErr)
			}
			readLastRun(t, f.lastRun)
		})
	}
}
