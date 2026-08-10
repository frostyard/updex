// Package e2e contains black-box end-to-end tests for the updex CLI.
//
// Unlike the package-level tests under cmd/updex and updex/, these tests
// build the real updex-cli binary and exercise it as a subprocess, the
// same way an operator would invoke it, against a fake HTTP transfer
// source. They intentionally avoid any operation that calls requireRoot
// (enable/disable/update/daemon/catalog add|remove) so they can run in
// ordinary CI without privileges; "features check", "features list",
// "components", "--help" and "completion" cover the read-only paths.
package e2e

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
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

// runUpdex runs the built updex binary with the given args and returns its
// combined stdout+stderr output.
func runUpdex(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(updexBinary(t), args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
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

// TestE2E_HelpAndCompletion exercises the read-only, no-config CLI surface:
// running the built binary with no arguments, --help, and the completion
// subcommand should always succeed and produce useful output.
func TestE2E_HelpAndCompletion(t *testing.T) {
	out, err := runUpdex(t, "--help")
	if err != nil {
		t.Fatalf("updex --help failed: %v\n%s", err, out)
	}
	for _, want := range []string{"updex", "features", "components", "daemon", "catalog"} {
		if !strings.Contains(out, want) {
			t.Errorf("--help output missing %q:\n%s", want, out)
		}
	}

	out, err = runUpdex(t, "completion", "bash")
	if err != nil {
		t.Fatalf("updex completion bash failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "bash completion") {
		t.Errorf("completion bash output missing expected header:\n%s", out)
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

	checkOut, err := runUpdex(t, "-C", configDir, "features", "check")
	if err != nil {
		t.Fatalf("updex features check failed: %v\n%s", err, checkOut)
	}
	for _, want := range []string{"e2efeature", "1.0.0"} {
		if !strings.Contains(checkOut, want) {
			t.Errorf("features check output missing %q:\n%s", want, checkOut)
		}
	}
}
