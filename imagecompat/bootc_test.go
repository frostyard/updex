package imagecompat

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeBootc struct {
	out    string
	err    error
	called int
}

func (f *fakeBootc) Status(context.Context) ([]byte, error) {
	f.called++
	return []byte(f.out), f.err
}

func writeOSRelease(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "os-release")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// rollbackQueuedJSON builds a "bootc status --json" answer. stagedVersion is
// omitted from the staged entry when it is empty.
func rollbackQueuedJSON(rollbackVersion, stagedVersion string, queued bool) string {
	queuedField := "false"
	if queued {
		queuedField = "true"
	}
	out := `{"status":{"rollbackQueued":` + queuedField + `,"rollback":{"image":{"version":"` + rollbackVersion + `"}}`
	if stagedVersion != "" {
		out += `,"staged":{"image":{"version":"` + stagedVersion + `"}}`
	}
	return out + `}}`
}

func TestNextBootImageVersion(t *testing.T) {
	booted := writeOSRelease(t, "ID=snow\nIMAGE_VERSION=20261001000000\n")
	noVersion := writeOSRelease(t, "ID=snow\n")
	missing := filepath.Join(t.TempDir(), "missing")
	const stagedJSON = `{"apiVersion":"org.containers.bootc/v1","kind":"BootcHost","spec":{},
	  "status":{"staged":{"image":{"image":{"image":"ghcr.io/frostyard/snow:latest","transport":"registry"},"version":"20261022021319","imageDigest":"sha256:aa"},"incompatible":false},
	  "booted":{"image":{"image":{"image":"ghcr.io/frostyard/snow:latest"},"version":"20261001000000"}},"rollback":null,"type":"bootcHost"}}`

	notFound := fmt.Errorf("bootc: %w", exec.ErrNotFound)
	tests := []struct {
		name       string
		runner     *fakeBootc
		paths      []string
		wantVer    string
		wantSource Source
		wantErr    string
	}{
		{name: "staged is newer than booted", runner: &fakeBootc{out: stagedJSON}, paths: []string{booted}, wantVer: "20261022021319", wantSource: SourceStaged},
		{name: "no staged deployment", runner: &fakeBootc{out: `{"status":{"staged":null,"booted":{"image":{"version":"20261001000000"}}}}`}, paths: []string{booted}, wantVer: "20261001000000", wantSource: SourceBootedOSRelease},
		{name: "queued rollback older than booted wins", runner: &fakeBootc{out: rollbackQueuedJSON("20260901000000", "", true)}, paths: []string{booted}, wantVer: "20260901000000", wantSource: SourceRollback},
		{name: "queued rollback wins over a staged deployment", runner: &fakeBootc{out: rollbackQueuedJSON("20260901000000", "20270101000000", true)}, paths: []string{booted}, wantVer: "20260901000000", wantSource: SourceRollback},
		{name: "queued rollback without an entry", runner: &fakeBootc{out: `{"status":{"rollbackQueued":true,"rollback":null}}`}, paths: []string{booted}, wantErr: "queued rollback without an image version"},
		{name: "queued rollback without an image", runner: &fakeBootc{out: `{"status":{"rollbackQueued":true,"rollback":{"image":null}}}`}, paths: []string{booted}, wantErr: "queued rollback without an image version"},
		{name: "queued rollback with a null version", runner: &fakeBootc{out: `{"status":{"rollbackQueued":true,"rollback":{"image":{"version":null}}}}`}, paths: []string{booted}, wantErr: "queued rollback without an image version"},
		{name: "queued rollback with an empty version", runner: &fakeBootc{out: `{"status":{"rollbackQueued":true,"rollback":{"image":{"version":""}}}}`}, paths: []string{booted}, wantErr: "queued rollback without an image version"},
		{name: "rollback entry not queued is ignored", runner: &fakeBootc{out: `{"status":{"rollbackQueued":false,"rollback":{"image":{"version":"20260901000000"}},"staged":{"image":{"version":"20270101000000"}}}}`}, paths: []string{booted}, wantVer: "20270101000000", wantSource: SourceStaged},
		{name: "rollback entry not queued, no staged deployment", runner: &fakeBootc{out: `{"status":{"rollbackQueued":false,"rollback":{"image":{"version":"20260901000000"}}}}`}, paths: []string{booted}, wantVer: "20261001000000", wantSource: SourceBootedOSRelease},
		{name: "staged key absent", runner: &fakeBootc{out: `{"status":{}}`}, paths: []string{booted}, wantVer: "20261001000000", wantSource: SourceBootedOSRelease},
		{name: "staged version null", runner: &fakeBootc{out: `{"status":{"staged":{"image":{"version":null}}}}`}, paths: []string{booted}, wantVer: "20261001000000", wantSource: SourceBootedOSRelease},
		{name: "staged version empty", runner: &fakeBootc{out: `{"status":{"staged":{"image":{"version":""}}}}`}, paths: []string{booted}, wantVer: "20261001000000", wantSource: SourceBootedOSRelease},
		{name: "staged image null", runner: &fakeBootc{out: `{"status":{"staged":{"image":null}}}`}, paths: []string{booted}, wantVer: "20261001000000", wantSource: SourceBootedOSRelease},
		{name: "bootc not installed", runner: &fakeBootc{err: notFound}, paths: []string{booted}, wantVer: "20261001000000", wantSource: SourceBootedOSRelease},
		{name: "first readable os-release wins", runner: &fakeBootc{err: notFound}, paths: []string{missing, booted, noVersion}, wantVer: "20261001000000", wantSource: SourceBootedOSRelease},
		{name: "os-release without IMAGE_VERSION", runner: &fakeBootc{err: notFound}, paths: []string{noVersion, booted}, wantVer: "", wantSource: SourceBootedOSRelease},
		{name: "no os-release readable", runner: &fakeBootc{err: notFound}, paths: []string{missing}, wantVer: "", wantSource: SourceBootedOSRelease},
		{name: "bootc fails", runner: &fakeBootc{err: errors.New("exit status 1: no sysroot")}, paths: []string{booted}, wantErr: "no sysroot"},
		{name: "malformed JSON", runner: &fakeBootc{out: `{"status":`}, paths: []string{booted}, wantErr: "parse bootc status output"},
		{name: "JSON of the wrong type", runner: &fakeBootc{out: `{"status":{"staged":{"image":{"version":20261022021319}}}}`}, paths: []string{booted}, wantErr: "parse bootc status output"},
		{name: "empty output", runner: &fakeBootc{out: ``}, paths: []string{booted}, wantErr: "parse bootc status output"},
		{name: "output over the cap", runner: &fakeBootc{out: `{"status":{},"x":"` + strings.Repeat("a", MaxBootcOutput) + `"}`}, paths: []string{booted}, wantErr: "larger than"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ver, src, err := NextBootImageVersion(t.Context(), Options{Runner: tt.runner, OSReleasePaths: tt.paths})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want one with %q", err, tt.wantErr)
				}
				if ver != "" {
					t.Errorf("version = %q with an error", ver)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if ver != tt.wantVer || src != tt.wantSource {
				t.Errorf("got (%q, %q), want (%q, %q)", ver, src, tt.wantVer, tt.wantSource)
			}
		})
	}
}

