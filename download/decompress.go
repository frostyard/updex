package download

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"

	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"
)

// decompressFile decompresses a file based on the compression type
func decompressFile(srcPath, dstPath, compressionType string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer func() { _ = src.Close() }()

	dst, err := os.Create(dstPath)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer func() { _ = dst.Close() }()

	var reader io.Reader

	switch compressionType {
	case "xz":
		xzReader, err := xz.NewReader(src)
		if err != nil {
			return fmt.Errorf("failed to create xz reader: %w", err)
		}
		reader = xzReader

	case "gz":
		gzReader, err := gzip.NewReader(src)
		if err != nil {
			return fmt.Errorf("failed to create gzip reader: %w", err)
		}
		defer func() { _ = gzReader.Close() }()
		reader = gzReader

	case "zstd":
		zstdReader, err := zstd.NewReader(src)
		if err != nil {
			return fmt.Errorf("failed to create zstd reader: %w", err)
		}
		defer zstdReader.Close()
		reader = zstdReader

	default:
		return fmt.Errorf("unsupported compression type: %s", compressionType)
	}

	_, err = io.Copy(dst, reader)
	if err != nil {
		return fmt.Errorf("failed to decompress: %w", err)
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
