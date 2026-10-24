package imagecompat

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// This file is the dump.erofs fallback reader for compressed EROFS images.
//
// The pure-Go reader (go-erofs) handles uncompressed EROFS only: it refuses a
// compressed filesystem at open. All frostyard sysexts are uncompressed, so
// the pure-Go reader stays the normal path. A compressed one is read with the
// erofs-utils "dump.erofs" program instead, which prints a file out of an
// image without mounting it. A host without a new enough dump.erofs fails
// closed: the minimum image version check never silently skips an image it
// cannot read.
//
// The subprocess is bounded and cancellable: every command runs with the
// caller's context, a minimal environment (PATH and LC_ALL=C), no shell, a
// fixed output limit, and its output is parsed here, never evaluated. The
// image path is handed to dump.erofs, which opens it again, so the caller
// checks after the read that the path still names the file it opened.

const (
	// ErofsUtilsMinVersion is the oldest erofs-utils release whose dump.erofs
	// has every option the fallback uses. --ls and --offset came with
	// erofs-utils 1.8; --cat, which prints a file, came with erofs-utils
	// 1.8.5.
	ErofsUtilsMinVersion = "1.8.5"

	// MaxErofsDumpListSize is the most output of "dump.erofs --ls" that is
	// read. One directory of an image names at most a few hundred entries.
	MaxErofsDumpListSize = 256 << 10

	// maxErofsDumpVersionSize is the most output of "dump.erofs -V" that is
	// read.
	maxErofsDumpVersionSize = 4 << 10

	// maxDumpRunTime bounds one dump.erofs command in addition to the
	// caller's context. Reading one small file from an image takes
	// milliseconds; a tool that hangs must not hang an update (the timer
	// runs with no deadline of its own).
	maxDumpRunTime = 2 * time.Minute

	// dumpBinary is the erofs-utils program the fallback runs.
	dumpBinary = "dump.erofs"

	// extensionReleaseAbsDir is extensionReleaseDir as an absolute path, the
	// form "dump.erofs --path" requires.
	extensionReleaseAbsDir = "/" + extensionReleaseDir

	// maxListedNameLen is the longest directory entry name EROFS can hold.
	maxListedNameLen = 255
)

// ErrErofsToolUnavailable means the fallback cannot read a compressed EROFS
// image: dump.erofs is not installed, or it is older than
// ErofsUtilsMinVersion. The error also wraps ErrUnreadableRelease, so callers
// that treat every read failure the same keep failing closed.
var ErrErofsToolUnavailable = errors.New("the dump.erofs fallback is not available")

// errDumpOutputTooLarge means a dump.erofs command printed more than its
// bound. It is internal: each public method turns it into the sentinel its
// caller understands.
var errDumpOutputTooLarge = errors.New("dump.erofs printed more output than allowed")

// ErofsDirEntry is one entry of a directory that dump.erofs lists.
type ErofsDirEntry struct {
	// Name is the entry name. EROFS has no paths, only names.
	Name string
	// Regular reports whether the entry is a regular file. The selection
	// rules read only regular files, so a directory or a symbolic link named
	// extension-release.<x> is never used.
	Regular bool
}

// ErofsDumpRunner reads files from an EROFS image with the erofs-utils
// "dump.erofs" program. The tests use a fake. Every method returns an error
// that wraps ErrUnreadableRelease when it fails, so a caller of
// ReadExtensionRelease never has to classify a tool failure; ReadFile wraps
// ErrReleaseFileTooLarge when the file is larger than the limit.
type ErofsDumpRunner interface {
	// Version returns the erofs-utils version that dump.erofs reports, such
	// as "1.8.6". It returns an error that wraps exec.ErrNotFound when
	// dump.erofs is not installed.
	Version(ctx context.Context) (string, error)
	// ReadFile returns the contents of the absolute path inside the EROFS
	// filesystem that starts at offset in image, and fails when the file is
	// larger than maxSize bytes.
	ReadFile(ctx context.Context, image string, offset int64, path string, maxSize int) ([]byte, error)
	// ListDir returns the entries of the absolute directory path inside the
	// EROFS filesystem that starts at offset in image.
	ListDir(ctx context.Context, image string, offset int64, path string) ([]ErofsDirEntry, error)
}

// DefaultErofsDumpRunner runs the real dump.erofs found in PATH.
type DefaultErofsDumpRunner struct{}