func TestNextBootImageVersionOSReleaseLimits(t *testing.T) {
	big := writeOSRelease(t, "IMAGE_VERSION=1\n#"+strings.Repeat("x", MaxReleaseFileSize))
	ok := writeOSRelease(t, "IMAGE_VERSION=\"20260101000000\"\n")
	// A file over the cap counts as unreadable, so the next path is used.
	ver, _, err := NextBootImageVersion(t.Context(), Options{Runner: &fakeBootc{err: exec.ErrNotFound}, OSReleasePaths: []string{big, ok}})
	if err != nil || ver != "20260101000000" {
		t.Errorf("got (%q, %v)", ver, err)
	}
	// os-release is usually a symlink: it must be followed.
	link := filepath.Join(t.TempDir(), "os-release")
	if err := os.Symlink(ok, link); err != nil {
		t.Fatal(err)
	}
	ver, _, err = NextBootImageVersion(t.Context(), Options{Runner: &fakeBootc{err: exec.ErrNotFound}, OSReleasePaths: []string{link}})
	if err != nil || ver != "20260101000000" {
		t.Errorf("symlink: got (%q, %v)", ver, err)
	}
}

func TestDefaultBootcRunnerNotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := DefaultBootcRunner{}.Status(t.Context())
	if !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("error = %v, want exec.ErrNotFound", err)
	}
	// With no bootc, NextBootImageVersion falls back.
	p := writeOSRelease(t, "IMAGE_VERSION=7\n")
	ver, src, err := NextBootImageVersion(t.Context(), Options{OSReleasePaths: []string{p}})
	if err != nil || ver != "7" || src != SourceBootedOSRelease {
		t.Errorf("got (%q, %q, %v)", ver, src, err)
	}
}

// fakeBootcBinary writes a shell script named bootc into a new directory and
// puts the directory first in PATH.
func fakeBootcBinary(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bootc"), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
}

func TestDefaultBootcRunnerProcess(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	t.Run("success passes the arguments", func(t *testing.T) {
		fakeBootcBinary(t, `[ "$1 $2" = "status --json" ] && echo '{"status":{"staged":{"image":{"version":"42"}}}}'`)
		ver, src, err := NextBootImageVersion(t.Context(), Options{})
		if err != nil || ver != "42" || src != SourceStaged {
			t.Errorf("got (%q, %q, %v)", ver, src, err)
		}
	})
	t.Run("failure carries stderr", func(t *testing.T) {
		fakeBootcBinary(t, "echo 'not root' >&2; exit 3")
		_, err := DefaultBootcRunner{}.Status(t.Context())
		if err == nil || !strings.Contains(err.Error(), "not root") {
			t.Errorf("error = %v", err)
		}
	})
	t.Run("output over the cap stops the command", func(t *testing.T) {
		fakeBootcBinary(t, "yes aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
		_, err := DefaultBootcRunner{}.Status(t.Context())
		if err == nil || !strings.Contains(err.Error(), "larger than") {
			t.Errorf("error = %v", err)
		}
	})
	t.Run("context stops a slow command", func(t *testing.T) {
		fakeBootcBinary(t, "exec sleep 30")
		ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
		defer cancel()
		_, err := DefaultBootcRunner{}.Status(ctx)
		if err == nil {
			t.Error("no error after the context ended")
		}
	})
}
