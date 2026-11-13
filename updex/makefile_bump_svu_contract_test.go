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

// TestMakefileBumpRecipeChecksMain pins the main-branch guard into `make bump`
// (#504, core ADR-0012): `make bump` is the only sanctioned tagging path and
// releases main, so the recipe must reject any other branch, a detached HEAD,
// or a HEAD that differs from origin/main — before it builds or tags anything,
// and once again after the long build, so a branch change during the build
// cannot slip through.
func TestMakefileBumpRecipeChecksMain(t *testing.T) {
	data, err := os.ReadFile("../Makefile")
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	makefile := string(data)
	recipe := extractRecipe(t, makefile, "bump:")

	checkIdx := strings.Index(recipe, "bump-main-check")
	if checkIdx < 0 {
		t.Fatalf("Makefile bump recipe does not run bump-main-check; got:\n%s", recipe)
	}
	buildIdx := strings.Index(recipe, "$(MAKE) build")
	if buildIdx < 0 {
		t.Fatalf("Makefile bump recipe does not build; got:\n%s", recipe)
	}
	if checkIdx > buildIdx {
		t.Errorf("Makefile bump recipe runs bump-main-check after the build; run it first")
	}

	if got := strings.Count(recipe, "bump-main-check"); got != 2 {
		t.Fatalf("Makefile bump recipe runs bump-main-check %d time(s), want 2 (before the build and again directly before tagging); got:\n%s", got, recipe)
	}

	// The second run must sit after the clean-tree check and before the tag is
	// derived, so a checkout during the build is caught before anything is
	// tagged or pushed.
	cleanIdx := strings.Index(recipe, "git status --porcelain")
	if cleanIdx < 0 {
		t.Fatalf("Makefile bump recipe has no clean-tree check; got:\n%s", recipe)
	}
	nextIdx := strings.Index(recipe, "$(SVU) next")
	if nextIdx < 0 {
		t.Fatalf("Makefile bump recipe does not call `$(SVU) next`; got:\n%s", recipe)
	}
	secondIdx := strings.Index(recipe[checkIdx+1:], "bump-main-check") + checkIdx + 1
	if secondIdx < cleanIdx {
		t.Errorf("Makefile bump recipe's second bump-main-check runs before the clean-tree check")
	}
	if secondIdx > nextIdx {
		t.Errorf("Makefile bump recipe's second bump-main-check runs after `$(SVU) next`; it must run before the tag is derived")
	}

	checkRecipe := extractRecipe(t, makefile, "bump-main-check:")
	for _, want := range []string{"git symbolic-ref", "git fetch", "refs/remotes/origin/main", "git rev-parse HEAD"} {
		if !strings.Contains(checkRecipe, want) {
			t.Errorf("Makefile bump-main-check recipe does not use %q; got:\n%s", want, checkRecipe)
		}
	}
	phony := ""
	for _, line := range strings.Split(makefile, "\n") {
		if strings.HasPrefix(line, ".PHONY:") {
			phony = line
			break
		}
	}
	if !strings.Contains(phony, "bump-main-check") {
		t.Errorf("Makefile's .PHONY line does not list bump-main-check; got: %q", phony)
	}
	if !strings.Contains(makefile, "## bump-main-check: ") {
		t.Errorf("Makefile bump-main-check has no `## bump-main-check: ...` help line")
	}
}

// TestBumpMainCheckBehaviour runs the real bump-main-check recipe in temporary
// repositories whose origin is a temporary bare repository, so each failure
// branch keeps its message: another branch, a detached HEAD, unpushed local
// commits, and a HEAD behind origin/main.
func TestBumpMainCheckBehaviour(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping the make bump-main-check subprocess test in -short mode")
	}
	for _, tool := range []string{"git", "make"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not on PATH", tool)
		}
	}

	makefile, err := filepath.Abs("../Makefile")
	if err != nil {
		t.Fatalf("resolve the module-root Makefile: %v", err)
	}

	commit := func(t *testing.T, repo string, name string) {
		t.Helper()
		writeRepoFile(t, repo, name)
		runGit(t, repo, "add", "-A")
		runGit(t, repo, "commit", "-m", "commit "+name)
	}

	tests := []struct {
		name         string
		prepare      func(t *testing.T, repo string)
		wantErr      bool
		wantContains []string
	}{
		{
			name:    "on main and equal to origin main",
			prepare: func(t *testing.T, repo string) {},
		},
		{
			name: "another branch",
			prepare: func(t *testing.T, repo string) {
				runGit(t, repo, "checkout", "-b", "release-candidate")
			},
			wantErr:      true,
			wantContains: []string{"must run on main", "release-candidate"},
		},
		{
			name: "detached HEAD",
			prepare: func(t *testing.T, repo string) {
				runGit(t, repo, "checkout", "--detach")
			},
			wantErr:      true,
			wantContains: []string{"must run on main", "detached HEAD"},
		},
		{
			name: "local commit origin does not have",
			prepare: func(t *testing.T, repo string) {
				commit(t, repo, "unpushed.txt")
			},
			wantErr:      true,
			wantContains: []string{"is not origin/main", "pull or push first"},
		},
		{
			name: "behind origin main",
			prepare: func(t *testing.T, repo string) {
				commit(t, repo, "pushed-elsewhere.txt")
				runGit(t, repo, "push", "origin", "main")
				runGit(t, repo, "reset", "--hard", "HEAD~1")
			},
			wantErr:      true,
			wantContains: []string{"is not origin/main", "pull or push first"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newBumpRepo(t)
			tt.prepare(t, repo)

			cmd := exec.Command("make", "-s", "-f", makefile, "bump-main-check")
			cmd.Dir = repo
			cmd.Env = isolatedGitEnv(t)
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

// newBumpRepo creates a working repository on main plus a bare repository as
// its origin, both with 1 pushed commit, and returns the working directory.
func newBumpRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	runGit(t, root, "init", "--bare", origin)
	runGit(t, origin, "symbolic-ref", "HEAD", "refs/heads/main")

	repo := filepath.Join(root, "work")
	runGit(t, root, "init", "-b", "main", repo)
	runGit(t, repo, "remote", "add", "origin", origin)
	writeRepoFile(t, repo, "first.txt")
	runGit(t, repo, "add", "-A")
	runGit(t, repo, "commit", "-m", "first commit")
	runGit(t, repo, "push", "origin", "main")

	return repo
}

// writeRepoFile writes a distinct file into repo so a commit has content.
func writeRepoFile(t *testing.T, repo, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, name), []byte(name+"\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// runGit runs 1 git command in dir with a fixed test identity and an isolated
// configuration, and fails the test when git exits non-zero.
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-c", "user.name=test", "-c", "user.email=test@example.org"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	cmd.Env = isolatedGitEnv(t)
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, errOut.String())
	}
	return strings.TrimSpace(out.String())
}

// isolatedGitEnv returns this process's environment with the host's global and
// system git configuration dropped, so a developer's user.name, remote
// rewrite, or safe.directory setting cannot decide a test result.
func isolatedGitEnv(t *testing.T) []string {
	t.Helper()
	env := make([]string, 0, len(os.Environ())+2)
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "GIT_CONFIG_GLOBAL=") || strings.HasPrefix(entry, "GIT_CONFIG_SYSTEM=") {
			continue
		}
		env = append(env, entry)
	}
	return append(env, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
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