var _ ErofsDumpRunner = DefaultErofsDumpRunner{}

// Version runs "dump.erofs -V" with ctx and returns its version.
//
// erofs-utils prints the version on one line, as
//
//	dump.erofs (erofs-utils) 1.8.6
//
// so the version is the last field of the first line whose last field parses
// as a version. A suffix after the numbers, such as "1.8.6-1", is ignored for
// the comparison.
func (DefaultErofsDumpRunner) Version(ctx context.Context) (string, error) {
	out, err := runDump(ctx, maxErofsDumpVersionSize, "-V")
	if errors.Is(err, errDumpOutputTooLarge) {
		return "", fmt.Errorf("%w: %s -V printed more than %d bytes", ErrUnreadableRelease, dumpBinary, maxErofsDumpVersionSize)
	}
	if err != nil {
		return "", err
	}
	return parseDumpVersion(string(out))
}

// ReadFile runs "dump.erofs --offset=<offset> --cat --path=<path> <image>".
func (DefaultErofsDumpRunner) ReadFile(ctx context.Context, image string, offset int64, path string, maxSize int) ([]byte, error) {
	out, err := runDump(ctx, maxSize, offsetArgs(offset, "--cat", "--path="+path, image)...)
	if errors.Is(err, errDumpOutputTooLarge) {
		return nil, fmt.Errorf("%w: dump.erofs printed more than %d bytes for %s", ErrReleaseFileTooLarge, maxSize, path)
	}
	return out, err
}

// ListDir runs "dump.erofs --offset=<offset> --ls --path=<path> <image>" and
// parses its table.
func (DefaultErofsDumpRunner) ListDir(ctx context.Context, image string, offset int64, path string) ([]ErofsDirEntry, error) {
	out, err := runDump(ctx, MaxErofsDumpListSize, offsetArgs(offset, "--ls", "--path="+path, image)...)
	if errors.Is(err, errDumpOutputTooLarge) {
		return nil, fmt.Errorf("%w: dump.erofs printed more than %d bytes listing %s", ErrUnreadableRelease, MaxErofsDumpListSize, path)
	}
	if err != nil {
		return nil, err
	}
	return parseDumpList(out)
}

// offsetArgs builds the argument list: the byte offset of the EROFS
// filesystem inside the image, then args. dump.erofs accepts them in any
// order; the image path is last.
func offsetArgs(offset int64, args ...string) []string {
	return append([]string{"--offset=" + strconv.FormatInt(offset, 10)}, args...)
}

// runDump runs dump.erofs with args and returns its standard output, at most
// max bytes.
//
// The environment is minimal: PATH and LC_ALL=C, so the messages and the
// version line have one form whatever the caller's locale. No shell is
// involved. The context stops the command: exec.CommandContext kills it, and
// the error names the context's error.
//
// erofs-utils reports some failures on standard error and still exits 0 (a
// missing inode, for example, prints "<E> erofs: read inode failed @ <path>"),
// so a line that starts with "<E>" is an error even when the exit status is
// 0. A line that starts with "<W>" is a warning and is ignored.
func runDump(ctx context.Context, max int, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, maxDumpRunTime)
	defer cancel()
	bin, err := exec.LookPath(dumpBinary)
	if err != nil {
		return nil, fmt.Errorf("%s is not installed: %w", dumpBinary, err)
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	// WaitDelay bounds Wait after the context is done: a killed tool whose
	// output pipe is still held open cannot hang the caller.
	cmd.WaitDelay = 2 * time.Second
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "LC_ALL=C"}
	var out, stderr limitedBuffer
	out.max = max
	stderr.max, stderr.truncate = 2048, true
	cmd.Stdout, cmd.Stderr = &out, &stderr
	runErr := cmd.Run()
	if out.exceeded {
		return nil, errDumpOutputTooLarge
	}
	name := dumpBinary + " " + strings.Join(args, " ")
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, fmt.Errorf("%w: %s stopped: %w", ErrUnreadableRelease, name, ctxErr)
	}
	msg := strings.TrimSpace(stderr.buf.String())
	if runErr != nil {
		return nil, fmt.Errorf("%w: %s: %v%s", ErrUnreadableRelease, name, runErr, stderrSuffix(msg))
	}
	if first := firstToolError(msg); first != "" {
		return nil, fmt.Errorf("%w: %s: %s", ErrUnreadableRelease, name, first)
	}
	return out.buf.Bytes(), nil
}

