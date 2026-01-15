package manifest

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"

	// TODO: Migrate to github.com/ProtonMail/go-crypto/openpgp - this package is deprecated
	// but still functional for our GPG verification needs.
	"golang.org/x/crypto/openpgp" //nolint:staticcheck
)

// Default keyring paths (matching systemd-sysupdate)
var keyringPaths = []string{
	"/etc/systemd/import-pubring.gpg",
	"/usr/lib/systemd/import-pubring.gpg",
}

// verifySignature verifies the GPG signature of the manifest content
func verifySignature(client *http.Client, sigURL string, content []byte) error {
	// Download signature
	resp, err := client.Get(sigURL)
	if err != nil {
		return fmt.Errorf("failed to fetch signature: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("signature fetch failed with status: %s", resp.Status)
	}

	sigData, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read signature: %w", err)
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
	)
	if err != nil {
		return fmt.Errorf("invalid signature: %w", err)
	}

	return nil
}

// loadKeyring loads the GPG keyring from default paths
func loadKeyring() (openpgp.EntityList, error) {
	for _, path := range keyringPaths {
		f, err := os.Open(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
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

	return nil, fmt.Errorf("no keyring found in %v", keyringPaths)
}

// SetKeyringPaths allows overriding the default keyring search paths
func SetKeyringPaths(paths []string) {
	keyringPaths = paths
}
