package manifest

import (
	"bytes"
	"context"
	"crypto"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/packet"

	"github.com/frostyard/updex/internal/retry"
)

// KeyringPaths are the filesystem locations checked, in order, for the GPG
// keyring used to verify manifest signatures (matching systemd-sysupdate's
// defaults). Tests in other packages that need a downloaded manifest to pass
// verification override this to point at a trusted test keyring.
var KeyringPaths = []string{
	"/etc/systemd/import-pubring.gpg",
	"/usr/lib/systemd/import-pubring.gpg",
}

// maxSigSize bounds detached-signature reads at 1 MiB to prevent memory
// exhaustion from a malicious or misbehaving server returning an unbounded
// response.
const maxSigSize = 1 << 20

// verifySignature verifies the GPG signature of the manifest content.
//
// The detached-signature GET and body read run inside the same bounded retry
// policy as the SHA256SUMS fetch (ADR-0008): transient network errors and
// 429/5xx responses are retried with rs; other failures return immediately.
// Keyring loading and signature checking happen after the fetch and are never
// retried.
func verifySignature(ctx context.Context, client *http.Client, sigURL string, content []byte, rs retrySettings) error {
	sigData, err := fetchSignature(ctx, client, sigURL, rs)
	if err != nil {
		return err
	}

	// Load keyring
	keyring, err := loadKeyring()
	if err != nil {
		return fmt.Errorf("failed to load keyring: %w", err)
	}

	signaturePacket, parseErr := packet.NewReader(bytes.NewReader(sigData)).Next()
	if parseErr == nil {
		if signature, ok := signaturePacket.(*packet.Signature); ok {
			switch signature.Hash {
			case crypto.SHA1, crypto.MD5, crypto.RIPEMD160:
				return fmt.Errorf("rejected signature message hash algorithm: %s", signature.Hash)
			}
		}
	}

	// Verify signature
	_, err = openpgp.CheckDetachedSignature(
		keyring,
		bytes.NewReader(content),
		bytes.NewReader(sigData),
		nil,
	)
	if err != nil {
		return fmt.Errorf("invalid signature: %w", err)
	}

	return nil
}

// fetchSignature downloads the detached signature at sigURL under the bounded
// retry policy rs, classifying failures exactly as Fetch does for SHA256SUMS.
func fetchSignature(ctx context.Context, client *http.Client, sigURL string, rs retrySettings) ([]byte, error) {
	var sigData []byte
	err := retry.Do(ctx, rs.cfg, rs.notify, func() error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, sigURL, nil)
		if err != nil {
			return fmt.Errorf("failed to create signature request: %w", err)
		}

		resp, err := client.Do(req)
		if err != nil {
			return retry.TransientIfNetwork(fmt.Errorf("failed to fetch signature: %w", err))
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError {
			return retry.Transient(fmt.Errorf("signature fetch failed with status: %s", resp.Status))
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("signature fetch failed with status: %s", resp.Status)
		}

		sigData, err = io.ReadAll(io.LimitReader(resp.Body, maxSigSize+1))
		if err != nil {
			return retry.TransientIfNetwork(fmt.Errorf("failed to read signature: %w", err))
		}
		if len(sigData) > maxSigSize {
			return fmt.Errorf("signature response exceeds maximum allowed size (%d bytes)", maxSigSize)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}
	return sigData, nil
}

// loadKeyring loads the GPG keyring from default paths
func loadKeyring() (openpgp.EntityList, error) {
	for _, path := range KeyringPaths {
		keyring, err := readKeyringFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}

		return keyring, nil
	}

	return nil, fmt.Errorf("no keyring found in %v", KeyringPaths)
}

// readKeyringFile reads a GPG keyring from a single file path.
// Returns os.ErrNotExist if the file does not exist.
func readKeyringFile(path string) (openpgp.EntityList, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	keyring, err := openpgp.ReadKeyRing(f)
	if err != nil {
		// Try armored format
		_, _ = f.Seek(0, 0)
		keyring, err = openpgp.ReadArmoredKeyRing(f)
		if err != nil {
			return nil, fmt.Errorf("failed to read keyring from %s: %w", path, err)
		}
	}

	return keyring, nil
}
