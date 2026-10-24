package imagecompat

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"io/fs"
	"os"
	"strings"
	"syscall"

	erofs "github.com/erofs/go-erofs"
)

const (
	// MaxReleaseFileSize is the largest extension-release or os-release file
	// the package reads. A larger file is refused.
	MaxReleaseFileSize = 64 << 10

	// extensionReleaseDir is the directory in a sysext that holds the
	// extension-release file (extension-release.d(5)).
	extensionReleaseDir    = "usr/lib/extension-release.d"
	extensionReleasePrefix = "extension-release."

	// Limits for the untrusted GPT.
	maxGPTEntries       = 256 // entries in the partition table array
	maxGPTEntrySize     = 1024
	maxPartitionsProbed = 16 // used partitions opened as EROFS
	gptSignature        = "EFI PART"
	gptMinHeaderSize    = 92

	// The EROFS superblock starts at byte offset 1024 of a filesystem and
	// begins with erofsSuperMagic as a little-endian uint32. It marks a
	// candidate region as an EROFS filesystem without asking go-erofs.
	erofsSuperOffset = 1024
	erofsSuperMagic  = 0xE0F5E1E2
	erofsMagicEnd    = erofsSuperOffset + 4
)

// openEROFS opens an EROFS filesystem. It is a variable so a test can
// replace it, for example with one that panics like a parser bug.
var openEROFS = func(r io.ReaderAt) (fs.FS, error) { return erofs.Open(r) }

var (
	// ErrUnsupportedImage means the image has no EROFS filesystem at all:
	// neither a GPT partition nor the bare image starts with the EROFS
	// superblock magic. Examples are a squashfs sysext and random bytes.
	// The image cannot be checked, and the caller decides what to do.
	//
	// Once an EROFS filesystem is found, no later failure wraps this error:
	// every such failure fails closed instead (ErrUnreadableRelease,
	// ErrNoExtensionRelease, ErrAmbiguousExtensionRelease or
	// ErrReleaseFileTooLarge).
	ErrUnsupportedImage = errors.New("unsupported sysext image")

	// ErrUnreadableRelease means the image has an EROFS filesystem but its
	// extension-release file cannot be read: the EROFS filesystem cannot be
	// opened, the file cannot be opened, inspected or read, or the EROFS
	// parser failed. Callers must fail closed: the image may carry a minimum
	// image version they cannot honour.
	ErrUnreadableRelease = errors.New("extension-release file in the image cannot be read")

	// ErrNoExtensionRelease means the image has an EROFS filesystem but no
	// usr/lib/extension-release.d/extension-release.* file.
	ErrNoExtensionRelease = errors.New("no extension-release file in image")

	// ErrAmbiguousExtensionRelease means the image has several
	// extension-release files and none matches the name hint.
	ErrAmbiguousExtensionRelease = errors.New("several extension-release files in image and none matches the name")

	// ErrReleaseFileTooLarge means the extension-release file is larger than
	// MaxReleaseFileSize.
	ErrReleaseFileTooLarge = errors.New("extension-release file is too large")
)

// ReadOptions configures ReadExtensionRelease.
type ReadOptions struct {
	// ErofsDumpRunner reads a compressed EROFS image, which go-erofs cannot
	// open, with the erofs-utils "dump.erofs" program. The zero value uses
	// DefaultErofsDumpRunner; a host without dump.erofs >= 1.8.5 then fails
	// closed. updex passes ClientConfig.ErofsDumpRunner here.
	ErofsDumpRunner ErofsDumpRunner
}

