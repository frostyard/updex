package updex

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// golangciLintPinPattern extracts the golangci-lint version from mise.toml's
// [tools] table.
var golangciLintPinPattern = regexp.MustCompile(`(?m)^golangci-lint = "([^"]*)"\s*$`)

// goModGoLinePattern matches go.mod's `go` directive in the exact form the
// repository baseline requires: go 1.<minor>.<patch>.
var goModGoLinePattern = regexp.MustCompile(`(?m)^go 1\.([0-9]+)\.([0-9]+)\s*$`)

// miseGolangciLintPin returns the golangci-lint release pinned in mise.toml,
// so the assertions below use the real pin instead of a hard-coded release.
func miseGolangciLintPin(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("../mise.toml")
	if err != nil {
		t.Fatalf("read mise.toml: %v", err)
	}
	match := golangciLintPinPattern.FindStringSubmatch(string(data))
	if match == nil || match[1] == "" {
		t.Fatalf("mise.toml pins no golangci-lint (make lint-version-check reads it)")
	}
	return match[1]
}

// TestRepositoryBaselineB11GoModIsTheOnlyGoPin pins baseline row B11 (core
// ADR-0071) and core ADR-0043: go.mod's `go 1.<minor>.<patch>` line is the
// only Go pin. actions/setup-go, mise and Snowcat workers read that line, so
// a `toolchain` line would be a second pin that can drift from it.
func TestRepositoryBaselineB11GoModIsTheOnlyGoPin(t *testing.T) {
	data, err := os.ReadFile("../go.mod")
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	goMod := string(data)

	if got := len(goModGoLinePattern.FindAllString(goMod, -1)); got != 1 {
		t.Errorf("go.mod has %d `go 1.<minor>.<patch>` line(s), want exactly 1 (baseline B11)", got)
	}
	if got := len(regexp.MustCompile(`(?m)^go\s`).FindAllString(goMod, -1)); got != 1 {
		t.Errorf("go.mod has %d `go` directive(s), want exactly 1 in the form go 1.<minor>.<patch>", got)
	}
	if regexp.MustCompile(`(?m)^toolchain\s`).MatchString(goMod) {
		t.Errorf("go.mod has a toolchain line; the go line is the only Go pin (baseline B11, core ADR-0043): run `go mod edit -toolchain=none`")
	}

	data, err = os.ReadFile("../mise.toml")
	if err != nil {
		t.Fatalf("read mise.toml: %v", err)
	}
	if regexp.MustCompile(`(?m)^go\s*=`).MatchString(string(data)) {
		t.Errorf("mise.toml pins go; Go comes from go.mod only (core ADR-0043)")
	}
}

// TestMakefileLintVersionCheckReadsGoLine pins where the Makefile reads the Go
// version: go.mod's `go` line. go.mod has no toolchain line (baseline B11), so
// a Makefile that reads one gets an empty version and its "built with" guard
// passes for every Go.
func TestMakefileLintVersionCheckReadsGoLine(t *testing.T) {
	data, err := os.ReadFile("../Makefile")
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	makefile := string(data)

	if strings.Contains(makefile, "^toolchain") || strings.Contains(makefile, "GO_TOOLCHAIN") {
		t.Errorf("Makefile reads a toolchain line from go.mod; read the go line (the only Go pin) instead")
	}
	if !strings.Contains(makefile, "GO_VERSION := ") || !strings.Contains(makefile, "s/^go ") {
		t.Errorf("Makefile does not read GO_VERSION from go.mod's go line")
	}
	recipe := extractRecipe(t, makefile, "lint-version-check:")
	if !strings.Contains(recipe, `test -n "$(GO_LANGUAGE)"`) {
		t.Errorf("Makefile lint-version-check does not fail when go.mod has no readable go line; got:\n%s", recipe)
	}
}

