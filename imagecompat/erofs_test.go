package imagecompat

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/frostyard/updex/v2/internal/testutil"
)

// Tests for the dump.erofs fallback reader of compressed EROFS images. The
// unit tests use a fake runner, so they need no erofs-utils; the tests of the
// default runner run a stand-in dump.erofs script from a temporary PATH, and
// the end-to-end test runs the real mkfs.erofs and dump.erofs when the host
// has erofs-utils >= 1.8.5 (CI may not).

// markCompressed sets the two EROFS superblock fields that say a filesystem
// holds compressed data, so the fixture takes the dump.erofs path. The file
// data stays uncompressed: the fake runner, not go-erofs, reads it.
func markCompressed(t *testing.T, image []byte) []byte {
	t.Helper()
	magic := []byte{0xE2, 0xE1, 0xF5, 0xE0} // 0xE0F5E1E2, little-endian
	off := -1
	for i := 0; i+len(magic) <= len(image); i++ {
		if string(image[i:i+len(magic)]) == string(magic) {
			off = i
			break
		}
	}
	if off < 0 {
		t.Fatal("fixture image has no EROFS magic")
	}
	// The magic is the first field of the superblock.
	if off+erofsCompressedFieldsEnd-erofsSuperOffset > len(image) {
		t.Fatalf("EROFS magic at %d leaves no complete superblock", off)
	}
	out := append([]byte(nil), image...)
	// feature_incompat = LZ4_0PADDING, available_compr_algs = 0xffff (lz4).
	copy(out[off+80:off+84], []byte{0x01, 0x00, 0x00, 0x00})
	copy(out[off+84:off+86], []byte{0xff, 0xff})
	return out
}

// compressedImage returns a fixture whose superblock says that its EROFS
// filesystem is compressed.
func compressedImage(t *testing.T, files map[string]string) []byte {
	t.Helper()
	return markCompressed(t, testutil.BuildEROFS(t, files))
}

// fakeDump is an ErofsDumpRunner that returns fixed answers and records the
// calls it got.
type fakeDump struct {
	version    string
	versionErr error
	entries    []ErofsDirEntry
	listErr    error
	data       []byte
	readErr    error
	// onRead runs in ReadFile before the answer is returned, so a test can
	// change the image under the reader.
	onRead func()

	calls      []string
	lastImage  string
	lastOffset int64
	lastPath   string
	lastMax    int
}

func (f *fakeDump) Version(context.Context) (string, error) {
	f.calls = append(f.calls, "version")
	if f.versionErr != nil {
		return "", f.versionErr
	}
	if f.version == "" {
		return "1.8.6", nil
	}
	return f.version, nil
}

func (f *fakeDump) ListDir(_ context.Context, image string, offset int64, path string) ([]ErofsDirEntry, error) {
	f.calls = append(f.calls, "list "+path)
	f.lastImage, f.lastOffset, f.lastPath = image, offset, path
	if f.listErr != nil {
		return nil, f.listErr
	}
	if f.entries == nil {
		return []ErofsDirEntry{{Name: ".", Regular: false}, {Name: "..", Regular: false}}, nil
	}
	return f.entries, nil
}

func (f *fakeDump) ReadFile(_ context.Context, image string, offset int64, path string, maxSize int) ([]byte, error) {
	f.calls = append(f.calls, "read "+path)
	f.lastImage, f.lastOffset, f.lastPath, f.lastMax = image, offset, path, maxSize
	if f.onRead != nil {
		f.onRead()
	}
	if f.readErr != nil {
		return nil, f.readErr
	}
	return f.data, nil
}

// toolError is what an ErofsDumpRunner returns when its command fails.
func toolError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrUnreadableRelease, fmt.Sprintf(format, args...))
}

// releaseEntries is a listing of usr/lib/extension-release.d with one
// regular extension-release file.
func releaseEntries(names ...ErofsDirEntry) []ErofsDirEntry {
	return append([]ErofsDirEntry{{Name: ".", Regular: false}, {Name: "..", Regular: false}}, names...)
}

