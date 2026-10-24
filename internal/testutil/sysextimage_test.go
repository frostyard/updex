package testutil

import (
	"bytes"
	"io"
	"io/fs"
	"testing"

	erofs "github.com/erofs/go-erofs"
)

func TestSysextImageIsAGPTWithAnEROFSPartition(t *testing.T) {
	image := SysextImage(t, "tool", "SYSEXT_ID=tool\n")

	if !bytes.Equal(image[512:520], []byte("EFI PART")) {
		t.Fatalf("no GPT header at LBA 1: %q", image[512:520])
	}
	if image[510] != 0x55 || image[511] != 0xAA {
		t.Error("no protective MBR signature")
	}
	// The partition starts at the 1 MiB alignment and holds the EROFS.
	part := bytes.NewReader(image[1<<20:])
	fsys, err := erofs.Open(part)
	if err != nil {
		t.Fatalf("open the partition as EROFS: %v", err)
	}
	data, err := fs.ReadFile(fsys, "usr/lib/extension-release.d/extension-release.tool")
	if err != nil || string(data) != "SYSEXT_ID=tool\n" {
		t.Errorf("extension-release = %q, %v", data, err)
	}
}

func TestBuildGPTSectorSizeAndOptions(t *testing.T) {
	data := BuildEROFS(t, SysextFiles("x", "A=1\n"))
	image := BuildGPT(t, GPTOptions{Sector: 4096, EntrySize: 256, Count: 32, BadHdrCRC: true, BadArrayCRC: true, EntriesLBA: 3},
		GPTPartition{TypeGUID: RootX86GUID, Data: data})
	if !bytes.Equal(image[4096:4104], []byte("EFI PART")) {
		t.Fatal("no GPT header at LBA 1 for 4096-byte sectors")
	}
}

func TestMemFileSeek(t *testing.T) {
	var m memFile
	if _, err := m.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		off    int64
		whence int
		want   int64
	}{{1, io.SeekStart, 1}, {1, io.SeekCurrent, 2}, {-1, io.SeekEnd, 4}} {
		got, err := m.Seek(tt.off, tt.whence)
		if err != nil || got != tt.want {
			t.Errorf("Seek(%d, %d) = %d, %v; want %d", tt.off, tt.whence, got, err, tt.want)
		}
	}
	if _, err := m.Seek(-10, io.SeekStart); err == nil {
		t.Error("a negative position was accepted")
	}
}