// stderrSuffix renders the tool's standard error for an error message, or ""
// when there is none.
func stderrSuffix(msg string) string {
	if msg == "" {
		return ""
	}
	return ": " + msg
}

// firstToolError returns the first line of a dump.erofs standard error that
// reports an error ("<E> ..."), with the marker and prefix removed, or "".
func firstToolError(msg string) string {
	for line := range strings.SplitSeq(msg, "\n") {
		line = strings.TrimSpace(line)
		rest, ok := strings.CutPrefix(line, "<E>")
		if !ok {
			continue
		}
		rest = strings.TrimSpace(rest)
		// erofs-utils prefixes its own messages with the program name.
		rest = strings.TrimPrefix(rest, "erofs: ")
		rest = strings.TrimPrefix(rest, "erofs_io: ")
		return rest
	}
	return ""
}

// parseDumpVersion returns the version from the output of "dump.erofs -V".
func parseDumpVersion(out string) (string, error) {
	for line := range strings.SplitSeq(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if v := fields[len(fields)-1]; validVersionString(v) {
			return v, nil
		}
	}
	return "", fmt.Errorf("%w: %s -V printed no version: %q", ErrUnreadableRelease, dumpBinary, strings.TrimSpace(out))
}

// validVersionString reports whether s starts with a version number such as
// "1.8.6". A suffix, such as the "-1" of a distribution package version, is
// allowed and is not part of the comparison.
func validVersionString(s string) bool {
	_, ok := parseVersionNumbers(s)
	return ok
}

// parseVersionNumbers returns the numbers of a version string such as
// "1.8.6" or "v1.8.6-1". Parsing stops at the first byte that is neither a
// digit nor a dot after the first number. The first component must be digits.
func parseVersionNumbers(s string) ([]int, bool) {
	s = strings.TrimPrefix(s, "v")
	var parts []int
	start := 0
	for start < len(s) {
		end := start
		for end < len(s) && s[end] >= '0' && s[end] <= '9' {
			end++
		}
		if end == start {
			break // no digits here: the numeric version ended (or never began)
		}
		n, err := strconv.Atoi(s[start:end])
		if err != nil {
			return nil, false
		}
		parts = append(parts, n)
		if end < len(s) && s[end] == '.' {
			start = end + 1
			continue
		}
		break
	}
	if len(parts) == 0 {
		return nil, false
	}
	return parts, true
}

// erofsVersionAtLeast reports whether a version such as "1.8.6" is at least
// want such as "1.8.5". Missing components count as 0, so "1.9" is at least
// "1.8.5". An unparseable version is not at least anything: the caller fails
// closed.
func erofsVersionAtLeast(got, want string) bool {
	g, ok := parseVersionNumbers(got)
	if !ok {
		return false
	}
	w, ok := parseVersionNumbers(want)
	if !ok {
		return false
	}
	for i := 0; i < max(len(g), len(w)); i++ {
		var a, b int
		if i < len(g) {
			a = g[i]
		}
		if i < len(w) {
			b = w[i]
		}
		if a != b {
			return a > b
		}
	}
	return true
}

