package testutil

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"testing"
	"testing/fstest"

	erofs "github.com/erofs/go-erofs"
)

// This file builds sysext fixture images for tests: an EROFS filesystem
// (written with go-erofs's writer) optionally wrapped in a GPT disk image,
// the shape snosi publishes. It is test code only; nothing outside tests
// imports this package.

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

// BuildEROFS returns an EROFS image holding files (path -> content).
func BuildEROFS(t *testing.T, files map[string]string) []byte {
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

// SysextFiles returns the files of a one-file sysext image: the
// extension-release.<name> file with content, and a binary.
func SysextFiles(name, content string) map[string]string {
	return map[string]string{
		"usr/lib/extension-release.d/extension-release." + name: content,
		"usr/bin/tool": "#!/bin/sh\n",
	}
}

// RootX86GUID is the on-disk (mixed-endian) type GUID of a root-x86-64
// partition, 4F68BCE3-E8CD-4DB1-96E7-FBCAF984B709.
var RootX86GUID = [16]byte{0xE3, 0xBC, 0x68, 0x4F, 0xCD, 0xE8, 0xB1, 0x4D, 0x96, 0xE7, 0xFB, 0xCA, 0xF9, 0x84, 0xB7, 0x09}

// GPTPartition is one partition of a fixture GPT image.
type GPTPartition struct {
	TypeGUID [16]byte
	Data     []byte
}

// GPTOptions tunes BuildGPT; the zero value gives a valid 512-byte-sector GPT.
type GPTOptions struct {
	Sector      int
	EntriesLBA  uint64 // 0: LBA 2
	BadHdrCRC   bool
	BadArrayCRC bool
	EntrySize   uint32 // 0: 128
	Count       uint32 // 0: 128
}

// BuildGPT wraps partitions in a GPT disk image with a protective MBR and a
// primary header (no backup header).
func BuildGPT(t *testing.T, o GPTOptions, parts ...GPTPartition) []byte {
	t.Helper()
	if o.Sector == 0 {
		o.Sector = 512
	}
	if o.EntriesLBA == 0 {
		o.EntriesLBA = 2
	}
	if o.EntrySize == 0 {
		o.EntrySize = 128
	}
	if o.Count == 0 {
		o.Count = 128
	}
	sector := int64(o.Sector)
	firstLBA := int64(1 << 20 / sector) // 1 MiB alignment
	array := make([]byte, int64(o.Count)*int64(o.EntrySize))
	disk := make([]byte, firstLBA*sector)
	lba := firstLBA
	for i, p := range parts {
		sectors := (int64(len(p.Data)) + sector - 1) / sector
		e := array[i*int(o.EntrySize):]
		copy(e[:16], p.TypeGUID[:])
		e[16] = byte(i + 1) // unique GUID
		binary.LittleEndian.PutUint64(e[32:], uint64(lba))
		binary.LittleEndian.PutUint64(e[40:], uint64(lba+sectors-1))
		for j, r := range "root-x86-64" {
			binary.LittleEndian.PutUint16(e[56+2*j:], uint16(r))
		}
		padded := make([]byte, sectors*sector)
		copy(padded, p.Data)
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
	binary.LittleEndian.PutUint64(hdr[72:], o.EntriesLBA)
	binary.LittleEndian.PutUint32(hdr[80:], o.Count)
	binary.LittleEndian.PutUint32(hdr[84:], o.EntrySize)
	arrayCRC := crc32.ChecksumIEEE(array)
	if o.BadArrayCRC {
		arrayCRC++
	}
	binary.LittleEndian.PutUint32(hdr[88:], arrayCRC)
	hdrCRC := crc32.ChecksumIEEE(hdr)
	if o.BadHdrCRC {
		hdrCRC++
	}
	binary.LittleEndian.PutUint32(hdr[16:], hdrCRC)

	if o.EntriesLBA == 2 {
		copy(disk[2*sector:], array) // fits in the gap before firstLBA
	}
	return disk
}

// SysextImage returns a GPT disk image with one root-x86-64 partition that
// holds an EROFS filesystem with one extension-release.<name> file. release is
// the file's content. It is the shape of a snosi sysext.
func SysextImage(t *testing.T, name, release string) []byte {
	t.Helper()
	return BuildGPT(t, GPTOptions{}, GPTPartition{TypeGUID: RootX86GUID, Data: BuildEROFS(t, SysextFiles(name, release))})
}