const (
	releaseWithKey = "SYSEXT_ID=tool\nUPDEX_MIN_IMAGE_VERSION=20261022021319\n"
	releaseNoKey   = "SYSEXT_ID=tool\n"
)

func TestReadExtensionReleaseCompressed(t *testing.T) {
	withKey := compressedImage(t, testutil.SysextFiles("tool", releaseWithKey))
	noKey := compressedImage(t, testutil.SysextFiles("tool", releaseNoKey))
	other := compressedImage(t, testutil.SysextFiles("other", "SYSEXT_ID=other\n"))
	twoFiles := compressedImage(t, map[string]string{
		"usr/lib/extension-release.d/extension-release.alpha": "SYSEXT_ID=alpha\n",
		"usr/lib/extension-release.d/extension-release.beta":  "SYSEXT_ID=beta\n",
	})
	noRelease := compressedImage(t, map[string]string{"usr/bin/tool": "x"})
	plainNoRelease := testutil.BuildEROFS(t, map[string]string{"usr/bin/tool": "x"})
	// A GPT puts its first partition at 1 MiB and pads every partition to a
	// whole sector.
	secondPartitionOffset := int64(1<<20) + int64((len(plainNoRelease)+511)/512*512)

	tests := []struct {
		name string
		// image is the file content; the runner is fake unless it says
		// otherwise.
		image   []byte
		hint    string
		dump    *fakeDump
		wantID  string
		wantMin string
		wantErr error
		// wantErrText is a substring of the error.
		wantErrText string
		// wantReadPath is the path the runner must read; "" for no read.
		wantReadPath string
		// wantOffset is the region offset the runner must get.
		wantOffset int64
	}{
		{
			name: "bare compressed image with the key", image: withKey, hint: "tool",
			dump:   &fakeDump{entries: releaseEntries(ErofsDirEntry{Name: "extension-release.tool", Regular: true}), data: []byte(releaseWithKey)},
			wantID: "tool", wantMin: "20261022021319", wantReadPath: extensionReleaseAbsDir + "/extension-release.tool",
		},
		{
			name: "bare compressed image without the key", image: noKey, hint: "tool",
			dump:         &fakeDump{entries: releaseEntries(ErofsDirEntry{Name: "extension-release.tool", Regular: true}), data: []byte(releaseNoKey)},
			wantID:       "tool",
			wantReadPath: extensionReleaseAbsDir + "/extension-release.tool",
		},
		{
			name: "GPT partition: the runner gets the partition offset", image: testutil.BuildGPT(t, testutil.GPTOptions{}, testutil.GPTPartition{TypeGUID: testutil.RootX86GUID, Data: withKey}), hint: "tool",
			dump:   &fakeDump{entries: releaseEntries(ErofsDirEntry{Name: "extension-release.tool", Regular: true}), data: []byte(releaseWithKey)},
			wantID: "tool", wantMin: "20261022021319", wantReadPath: extensionReleaseAbsDir + "/extension-release.tool",
			wantOffset: 1 << 20,
		},
		{
			name: "GPT, compressed partition after a plain one without a release file", image: testutil.BuildGPT(t, testutil.GPTOptions{},
				testutil.GPTPartition{TypeGUID: [16]byte{1}, Data: plainNoRelease},
				testutil.GPTPartition{TypeGUID: testutil.RootX86GUID, Data: withKey}), hint: "tool",
			dump:   &fakeDump{entries: releaseEntries(ErofsDirEntry{Name: "extension-release.tool", Regular: true}), data: []byte(releaseWithKey)},
			wantID: "tool", wantMin: "20261022021319",
			wantReadPath: extensionReleaseAbsDir + "/extension-release.tool", wantOffset: secondPartitionOffset,
		},
		{
			name: "hint matches one of two files", image: twoFiles, hint: "beta",
			dump:   &fakeDump{entries: releaseEntries(ErofsDirEntry{Name: "extension-release.alpha", Regular: true}, ErofsDirEntry{Name: "extension-release.beta", Regular: true}), data: []byte("SYSEXT_ID=beta\n")},
			wantID: "beta", wantReadPath: extensionReleaseAbsDir + "/extension-release.beta",
		},
		{
			name: "two files without a match: ambiguous", image: twoFiles, hint: "gamma",
			dump:    &fakeDump{entries: releaseEntries(ErofsDirEntry{Name: "extension-release.alpha", Regular: true}, ErofsDirEntry{Name: "extension-release.beta", Regular: true})},
			wantErr: ErrAmbiguousExtensionRelease,
		},
		{
			name: "a directory named like the release file is not read", image: other, hint: "tool",
			dump:   &fakeDump{entries: releaseEntries(ErofsDirEntry{Name: "extension-release.tool", Regular: false}, ErofsDirEntry{Name: "extension-release.other", Regular: true}), data: []byte("SYSEXT_ID=other\n")},
			wantID: "other", wantReadPath: extensionReleaseAbsDir + "/extension-release.other",
		},
		{
			name: "a symbolic link named like the release file is not read", image: other, hint: "tool",
			dump:    &fakeDump{entries: releaseEntries(ErofsDirEntry{Name: "extension-release.tool", Regular: false})},
			wantErr: ErrNoExtensionRelease,
		},
		{
			name: "no release file", image: noRelease,
			dump:    &fakeDump{entries: releaseEntries()},
			wantErr: ErrNoExtensionRelease,
		},
		{
			name: "tool missing", image: withKey, hint: "tool",
			dump:        &fakeDump{versionErr: fmt.Errorf("dump.erofs is not installed: %w", exec.ErrNotFound)},
			wantErr:     ErrErofsToolUnavailable,
			wantErrText: "erofs-utils >= 1.8.5 is required to read compressed EROFS images; install erofs-utils",
		},
		{
			name: "tool too old: 1.8.4", image: withKey, hint: "tool",
			dump:        &fakeDump{version: "1.8.4"},
			wantErr:     ErrErofsToolUnavailable,
			wantErrText: "dump.erofs 1.8.4 is too old; erofs-utils >= 1.8.5 is required to read compressed EROFS images",
		},
		{
			name: "tool too old: 1.7.1", image: withKey, hint: "tool",
			dump:        &fakeDump{version: "1.7.1"},
			wantErr:     ErrErofsToolUnavailable,
			wantErrText: "dump.erofs 1.7.1 is too old",
		},
		{
			name: "version unparseable: fail closed", image: withKey, hint: "tool",
			dump:        &fakeDump{version: "unknown"},
			wantErr:     ErrErofsToolUnavailable,
			wantErrText: "is too old",
		},
		{
			name: "the minimum version works", image: withKey, hint: "tool",
			dump:         &fakeDump{version: "1.8.5", entries: releaseEntries(ErofsDirEntry{Name: "extension-release.tool", Regular: true}), data: []byte(releaseWithKey)},
			wantID:       "tool",
			wantMin:      "20261022021319",
			wantReadPath: extensionReleaseAbsDir + "/extension-release.tool",
		},
		{
			name: "listing fails", image: withKey, hint: "tool",
			dump:    &fakeDump{listErr: toolError("erofs: read inode failed @ %s", extensionReleaseAbsDir)},
			wantErr: ErrUnreadableRelease, wantErrText: "read inode failed",
		},
		{
			name: "read fails", image: withKey, hint: "tool",
			dump:    &fakeDump{entries: releaseEntries(ErofsDirEntry{Name: "extension-release.tool", Regular: true}), readErr: toolError("exit status 234")},
			wantErr: ErrUnreadableRelease, wantErrText: "exit status 234",
		},
		{
			name: "the release file is too large", image: withKey, hint: "tool",
			dump:    &fakeDump{entries: releaseEntries(ErofsDirEntry{Name: "extension-release.tool", Regular: true}), readErr: fmt.Errorf("%w: over the bound", ErrReleaseFileTooLarge)},
			wantErr: ErrReleaseFileTooLarge,
		},
		{
			name: "listed name with a slash: fail closed", image: withKey, hint: "tool",
			dump:    &fakeDump{entries: releaseEntries(ErofsDirEntry{Name: "a/b", Regular: true})},
			wantErr: ErrUnreadableRelease, wantErrText: "unsafe entry name",
		},
		{
			name: "listed name that climbs out: fail closed", image: withKey, hint: "tool",
			dump:    &fakeDump{entries: releaseEntries(ErofsDirEntry{Name: "../x", Regular: true})},
			wantErr: ErrUnreadableRelease, wantErrText: "unsafe entry name",
		},
		{
			name: "listed name with a control character: fail closed", image: withKey, hint: "tool",
			dump:    &fakeDump{entries: releaseEntries(ErofsDirEntry{Name: "extension-release.a\nNID", Regular: true})},
			wantErr: ErrUnreadableRelease, wantErrText: "unsafe entry name",
		},
		{
			name: "empty listed name: fail closed", image: withKey, hint: "tool",
			dump:    &fakeDump{entries: releaseEntries(ErofsDirEntry{Name: "", Regular: true})},
			wantErr: ErrUnreadableRelease, wantErrText: "invalid entry name",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeImage(t, tt.image)
			got, err := ReadExtensionRelease(t.Context(), path, tt.hint, ReadOptions{ErofsDumpRunner: tt.dump})
			if tt.wantErr != nil || tt.wantErrText != "" {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("error = %v, want %v", err, tt.wantErr)
				}
				if tt.wantErr == nil {
					t.Fatalf("error = %v, want an error matching %q", err, tt.wantErrText)
				}
				if errors.Is(err, ErrUnsupportedImage) {
					t.Errorf("error = %v matches ErrUnsupportedImage, so the caller would skip the check", err)
				}
				if tt.wantErrText != "" && !strings.Contains(err.Error(), tt.wantErrText) {
					t.Errorf("error = %v, want it to contain %q", err, tt.wantErrText)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got["SYSEXT_ID"] != tt.wantID {
				t.Errorf("SYSEXT_ID = %q, want %q", got["SYSEXT_ID"], tt.wantID)
			}
			if got[MinImageVersionKey] != tt.wantMin {
				t.Errorf("%s = %q, want %q", MinImageVersionKey, got[MinImageVersionKey], tt.wantMin)
			}
			if tt.dump.lastImage != path {
				t.Errorf("runner image = %q, want %q", tt.dump.lastImage, path)
			}
			if tt.dump.lastOffset != tt.wantOffset {
				t.Errorf("runner offset = %d, want %d", tt.dump.lastOffset, tt.wantOffset)
			}
			if tt.dump.lastPath != tt.wantReadPath {
				t.Errorf("runner path = %q, want %q", tt.dump.lastPath, tt.wantReadPath)
			}
			if tt.dump.lastMax != MaxReleaseFileSize {
				t.Errorf("runner max size = %d, want %d", tt.dump.lastMax, MaxReleaseFileSize)
			}
			if len(tt.dump.calls) < 2 || tt.dump.calls[0] != "version" || tt.dump.calls[1] != "list "+extensionReleaseAbsDir {
				t.Errorf("calls = %v, want the version check, then the listing", tt.dump.calls)
			}
		})
	}
}

