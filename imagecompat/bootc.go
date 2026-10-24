package imagecompat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"strings"
)

// Source says where NextBootImageVersion got the version.
type Source string

const (
	// SourceRollback means the version is the rollback deployment's image
	// version from "bootc status --json" (.status.rollback.image.version),
	// used because bootc reports the rollback as queued for the next boot
	// (.status.rollbackQueued).
	SourceRollback Source = "rollback"
	// SourceStaged means the version is the staged deployment's image
	// version from "bootc status --json" (.status.staged.image.version).
	SourceStaged Source = "staged"
	// SourceBootedOSRelease means the version is IMAGE_VERSION from the
	// booted image's os-release.
	SourceBootedOSRelease Source = "booted-os-release"
)

// MaxBootcOutput is the most output of "bootc status --json" that is read.
const MaxBootcOutput = 1 << 20

// DefaultOSReleasePaths are the os-release files read, in order, when
// Options.OSReleasePaths is empty. The first readable file wins.
var DefaultOSReleasePaths = []string{"/etc/os-release", "/usr/lib/os-release"}

// BootcRunner runs "bootc status --json". The tests use a fake.
type BootcRunner interface {
	// Status returns the standard output of "bootc status --json". It
	// returns an error that wraps exec.ErrNotFound when bootc is not
	// installed. It must not return more than MaxBootcOutput bytes without
	// an error.
	Status(ctx context.Context) ([]byte, error)
}

// DefaultBootcRunner runs the real bootc found in PATH.
type DefaultBootcRunner struct{}

var _ BootcRunner = DefaultBootcRunner{}

// Status runs "bootc status --json" with ctx. It stops reading, and so
// stops the command with a broken pipe, when the output is longer than
// MaxBootcOutput, and returns an error.
func (DefaultBootcRunner) Status(ctx context.Context) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "bootc", "status", "--json")
	var out limitedBuffer
	var stderr limitedBuffer
	out.max = MaxBootcOutput
	stderr.max, stderr.truncate = 1024, true
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if out.exceeded {
			return nil, errOutputTooLarge
		}
		msg := strings.TrimSpace(stderr.buf.String())
		if msg != "" {
			return nil, fmt.Errorf("bootc status --json: %w: %s", err, msg)
		}
		return nil, fmt.Errorf("bootc status --json: %w", err)
	}
	return out.buf.Bytes(), nil
}

var errOutputTooLarge = fmt.Errorf("bootc status output is larger than %d bytes", MaxBootcOutput)

// limitedBuffer keeps up to max bytes. When more arrives, Write fails, which
// makes os/exec close the pipe.
type limitedBuffer struct {
	buf      bytes.Buffer
	max      int
	truncate bool // drop the excess silently instead of failing
	exceeded bool
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if l.buf.Len()+len(p) > l.max {
		if !l.truncate {
			l.exceeded = true
			return 0, errOutputTooLarge
		}
		// stderr is only for the error message: keep the start, drop the rest.
		l.buf.Write(p[:max(0, l.max-l.buf.Len())])
		return len(p), nil
	}
	return l.buf.Write(p)
}

// Options configures NextBootImageVersion.
type Options struct {
	// Runner runs bootc. The zero value uses DefaultBootcRunner.
	Runner BootcRunner
	// OSReleasePaths are the os-release files for the fallback, in order.
	// The zero value uses DefaultOSReleasePaths. The updex client passes
	// RuntimePaths.OSReleasePaths here.
	OSReleasePaths []string
}

// bootcHost is the part of the "bootc status --json" output that is used.
// The shape is bootc's Host type (crates/lib/src/spec.rs, schema
// docs/src/host-v1.schema.json): status.rollbackQueued is a boolean,
// status.rollback and status.staged are BootEntry objects or null, and a
// BootEntry's image is an ImageStatus or null whose version is a string or
// null.
type bootcHost struct {
	Status struct {
		RollbackQueued bool        `json:"rollbackQueued"`
		Rollback       *bootcEntry `json:"rollback"`
		Staged         *bootcEntry `json:"staged"`
	} `json:"status"`
}

