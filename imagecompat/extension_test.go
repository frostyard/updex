package imagecompat

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/frostyard/updex/v2/internal/testutil"
)

// replaceOpenEROFS replaces the go-erofs seam for the duration of the test.
func replaceOpenEROFS(t *testing.T, open func(io.ReaderAt) (fs.FS, error)) {
	t.Helper()
	prev := openEROFS
	openEROFS = open
	t.Cleanup(func() { openEROFS = prev })
}

// wantFailClosed asserts that err is a fail-closed read failure: it matches
// ErrUnreadableRelease and not ErrUnsupportedImage, the error that lets the
// caller skip the check.
func wantFailClosed(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, ErrUnreadableRelease) {
		t.Fatalf("error = %v, want ErrUnreadableRelease", err)
	}
	if errors.Is(err, ErrUnsupportedImage) {
		t.Errorf("error = %v also matches ErrUnsupportedImage, so the caller would skip the check", err)
	}
}

// zeroSuperBlockAfterMagic keeps an image's EROFS magic and clears the rest
// of the superblock, so go-erofs finds the magic and refuses the filesystem.
func zeroSuperBlockAfterMagic(t *testing.T, image []byte) []byte {
	t.Helper()
	magic := []byte{0xE2, 0xE1, 0xF5, 0xE0} // 0xE0F5E1E2, little-endian
	off := bytes.Index(image, magic)
	if off < 0 {
		t.Fatal("fixture image has no EROFS magic")
	}
	out := bytes.Clone(image)
	clear(out[off+len(magic) : off+128]) // the superblock is 128 bytes
	return out
}

