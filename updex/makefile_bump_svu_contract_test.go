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

// svuPinPattern extracts the svu version from mise.toml's [tools] table.
var svuPinPattern = regexp.MustCompile(`(?m)^svu = "([^"]*)"\s*$`)

// miseSvuPin returns the svu release pinned in mise.toml. It fails the test
// when the pin is missing, so every other assertion in this file can use the
// real pin instead of hard-coding a release.
func miseSvuPin(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("../mise.toml")
	if err != nil {
		t.Fatalf("read mise.toml: %v", err)
	}
	match := svuPinPattern.FindStringSubmatch(string(data))
	if match == nil {
		t.Fatalf("mise.toml pins no svu; add `svu = \"3.4.1\"` under [tools] (make bump reads it)")
	}
	if match[1] == "" {
		t.Fatalf("mise.toml pins svu with an empty version")
	}
	return match[1]
}

// TestMiseTomlPinsSvu pins the svu release `make bump` tags with (#425). svu
// derives the next version from Conventional Commit history and reads
// .svu.yml; releases before 3.x read a different config name, so whatever
// svu happened to be on PATH could tag a different version than the
// configured one. mise.toml is the single pin (core ADR-0043); mise.lock must
// carry the checksum for the platforms maintainers tag from.
func TestMiseTomlPinsSvu(t *testing.T) {
	pin := miseSvuPin(t)
	if !regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(pin) {
		t.Errorf("mise.toml pins svu %q, want an exact semantic version such as 3.4.1", pin)
	}

	data, err := os.ReadFile("../mise.lock")
	if err != nil {
		t.Fatalf("read mise.lock: %v", err)
	}
	lock := string(data)

	if !strings.Contains(lock, "[[tools.svu]]") {
		t.Fatalf("mise.lock has no [[tools.svu]] entry; run `mise lock` after pinning svu in mise.toml")
	}
	versionLine := fmt.Sprintf("version = %q", pin)
	block := lockSection(lock, "[[tools.svu]]")
	if !strings.Contains(block, versionLine) {
		t.Errorf("mise.lock's [[tools.svu]] entry does not record %s; run `mise lock`", versionLine)
	}

	for _, platform := range []string{"linux-x64", "linux-arm64"} {
		header := fmt.Sprintf("[tools.svu.\"platforms.%s\"]", platform)
		if !strings.Contains(lock, header) {
			t.Errorf("mise.lock has no %s block for svu; run `mise lock --platform linux-arm64,linux-arm64-musl,linux-x64,linux-x64-musl,macos-arm64,macos-x64,windows-x64`", header)
			continue
		}
		platformBlock := lockSection(lock, header)
		if !regexp.MustCompile(`(?m)^checksum = "sha256:[0-9a-f]{64}"$`).MatchString(platformBlock) {
			t.Errorf("mise.lock's %s block records no sha256 checksum for svu", header)
		}
	}
}

// lockSection returns the lines of the lockfile section whose header line is
// exactly header, up to the next top-level table header.
func lockSection(lock, header string) string {
	var section []string
	inSection := false
	for _, line := range strings.Split(lock, "\n") {
		if strings.HasPrefix(line, "[") {
			if inSection {
				break
			}
			inSection = line == header
			continue
		}
		if inSection {
			section = append(section, line)
		}
	}
	return strings.Join(section, "\n")
}