// parseDumpList parses the table that "dump.erofs --ls" prints. erofs-utils
// 1.8.6 prints a block of inode details, a blank line, then a table:
//
//	NID TYPE  FILENAME
//	 51    2  .
//	 47    2  ..
//	 56    1  extension-release.tool
//
// The header line names the columns; every line after it starts with the
// numeric NID and numeric TYPE fields, and the file name is the rest of the
// line, which may contain spaces. The EROFS file type is 1 for a regular
// file, 2 for a directory, and 7 for a symbolic link.
//
// Output without the header (a regular file listed as the path, for example)
// is an error: nothing is known about its directory.
func parseDumpList(out []byte) ([]ErofsDirEntry, error) {
	var entries []ErofsDirEntry
	inTable := false
	for line := range strings.SplitSeq(string(out), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if !inTable {
			if isListHeader(line) {
				inTable = true
			}
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		typ, name, ok := cutListRow(line)
		if !ok {
			return nil, fmt.Errorf("%w: %s --ls printed an unexpected line %q", ErrUnreadableRelease, dumpBinary, line)
		}
		entries = append(entries, ErofsDirEntry{Name: name, Regular: typ == erofsTypeRegular})
	}
	if !inTable {
		return nil, fmt.Errorf("%w: %s --ls printed no file table", ErrUnreadableRelease, dumpBinary)
	}
	return entries, nil
}

const (
	// erofsTypeRegular is EROFS_FT_REG_FILE, the type dump.erofs --ls prints
	// in its TYPE column.
	erofsTypeRegular = 1
)

// isListHeader reports whether line is the "NID TYPE FILENAME" header of the
// table dump.erofs --ls prints.
func isListHeader(line string) bool {
	fields := strings.Fields(line)
	return len(fields) == 3 && fields[0] == "NID" && fields[1] == "TYPE" && fields[2] == "FILENAME"
}

// cutListRow splits one row of the table into its type and its name. The
// name is the rest of the line after the two numeric fields.
func cutListRow(line string) (typ int, name string, ok bool) {
	rest := strings.TrimLeft(line, " \t")
	nid, rest, ok := cutField(rest)
	if !ok || !isDigits(nid) {
		return 0, "", false
	}
	typeField, rest, ok := cutField(rest)
	if !ok || !isDigits(typeField) {
		return 0, "", false
	}
	t, err := strconv.Atoi(typeField)
	if err != nil {
		return 0, "", false
	}
	name = strings.TrimLeft(rest, " \t")
	if name == "" {
		return 0, "", false
	}
	return t, name, true
}

// cutField returns the first space-separated field of s and the rest of s
// after the run of spaces that follows it.
func cutField(s string) (field, rest string, ok bool) {
	i := strings.IndexAny(s, " \t")
	if i < 0 {
		return "", "", false
	}
	return s[:i], strings.TrimLeft(s[i:], " \t"), true
}

// erofsFallback reads compressed EROFS candidates of one image through
// dump.erofs. The zero value is not usable: ReadExtensionRelease fills runner.
//
// It is not safe for concurrent use; one ReadExtensionRelease call reads its
// candidates one after another.
type erofsFallback struct {
	runner ErofsDumpRunner
	// usable reports whether the runner's version is known to be new enough.
	usable bool
}

// readRelease reads and parses the extension-release file of one compressed
// candidate region, without mounting the image.
//
// It lists usr/lib/extension-release.d first and reads the file it selects,
// so a directory or a symbolic link named extension-release.<x> is never read
// as the file: the listing gives each entry's type.
//
// The image is untrusted input that the tool parses, so every listed name is
// validated before it reaches a command line and every read is bounded.
func (b *erofsFallback) readRelease(ctx context.Context, f *os.File, path string, r region, name string) (map[string]string, error) {
	if err := b.checkVersion(ctx); err != nil {
		return nil, err
	}
	entries, err := b.runner.ListDir(ctx, path, r.offset, extensionReleaseAbsDir)
	if err != nil {
		return nil, err
	}
	candidates, err := releaseCandidates(entries)
	if err != nil {
		return nil, err
	}
	file, err := pickReleaseName(candidates, name)
	if err != nil {
		return nil, err
	}
	data, err := b.runner.ReadFile(ctx, path, r.offset, extensionReleaseAbsDir+"/"+file, MaxReleaseFileSize)
	if err != nil {
		return nil, err
	}
	// dump.erofs opened the path again. Refuse a path that no longer names
	// the file this call opened and validated.
	if err := unchangedImage(f, path); err != nil {
		return nil, err
	}
	return ParseOSRelease(data), nil
}

// checkVersion runs "dump.erofs -V" on the first use and fails closed when
// the tool is missing or older than ErofsUtilsMinVersion.
func (b *erofsFallback) checkVersion(ctx context.Context) error {
	if b.usable {
		return nil
	}
	version, err := b.runner.Version(ctx)
	switch {
	case errors.Is(err, exec.ErrNotFound):
		return errToolMissing()
	case err != nil:
		return err
	}
	if !erofsVersionAtLeast(version, ErofsUtilsMinVersion) {
		return errToolTooOld(version)
	}
	b.usable = true
	return nil
}

// errToolMissing is the fail-closed error for a host without dump.erofs.
func errToolMissing() error {
	return fmt.Errorf("%w: %w: erofs-utils >= %s is required to read compressed EROFS images; install erofs-utils",
		ErrUnreadableRelease, ErrErofsToolUnavailable, ErofsUtilsMinVersion)
}

// errToolTooOld is the fail-closed error for a dump.erofs that has no --cat.
func errToolTooOld(version string) error {
	return fmt.Errorf("%w: %w: %s %s is too old; erofs-utils >= %s is required to read compressed EROFS images",
		ErrUnreadableRelease, ErrErofsToolUnavailable, dumpBinary, version, ErofsUtilsMinVersion)
}

// releaseCandidates validates a dump.erofs directory listing and returns the
// names of its regular extension-release files.
//
// Every listed name is checked before it is used in a path: a name that is
// empty, longer than EROFS allows, or holds anything but printable ASCII
// without a slash is refused, and the read fails closed. The "." and ".."
// entries that every directory lists are expected and skipped.
func releaseCandidates(entries []ErofsDirEntry) ([]string, error) {
	var candidates []string
	for _, e := range entries {
		if e.Name == "." || e.Name == ".." {
			continue
		}
		if err := checkListedName(e.Name); err != nil {
			return nil, err
		}
		if e.Regular && isReleaseName(e.Name) {
			candidates = append(candidates, e.Name)
		}
	}
	return candidates, nil
}

// checkListedName refuses a directory entry name that is not safe to use as
// one path component: it must be printable ASCII without a slash.
func checkListedName(name string) error {
	if name == "" || len(name) > maxListedNameLen {
		return fmt.Errorf("%w: %s listed an invalid entry name %q", ErrUnreadableRelease, dumpBinary, name)
	}
	for i := 0; i < len(name); i++ {
		if b := name[i]; b < 0x20 || b > 0x7e || b == '/' {
			return fmt.Errorf("%w: %s listed an unsafe entry name %q", ErrUnreadableRelease, dumpBinary, name)
		}
	}
	return nil
}

// unchangedImage reports whether path still names the open file f.
//
// The fallback hands the path to dump.erofs, which opens it again; a file
// swapped in after openRegular's O_NOFOLLOW open would be read instead of the
// image this call checked. The two are compared after the read, and the read
// fails closed when they differ.
func unchangedImage(f *os.File, path string) error {
	opened, err := f.Stat()
	if err != nil {
		return fmt.Errorf("%w: stat open image: %v", ErrUnreadableRelease, err)
	}
	current, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("%w: stat image after the read: %v", ErrUnreadableRelease, err)
	}
	if !current.Mode().IsRegular() || !os.SameFile(opened, current) {
		return fmt.Errorf("%w: the image changed while %s read it", ErrUnreadableRelease, dumpBinary)
	}
	return nil
}

