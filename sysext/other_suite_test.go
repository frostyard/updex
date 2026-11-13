package sysext

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/frostyard/updex/v2/config"
	"github.com/frostyard/updex/v2/version"
)

// suiteTransfer is the transfer "foo" with the target pattern
// foo_@v_%w_x86-64.raw on a host whose VERSION_ID is current, as config
// loads it: the current pattern and its any-suite variant.
func suiteTransfer(dir, current string) *config.Transfer {
	return &config.Transfer{
		Component: "foo",
		Transfer:  config.TransferSection{InstancesMax: 2},
		Target: config.TargetSection{
			Path:             dir,
			MatchPattern:     "foo_@v_" + current + "_x86-64.raw",
			MatchPatterns:    []string{"foo_@v_" + current + "_x86-64.raw"},
			AnySuitePatterns: []string{"foo_@v_" + version.SuiteWildcard + "_x86-64.raw"},
		},
	}
}

func writeImages(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func listDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// The vacuum keeps the running suite's rule unchanged, keeps the newest
// image of each other suite and the image the link points to (here an older
// image of another suite, as right after a suite switch), and removes the
// older images of the other suites. It never touches an image of another
// component, nor an entry that is not a regular file.
func TestVacuumWithOtherSuiteImages(t *testing.T) {
	dir := t.TempDir()
	linkDir := t.TempDir()
	writeImages(t, dir,
		"foo_1.0_14_x86-64.raw", "foo_2.0_14_x86-64.raw", "foo_3.0_14_x86-64.raw",
		"foo_1.0_13_x86-64.raw", "foo_2.0_13_x86-64.raw", "foo_3.0_13_x86-64.raw",
		"foo_1.0_12_x86-64.raw", "foo_2.0_12_x86-64.raw",
		"foo_bar_1.0_13_x86-64.raw",
	)
	if err := os.Mkdir(filepath.Join(dir, "foo_0.5_13_x86-64.raw"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("foo_1.0_13_x86-64.raw", filepath.Join(dir, "foo_0.1_13_x86-64.raw")); err != nil {
		t.Fatal(err)
	}
	// The link still points to an old image of suite 12.
	if err := os.Symlink(filepath.Join(dir, "foo_1.0_12_x86-64.raw"), filepath.Join(linkDir, "foo.raw")); err != nil {
		t.Fatal(err)
	}

	tr := suiteTransfer(dir, "14")
	paths := VacuumPaths{LinkDir: linkDir}

	plan, err := PlanVacuumWithReport(tr, "", paths)
	if err != nil {
		t.Fatalf("PlanVacuumWithReport() error = %v", err)
	}
	wantOtherRemoved := []string{"foo_1.0_13_x86-64.raw", "foo_2.0_13_x86-64.raw"}
	if !slices.Equal(plan.OtherSuiteRemoved, wantOtherRemoved) {
		t.Errorf("plan OtherSuiteRemoved = %v, want %v", plan.OtherSuiteRemoved, wantOtherRemoved)
	}
	if got := len(listDir(t, dir)); got != 11 {
		t.Errorf("plan removed files: %d entries left, want 11", got)
	}

	report, err := VacuumWithReport(tr, paths)
	if err != nil {
		t.Fatalf("VacuumWithReport() error = %v", err)
	}
	if !slices.Equal(report.Removed, []string{"1.0"}) || !slices.Equal(report.Kept, []string{"3.0", "2.0"}) {
		t.Errorf("current suite removed %v kept %v, want [1.0] and [3.0 2.0]", report.Removed, report.Kept)
	}
	if !slices.Equal(report.OtherSuiteRemoved, wantOtherRemoved) {
		t.Errorf("OtherSuiteRemoved = %v, want %v", report.OtherSuiteRemoved, wantOtherRemoved)
	}
	wantOtherKept := []string{"foo_1.0_12_x86-64.raw", "foo_2.0_12_x86-64.raw", "foo_3.0_13_x86-64.raw"}
	if !slices.Equal(report.OtherSuiteKept, wantOtherKept) {
		t.Errorf("OtherSuiteKept = %v, want %v", report.OtherSuiteKept, wantOtherKept)
	}
	wantLeft := []string{
		"foo_0.1_13_x86-64.raw", // symlink
		"foo_0.5_13_x86-64.raw", // directory
		"foo_1.0_12_x86-64.raw", // linked
		"foo_2.0_12_x86-64.raw",
		"foo_2.0_14_x86-64.raw",
		"foo_3.0_13_x86-64.raw",
		"foo_3.0_14_x86-64.raw",
		"foo_bar_1.0_13_x86-64.raw", // another component
	}
	if got := listDir(t, dir); !slices.Equal(got, wantLeft) {
		t.Errorf("left = %v, want %v", got, wantLeft)
	}
}

// An image of another suite that the merged state records stays.
func TestVacuumWithKeepsMergedOtherSuiteImage(t *testing.T) {
	dir := t.TempDir()
	hierarchy := t.TempDir()
	writeImages(t, dir, "foo_1.0_13_x86-64.raw", "foo_2.0_13_x86-64.raw", "foo_3.0_13_x86-64.raw")
	stateDir := filepath.Join(hierarchy, MergedStateDirName)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	origin := `{"extensions":{"foo":{"path":"` + filepath.Join(dir, "foo_1.0_13_x86-64.raw") + `"}}}`
	if err := os.WriteFile(filepath.Join(stateDir, "origin"), []byte(origin), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := VacuumWithReport(suiteTransfer(dir, "14"), VacuumPaths{Hierarchies: []string{hierarchy}})
	if err != nil {
		t.Fatalf("VacuumWithReport() error = %v", err)
	}
	if !slices.Equal(report.OtherSuiteRemoved, []string{"foo_2.0_13_x86-64.raw"}) {
		t.Errorf("OtherSuiteRemoved = %v, want [foo_2.0_13_x86-64.raw]", report.OtherSuiteRemoved)
	}
	if got := listDir(t, dir); !slices.Equal(got, []string{"foo_1.0_13_x86-64.raw", "foo_3.0_13_x86-64.raw"}) {
		t.Errorf("left = %v", got)
	}
}

// The vacuum reads the link before it deletes anything: an unreadable link
// fails it, and every image stays, also when only other-suite images exist.
func TestVacuumWithOtherSuiteFailsClosed(t *testing.T) {
	dir := t.TempDir()
	writeImages(t, dir, "foo_1.0_13_x86-64.raw", "foo_2.0_13_x86-64.raw")
	// A regular file as the link directory makes Lstat fail with ENOTDIR.
	linkDir := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(linkDir, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := VacuumWithReport(suiteTransfer(dir, "14"), VacuumPaths{LinkDir: linkDir})
	if err == nil || !strings.Contains(err.Error(), "read linked version") {
		t.Fatalf("VacuumWithReport() error = %v, want a link read error", err)
	}
	if got := listDir(t, dir); len(got) != 2 {
		t.Errorf("left = %v, want both images", got)
	}
}

// A host with only its own suite's images gets exactly the result it got
// before the any-suite patterns existed.
func TestVacuumWithOwnSuiteOnlyUnchanged(t *testing.T) {
	images := []string{"foo_1.0_13_x86-64.raw", "foo_2.0_13_x86-64.raw", "foo_3.0_13_x86-64.raw", "foo_bar_1.0_14_x86-64.raw"}
	withSuite, without := t.TempDir(), t.TempDir()
	writeImages(t, withSuite, images...)
	writeImages(t, without, images...)

	trSuite := suiteTransfer(withSuite, "13")
	trPlain := suiteTransfer(without, "13")
	trPlain.Target.AnySuitePatterns = nil

	r1, k1, err1 := VacuumWith(trSuite, VacuumPaths{})
	r2, k2, err2 := VacuumWith(trPlain, VacuumPaths{})
	if err1 != nil || err2 != nil {
		t.Fatalf("VacuumWith() errors = %v, %v", err1, err2)
	}
	if !slices.Equal(r1, r2) || !slices.Equal(k1, k2) {
		t.Errorf("with any-suite patterns: removed %v kept %v; without: removed %v kept %v", r1, k1, r2, k2)
	}
	if got, want := listDir(t, withSuite), listDir(t, without); !slices.Equal(got, want) {
		t.Errorf("left %v, want %v", got, want)
	}

	report, err := PlanVacuumWithReport(trSuite, "", VacuumPaths{})
	if err != nil || report.OtherSuiteRemoved != nil || report.OtherSuiteKept != nil {
		t.Errorf("PlanVacuumWithReport() = %+v, %v; want no other-suite images", report, err)
	}

	removed1, err1 := RemoveAllVersionsAt(trSuite, "")
	removed2, err2 := RemoveAllVersionsAt(trPlain, "")
	if err1 != nil || err2 != nil {
		t.Fatalf("RemoveAllVersionsAt() errors = %v, %v", err1, err2)
	}
	if len(removed1) != len(removed2) {
		t.Errorf("RemoveAllVersionsAt removed %v with any-suite patterns, %v without", removed1, removed2)
	}
	if got := listDir(t, withSuite); !slices.Equal(got, []string{"foo_bar_1.0_14_x86-64.raw"}) {
		t.Errorf("left after remove = %v", got)
	}
}

// Disable removes the images of every suite, and keeps another component's
// images and an entry that matches only by suite and is not a regular file.
func TestRemoveAllVersionsAtRemovesEverySuite(t *testing.T) {
	dir := t.TempDir()
	writeImages(t, dir,
		"foo_1.0_14_x86-64.raw", "foo_2.0_13_x86-64.raw", "foo_3.0_12_x86-64.raw",
		"foo_bar_1.0_13_x86-64.raw",
	)
	if err := os.Mkdir(filepath.Join(dir, "foo_0.5_13_x86-64.raw"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("foo_bar_1.0_13_x86-64.raw", filepath.Join(dir, "foo_0.1_13_x86-64.raw")); err != nil {
		t.Fatal(err)
	}

	removed, err := RemoveAllVersionsAt(suiteTransfer(dir, "14"), "")
	if err != nil {
		t.Fatalf("RemoveAllVersionsAt() error = %v", err)
	}
	want := []string{
		filepath.Join(dir, "foo_1.0_14_x86-64.raw"),
		filepath.Join(dir, "foo_2.0_13_x86-64.raw"),
		filepath.Join(dir, "foo_3.0_12_x86-64.raw"),
	}
	if !slices.Equal(removed, want) {
		t.Errorf("removed = %v, want %v", removed, want)
	}
	wantLeft := []string{"foo_0.1_13_x86-64.raw", "foo_0.5_13_x86-64.raw", "foo_bar_1.0_13_x86-64.raw"}
	if got := listDir(t, dir); !slices.Equal(got, wantLeft) {
		t.Errorf("left = %v, want %v", got, wantLeft)
	}
}

// ActiveOtherSuiteWith sees every active image of another suite that
// RemoveAllVersionsAt would remove, and nothing of the running suite or of
// another component.
func TestActiveOtherSuiteWith(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, dir, hierarchy, runDir string, tr *config.Transfer)
		want  string
	}{
		{name: "origin path", want: "foo_1.0_13_x86-64.raw", setup: func(t *testing.T, dir, hierarchy, _ string, _ *config.Transfer) {
			writeMergedStateFile(t, hierarchy, "origin", `{"extensions":{"foo_1.0_13_x86-64":{"path":"`+filepath.Join(dir, "foo_1.0_13_x86-64.raw")+`"}}}`)
		}},
		{name: "versioned name in the extensions list", want: "foo_1.0_13_x86-64.raw", setup: func(t *testing.T, _, hierarchy, _ string, _ *config.Transfer) {
			writeMergedStateFile(t, hierarchy, "extensions", "bar\nfoo_1.0_13_x86-64\n")
		}},
		{name: "runtime directory", want: "foo_2.0_12_x86-64.raw", setup: func(t *testing.T, _, _, runDir string, _ *config.Transfer) {
			writeImages(t, runDir, "foo_2.0_12_x86-64.raw")
		}},
		{name: "current symlink", want: "foo_1.0_13_x86-64.raw", setup: func(t *testing.T, dir, _, _ string, tr *config.Transfer) {
			tr.Target.CurrentSymlink = "foo.raw"
			if err := os.Symlink("foo_1.0_13_x86-64.raw", filepath.Join(dir, "foo.raw")); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "running suite only", setup: func(t *testing.T, dir, hierarchy, runDir string, _ *config.Transfer) {
			writeMergedStateFile(t, hierarchy, "origin", `{"extensions":{"foo":{"path":"`+filepath.Join(dir, "foo_1.0_14_x86-64.raw")+`"}}}`)
			writeMergedStateFile(t, hierarchy, "extensions", "foo\nfoo_1.0_14_x86-64\nfoo_bar_1.0_13_x86-64\n")
			writeImages(t, runDir, "foo_1.0_14_x86-64.raw", "foo_bar_1.0_13_x86-64.raw")
		}},
		{name: "no %w", setup: func(t *testing.T, _, hierarchy, _ string, tr *config.Transfer) {
			tr.Target.AnySuitePatterns = nil
			writeMergedStateFile(t, hierarchy, "extensions", "foo_1.0_13_x86-64\n")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, hierarchy, runDir := t.TempDir(), t.TempDir(), t.TempDir()
			writeImages(t, dir, "foo_1.0_14_x86-64.raw", "foo_1.0_13_x86-64.raw")
			tr := suiteTransfer(dir, "14")
			tt.setup(t, dir, hierarchy, runDir, tr)
			got, err := ActiveOtherSuiteWith(tr, ActiveStatePaths{Hierarchies: []string{hierarchy}, RunExtensionsDir: runDir})
			if err != nil || got != tt.want {
				t.Errorf("ActiveOtherSuiteWith() = %q, %v; want %q", got, err, tt.want)
			}
		})
	}

	t.Run("unreadable state fails closed", func(t *testing.T) {
		dir, hierarchy := t.TempDir(), t.TempDir()
		// A directory where the origin file belongs makes the read fail.
		if err := os.MkdirAll(filepath.Join(hierarchy, MergedStateDirName, "origin"), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := ActiveOtherSuiteWith(suiteTransfer(dir, "14"), ActiveStatePaths{Hierarchies: []string{hierarchy}}); err == nil {
			t.Error("ActiveOtherSuiteWith() error = nil, want a read error")
		}
	})
}

// Older merged-state metadata names only the extension ("foo" in the
// extensions list, no origin): the merged image could be any image of the
// component, so the vacuum keeps every image of another suite and reports
// it. The running suite's rule does not change.
func TestVacuumWithComponentOnlyMetadataKeepsOtherSuites(t *testing.T) {
	dir, linkDir, hierarchy := t.TempDir(), t.TempDir(), t.TempDir()
	writeImages(t, dir,
		"foo_1.0_13_x86-64.raw", "foo_2.0_13_x86-64.raw",
		"foo_1.0_14_x86-64.raw", "foo_2.0_14_x86-64.raw", "foo_3.0_14_x86-64.raw",
	)
	// The link names a newer image than the one that is merged.
	if err := os.Symlink(filepath.Join(dir, "foo_3.0_14_x86-64.raw"), filepath.Join(linkDir, "foo.raw")); err != nil {
		t.Fatal(err)
	}
	writeMergedStateFile(t, hierarchy, "extensions", "foo\n")

	report, err := VacuumWithReport(suiteTransfer(dir, "14"), VacuumPaths{LinkDir: linkDir, Hierarchies: []string{hierarchy}})
	if err != nil {
		t.Fatalf("VacuumWithReport() error = %v", err)
	}
	if !report.OtherSuiteMergedUnknown {
		t.Error("OtherSuiteMergedUnknown = false, want the report to say why every other-suite image stays")
	}
	if len(report.OtherSuiteRemoved) != 0 || !slices.Equal(report.OtherSuiteKept, []string{"foo_1.0_13_x86-64.raw", "foo_2.0_13_x86-64.raw"}) {
		t.Errorf("OtherSuiteRemoved = %v, OtherSuiteKept = %v; want none removed, both kept", report.OtherSuiteRemoved, report.OtherSuiteKept)
	}
	if !slices.Equal(report.Removed, []string{"1.0"}) || !slices.Equal(report.Kept, []string{"3.0", "2.0"}) {
		t.Errorf("current suite removed %v kept %v, want [1.0] and [3.0 2.0]", report.Removed, report.Kept)
	}

	// Metadata that names an image file resolves it: the older image of the
	// other suite goes.
	writeImages(t, dir, "foo_1.0_14_x86-64.raw")
	writeMergedStateFile(t, hierarchy, "origin", `{"extensions":{"foo":{"path":"`+filepath.Join(dir, "foo_2.0_14_x86-64.raw")+`"}}}`)
	report, err = VacuumWithReport(suiteTransfer(dir, "14"), VacuumPaths{LinkDir: linkDir, Hierarchies: []string{hierarchy}})
	if err != nil {
		t.Fatalf("VacuumWithReport() error = %v", err)
	}
	if report.OtherSuiteMergedUnknown || !slices.Equal(report.OtherSuiteRemoved, []string{"foo_1.0_13_x86-64.raw"}) {
		t.Errorf("report = %+v, want foo_1.0_13_x86-64.raw removed", report)
	}
}

// A CurrentSymlink that cannot be inspected fails the vacuum before it
// deletes anything; an absent one and a regular file in its place do not.
func TestVacuumWithOtherSuiteFailsClosedOnCurrentSymlink(t *testing.T) {
	images := []string{"foo_1.0_13_x86-64.raw", "foo_2.0_13_x86-64.raw", "foo_3.0_13_x86-64.raw",
		"foo_1.0_14_x86-64.raw", "foo_2.0_14_x86-64.raw", "foo_3.0_14_x86-64.raw"}

	t.Run("inspection failure", func(t *testing.T) {
		dir := t.TempDir()
		writeImages(t, dir, images...)
		// A regular file as a path component makes Lstat fail with ENOTDIR.
		writeImages(t, dir, "sub")
		tr := suiteTransfer(dir, "14")
		tr.Target.CurrentSymlink = "sub/foo.raw"

		_, err := VacuumWithReport(tr, VacuumPaths{})
		if err == nil || !strings.Contains(err.Error(), "inspect current symlink") {
			t.Fatalf("VacuumWithReport() error = %v, want a current symlink error", err)
		}
		if got := listDir(t, dir); len(got) != len(images)+1 {
			t.Errorf("left = %v, want every image", got)
		}
	})

	for name, setup := range map[string]func(dir string){
		"absent":        func(string) {},
		"not a symlink": func(dir string) { writeImages(t, dir, "foo.raw") },
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeImages(t, dir, images...)
			setup(dir)
			tr := suiteTransfer(dir, "14")
			tr.Target.CurrentSymlink = "foo.raw"
			report, err := VacuumWithReport(tr, VacuumPaths{})
			if err != nil || !slices.Equal(report.OtherSuiteRemoved, []string{"foo_1.0_13_x86-64.raw", "foo_2.0_13_x86-64.raw"}) {
				t.Errorf("VacuumWithReport() = %+v, %v; want the older images of suite 13 removed", report, err)
			}
		})
	}
}
