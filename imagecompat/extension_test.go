package imagecompat

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	erofs "github.com/erofs/go-erofs"
)

// memFile is an in-memory io.WriteSeeker for the EROFS writer.
type memFile struct {
	buf []byte
	pos int
}

func (m *memFile) Write(p []byte) (int, error) {
	if end := m.pos + len(p); end > len(m.buf) {
		m.buf = append(m.buf, make([]byte, end-len(m.buf))...)
	}
	copy(m.buf[m.pos:], p)
	m.pos += len(p)
	return len(p), nil
}

func (m *memFile) Seek(off int64, whence int) (int64, error) {
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = off
	case io.SeekCurrent:
		abs = int64(m.pos) + off
	case io.SeekEnd:
		abs = int64(len(m.buf)) + off
	}
	if abs < 0 {
		return 0, errors.New("negative seek")
	}
	m.pos = int(abs)
	return abs, nil
}

// buildEROFS returns an EROFS image holding files (path -> content).
func buildEROFS(t *testing.T, files map[string]string) []byte {
	t.Helper()
	src := fstest.MapFS{}
	for name, content := range files {
		src[name] = &fstest.MapFile{Data: []byte(content), Mode: 0o644}
	}
	var out memFile
	w := erofs.Create(&out)
	if err := w.CopyFrom(src); err != nil {
		t.Fatalf("copy files into EROFS: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close EROFS writer: %v", err)
	}
	return out.buf
}

// sysextFiles returns the files of a one-file sysext image.
func sysextFiles(name, content string) map[string]string {
	return map[string]string{
		"usr/lib/extension-release.d/extension-release." + name: content,
		"usr/bin/tool": "#!/bin/sh\n",
	}
}

var rootX86GUID = [16]byte{0xE3, 0xBC, 0x68, 0x4F, 0xCD, 0xE8, 0xB1, 0x4D, 0x96, 0xE7, 0xFB, 0xCA, 0xF9, 0x84, 0xB7, 0x09}

type gptPart struct {
	typeGUID [16]byte
	data     []byte
}

type gptOpts struct {
	sector      int
	entriesLBA  uint64 // 0: LBA 2
	badHdrCRC   bool
	badArrayCRC bool
	entrySize   uint32 // 0: 128
	count       uint32 // 0: 128
}