// entryVersion returns the entry's image version, or "" when the entry, its
// image, or the version is null or empty.
func entryVersion(e *bootcEntry) string {
	if e == nil || e.Image == nil || e.Image.Version == nil {
		return ""
	}
	return *e.Image.Version
}

type bootcEntry struct {
	Image *struct {
		Version *string `json:"version"`
	} `json:"image"`
}

// NextBootImageVersion returns the IMAGE_VERSION of the image that boots
// next, and where it came from.
//
// It runs "bootc status --json". When bootc reports that a rollback is queued
// for the next boot (status.rollbackQueued), the next boot is the rollback
// deployment, so its image version wins (SourceRollback). Otherwise, when
// bootc reports a staged deployment with an image version, that version wins
// (SourceStaged). Otherwise it falls back to IMAGE_VERSION in the first
// readable os-release file of Options.OSReleasePaths
// (SourceBootedOSRelease). The fallback is used when bootc is not installed,
// when there is no staged deployment, and when the staged image has no
// version.
//
// A queued rollback whose entry or version is missing is an error, not a
// fallback: the next boot is known to be the rollback, but its version is
// not, and failing closed is the only safe answer.
//
// An empty version with a nil error means the version is unknown: no
// os-release file is readable or it has no IMAGE_VERSION. CheckMinImageVersion
// then fails closed. The os-release files are read with a size limit and
// symlinks are followed (/etc/os-release is usually a link).
//
// A bootc that is installed but fails, prints output over MaxBootcOutput
// bytes, or prints JSON that does not parse, gives an error, and there is no
// fallback. This fails closed on purpose: when bootc exists, the staged
// deployment can exist, and the booted os-release can then be older than the
// image that boots next. A silent fallback could link a revision that breaks
// the next boot. The caller keeps the old link and reports the error for the
// features that need the check.
func NextBootImageVersion(ctx context.Context, opts Options) (string, Source, error) {
	runner := opts.Runner
	if runner == nil {
		runner = DefaultBootcRunner{}
	}
	paths := opts.OSReleasePaths
	if len(paths) == 0 {
		paths = DefaultOSReleasePaths
	}

	out, err := runner.Status(ctx)
	switch {
	case err == nil:
		if len(out) > MaxBootcOutput {
			return "", "", errOutputTooLarge
		}
		var host bootcHost
		if err := json.Unmarshal(out, &host); err != nil {
			return "", "", fmt.Errorf("parse bootc status output: %w", err)
		}
		if host.Status.RollbackQueued {
			version := entryVersion(host.Status.Rollback)
			if version == "" {
				return "", "", errors.New("bootc reports a queued rollback without an image version")
			}
			return version, SourceRollback, nil
		}
		if v := entryVersion(host.Status.Staged); v != "" {
			return v, SourceStaged, nil
		}
	case errors.Is(err, exec.ErrNotFound):
		// bootc is not installed: not an image-based host.
	default:
		return "", "", err
	}
	return osReleaseImageVersion(paths), SourceBootedOSRelease, nil
}

// BootedImageVersion returns IMAGE_VERSION from the first readable os-release
// file in paths, or "" when none is readable or none sets it. The zero value
// of paths means DefaultOSReleasePaths.
//
// It describes the running image: on a bootc host /etc/os-release and
// /usr/lib/os-release belong to the booted deployment, not to a staged one.
// It runs no command, so it is safe where bootc must not be called. A
// systemd-sysext refresh merges into this image, so it is what an immediate
// activation is compared with, unlike NextBootImageVersion, which describes
// the image that boots next.
func BootedImageVersion(paths []string) string {
	if len(paths) == 0 {
		paths = DefaultOSReleasePaths
	}
	return osReleaseImageVersion(paths)
}

// osReleaseImageVersion returns IMAGE_VERSION from the first readable file,
// or "" when there is none.
func osReleaseImageVersion(paths []string) string {
	for _, p := range paths {
		data, err := readLimited(p)
		if err != nil {
			continue
		}
		return ParseOSRelease(data)["IMAGE_VERSION"]
	}
	return ""
}

// readLimited reads a file of at most MaxReleaseFileSize bytes. A larger file
// is an error.
func readLimited(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, MaxReleaseFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxReleaseFileSize {
		return nil, fs.ErrInvalid
	}
	return data, nil
}
