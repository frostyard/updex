package manifest

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/ProtonMail/go-crypto/openpgp"
)

// Default keyring paths (matching systemd-sysupdate)
var keyringPaths = []string{
	"/etc/systemd/import-pubring.gpg",
	"/usr/lib/systemd/import-pubring.gpg",
}

// verifySignature verifies the GPG signature of the manifest content
func verifySignature(ctx context.Context, client *http.Client, sigURL string, content []byte) error {
	// Download signature
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sigURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create signature request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch signature: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("signature fetch failed with status: %s", resp.Status)
	}

	// Limit signature reads to 1 MiB to prevent memory exhaustion from
	// a malicious or misbehaving server returning an unbounded response.
	const maxSigSize = 1 << 20 // 1 MiB
	sigData, err := io.ReadAll(io.LimitReader(resp.Body, maxSigSize+1))
	if err != nil {
		return fmt.Errorf("failed to read signature: %w", err)
	}
	if len(sigData) > maxSigSize {
		return fmt.Errorf("signature response exceeds maximum allowed size (%d bytes)", maxSigSize)
	}

	// Load keyring
	keyring, err := loadKeyring()
	if err != nil {
		return fmt.Errorf("failed to load keyring: %w", err)
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

// loadKeyring loads the GPG keyring from default paths
func loadKeyring() (openpgp.EntityList, error) {
	for _, path := range keyringPaths {
		keyring, err := readKeyringFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}

		return keyring, nil
	}

	return nil, fmt.Errorf("no keyring found in %v", keyringPaths)
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