// An uncompressed image is read with go-erofs; the dump.erofs runner is never
// called, so a host without erofs-utils still updates ordinary sysexts.
func TestReadExtensionReleaseUncompressedNeverRunsTheTool(t *testing.T) {
	fake := &fakeDump{versionErr: errors.New("the runner must not be called")}
	for name, image := range map[string][]byte{
		"bare": testutil.BuildEROFS(t, testutil.SysextFiles("tool", releaseWithKey)),
		"GPT":  testutil.SysextImage(t, "tool", releaseWithKey),
	} {
		t.Run(name, func(t *testing.T) {
			got, err := ReadExtensionRelease(t.Context(), writeImage(t, image), "tool", ReadOptions{ErofsDumpRunner: fake})
			if err != nil {
				t.Fatal(err)
			}
			if got[MinImageVersionKey] != "20261022021319" {
				t.Errorf("%s = %q", MinImageVersionKey, got[MinImageVersionKey])
			}
			if len(fake.calls) != 0 {
				t.Errorf("the runner was called: %v", fake.calls)
			}
		})
	}
}

// The fallback hands the path to dump.erofs, which opens it again. An image
// swapped for another file during the read fails closed.
func TestReadExtensionReleaseCompressedImageSwapped(t *testing.T) {
	image := compressedImage(t, testutil.SysextFiles("tool", releaseWithKey))
	path := writeImage(t, image)
	other := filepath.Join(filepath.Dir(path), "other.raw")
	if err := os.WriteFile(other, image, 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeDump{
		entries: releaseEntries(ErofsDirEntry{Name: "extension-release.tool", Regular: true}),
		data:    []byte(releaseWithKey),
		onRead: func() {
			// Replace the image with a fresh file, as an attacker with write
			// access to the staging directory could.
			tmp := path + ".tmp"
			if err := os.WriteFile(tmp, image, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(tmp, path); err != nil {
				t.Fatal(err)
			}
		},
	}
	_, err := ReadExtensionRelease(t.Context(), path, "tool", ReadOptions{ErofsDumpRunner: fake})
	wantFailClosed(t, err)
	if !strings.Contains(err.Error(), "the image changed") {
		t.Errorf("error = %v, want the image-changed message", err)
	}
}

// The table that dump.erofs --ls prints, as erofs-utils 1.8.6 prints it for
// a directory that holds a regular file, a directory, a symbolic link, and a
// regular file whose name holds a space.
const dumpListSample = `Path : /usr/lib/extension-release.d
Size: 165  On-disk size: 165  directory
NID: 48   Links: 3   Layout: 2   Compression ratio: 100.00%
Inode size: 64   Xattr size: 0
Uid: 1000   Gid: 1000  Access: 0775/rwxrwxr-x
Timestamp: 2026-10-23 20:40:14.414222220

       NID TYPE  FILENAME
        48    2  .
        44    2  ..
        56    1  extension-release.a
        59    2  extension-release.dir
        62    7  extension-release.link
        65    1  extension-release.space name
`

func TestParseDumpList(t *testing.T) {
	got, err := parseDumpList([]byte(dumpListSample))
	if err != nil {
		t.Fatal(err)
	}
	want := []ErofsDirEntry{
		{Name: ".", Regular: false},
		{Name: "..", Regular: false},
		{Name: "extension-release.a", Regular: true},
		{Name: "extension-release.dir", Regular: false},
		{Name: "extension-release.link", Regular: false},
		{Name: "extension-release.space name", Regular: true},
	}
	if len(got) != len(want) {
		t.Fatalf("entries = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseDumpListRefusesBadOutput(t *testing.T) {
	table := "       NID TYPE  FILENAME\n"
	tests := map[string]string{
		"no table":         "Path : /usr/lib/extension-release.d\nSize: 96  directory\n",
		"empty output":     "",
		"non-numeric NID":  table + "        x    1  extension-release.tool\n",
		"non-numeric TYPE": table + "       56    x  extension-release.tool\n",
		"missing name":     table + "       56    1\n",
		"extra header":     "INODE TYPE NAME\n",
	}
	for name, out := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := parseDumpList([]byte(out)); !errors.Is(err, ErrUnreadableRelease) {
				t.Errorf("error = %v, want ErrUnreadableRelease", err)
			}
		})
	}
}

func TestParseDumpVersion(t *testing.T) {
	tests := map[string]string{
		"dump.erofs (erofs-utils) 1.8.6\n":                        "1.8.6",
		"dump.erofs (erofs-utils) 1.8.6\nsome note\n":             "1.8.6",
		"<W> something\n<W> else\ndump.erofs (erofs-utils) 1.9\n": "1.9",
		"dump.erofs (erofs-utils) 1.8.6-1\n":                      "1.8.6-1",
	}
	for out, want := range tests {
		got, err := parseDumpVersion(out)
		if err != nil || got != want {
			t.Errorf("parseDumpVersion(%q) = %q, %v; want %q", out, got, err, want)
		}
	}
	for _, out := range []string{"", "dump.erofs\n", "no version here\n"} {
		if _, err := parseDumpVersion(out); !errors.Is(err, ErrUnreadableRelease) {
			t.Errorf("parseDumpVersion(%q) error = %v, want ErrUnreadableRelease", out, err)
		}
	}
}

func TestErofsVersionAtLeast(t *testing.T) {
	tests := []struct {
		got, want string
		ok        bool
	}{
		{"1.8.6", "1.8.5", true},
		{"1.8.5", "1.8.5", true},
		{"1.8.4", "1.8.5", false},
		{"1.7.1", "1.8.5", false},
		{"1.9", "1.8.5", true},
		{"2.0.0", "1.8.5", true},
		{"1.8.6-1", "1.8.5", true},
		{"v1.8.6", "1.8.5", true},
		{"1.10.0", "1.8.5", true},
		{"unknown", "1.8.5", false},
		{"", "1.8.5", false},
	}
	for _, tt := range tests {
		if got := erofsVersionAtLeast(tt.got, tt.want); got != tt.ok {
			t.Errorf("erofsVersionAtLeast(%q, %q) = %v, want %v", tt.got, tt.want, got, tt.ok)
		}
	}
}

// stubDumpProgram puts an executable named dump.erofs first in PATH, so the
// default runner runs it. body is a shell script body; it receives the
// arguments in "$@".
func stubDumpProgram(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, dumpBinary), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	return dir
}

