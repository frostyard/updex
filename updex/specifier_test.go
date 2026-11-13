package updex

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/frostyard/std/reporter"
	"github.com/frostyard/updex/v2/internal/fault"
	"github.com/frostyard/updex/v2/sysext"
)

// suiteFixture is feature "grp" with 1 transfer "foo" whose patterns select
// the suite with %w (foo_@v_%w.raw), the shape of the snosi transfers. The
// host's os-release, the published images, the staged images and the link
// target come from the test.
type suiteFixture struct {
	client    *Client
	out       *bytes.Buffer
	server    *revisionServer
	runner    *countingRunner
	targetDir string
	linkDir   string
	lastRun   string
}

func newSuiteFixture(t *testing.T, osRelease string, staged []string, linkTarget string) *suiteFixture {
	t.Helper()
	root := t.TempDir()
	defDir := filepath.Join(root, "sysupdate.d")
	if err := os.MkdirAll(defDir, 0o755); err != nil {
		t.Fatal(err)
	}
	osReleasePath := filepath.Join(t.TempDir(), "os-release")
	if err := os.WriteFile(osReleasePath, []byte(osRelease), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &suiteFixture{
		out:       &bytes.Buffer{},
		server:    newRevisionServer(t, "foo"),
		runner:    &countingRunner{DefaultRunner: &sysext.DefaultRunner{}},
		targetDir: t.TempDir(),
		linkDir:   t.TempDir(),
		lastRun:   filepath.Join(t.TempDir(), "last-run.json"),
	}
	createFeatureFile(t, defDir, "grp", true)
	transfer := "[Transfer]\nFeatures=grp\nVerify=false\n\n[Source]\nType=url-file\nPath=" + f.server.URL +
		"\nMatchPattern=foo_@v_%w.raw\n\n[Target]\nPath=" + f.targetDir + "\nMatchPattern=foo_@v_%w.raw\n"
	if err := os.WriteFile(filepath.Join(defDir, "foo.transfer"), []byte(transfer), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range staged {
		if err := os.WriteFile(filepath.Join(f.targetDir, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if linkTarget != "" {
		if err := os.Symlink(filepath.Join(f.targetDir, linkTarget), filepath.Join(f.linkDir, "foo.raw")); err != nil {
			t.Fatal(err)
		}
	}
	f.client = NewClient(ClientConfig{
		SysextRunner: f.runner,
		Progress:     reporter.NewTextReporter(f.out),
		Paths: RuntimePaths{
			DefinitionRoots:   []string{root},
			OSReleasePaths:    []string{osReleasePath},
			SysextLinkDir:     f.linkDir,
			SysextHierarchies: []string{t.TempDir()},
			RunExtensionsDir:  t.TempDir(),
			LockPath:          filepath.Join(t.TempDir(), "lock"),
			RollbackDir:       filepath.Join(t.TempDir(), "rollback"),
			LastRunPath:       f.lastRun,
		},
	})
	return f
}

func (f *suiteFixture) link(t *testing.T) string {
	t.Helper()
	target, err := os.Readlink(filepath.Join(f.linkDir, "foo.raw"))
	if err != nil {
		return ""
	}
	return filepath.Base(target)
}

func (f *suiteFixture) staged(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(f.targetDir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// noVersionID is an os-release without VERSION_ID, so %w is empty (#511).
const noVersionID = "ID=debian\nPRETTY_NAME=\"Debian GNU/Linux forky/sid\"\n"

// emptySuiteError is the message without its section: the Source pattern is
// read first, so the message starts with `Source MatchPattern`.
const emptySuiteError = `MatchPattern "foo_@v_%w.raw": specifier %w is empty: os-release has no VERSION_ID`

// With %w empty, update, check, enable and list name the empty specifier
// and its source as a configuration error, instead of "no versions
// available"; nothing is downloaded or linked.
func TestEmptySpecifierFailsLoud(t *testing.T) {
	f := newSuiteFixture(t, noVersionID, []string{"foo_1.0_13.raw"}, "foo_1.0_13.raw")
	f.server.publish("2.0_13")

	t.Run("update", func(t *testing.T) {
		results, err := f.client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{NoRefresh: true})
		if err == nil || !strings.Contains(err.Error(), emptySuiteError) {
			t.Fatalf("UpdateFeatures() error = %v, want it to contain %q", err, emptySuiteError)
		}
		if strings.Contains(err.Error(), "no versions available") {
			t.Errorf("UpdateFeatures() error = %v, still says no versions available", err)
		}
		if class, ok := fault.ClassOf(err); !ok || class != fault.Config {
			t.Errorf("error class = %v, %v; want config", class, ok)
		}
		if r := resultsByComponent(t, results)["foo"]; !strings.Contains(r.Error, emptySuiteError) {
			t.Errorf("result error = %q", r.Error)
		}
		record, err := os.ReadFile(f.lastRun)
		if err != nil {
			t.Fatalf("read last-run record: %v", err)
		}
		if !strings.Contains(string(record), `"error_class": "config"`) {
			t.Errorf("last-run record has no config error class:\n%s", record)
		}
		if f.server.fileGets.Load() != 0 {
			t.Errorf("update downloaded %d files", f.server.fileGets.Load())
		}
		if got := f.link(t); got != "foo_1.0_13.raw" {
			t.Errorf("link = %q, want it unchanged", got)
		}
		if got := f.staged(t); !slices.Equal(got, []string{"foo_1.0_13.raw"}) {
			t.Errorf("staged = %v, want it unchanged", got)
		}
	})

	t.Run("check", func(t *testing.T) {
		results, err := f.client.CheckFeatures(t.Context(), CheckFeaturesOptions{})
		var msg string
		if err != nil {
			msg = err.Error()
		}
		for _, fr := range results {
			for _, r := range fr.Results {
				msg += "\n" + r.Error
			}
		}
		if !strings.Contains(msg, emptySuiteError) {
			t.Errorf("CheckFeatures() = %q, want %q", msg, emptySuiteError)
		}
	})

	t.Run("enable", func(t *testing.T) {
		_, err := f.client.EnableFeature(t.Context(), "grp", EnableFeatureOptions{Now: true})
		if err == nil || !strings.Contains(err.Error(), emptySuiteError) {
			t.Errorf("EnableFeature(Now) error = %v, want %q", err, emptySuiteError)
		}
	})

	t.Run("list", func(t *testing.T) {
		features, err := f.client.Features(t.Context())
		if err != nil {
			t.Fatalf("Features() error = %v", err)
		}
		if len(features) != 1 || len(features[0].TransferStatus) != 1 ||
			!strings.Contains(features[0].TransferStatus[0].Error, emptySuiteError) {
			t.Errorf("Features() = %+v, want the transfer status to carry %q", features, emptySuiteError)
		}
	})
}

// An empty specifier does not block teardown: `features disable --now`
// removes the link on such a host, and the load of the domain succeeds.
func TestEmptySpecifierDisableStillWorks(t *testing.T) {
	f := newSuiteFixture(t, noVersionID, []string{"foo_1.0_13.raw"}, "foo_1.0_13.raw")

	if _, err := f.client.DisableFeature(t.Context(), "grp", DisableFeatureOptions{Now: true, Force: true}); err != nil {
		t.Fatalf("DisableFeature(Now) error = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(f.linkDir, "foo.raw")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("link still exists after disable: %v", err)
	}
}

// A host whose os-release has VERSION_ID sees no SpecifierError and updates
// as before.
func TestSpecifierPresentUpdatesAsBefore(t *testing.T) {
	f := newSuiteFixture(t, "ID=debian\nVERSION_ID=13\n", []string{"foo_1.0_13.raw"}, "foo_1.0_13.raw")
	f.server.publish("2.0_13")

	if _, err := f.client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{NoRefresh: true}); err != nil {
		t.Fatalf("UpdateFeatures() error = %v", err)
	}
	if got := f.link(t); got != "foo_2.0_13.raw" {
		t.Errorf("link = %q, want foo_2.0_13.raw", got)
	}
}