func writeImage(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "image.raw")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadExtensionRelease(t *testing.T) {
	withKey := "ID=debian\nSYSEXT_ID=tool\nSYSEXT_VERSION_ID=1.2\nUPDEX_MIN_IMAGE_VERSION=20261022021319\n"
	noKey := "ID=_any\nSYSEXT_ID=tool\n"
	erofsKey := testutil.BuildEROFS(t, testutil.SysextFiles("tool", withKey))
	erofsNoKey := testutil.BuildEROFS(t, testutil.SysextFiles("tool", noKey))
	two := testutil.BuildEROFS(t, map[string]string{
		"usr/lib/extension-release.d/extension-release.alpha": "SYSEXT_ID=alpha\n",
		"usr/lib/extension-release.d/extension-release.beta":  "SYSEXT_ID=beta\n",
	})
	noRelease := testutil.BuildEROFS(t, map[string]string{"usr/bin/tool": "x"})
	noise := make([]byte, 64<<10)
	if _, err := rand.Read(noise); err != nil {
		t.Fatal(err)
	}
	huge := testutil.BuildEROFS(t, testutil.SysextFiles("tool", "SYSEXT_ID=tool\n#"+strings.Repeat("x", MaxReleaseFileSize)+"\n"))
	rootOnly := testutil.GPTPartition{TypeGUID: testutil.RootX86GUID}
	squashfs := make([]byte, 64<<10)
	copy(squashfs, "hsqs")
	corruptSuperBlock := zeroSuperBlockAfterMagic(t, erofsKey)
	withRelease := testutil.GPTPartition{TypeGUID: testutil.RootX86GUID, Data: erofsKey}

	tests := []struct {
		name    string
		image   []byte
		hint    string
		wantID  string
		wantMin string
		wantErr error
	}{
		{name: "GPT with the key", image: testutil.BuildGPT(t, testutil.GPTOptions{}, testutil.GPTPartition{TypeGUID: testutil.RootX86GUID, Data: erofsKey}), hint: "tool", wantID: "tool", wantMin: "20261022021319"},
		{name: "GPT without the key", image: testutil.BuildGPT(t, testutil.GPTOptions{}, testutil.GPTPartition{TypeGUID: testutil.RootX86GUID, Data: erofsNoKey}), hint: "tool", wantID: "tool"},
		{name: "GPT with 4096-byte sectors", image: testutil.BuildGPT(t, testutil.GPTOptions{Sector: 4096}, testutil.GPTPartition{TypeGUID: testutil.RootX86GUID, Data: erofsKey}), hint: "tool", wantID: "tool", wantMin: "20261022021319"},
		{name: "GPT, EROFS in the second partition", image: testutil.BuildGPT(t, testutil.GPTOptions{}, testutil.GPTPartition{TypeGUID: [16]byte{1}, Data: noise}, testutil.GPTPartition{TypeGUID: testutil.RootX86GUID, Data: erofsKey}), wantID: "tool", wantMin: "20261022021319"},
		{name: "bare EROFS", image: erofsKey, hint: "tool", wantID: "tool", wantMin: "20261022021319"},
		{name: "one file, hint differs", image: erofsKey, hint: "other", wantID: "tool", wantMin: "20261022021319"},
		{name: "two files, hint matches", image: two, hint: "beta", wantID: "beta"},
		{name: "two files, no match", image: two, hint: "gamma", wantErr: ErrAmbiguousExtensionRelease},
		{name: "two files, no hint", image: two, wantErr: ErrAmbiguousExtensionRelease},
		{name: "no extension-release file", image: noRelease, wantErr: ErrNoExtensionRelease},
		{name: "GPT, no extension-release file", image: testutil.BuildGPT(t, testutil.GPTOptions{}, testutil.GPTPartition{TypeGUID: testutil.RootX86GUID, Data: noRelease}), wantErr: ErrNoExtensionRelease},
		{name: "partition is not EROFS", image: testutil.BuildGPT(t, testutil.GPTOptions{}, testutil.GPTPartition{TypeGUID: testutil.RootX86GUID, Data: noise}), wantErr: ErrUnsupportedImage},
		{name: "squashfs image", image: squashfs, wantErr: ErrUnsupportedImage},
		{name: "GPT with a squashfs partition", image: testutil.BuildGPT(t, testutil.GPTOptions{}, testutil.GPTPartition{TypeGUID: testutil.RootX86GUID, Data: squashfs}), wantErr: ErrUnsupportedImage},
		{name: "EROFS magic, corrupt superblock", image: corruptSuperBlock, wantErr: ErrUnreadableRelease},
		{name: "GPT, EROFS magic and a corrupt superblock", image: testutil.BuildGPT(t, testutil.GPTOptions{}, testutil.GPTPartition{TypeGUID: testutil.RootX86GUID, Data: corruptSuperBlock}), wantErr: ErrUnreadableRelease},
		{name: "GPT, corrupt EROFS partition before a good one", image: testutil.BuildGPT(t, testutil.GPTOptions{}, testutil.GPTPartition{TypeGUID: [16]byte{1}, Data: corruptSuperBlock}, withRelease), wantID: "tool", wantMin: "20261022021319"},
		{name: "GPT, no-release EROFS partition before a good one", image: testutil.BuildGPT(t, testutil.GPTOptions{}, testutil.GPTPartition{TypeGUID: [16]byte{1}, Data: noRelease}, withRelease), wantID: "tool", wantMin: "20261022021319"},
		{name: "GPT, two EROFS partitions without a release file", image: testutil.BuildGPT(t, testutil.GPTOptions{}, testutil.GPTPartition{TypeGUID: [16]byte{1}, Data: noRelease}, testutil.GPTPartition{TypeGUID: testutil.RootX86GUID, Data: noRelease}), wantErr: ErrNoExtensionRelease},
		{name: "GPT, no-release EROFS before a corrupt one", image: testutil.BuildGPT(t, testutil.GPTOptions{}, testutil.GPTPartition{TypeGUID: [16]byte{1}, Data: noRelease}, testutil.GPTPartition{TypeGUID: testutil.RootX86GUID, Data: corruptSuperBlock}), wantErr: ErrNoExtensionRelease},
		{name: "GPT without partitions", image: testutil.BuildGPT(t, testutil.GPTOptions{}), wantErr: ErrUnsupportedImage},
		{name: "random file", image: noise, wantErr: ErrUnsupportedImage},
		{name: "empty file", image: nil, wantErr: ErrUnsupportedImage},
		{name: "entry array past the end of the file", image: testutil.BuildGPT(t, testutil.GPTOptions{EntriesLBA: 1 << 40}, rootOnly), wantErr: ErrUnsupportedImage},
		{name: "entry array only partly in the file", image: testutil.BuildGPT(t, testutil.GPTOptions{EntriesLBA: uint64(len(erofsKey)/512) + 2048 - 1}, testutil.GPTPartition{TypeGUID: testutil.RootX86GUID, Data: erofsKey}), wantErr: ErrUnsupportedImage},
		{name: "image cut after the header", image: testutil.BuildGPT(t, testutil.GPTOptions{EntriesLBA: 0}, rootOnly)[:1024], wantErr: ErrUnsupportedImage},
		{name: "header checksum wrong", image: testutil.BuildGPT(t, testutil.GPTOptions{BadHdrCRC: true}, testutil.GPTPartition{TypeGUID: testutil.RootX86GUID, Data: erofsKey}), wantErr: ErrUnsupportedImage},
		{name: "entry array checksum wrong", image: testutil.BuildGPT(t, testutil.GPTOptions{BadArrayCRC: true}, testutil.GPTPartition{TypeGUID: testutil.RootX86GUID, Data: erofsKey}), wantErr: ErrUnsupportedImage},
		{name: "entry size invalid", image: testutil.BuildGPT(t, testutil.GPTOptions{EntrySize: 100}, testutil.GPTPartition{TypeGUID: testutil.RootX86GUID, Data: erofsKey}), wantErr: ErrUnsupportedImage},
		{name: "entry count too large", image: testutil.BuildGPT(t, testutil.GPTOptions{Count: 1 << 20}, rootOnly), wantErr: ErrUnsupportedImage},
		{name: "extension-release file too large", image: testutil.BuildGPT(t, testutil.GPTOptions{}, testutil.GPTPartition{TypeGUID: testutil.RootX86GUID, Data: huge}), wantErr: ErrReleaseFileTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ReadExtensionRelease(writeImage(t, tt.image), tt.hint)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("error = %v, want %v", err, tt.wantErr)
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
		})
	}
}

