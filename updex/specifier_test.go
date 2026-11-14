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
	"github.com/frostyard/updex/v2/config"
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
	defDir    string
	hierarchy string
}

func newSuiteFixture(t *testing.T, osRelease string, staged []string, linkTarget string) *suiteFixture {
	t.Helper()
	return newSuiteFixtureTransfer(t, osRelease, staged, linkTarget, "foo_@v_%w.raw", "")
}

// newSuiteFixtureTransfer is newSuiteFixture with the transfer's source and
// target pattern and extra [Transfer] lines (each ending in a newline).
func newSuiteFixtureTransfer(t *testing.T, osRelease string, staged []string, linkTarget, pattern, transferLines string) *suiteFixture {
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
		defDir:    defDir,
		hierarchy: t.TempDir(),
	}
	createFeatureFile(t, defDir, "grp", true)
	transfer := "[Transfer]\nFeatures=grp\nVerify=false\n" + transferLines + "\n[Source]\nType=url-file\nPath=" + f.server.URL +
		"\nMatchPattern=" + pattern + "\n\n[Target]\nPath=" + f.targetDir + "\nMatchPattern=" + pattern + "\n"
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
			SysextHierarchies: []string{f.hierarchy},
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

// The features list warning for a ProtectVersion= value with an empty
// specifier names the empty specifier and says that the vacuum of the
// transfer does not run.
func TestProtectVersionWarningSaysVacuumDoesNotRun(t *testing.T) {
	cause := fault.Mark(fault.Config, errors.New(`ProtectVersion "1%w": specifier %w is empty: os-release has no VERSION_ID`))
	tr := &config.Transfer{Component: "foo", ProtectVersionError: cause}
	tr.Transfer.ProtectVersion = "1"
	want := `foo: ProtectVersion "1%w": specifier %w is empty: os-release has no VERSION_ID; the vacuum of this transfer does not run and removes no image until ProtectVersion= expands without an empty specifier`
	if got := protectVersionWarning(tr); got != want {
		t.Errorf("protectVersionWarning() = %q, want %q", got, want)
	}
}

// A ProtectVersion= value with an empty specifier stops the vacuum of the
// transfer: UpdateFeatures downloads and links the new version as usual,
// removes no staged image (also beyond InstancesMax), and warns that the
// vacuum failed and nothing was removed. A dry run plans no removal.
func TestProtectVersionEmptySpecifierStopsVacuum(t *testing.T) {
	staged := []string{"foo_0.1.raw", "foo_0.2.raw", "foo_0.3.raw", "foo_1.0.raw"}
	for _, protect := range []string{"%w", "1%w"} {
		t.Run(protect, func(t *testing.T) {
			f := newSuiteFixtureTransfer(t, noVersionID, staged, "foo_1.0.raw", "foo_@v.raw",
				"InstancesMax=2\nProtectVersion="+protect+"\n")
			f.server.publish("2.0")

			dry, err := f.client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{NoRefresh: true, DryRun: true})
			if err != nil {
				t.Fatalf("UpdateFeatures(DryRun) error = %v", err)
			}
			if r := resultsByComponent(t, dry)["foo"]; len(r.RemovedVersions) != 0 {
				t.Errorf("dry run RemovedVersions = %v, want none", r.RemovedVersions)
			}

			results, err := f.client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{NoRefresh: true})
			if err != nil {
				t.Fatalf("UpdateFeatures() error = %v", err)
			}
			r := resultsByComponent(t, results)["foo"]
			if r.Error != "" || !r.Downloaded || !r.Installed || r.Version != "2.0" {
				t.Errorf("result = %+v, want version 2.0 downloaded and installed without error", r)
			}
			if got := f.link(t); got != "foo_2.0.raw" {
				t.Errorf("link = %q, want foo_2.0.raw", got)
			}
			want := append(slices.Clone(staged), "foo_2.0.raw")
			slices.Sort(want)
			if got := f.staged(t); !slices.Equal(got, want) {
				t.Errorf("staged = %v, want %v (nothing removed)", got, want)
			}
			wantWarn := `vacuum failed for foo: ProtectVersion "` + protect + `": specifier %w is empty: os-release has no VERSION_ID; nothing was removed`
			if !strings.Contains(f.out.String(), wantWarn) {
				t.Errorf("output does not contain %q:\n%s", wantWarn, f.out.String())
			}
			if strings.Contains(f.out.String(), "protects that version") || strings.Contains(f.out.String(), "protects no version") {
				t.Errorf("output still carries the old ProtectVersion warning:\n%s", f.out.String())
			}

			f.out.Reset()
			if _, err := f.client.Features(t.Context()); err != nil {
				t.Fatalf("Features() error = %v", err)
			}
			if !strings.Contains(f.out.String(), "the vacuum of this transfer does not run") {
				t.Errorf("Features() output does not say that the vacuum does not run:\n%s", f.out.String())
			}
		})
	}
}