func TestDefaultErofsDumpRunner(t *testing.T) {
	image := "/var/lib/extensions.d/tool_2.0.0.raw"
	t.Run("version", func(t *testing.T) {
		stubDumpProgram(t, `echo "dump.erofs (erofs-utils) 1.8.6"`)
		got, err := DefaultErofsDumpRunner{}.Version(t.Context())
		if err != nil || got != "1.8.6" {
			t.Fatalf("Version = %q, %v; want 1.8.6", got, err)
		}
	})
	t.Run("arguments of read and list", func(t *testing.T) {
		argsFile := filepath.Join(t.TempDir(), "args")
		stubDumpProgram(t, `printf '%s\n' "$@" > `+argsFile+`
case "$2" in
--cat) printf 'SYSEXT_ID=tool\n' ;;
--ls) printf '       NID TYPE  FILENAME\n        56    1  extension-release.tool\n' ;;
esac`)
		data, err := DefaultErofsDumpRunner{}.ReadFile(t.Context(), image, 1<<20, extensionReleaseAbsDir+"/extension-release.tool", MaxReleaseFileSize)
		if err != nil || string(data) != "SYSEXT_ID=tool\n" {
			t.Fatalf("ReadFile = %q, %v", data, err)
		}
		gotArgs, err := os.ReadFile(argsFile)
		if err != nil {
			t.Fatal(err)
		}
		want := "--offset=1048576\n--cat\n--path=" + extensionReleaseAbsDir + "/extension-release.tool\n" + image + "\n"
		if string(gotArgs) != want {
			t.Errorf("arguments = %q, want %q", gotArgs, want)
		}

		entries, err := DefaultErofsDumpRunner{}.ListDir(t.Context(), image, 0, extensionReleaseAbsDir)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 || entries[0] != (ErofsDirEntry{Name: "extension-release.tool", Regular: true}) {
			t.Errorf("ListDir = %v", entries)
		}
		want = "--offset=0\n--ls\n--path=" + extensionReleaseAbsDir + "\n" + image + "\n"
		gotArgs, err = os.ReadFile(argsFile)
		if err != nil {
			t.Fatal(err)
		}
		if string(gotArgs) != want {
			t.Errorf("arguments = %q, want %q", gotArgs, want)
		}
	})
	t.Run("the tool is not installed", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		_, err := DefaultErofsDumpRunner{}.Version(t.Context())
		if !errors.Is(err, exec.ErrNotFound) {
			t.Fatalf("error = %v, want it to wrap exec.ErrNotFound", err)
		}
	})
	t.Run("an error line on stderr with exit status 0", func(t *testing.T) {
		stubDumpProgram(t, `echo "<E> erofs: read inode failed @ /usr/lib/extension-release.d" >&2; exit 0`)
		_, err := DefaultErofsDumpRunner{}.ListDir(t.Context(), image, 0, extensionReleaseAbsDir)
		if !errors.Is(err, ErrUnreadableRelease) || !strings.Contains(err.Error(), "read inode failed") {
			t.Fatalf("error = %v, want the tool's message", err)
		}
	})
	t.Run("a warning line is not an error", func(t *testing.T) {
		stubDumpProgram(t, `echo "<W> erofs: something to note" >&2; echo "dump.erofs (erofs-utils) 1.8.6"`)
		got, err := DefaultErofsDumpRunner{}.Version(t.Context())
		if err != nil || got != "1.8.6" {
			t.Fatalf("Version = %q, %v", got, err)
		}
	})
	t.Run("a failed command", func(t *testing.T) {
		stubDumpProgram(t, `echo "<E> erofs: cannot find valid erofs superblock" >&2; exit 234`)
		data, err := DefaultErofsDumpRunner{}.ReadFile(t.Context(), image, 0, "/usr/lib/extension-release.d/x", MaxReleaseFileSize)
		if !errors.Is(err, ErrUnreadableRelease) || !strings.Contains(err.Error(), "cannot find valid erofs superblock") {
			t.Fatalf("error = %v, want the tool's message", err)
		}
		if data != nil {
			t.Errorf("ReadFile returned %q with an error", data)
		}
	})
	t.Run("too much read output", func(t *testing.T) {
		stubDumpProgram(t, `i=0; while [ $i -lt 700 ]; do printf '%0100d\n' $i; i=$((i+1)); done`)
		_, err := DefaultErofsDumpRunner{}.ReadFile(t.Context(), image, 0, "/usr/lib/extension-release.d/x", MaxReleaseFileSize)
		if !errors.Is(err, ErrReleaseFileTooLarge) {
			t.Fatalf("error = %v, want ErrReleaseFileTooLarge", err)
		}
	})
	t.Run("too much listing output", func(t *testing.T) {
		stubDumpProgram(t, `i=0; while [ $i -lt 5000 ]; do printf '%0100d\n' $i; i=$((i+1)); done`)
		_, err := DefaultErofsDumpRunner{}.ListDir(t.Context(), image, 0, extensionReleaseAbsDir)
		if !errors.Is(err, ErrUnreadableRelease) {
			t.Fatalf("error = %v, want ErrUnreadableRelease", err)
		}
		if !strings.Contains(err.Error(), "more than") {
			t.Errorf("error = %v, want the bound in the message", err)
		}
	})
	t.Run("the context stops the command", func(t *testing.T) {
		stubDumpProgram(t, `exec sleep 10; echo done`)
		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		defer cancel()
		start := time.Now()
		_, err := DefaultErofsDumpRunner{}.Version(ctx)
		if !errors.Is(err, ErrUnreadableRelease) || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("error = %v, want a fail-closed deadline error", err)
		}
		if elapsed := time.Since(start); elapsed > 5*time.Second {
			t.Errorf("the command ran %v after its context expired", elapsed)
		}
	})
}

