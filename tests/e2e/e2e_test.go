// Package e2e contains black-box end-to-end tests for the updex CLI.
//
// Unlike the package-level tests under cmd/updex and updex/, these tests
// build the real updex-cli binary and exercise it as a subprocess, the
// same way an operator would invoke it, against a fake HTTP transfer
// source. Successful command execution is limited to read-only operations
// so the suite can run without root; mutating commands are exercised only
// through argument validation, before their handlers run.
package e2e

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// buildOnce ensures the binary is compiled a single time for all tests in
// this package, since building is the slowest part of each test.
var (
	buildOnce sync.Once
	binPath   string
	buildErr  error
)

type commandResult struct {
	stdout   string
	stderr   string
	exitCode int
	err      error
}

func updexBinary(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "updex-e2e-bin")
		if err != nil {
			buildErr = err
			return
		}
		binPath = filepath.Join(dir, "updex")

		repoRoot, err := repoRootDir()
		if err != nil {
			buildErr = err
			return
		}

		cmd := exec.Command("go", "build", "-o", binPath, "./cmd/updex-cli")
		cmd.Dir = repoRoot
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		if err := cmd.Run(); err != nil {
			buildErr = fmt.Errorf("building updex binary: %w\n%s", err, out.String())
		}
	})
	if buildErr != nil {
		t.Fatalf("failed to build updex binary: %v", buildErr)
	}
	return binPath
}

// repoRootDir locates the module root (the directory containing go.mod)
// starting from this source file's directory, so the build works
// regardless of the working directory `go test` is invoked from.
func repoRootDir() (string, error) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("could not determine caller info")
	}
	dir := filepath.Dir(thisFile)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found above %s", thisFile)
		}
		dir = parent
	}
}

func runUpdexCommand(t *testing.T, args ...string) commandResult {
	t.Helper()
	cmd := exec.Command(updexBinary(t), args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	result := commandResult{
		stdout: stdout.String(),
		stderr: stderr.String(),
		err:    err,
	}
	if err == nil {
		return result
	}

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("failed to run updex: %v", err)
	}
	result.exitCode = exitErr.ExitCode()
	return result
}

// runUpdex preserves the combined-output helper used by lifecycle tests.
func runUpdex(t *testing.T, args ...string) (string, error) {
	t.Helper()
	result := runUpdexCommand(t, args...)
	return result.stdout + result.stderr, result.err
}

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// newFakeTransferServer serves a SHA256SUMS manifest plus file content, in
// the shape systemd-sysupdate's url-file transfer source expects.
func newFakeTransferServer(t *testing.T, filename string, content []byte) *httptest.Server {
	t.Helper()
	hash := sha256Hex(content)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		switch path {
		case "SHA256SUMS":
			fmt.Fprintf(w, "%s  %s\n", hash, filename)
		case filename:
			_, _ = w.Write(content)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write %s: %v", path, err)
	}
}

// TestE2E_HelpAndCompletion exercises the read-only, no-config CLI surface.
func TestE2E_HelpAndCompletion(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"--help"},
		{"features", "--help"},
		{"components", "--help"},
		{"daemon", "--help"},
		{"catalog", "--help"},
	} {
		result := runUpdexCommand(t, args...)
		if result.err != nil {
			t.Fatalf("updex %v failed: %v\n%s", args, result.err, result.stderr)
		}
		if !strings.Contains(result.stdout, "updex") {
			t.Errorf("updex %v output missing command name:\n%s", args, result.stdout)
		}
	}

	out, err := runUpdex(t, "--help")
	if err != nil {
		t.Fatalf("updex --help failed: %v\n%s", err, out)
	}
	for _, want := range []string{"updex", "features", "components", "daemon", "catalog"} {
		if !strings.Contains(out, want) {
			t.Errorf("--help output missing %q:\n%s", want, out)
		}
	}

	// fang's help renderer strips every bracketed group out of a cobra Use
	// string and re-appends it after the remaining words, so a Use like
	// "add [REPO/]NAME" printed the never-accepted "add NAME [REPO/]". The
	// usage line must read as the command is invoked; the REPO/NAME form is
	// documented in the Long text and examples instead.
	for _, sub := range []string{"add", "remove"} {
		t.Run("catalog_"+sub+"_usage", func(t *testing.T) {
			result := runUpdexCommand(t, "catalog", sub, "--help")
			if result.err != nil {
				t.Fatalf("catalog %s --help failed: %v\n%s", sub, result.err, result.stderr)
			}
			if strings.Contains(result.stdout, "[REPO/]") {
				t.Errorf("catalog %s --help prints a usage form the command does not accept:\n%s", sub, result.stdout)
			}
			if !strings.Contains(result.stdout, "catalog "+sub+" NAME") {
				t.Errorf("catalog %s --help usage line does not read %q:\n%s", sub, "catalog "+sub+" NAME", result.stdout)
			}
		})
	}

	version := runUpdexCommand(t, "--version")
	if version.err != nil {
		t.Fatalf("updex --version failed: %v\n%s", version.err, version.stderr)
	}
	if !strings.Contains(version.stdout, "updex version dev") {
		t.Errorf("--version output missing build version:\n%s", version.stdout)
	}

	completions := []struct {
		shell string
		want  string
	}{
		{shell: "bash", want: "bash completion"},
		{shell: "zsh", want: "compdef"},
		{shell: "fish", want: "complete"},
		{shell: "powershell", want: "Register-ArgumentCompleter"},
	}
	for _, tc := range completions {
		t.Run("completion_"+tc.shell, func(t *testing.T) {
			result := runUpdexCommand(t, "completion", tc.shell)
			if result.err != nil {
				t.Fatalf("completion %s failed: %v\n%s", tc.shell, result.err, result.stderr)
			}
			if !strings.Contains(result.stdout, tc.want) {
				t.Errorf("completion %s output missing %q", tc.shell, tc.want)
			}
		})
	}
}

