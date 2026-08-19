package download

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"
)

// ErrDecompressedTooLarge reports that decompressed output crossed the
// configured maximum size.
var ErrDecompressedTooLarge = errors.New("decompressed image exceeds the maximum size")

// decompressFile decompresses a file based on the compression type
func decompressFile(srcPath, dstPath, compressionType string, maxBytes int64) (retErr error) {
	if maxBytes <= 0 {
		return fmt.Errorf("maximum decompressed size must be greater than zero")
	}

	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer func() { _ = src.Close() }()

	dst, err := os.Create(dstPath)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer func() {
		if err := dst.Close(); err != nil && retErr == nil {
			retErr = fmt.Errorf("failed to close destination file: %w", err)
		}
	}()

	reader, err := DecompressReader(src, compressionType)
	if err != nil {
		return err
	}
	defer func() { _ = reader.Close() }()

	written, err := io.CopyN(dst, reader, maxBytes)
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("failed to decompress: %w", err)
	}
	if written == maxBytes {
		var extra [1]byte
		extraBytes, readErr := io.ReadFull(reader, extra[:])
		if extraBytes > 0 {
			return fmt.Errorf("%w (%d bytes)", ErrDecompressedTooLarge, maxBytes)
		}
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return fmt.Errorf("failed to decompress: %w", readErr)
		}
	}

	// Persist the decompressed output before the caller renames it into
	// place, so a crash after install cannot leave a partial image behind.
	if err := syncFile(dst); err != nil {
		return fmt.Errorf("failed to sync destination file: %w", err)
	}

	return nil
}

// DecompressReader returns a reader that decompresses on-the-fly
func DecompressReader(r io.Reader, compressionType string) (io.ReadCloser, error) {
	switch compressionType {
	case "xz":
		xzReader, err := xz.NewReader(r)
		if err != nil {
			return nil, fmt.Errorf("failed to create xz reader: %w", err)
		}
		return io.NopCloser(xzReader), nil

	case "gz":
		return gzip.NewReader(r)

	case "zstd":
		zstdReader, err := zstd.NewReader(r)
		if err != nil {
			return nil, fmt.Errorf("failed to create zstd reader: %w", err)
		}
		return &zstdReadCloser{zstdReader}, nil

	case "":
		return io.NopCloser(r), nil

	default:
		return nil, fmt.Errorf("unsupported compression type: %s", compressionType)
	}
}

// zstdReadCloser wraps zstd.Decoder to implement io.ReadCloser
type zstdReadCloser struct {
	*zstd.Decoder
}

func (z *zstdReadCloser) Close() error {
	z.Decoder.Close()
	return nil
}