// A host without dump.erofs fails closed through the whole read: the error
// names the install and is never ErrUnsupportedImage.
func TestReadExtensionReleaseCompressedMissingTool(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // no dump.erofs
	image := compressedImage(t, testutil.SysextFiles("tool", releaseWithKey))
	_, err := ReadExtensionRelease(t.Context(), writeImage(t, image), "tool", ReadOptions{})
	wantFailClosed(t, err)
	if !errors.Is(err, ErrErofsToolUnavailable) {
		t.Errorf("error = %v, want ErrErofsToolUnavailable", err)
	}
	if !strings.Contains(err.Error(), "install erofs-utils") {
		t.Errorf("error = %v, want the install hint", err)
	}
}

// TestReadExtensionReleaseRealCompressed builds real compressed EROFS images
// with mkfs.erofs and reads them through the real dump.erofs. It skips when
// the host has no erofs-utils >= 1.8.5.
func TestReadExtensionReleaseRealCompressed(t *testing.T) {
	if _, err := exec.LookPath("mkfs.erofs"); err != nil {
		t.Skip("mkfs.erofs is not installed")
	}
	version, err := DefaultErofsDumpRunner{}.Version(t.Context())
	if err != nil {
		t.Skipf("dump.erofs cannot run: %v", err)
	}
	if !erofsVersionAtLeast(version, ErofsUtilsMinVersion) {
		t.Skipf("erofs-utils %s is older than %s", version, ErofsUtilsMinVersion)
	}

	src := filepath.Join(t.TempDir(), "src")
	release := "SYSEXT_ID=tool\nUPDEX_MIN_IMAGE_VERSION=20261022021319\n"
	if err := os.MkdirAll(filepath.Join(src, "usr/lib/extension-release.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "usr/lib/extension-release.d/extension-release.tool"), []byte(release), 0o644); err != nil {
		t.Fatal(err)
	}
	// Compressible data, so the image really holds compressed blocks.
	big := make([]byte, 256<<10)
	for i := range big {
		big[i] = byte(i * 7)
	}
	if err := os.MkdirAll(filepath.Join(src, "usr/share"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "usr/share/big"), big, 0o644); err != nil {
		t.Fatal(err)
	}
	imagePath := filepath.Join(t.TempDir(), "compressed.erofs")
	cmd := exec.CommandContext(t.Context(), "mkfs.erofs", "-zlz4hc", imagePath, src)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("mkfs.erofs: %v: %s", err, out)
	}
	image, err := os.ReadFile(imagePath)
	if err != nil {
		t.Fatal(err)
	}
	if !compressedSuperblock(bytes.NewReader(image), region{0, int64(len(image))}) {
		t.Fatal("the mkfs.erofs image does not have the compression fields set")
	}

	t.Run("bare image", func(t *testing.T) {
		got, err := ReadExtensionRelease(t.Context(), imagePath, "tool", ReadOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if got[MinImageVersionKey] != "20261022021319" {
			t.Errorf("%s = %q (all keys %v)", MinImageVersionKey, got[MinImageVersionKey], got)
		}
	})
	t.Run("GPT image", func(t *testing.T) {
		path := writeImage(t, testutil.BuildGPT(t, testutil.GPTOptions{}, testutil.GPTPartition{TypeGUID: testutil.RootX86GUID, Data: image}))
		got, err := ReadExtensionRelease(t.Context(), path, "tool", ReadOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if got[MinImageVersionKey] != "20261022021319" {
			t.Errorf("%s = %q", MinImageVersionKey, got[MinImageVersionKey])
		}
	})
}

// The platform must never make the fallback a silent skip.
func TestErofsToolUnavailableIsAFailClosedError(t *testing.T) {
	if errors.Is(errToolMissing(), ErrUnsupportedImage) {
		t.Error("the missing-tool error matches ErrUnsupportedImage")
	}
	if !errors.Is(errToolMissing(), ErrErofsToolUnavailable) {
		t.Error("the missing-tool error does not match ErrErofsToolUnavailable")
	}
	wantFailClosed(t, errToolMissing())
	wantFailClosed(t, errToolTooOld("1.8.4"))
}