// buildGPT wraps partitions in a GPT disk image with a protective MBR and a
// primary header (no backup header).
func buildGPT(t *testing.T, o gptOpts, parts ...gptPart) []byte {
	t.Helper()
	if o.sector == 0 {
		o.sector = 512
	}
	if o.entriesLBA == 0 {
		o.entriesLBA = 2
	}
	if o.entrySize == 0 {
		o.entrySize = 128
	}
	if o.count == 0 {
		o.count = 128
	}
	sector := int64(o.sector)
	firstLBA := int64(1 << 20 / sector) // 1 MiB alignment
	array := make([]byte, int64(o.count)*int64(o.entrySize))
	disk := make([]byte, firstLBA*sector)
	lba := firstLBA
	for i, p := range parts {
		sectors := (int64(len(p.data)) + sector - 1) / sector
		e := array[i*int(o.entrySize):]
		copy(e[:16], p.typeGUID[:])
		e[16] = byte(i + 1) // unique GUID
		binary.LittleEndian.PutUint64(e[32:], uint64(lba))
		binary.LittleEndian.PutUint64(e[40:], uint64(lba+sectors-1))
		for j, r := range "root-x86-64" {
			binary.LittleEndian.PutUint16(e[56+2*j:], uint16(r))
		}
		padded := make([]byte, sectors*sector)
		copy(padded, p.data)
		disk = append(disk, padded...)
		lba += sectors
	}

	// Protective MBR.
	disk[446+4] = 0xEE
	binary.LittleEndian.PutUint32(disk[446+8:], 1)
	disk[510], disk[511] = 0x55, 0xAA

	hdr := disk[sector : sector+92]
	copy(hdr, "EFI PART")
	binary.LittleEndian.PutUint32(hdr[8:], 0x00010000)
	binary.LittleEndian.PutUint32(hdr[12:], 92)
	binary.LittleEndian.PutUint64(hdr[24:], 1)
	binary.LittleEndian.PutUint64(hdr[72:], o.entriesLBA)
	binary.LittleEndian.PutUint32(hdr[80:], o.count)
	binary.LittleEndian.PutUint32(hdr[84:], o.entrySize)
	arrayCRC := crc32.ChecksumIEEE(array)
	if o.badArrayCRC {
		arrayCRC++
	}
	binary.LittleEndian.PutUint32(hdr[88:], arrayCRC)
	hdrCRC := crc32.ChecksumIEEE(hdr)
	if o.badHdrCRC {
		hdrCRC++
	}
	binary.LittleEndian.PutUint32(hdr[16:], hdrCRC)

	if o.entriesLBA == 2 {
		copy(disk[2*sector:], array) // fits in the gap before firstLBA
	}
	return disk
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
	erofsKey := buildEROFS(t, sysextFiles("tool", withKey))
	erofsNoKey := buildEROFS(t, sysextFiles("tool", noKey))
	two := buildEROFS(t, map[string]string{
		"usr/lib/extension-release.d/extension-release.alpha": "SYSEXT_ID=alpha\n",
		"usr/lib/extension-release.d/extension-release.beta":  "SYSEXT_ID=beta\n",
	})
	noRelease := buildEROFS(t, map[string]string{"usr/bin/tool": "x"})
	noise := make([]byte, 64<<10)
	if _, err := rand.Read(noise); err != nil {
		t.Fatal(err)
	}
	huge := buildEROFS(t, sysextFiles("tool", "SYSEXT_ID=tool\n#"+strings.Repeat("x", MaxReleaseFileSize)+"\n"))
	rootOnly := gptPart{typeGUID: rootX86GUID}

	tests := []struct {
		name    string
		image   []byte
		hint    string
		wantID  string
		wantMin string
		wantErr error
	}{
		{name: "GPT with the key", image: buildGPT(t, gptOpts{}, gptPart{rootX86GUID, erofsKey}), hint: "tool", wantID: "tool", wantMin: "20261022021319"},
		{name: "GPT without the key", image: buildGPT(t, gptOpts{}, gptPart{rootX86GUID, erofsNoKey}), hint: "tool", wantID: "tool"},
		{name: "GPT with 4096-byte sectors", image: buildGPT(t, gptOpts{sector: 4096}, gptPart{rootX86GUID, erofsKey}), hint: "tool", wantID: "tool", wantMin: "20261022021319"},
		{name: "GPT, EROFS in the second partition", image: buildGPT(t, gptOpts{}, gptPart{[16]byte{1}, noise}, gptPart{rootX86GUID, erofsKey}), wantID: "tool", wantMin: "20261022021319"},
		{name: "bare EROFS", image: erofsKey, hint: "tool", wantID: "tool", wantMin: "20261022021319"},
		{name: "one file, hint differs", image: erofsKey, hint: "other", wantID: "tool", wantMin: "20261022021319"},
		{name: "two files, hint matches", image: two, hint: "beta", wantID: "beta"},
		{name: "two files, no match", image: two, hint: "gamma", wantErr: ErrAmbiguousExtensionRelease},
		{name: "two files, no hint", image: two, wantErr: ErrAmbiguousExtensionRelease},
		{name: "no extension-release file", image: noRelease, wantErr: ErrNoExtensionRelease},
		{name: "GPT, no extension-release file", image: buildGPT(t, gptOpts{}, gptPart{rootX86GUID, noRelease}), wantErr: ErrNoExtensionRelease},
		{name: "partition is not EROFS", image: buildGPT(t, gptOpts{}, gptPart{rootX86GUID, noise}), wantErr: ErrUnsupportedImage},
		{name: "GPT without partitions", image: buildGPT(t, gptOpts{}), wantErr: ErrUnsupportedImage},
		{name: "random file", image: noise, wantErr: ErrUnsupportedImage},
		{name: "empty file", image: nil, wantErr: ErrUnsupportedImage},
		{name: "entry array past the end of the file", image: buildGPT(t, gptOpts{entriesLBA: 1 << 40}, rootOnly), wantErr: ErrUnsupportedImage},
		{name: "entry array only partly in the file", image: buildGPT(t, gptOpts{entriesLBA: uint64(len(erofsKey)/512) + 2048 - 1}, gptPart{rootX86GUID, erofsKey}), wantErr: ErrUnsupportedImage},
		{name: "image cut after the header", image: buildGPT(t, gptOpts{entriesLBA: 0}, rootOnly)[:1024], wantErr: ErrUnsupportedImage},
		{name: "header checksum wrong", image: buildGPT(t, gptOpts{badHdrCRC: true}, gptPart{rootX86GUID, erofsKey}), wantErr: ErrUnsupportedImage},
		{name: "entry array checksum wrong", image: buildGPT(t, gptOpts{badArrayCRC: true}, gptPart{rootX86GUID, erofsKey}), wantErr: ErrUnsupportedImage},
		{name: "entry size invalid", image: buildGPT(t, gptOpts{entrySize: 100}, gptPart{rootX86GUID, erofsKey}), wantErr: ErrUnsupportedImage},
		{name: "entry count too large", image: buildGPT(t, gptOpts{count: 1 << 20}, rootOnly), wantErr: ErrUnsupportedImage},
		{name: "extension-release file too large", image: buildGPT(t, gptOpts{}, gptPart{rootX86GUID, huge}), wantErr: ErrReleaseFileTooLarge},
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
	if err := os.WriteFile(image, buildEROFS(t, sysextFiles("tool", "SYSEXT_ID=tool\n")), 0o600); err != nil {
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
	image := buildGPT(t, gptOpts{}, gptPart{rootX86GUID, buildEROFS(t, sysextFiles("tool", "SYSEXT_ID=tool\n"))})
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