// TestMakefileBumpRecipeChecksSvuVersion pins the guard into `make bump`: the
// recipe must run svu-version-check first, before it builds or tags anything,
// and must invoke svu through $(SVU) so the check and the tag come from the
// same binary. The tag is pushed at once, so a wrong svu must stop the recipe
// before it builds, tests, or tags anything.
func TestMakefileBumpRecipeChecksSvuVersion(t *testing.T) {
	data, err := os.ReadFile("../Makefile")
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	recipe := extractRecipe(t, string(data), "bump:")

	checkIdx := strings.Index(recipe, "svu-version-check")
	if checkIdx < 0 {
		t.Fatalf("Makefile bump recipe does not run svu-version-check; got:\n%s", recipe)
	}

	nextIdx := strings.Index(recipe, "$(SVU) next")
	if nextIdx < 0 {
		t.Fatalf("Makefile bump recipe does not call `$(SVU) next`; got:\n%s", recipe)
	}
	if checkIdx > nextIdx {
		t.Errorf("Makefile bump recipe runs svu-version-check after it derives the next version; run it first")
	}

	firstCommand := ""
	for _, line := range strings.Split(recipe, "\n") {
		trimmed := strings.TrimPrefix(line, "\t")
		if trimmed == "" || strings.HasPrefix(trimmed, "bump:") {
			continue
		}
		firstCommand = trimmed
		break
	}
	if !strings.Contains(firstCommand, "svu-version-check") {
		t.Errorf("Makefile bump recipe's first command is %q, want svu-version-check", firstCommand)
	}

	if regexp.MustCompile(`(?:^|[^)$\w])svu next`).MatchString(recipe) {
		t.Errorf("Makefile bump recipe calls a bare `svu next` instead of `$(SVU) next`; got:\n%s", recipe)
	}
}

// TestMakefileSvuVersionCheckRecipeNamesMiseInstall pins the failure message's
// fix. The check exists to tell an operator how to get the pinned svu, so its
// recipe must name `mise install` exactly as `make lint`'s does.
func TestMakefileSvuVersionCheckRecipeNamesMiseInstall(t *testing.T) {
	data, err := os.ReadFile("../Makefile")
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	recipe := extractRecipe(t, string(data), "svu-version-check:")
	if !strings.Contains(recipe, "mise install") {
		t.Errorf("Makefile svu-version-check recipe does not tell the operator to run `mise install`; got:\n%s", recipe)
	}
	if !strings.Contains(string(data), "svu-version-check") {
		t.Errorf("Makefile does not define svu-version-check")
	}
	if !strings.Contains(string(data), "SVU_VERSION") {
		t.Errorf("Makefile does not read SVU_VERSION from mise.toml")
	}
}

// TestSvuVersionCheckBehaviour runs the real recipe against fake svu binaries
// so each failure branch keeps its message and exit status: absent binary,
// wrong release, and old svu builds that print no GitVersion line.
func TestSvuVersionCheckBehaviour(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping the make svu-version-check subprocess test in -short mode")
	}
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make is not on PATH")
	}

	pin := miseSvuPin(t)
	missing := filepath.Join(t.TempDir(), "svu-not-installed")

	tests := []struct {
		name         string
		svu          string
		versionLine  string
		wantErr      bool
		wantContains []string
	}{
		{
			name:         "missing binary",
			svu:          missing,
			wantErr:      true,
			wantContains: []string{"not installed", "mise install"},
		},
		{
			name:         "wrong version",
			versionLine:  "GitVersion:    3.3.0",
			wantErr:      true,
			wantContains: []string{"expected svu " + pin + ", found 3.3.0"},
		},
		{
			name:         "svu build without a GitVersion line",
			versionLine:  "svu version dev",
			wantErr:      true,
			wantContains: []string{"expected svu " + pin + ", found unknown"},
		},
		{
			name:        "pinned version",
			versionLine: "GitVersion:    " + pin,
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svu := tt.svu
			if svu == "" {
				svu = writeFakeSvu(t, tt.versionLine)
			}

			cmd := exec.Command("make", "-s", "svu-version-check", "SVU="+svu)
			cmd.Dir = ".." // the module root, where the Makefile lives
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

// writeFakeSvu writes an executable stand-in whose `--version` prints line, so
// the recipe's GitVersion parsing is exercised without a real svu install.
func writeFakeSvu(t *testing.T, line string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "svu")
	script := fmt.Sprintf("#!/bin/sh\necho %q\n", line)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake svu: %v", err)
	}
	return path
}