// The EROFS superblock fields that say whether the filesystem holds
// compressed data. They are read from the 128-byte superblock that starts at
// byte 1024 of the filesystem:
//
//	offset 80: feature_incompat, a little-endian uint32
//	offset 84: available_compr_algs, a little-endian uint16 (a union with
//	           lz4_max_distance)
//
// These two fields decide a question go-erofs can only answer by failing:
// it refuses a compressed filesystem when it opens it. Reading the fields
// makes the choice explicit and independent of an error message.
const (
	// erofsIncompatFieldOffset is the offset of feature_incompat in the
	// superblock.
	erofsIncompatFieldOffset = erofsSuperOffset + 80
	// erofsCompressedFieldsEnd is the first byte after available_compr_algs.
	erofsCompressedFieldsEnd = erofsSuperOffset + 86

	// EROFS_FEATURE_INCOMPAT_LZ4_0PADDING (0x1) and
	// EROFS_FEATURE_INCOMPAT_COMPR_CFGS (0x2) are the feature_incompat flags
	// mkfs.erofs sets on a compressed filesystem.
	erofsIncompatLZ4Padding = 0x1
	erofsIncompatComprCfgs  = 0x2
	erofsIncompatCompressed = erofsIncompatLZ4Padding | erofsIncompatComprCfgs
)

// compressedSuperblock reports whether the EROFS superblock of r describes a
// filesystem with compressed data, which go-erofs cannot open. A region too
// short to hold the fields is not compressed; the read of it fails later.
func compressedSuperblock(f io.ReaderAt, r region) bool {
	if r.length < erofsCompressedFieldsEnd {
		return false
	}
	var fields [6]byte
	if _, err := f.ReadAt(fields[:], r.offset+erofsIncompatFieldOffset); err != nil {
		return false
	}
	le := binary.LittleEndian
	incompat := le.Uint32(fields[0:4])
	comprAlgs := le.Uint16(fields[4:6])
	return incompat&erofsIncompatCompressed != 0 || comprAlgs != 0
}