func TestE2E_ArgumentValidationAndExitCodes(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "unknown global flag", args: []string{"--not-a-real-flag"}, want: "unknown flag"},
		{name: "features list", args: []string{"features", "list", "extra"}, want: "unknown command"},
		{name: "features enable", args: []string{"features", "enable"}, want: "accepts 1 arg"},
		{name: "features disable", args: []string{"features", "disable", "one", "two"}, want: "accepts 1 arg"},
		{name: "features update", args: []string{"features", "update", "extra"}, want: "unknown command"},
		{name: "features check", args: []string{"features", "check", "extra"}, want: "unknown command"},
		{name: "components", args: []string{"components", "extra"}, want: "unknown command"},
		{name: "daemon enable", args: []string{"daemon", "enable", "extra"}, want: "unknown command"},
		{name: "daemon disable", args: []string{"daemon", "disable", "extra"}, want: "unknown command"},
		{name: "daemon status", args: []string{"daemon", "status", "extra"}, want: "unknown command"},
		{name: "catalog list", args: []string{"catalog", "list", "extra"}, want: "unknown command"},
		{name: "catalog search", args: []string{"catalog", "search"}, want: "accepts 1 arg"},
		{name: "catalog add", args: []string{"catalog", "add", "one", "two"}, want: "accepts 1 arg"},
		{name: "catalog remove", args: []string{"catalog", "remove"}, want: "accepts 1 arg"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := runUpdexCommand(t, tc.args...)
			if result.exitCode != 1 {
				t.Fatalf("exit code = %d, want 1 (err=%v)\nstdout:\n%s\nstderr:\n%s",
					result.exitCode, result.err, result.stdout, result.stderr)
			}
			if !strings.Contains(strings.ToLower(result.stderr), tc.want) {
				t.Errorf("stderr missing %q:\n%s", tc.want, result.stderr)
			}
			if result.stdout != "" {
				t.Errorf("argument error wrote to stdout:\n%s", result.stdout)
			}
		})
	}
}

func TestE2E_ConfigurationErrors(t *testing.T) {
	t.Run("malformed feature file", func(t *testing.T) {
		configDir := t.TempDir()
		writeFile(t, filepath.Join(configDir, "broken.feature"), "[Feature\nEnabled=true\n")

		result := runUpdexCommand(t, "-C", configDir, "features", "list")
		if result.exitCode != 1 {
			t.Fatalf("exit code = %d, want 1\nstdout:\n%s\nstderr:\n%s",
				result.exitCode, result.stdout, result.stderr)
		}
		if !strings.Contains(strings.ToLower(result.stderr), "failed to parse") {
			t.Errorf("stderr missing configuration parse error:\n%s", result.stderr)
		}
	})

	t.Run("conflicting scopes preserve JSON output", func(t *testing.T) {
		result := runUpdexCommand(t,
			"-C", t.TempDir(), "--silent", "--json",
			"features", "check", "--component", "demo",
		)
		if result.exitCode != 1 {
			t.Fatalf("exit code = %d, want 1\nstdout:\n%s\nstderr:\n%s",
				result.exitCode, result.stdout, result.stderr)
		}
		if strings.TrimSpace(result.stdout) != "[]" {
			t.Errorf("stdout = %q, want JSON array", result.stdout)
		}
		if !strings.Contains(strings.ToLower(result.stderr), "cannot combine --definitions with --component") {
			t.Errorf("stderr missing conflicting-scope error:\n%s", result.stderr)
		}
	})
}