func TestReadExtensionReleaseRefusesNonRegular(t *testing.T) {
	dir := t.TempDir()
	image := filepath.Join(dir, "real.raw")
	if err := os.WriteFile(image, testutil.BuildEROFS(t, testutil.SysextFiles("tool", "SYSEXT_ID=tool\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.raw")
	if err := os.Symlink(image, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadExtensionRelease(link, "tool"); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("symlink: error = %v, want a refusal", err)
	}
	if _, err := ReadExtensionRelease(dir, "tool"); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("directory: error = %v, want a refusal", err)
	}
	if _, err := ReadExtensionRelease(filepath.Join(dir, "missing.raw"), "tool"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("missing file: error = %v, want not-exist", err)
	}
	if _, err := ReadExtensionRelease(image, "tool"); err != nil {
		t.Errorf("regular file: %v", err)
	}
}

// TestReadExtensionReleaseTruncated cuts a valid image at every 4 KiB step.
// None of the reads may panic or hang, and none may succeed.
func TestReadExtensionReleaseTruncated(t *testing.T) {
	image := testutil.BuildGPT(t, testutil.GPTOptions{}, testutil.GPTPartition{TypeGUID: testutil.RootX86GUID, Data: testutil.BuildEROFS(t, testutil.SysextFiles("tool", "SYSEXT_ID=tool\n"))})
	for cut := 1 << 20; cut < len(image); cut += 4096 {
		_, err := ReadExtensionRelease(writeImage(t, image[:cut]), "tool")
		if err == nil {
			t.Fatalf("cut at %d: read succeeded", cut)
		}
	}
}

// TestReadExtensionReleaseRealImage reads a real snosi sysext when one is on
// this machine. CI skips it.
func TestReadExtensionReleaseRealImage(t *testing.T) {
	const path = "/tmp/erofsprobe/1password-cli_2.41.1-1_13_x86-64.raw"
	if _, err := os.Stat(path); err != nil {
		t.Skipf("no real image: %v", err)
	}
	got, err := ReadExtensionRelease(path, "1password-cli")
	if err != nil {
		t.Fatal(err)
	}
	if got["SYSEXT_ID"] != "1password-cli" {
		t.Errorf("SYSEXT_ID = %q, want 1password-cli (all keys: %v)", got["SYSEXT_ID"], fmt.Sprint(got))
	}
}

func TestParseOSRelease(t *testing.T) {
	data := "# comment\n; other comment\n\nID=debian\nNAME=\"Debian \\\"Trixie\\\"\"\nQ='single $x'\nUNQ=a\\ b\nEMPTY=\nCRLF=1\r\nbad line\n=novalue\n1BAD=x\nID=ubuntu\nTAIL=\"x\" ignored\nOPEN=\"unterminated\n  SPACED = x\n"
	got := ParseOSRelease([]byte(data))
	want := map[string]string{
		"ID":    "ubuntu",
		"NAME":  `Debian "Trixie"`,
		"Q":     "single $x",
		"UNQ":   "a b",
		"EMPTY": "",
		"CRLF":  "1",
		"TAIL":  "x",
		"OPEN":  "unterminated",
	}
	if len(got) != len(want) {
		t.Errorf("got %v, want %v", got, want)
	}
	for k, v := range want {
		if g, ok := got[k]; !ok || g != v {
			t.Errorf("%s = %q (set %v), want %q", k, g, ok, v)
		}
	}
	if !bytes.Equal([]byte(got["ID"]), []byte("ubuntu")) {
		t.Error("later assignment does not win")
	}
}

// brokenReleaseFS is an fs.FS for the openEROFS seam. It lists one
// extension-release file in usr/lib/extension-release.d and fails at the step
// the test names: opening the file, its stat, or its read.
type brokenReleaseFS struct{ step string }

func (b brokenReleaseFS) Open(name string) (fs.File, error) {
	if name == extensionReleaseDir {
		return &brokenDir{entries: []fs.DirEntry{brokenEntry{name: extensionReleasePrefix + "tool"}}}, nil
	}
	if name != extensionReleaseDir+"/"+extensionReleasePrefix+"tool" {
		return nil, fs.ErrNotExist
	}
	if b.step == "open" {
		return nil, errors.New("injected open failure")
	}
	return &brokenFile{step: b.step}, nil
}

type brokenDir struct{ entries []fs.DirEntry }

func (d *brokenDir) Stat() (fs.FileInfo, error)         { return nil, errors.New("no stat for a directory") }
func (d *brokenDir) Read([]byte) (int, error)           { return 0, io.EOF }
func (d *brokenDir) Close() error                       { return nil }
func (d *brokenDir) ReadDir(int) ([]fs.DirEntry, error) { return d.entries, nil }

type brokenEntry struct{ name string }

func (e brokenEntry) Name() string               { return e.name }
func (e brokenEntry) IsDir() bool                { return false }
func (e brokenEntry) Type() fs.FileMode          { return 0 }
func (e brokenEntry) Info() (fs.FileInfo, error) { return brokenInfo{}, nil }

type brokenInfo struct{}

func (brokenInfo) Name() string       { return extensionReleasePrefix + "tool" }
func (brokenInfo) Size() int64        { return 8 }
func (brokenInfo) Mode() fs.FileMode  { return 0o644 }
func (brokenInfo) ModTime() time.Time { return time.Time{} }
func (brokenInfo) IsDir() bool        { return false }
func (brokenInfo) Sys() any           { return nil }

type brokenFile struct{ step string }

func (f *brokenFile) Stat() (fs.FileInfo, error) {
	if f.step == "stat" {
		return nil, errors.New("injected stat failure")
	}
	return brokenInfo{}, nil
}

func (f *brokenFile) Read([]byte) (int, error) {
	if f.step == "read" {
		return 0, errors.New("injected read failure")
	}
	return 0, io.EOF
}

func (f *brokenFile) Close() error { return nil }

// An EROFS image whose release file cannot be opened, inspected or read is a
// fail-closed error, never ErrUnsupportedImage: the caller must not link it.
func TestReadExtensionReleaseUnreadableFile(t *testing.T) {
	image := testutil.BuildEROFS(t, testutil.SysextFiles("tool", "SYSEXT_ID=tool\n"))
	for _, step := range []string{"open", "stat", "read"} {
		t.Run(step, func(t *testing.T) {
			replaceOpenEROFS(t, func(io.ReaderAt) (fs.FS, error) {
				return brokenReleaseFS{step: step}, nil
			})
			_, err := ReadExtensionRelease(writeImage(t, image), "tool")
			wantFailClosed(t, err)
		})
	}
	t.Run("erofs open", func(t *testing.T) {
		replaceOpenEROFS(t, func(io.ReaderAt) (fs.FS, error) {
			return nil, errors.New("injected EROFS open failure")
		})
		_, err := ReadExtensionRelease(writeImage(t, image), "tool")
		wantFailClosed(t, err)
	})
}

// A parser panic in a candidate with the EROFS magic is caught per candidate
// and fails closed.
func TestReadExtensionReleaseParserPanic(t *testing.T) {
	image := testutil.BuildEROFS(t, testutil.SysextFiles("tool", "SYSEXT_ID=tool\n"))
	replaceOpenEROFS(t, func(io.ReaderAt) (fs.FS, error) { panic("injected parser panic") })
	_, err := ReadExtensionRelease(writeImage(t, image), "tool")
	wantFailClosed(t, err)
}

// Without the EROFS magic go-erofs is never called, so an image that is not
// EROFS at all stays ErrUnsupportedImage (the caller may link it).
func TestReadExtensionReleaseNoParserWithoutMagic(t *testing.T) {
	calls := 0
	replaceOpenEROFS(t, func(io.ReaderAt) (fs.FS, error) {
		calls++
		panic("go-erofs must not be called for an image without the magic")
	})
	noise := make([]byte, 64<<10)
	if _, err := rand.Read(noise); err != nil {
		t.Fatal(err)
	}
	squashfs := make([]byte, 64<<10)
	copy(squashfs, "hsqs")
	for name, image := range map[string][]byte{"random bytes": noise, "squashfs": squashfs} {
		_, err := ReadExtensionRelease(writeImage(t, image), "tool")
		if !errors.Is(err, ErrUnsupportedImage) {
			t.Errorf("%s: error = %v, want ErrUnsupportedImage", name, err)
		}
		if errors.Is(err, ErrUnreadableRelease) {
			t.Errorf("%s: error = %v matches ErrUnreadableRelease; it must fail open", name, err)
		}
	}
	if calls != 0 {
		t.Errorf("go-erofs was called %d times for images without the magic", calls)
	}
}

// The first EROFS candidate that gives a release file wins: a candidate that
// panics does not hide a later good partition.
func TestReadExtensionReleaseLaterCandidateWins(t *testing.T) {
	image := testutil.BuildGPT(t, testutil.GPTOptions{},
		testutil.GPTPartition{TypeGUID: [16]byte{1}, Data: testutil.BuildEROFS(t, testutil.SysextFiles("tool", "SYSEXT_ID=broken\n"))},
		testutil.GPTPartition{TypeGUID: testutil.RootX86GUID, Data: testutil.BuildEROFS(t, testutil.SysextFiles("tool", "SYSEXT_ID=good\n"))},
	)
	prev := openEROFS
	calls := 0
	openEROFS = func(r io.ReaderAt) (fs.FS, error) {
		calls++
		if calls == 1 { // candidates are tried in table order
			panic("injected parser panic in the first candidate")
		}
		return prev(r)
	}
	t.Cleanup(func() { openEROFS = prev })

	got, err := ReadExtensionRelease(writeImage(t, image), "tool")
	if err != nil {
		t.Fatalf("error = %v, want the second partition's release file", err)
	}
	if got["SYSEXT_ID"] != "good" {
		t.Errorf("SYSEXT_ID = %q, want good", got["SYSEXT_ID"])
	}
	if calls != 2 {
		t.Errorf("go-erofs opened %d candidates, want both EROFS candidates tried", calls)
	}
}
