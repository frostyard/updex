package imagecompat

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frostyard/updex/v2/internal/testutil"
)

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