func TestE2E_FeaturesListRejectsExtraArgs(t *testing.T) {
	out, err := runUpdex(t, "features", "list", "unexpected")
	if err == nil {
		t.Fatalf("updex features list unexpectedly accepted an argument:\n%s", out)
	}
	if !strings.Contains(out, "Unknown command") {
		t.Errorf("features list error missing argument validation message:\n%s", out)
	}
}

// TestE2E_FeaturesListAndCheck drives the full read-only feature lifecycle
// against a real HTTP transfer source and a real config directory on disk:
// discover the feature via "features list", then confirm "features check"
// detects the version served by the fake source, all through the compiled
// binary rather than in-process SDK calls.
func TestE2E_FeaturesListAndCheck(t *testing.T) {
	configDir := t.TempDir()

	extContent := []byte("fake sysext content for e2e test")
	server := newFakeTransferServer(t, "testext_1.0.0.raw", extContent)
	defer server.Close()

	writeFile(t, filepath.Join(configDir, "e2efeature.feature"), ""+
		"[Feature]\nDescription=E2E test feature\nEnabled=true\n")
	writeFile(t, filepath.Join(configDir, "testext.transfer"), ""+
		"[Transfer]\nFeatures=e2efeature\nVerify=false\n\n"+
		"[Source]\nType=url-file\nPath="+server.URL+"\nMatchPattern=testext_@v.raw\n\n"+
		"[Target]\nPath="+t.TempDir()+"\nMatchPattern=testext_@v.raw\n")

	listOut, err := runUpdex(t, "-C", configDir, "features", "list")
	if err != nil {
		t.Fatalf("updex features list failed: %v\n%s", err, listOut)
	}
	if !strings.Contains(listOut, "e2efeature") {
		t.Errorf("features list output missing feature name:\n%s", listOut)
	}

	listJSON := runUpdexCommand(t, "-C", configDir, "--silent", "--json", "features", "list")
	if listJSON.err != nil {
		t.Fatalf("updex features list --json failed: %v\n%s", listJSON.err, listJSON.stderr)
	}
	var listed []struct {
		Name      string   `json:"name"`
		Enabled   bool     `json:"enabled"`
		Transfers []string `json:"transfers"`
	}
	if err := json.Unmarshal([]byte(listJSON.stdout), &listed); err != nil {
		t.Fatalf("features list returned invalid JSON: %v\n%s", err, listJSON.stdout)
	}
	if len(listed) != 1 || listed[0].Name != "e2efeature" || !listed[0].Enabled {
		t.Errorf("unexpected features list JSON: %+v", listed)
	}
	if len(listed[0].Transfers) != 1 || listed[0].Transfers[0] != "testext" {
		t.Errorf("unexpected feature transfers: %+v", listed[0].Transfers)
	}

	checkOut, err := runUpdex(t, "-C", configDir, "features", "check")
	if err != nil {
		t.Fatalf("updex features check failed: %v\n%s", err, checkOut)
	}
	for _, want := range []string{"e2efeature", "1.0.0"} {
		if !strings.Contains(checkOut, want) {
			t.Errorf("features check output missing %q:\n%s", want, checkOut)
		}
	}

	checkJSON := runUpdexCommand(t, "-C", configDir, "--silent", "--json", "features", "check")
	if checkJSON.err != nil {
		t.Fatalf("updex features check --json failed: %v\n%s", checkJSON.err, checkJSON.stderr)
	}
	var checked []struct {
		Feature string `json:"feature"`
		Results []struct {
			NewestVersion   string `json:"newest_version"`
			UpdateAvailable bool   `json:"update_available"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(checkJSON.stdout), &checked); err != nil {
		t.Fatalf("features check returned invalid JSON: %v\n%s", err, checkJSON.stdout)
	}
	if len(checked) != 1 || checked[0].Feature != "e2efeature" || len(checked[0].Results) != 1 {
		t.Fatalf("unexpected features check JSON: %+v", checked)
	}
	if checked[0].Results[0].NewestVersion != "1.0.0" || !checked[0].Results[0].UpdateAvailable {
		t.Errorf("unexpected check result: %+v", checked[0].Results[0])
	}
}