// ReadExtensionRelease reads and parses the extension-release file of the
// staged sysext image at path, without mounting the image.
//
// The path must name a regular file: a symlink or other file type is refused
// (the open uses O_NOFOLLOW, so a link planted after the check is refused
// too). The image can be a GPT disk image (512 or 4096-byte sectors) or a
// bare image. Its candidate regions are tried in table order, and a region
// is an EROFS candidate only when its superblock at offset 1024 has the
// EROFS magic; a region without the magic is skipped.
//
// An uncompressed candidate is read with the pure-Go reader (go-erofs). A
// compressed candidate, which go-erofs refuses, is read with the erofs-utils
// program that ReadOptions.ErofsDumpRunner runs, so a compressed image is
// checked too instead of being skipped. That fallback needs erofs-utils >=
// 1.8.5; without it, or when it fails, the read fails closed with an error
// that wraps ErrUnreadableRelease or ErrReleaseFileTooLarge, and never
// ErrUnsupportedImage.
//
// Once a candidate has the EROFS magic, its failures are fail-closed errors:
// ErrUnreadableRelease, ErrReleaseFileTooLarge, ErrNoExtensionRelease,
// ErrAmbiguousExtensionRelease, ErrErofsToolUnavailable, or a plain error
// from the fallback's runner. When several candidates have the magic, the
// first one that gives a release file is used; when none does, the error of
// the first EROFS candidate is returned. Only when no candidate has the
// magic is the error ErrUnsupportedImage.
//
// In usr/lib/extension-release.d, a single extension-release.* regular file
// is used. When there are several, the file named extension-release.<name>
// is used; without a match the call fails with
// ErrAmbiguousExtensionRelease.
//
// The image is treated as untrusted input even though its SHA256 was
// verified: every read is bounded, the GPT values must point inside the
// file, at most 16 partitions are opened, the extension-release file must
// not be larger than MaxReleaseFileSize, and the fallback validates every
// directory entry name that the tool lists before it uses it in a path.
func ReadExtensionRelease(ctx context.Context, path, name string, opts ReadOptions) (map[string]string, error) {
	f, size, err := openRegular(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	regions, err := imageRegions(f, size)
	if err != nil {
		return nil, err
	}
	fallback := &erofsFallback{runner: opts.ErofsDumpRunner}
	if fallback.runner == nil {
		fallback.runner = DefaultErofsDumpRunner{}
	}
	var firstErr error
	for _, r := range regions {
		if !hasEROFSMagic(f, r) {
			continue
		}
		var release map[string]string
		var err error
		if compressedSuperblock(f, r) {
			release, err = fallback.readRelease(ctx, f, path, r, name)
		} else {
			release, err = readCandidateRelease(f, r, name)
		}
		if err == nil {
			return release, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return nil, fmt.Errorf("%w: no EROFS filesystem found", ErrUnsupportedImage)
}

// hasEROFSMagic reports whether the region holds an EROFS filesystem, by the
// EROFS superblock magic at offset 1024. A region too short to hold the magic
// does not.
func hasEROFSMagic(f io.ReaderAt, r region) bool {
	if r.length < erofsMagicEnd {
		return false
	}
	var magic [4]byte
	if _, err := f.ReadAt(magic[:], r.offset+erofsSuperOffset); err != nil {
		return false
	}
	return binary.LittleEndian.Uint32(magic[:]) == erofsSuperMagic
}

// readCandidateRelease reads and parses the extension-release file of one
// candidate region that has the EROFS magic.
//
// The region is untrusted input and go-erofs parses it, so a parser panic is
// caught here, per candidate, and becomes ErrUnreadableRelease. This is why
// the recover is not in ReadExtensionRelease: past the magic check every
// failure of a candidate is a fail-closed error.
func readCandidateRelease(f io.ReaderAt, r region, name string) (release map[string]string, err error) {
	defer func() {
		if p := recover(); p != nil {
			release = nil
			err = fmt.Errorf("%w: EROFS parser failure: %v", ErrUnreadableRelease, p)
		}
	}()

	fsys, err := openEROFS(io.NewSectionReader(f, r.offset, r.length))
	if err != nil {
		return nil, fmt.Errorf("%w: open EROFS filesystem: %v", ErrUnreadableRelease, err)
	}
	entries, err := fs.ReadDir(fsys, extensionReleaseDir)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNoExtensionRelease, err)
	}
	file, err := pickReleaseFile(entries, name)
	if err != nil {
		return nil, err
	}
	data, err := readBounded(fsys, extensionReleaseDir+"/"+file)
	if err != nil {
		return nil, err
	}
	return ParseOSRelease(data), nil
}

// openRegular opens a regular file without following a symlink and returns
// it with its size.
func openRegular(path string) (*os.File, int64, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, 0, fmt.Errorf("stat image: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, 0, fmt.Errorf("image is not a regular file")
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, 0, fmt.Errorf("open image: %w", err)
	}
	// Check the opened file too: the path could have changed after the Lstat.
	info, err = f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = f.Close()
		if err == nil {
			err = errors.New("image is not a regular file")
		}
		return nil, 0, fmt.Errorf("stat image: %w", err)
	}
	return f, info.Size(), nil
}

// pickReleaseFile selects the extension-release file name from the entries
// of usr/lib/extension-release.d that the pure-Go reader listed. Directories
// are not files; the other rules are those of pickReleaseName.
func pickReleaseFile(entries []fs.DirEntry, name string) (string, error) {
	var candidates []string
	for _, e := range entries {
		if e.IsDir() || !isReleaseName(e.Name()) {
			continue
		}
		candidates = append(candidates, e.Name())
	}
	return pickReleaseName(candidates, name)
}

// pickReleaseName selects the extension-release file to read from the names
// of the regular extension-release files in usr/lib/extension-release.d: the
// only one, or the one named extension-release.<name> when there are several.
// The pure-Go reader and the dump.erofs fallback share these rules.
func pickReleaseName(candidates []string, name string) (string, error) {
	switch len(candidates) {
	case 0:
		return "", ErrNoExtensionRelease
	case 1:
		return candidates[0], nil
	}
	if name != "" {
		for _, c := range candidates {
			if c == extensionReleasePrefix+name {
				return c, nil
			}
		}
	}
	return "", fmt.Errorf("%w (%d files)", ErrAmbiguousExtensionRelease, len(candidates))
}