// TestLintVersionCheckBehaviour runs the real lint-version-check recipe in a
// temporary directory with a fake golangci-lint on PATH, so each branch keeps
// its result: the pinned release built with the same Go minor passes whatever
// the patch, an older minor fails, a wrong release fails, and a go.mod without
// a readable go line fails closed.
func TestLintVersionCheckBehaviour(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping the make lint-version-check subprocess test in -short mode")
	}
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make is not on PATH")
	}

	pin := miseGolangciLintPin(t)
	makefile, err := os.ReadFile("../Makefile")
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	miseToml, err := os.ReadFile("../mise.toml")
	if err != nil {
		t.Fatalf("read mise.toml: %v", err)
	}

	const module = "module example.org/lintcheck\n\n"
	tests := []struct {
		name         string
		goMod        string
		short        string
		builtWith    string
		wantErr      bool
		wantContains []string
	}{
		{
			name:      "pinned release built with the same minor and an older patch",
			goMod:     module + "go 1.27.4\n",
			short:     pin,
			builtWith: "1.27.0",
		},
		{
			name:      "pinned release built with a newer minor",
			goMod:     module + "go 1.27.4\n",
			short:     pin,
			builtWith: "1.28.1",
		},
		{
			name:         "pinned release built with an older minor",
			goMod:        module + "go 1.27.4\n",
			short:        pin,
			builtWith:    "1.26.11",
			wantErr:      true,
			wantContains: []string{"was built with go1.26.11, older than go.mod's go1.27"},
		},
		{
			name:         "wrong golangci-lint release",
			goMod:        module + "go 1.27.4\n",
			short:        "2.13.1",
			builtWith:    "1.27.0",
			wantErr:      true,
			wantContains: []string{"expected golangci-lint " + pin + ", found 2.13.1", "mise install"},
		},
		{
			name:         "go.mod without a go line",
			goMod:        module + "toolchain go1.27.4\n",
			short:        pin,
			builtWith:    "1.27.0",
			wantErr:      true,
			wantContains: []string{"go.mod has no readable go line"},
		},
		{
			name:         "go.mod with an unreadable go line",
			goMod:        module + "go latest\n",
			short:        pin,
			builtWith:    "1.27.0",
			wantErr:      true,
			wantContains: []string{"go.mod has no readable go line"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, content := range map[string][]byte{
				"Makefile":  makefile,
				"mise.toml": miseToml,
				"go.mod":    []byte(tt.goMod),
			} {
				if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
					t.Fatalf("write %s: %v", name, err)
				}
			}
			binDir := writeFakeGolangciLint(t, tt.short, tt.builtWith)

			cmd := exec.Command("make", "-s", "lint-version-check")
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
			var output bytes.Buffer
			cmd.Stdout = &output
			cmd.Stderr = &output
			err := cmd.Run()

			if tt.wantErr && err == nil {
				t.Fatalf("%s exited 0, want a failure; output:\n%s", strings.Join(cmd.Args, " "), output.String())
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("%s: %v; output:\n%s", strings.Join(cmd.Args, " "), err, output.String())
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(output.String(), want) {
					t.Errorf("output does not contain %q; got:\n%s", want, output.String())
				}
			}
		})
	}
}

// writeFakeGolangciLint writes an executable golangci-lint stand-in into a
// fresh directory and returns that directory for PATH. `version --short`
// prints short; `version` prints the real release's line with builtWith.
func writeFakeGolangciLint(t *testing.T, short, builtWith string) string {
	t.Helper()
	dir := t.TempDir()
	script := fmt.Sprintf(`#!/bin/sh
if [ "$1" = version ] && [ "$2" = --short ]; then
	echo %q
	exit 0
fi
if [ "$1" = version ]; then
	echo "golangci-lint has version %s built with go%s from 0000000 on 2026-01-01T00:00:00Z"
	exit 0
fi
exit 1
`, short, short, builtWith)
	if err := os.WriteFile(filepath.Join(dir, "golangci-lint"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake golangci-lint: %v", err)
	}
	return dir
}