// isReleaseName reports whether n is an "extension-release.<x>" name with
// something after the prefix.
func isReleaseName(n string) bool {
	return strings.HasPrefix(n, extensionReleasePrefix) && len(n) > len(extensionReleasePrefix)
}

// readBounded reads a file of the image and refuses one larger than
// MaxReleaseFileSize. It checks the size before it reads and limits the
// read itself, so a wrong size in the inode cannot cause a large read.
func readBounded(fsys fs.FS, name string) ([]byte, error) {
	f, err := fsys.Open(name)
	if err != nil {
		return nil, fmt.Errorf("%w: open %s: %v", ErrUnreadableRelease, name, err)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("%w: stat %s: %v", ErrUnreadableRelease, name, err)
	}
	if info.Size() > MaxReleaseFileSize {
		return nil, fmt.Errorf("%w: %d bytes", ErrReleaseFileTooLarge, info.Size())
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxReleaseFileSize+1))
	if err != nil {
		return nil, fmt.Errorf("%w: read %s: %v", ErrUnreadableRelease, name, err)
	}
	if len(data) > MaxReleaseFileSize {
		return nil, ErrReleaseFileTooLarge
	}
	return data, nil
}

// region is a byte range of the image that can hold an EROFS filesystem.
type region struct{ offset, length int64 }

// imageRegions returns the byte ranges to try as EROFS: the used partitions
// when the image has a GPT, else the whole file.
func imageRegions(f io.ReaderAt, size int64) ([]region, error) {
	for _, sector := range []int64{512, 4096} {
		regions, found, err := gptRegions(f, size, sector)
		if err != nil {
			return nil, err
		}
		if found {
			return regions, nil
		}
	}
	return []region{{0, size}}, nil
}

// gptRegions parses the primary GPT at LBA 1 for a sector size. found is
// false when the "EFI PART" signature is not there.
func gptRegions(f io.ReaderAt, size, sector int64) (regions []region, found bool, err error) {
	if size < 2*sector+gptMinHeaderSize {
		return nil, false, nil
	}
	hdr := make([]byte, sector)
	if _, err := f.ReadAt(hdr, sector); err != nil {
		return nil, false, nil
	}
	if string(hdr[:8]) != gptSignature {
		return nil, false, nil
	}
	bad := func(format string, args ...any) ([]region, bool, error) {
		return nil, true, fmt.Errorf("%w: GPT %s", ErrUnsupportedImage, fmt.Sprintf(format, args...))
	}

	le := binary.LittleEndian
	hdrSize := int64(le.Uint32(hdr[12:16]))
	if hdrSize < gptMinHeaderSize || hdrSize > sector {
		return bad("header size %d is invalid", hdrSize)
	}
	// The header CRC32 covers hdrSize bytes with the CRC field set to zero.
	crcInput := bytes.Clone(hdr[:hdrSize])
	wantCRC := le.Uint32(crcInput[16:20])
	clear(crcInput[16:20])
	if crc32.ChecksumIEEE(crcInput) != wantCRC {
		return bad("header checksum does not match")
	}
	entriesLBA := le.Uint64(hdr[72:80])
	count := int64(le.Uint32(hdr[80:84]))
	entrySize := int64(le.Uint32(hdr[84:88]))
	arrayCRC := le.Uint32(hdr[88:92])
	if count < 1 || count > maxGPTEntries {
		return bad("entry count %d is invalid", count)
	}
	if entrySize < 128 || entrySize > maxGPTEntrySize || entrySize%8 != 0 {
		return bad("entry size %d is invalid", entrySize)
	}
	// Compare in sectors and bytes without overflow: entriesLBA comes from
	// the file.
	if entriesLBA < 2 || entriesLBA > uint64(size/sector) {
		return bad("entry array position is outside the file")
	}
	arrayOff := int64(entriesLBA) * sector
	arrayLen := count * entrySize
	if arrayLen > size-arrayOff {
		return bad("entry array is outside the file")
	}
	array := make([]byte, arrayLen)
	if _, err := f.ReadAt(array, arrayOff); err != nil {
		return bad("entry array cannot be read: %v", err)
	}
	if crc32.ChecksumIEEE(array) != arrayCRC {
		return bad("entry array checksum does not match")
	}

	sectors := uint64(size / sector)
	for i := int64(0); i < count && len(regions) < maxPartitionsProbed; i++ {
		e := array[i*entrySize : (i+1)*entrySize]
		if allZero(e[:16]) { // unused entry
			continue
		}
		first, last := le.Uint64(e[32:40]), le.Uint64(e[40:48])
		if first < 2 || last < first || last >= sectors {
			continue // partition outside the file
		}
		regions = append(regions, region{
			offset: int64(first) * sector,
			length: int64(last-first+1) * sector,
		})
	}
	return regions, true, nil
}

func allZero(b []byte) bool {
	for _, c := range b {
		if c != 0 {
			return false
		}
	}
	return true
}
