package manifest

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
)

func TestVerifySignature(t *testing.T) {
	content := []byte("0123456789abcdef  image.raw\n")
	entity := newTestEntity(t)

	var signature bytes.Buffer
	if err := openpgp.DetachSign(&signature, entity, bytes.NewReader(content), nil); err != nil {
		t.Fatalf("DetachSign() error = %v", err)
	}

	keyringPath := writeTestKeyring(t, entity, true)
	setTestKeyringPaths(t, keyringPath)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(signature.Bytes())
	}))
	defer server.Close()

	if err := verifySignature(t.Context(), server.Client(), server.URL, content); err != nil {
		t.Fatalf("verifySignature() error = %v", err)
	}

	err := verifySignature(t.Context(), server.Client(), server.URL, []byte("tampered manifest"))
	if err == nil || !strings.Contains(err.Error(), "invalid signature") {
		t.Fatalf("verifySignature() error = %v, want invalid signature", err)
	}
}

func TestVerifySignatureMissingKeyring(t *testing.T) {
	setTestKeyringPaths(t, filepath.Join(t.TempDir(), "missing.gpg"))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("signature"))
	}))
	defer server.Close()

	err := verifySignature(t.Context(), server.Client(), server.URL, []byte("manifest"))
	if err == nil || !strings.Contains(err.Error(), "failed to load keyring") {
		t.Fatalf("verifySignature() error = %v, want missing keyring error", err)
	}
}

func TestVerifySignatureHTTPFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	err := verifySignature(t.Context(), server.Client(), server.URL, []byte("manifest"))
	if err == nil || !strings.Contains(err.Error(), "503 Service Unavailable") {
		t.Fatalf("verifySignature() error = %v, want HTTP status error", err)
	}
}

func TestVerifySignatureCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := verifySignature(ctx, http.DefaultClient, "http://example.invalid/signature", []byte("manifest"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("verifySignature() error = %v, want context.Canceled", err)
	}
}

func TestReadKeyringFile(t *testing.T) {
	entity := newTestEntity(t)

	for _, armored := range []bool{false, true} {
		name := "binary"
		if armored {
			name = "armored"
		}

		t.Run(name, func(t *testing.T) {
			keyring, err := readKeyringFile(writeTestKeyring(t, entity, armored))
			if err != nil {
				t.Fatalf("readKeyringFile() error = %v", err)
			}
			if len(keyring) != 1 {
				t.Fatalf("len(keyring) = %d, want 1", len(keyring))
			}
		})
	}
}

func TestLoadKeyringSkipsMissingPath(t *testing.T) {
	entity := newTestEntity(t)
	keyringPath := writeTestKeyring(t, entity, false)
	setTestKeyringPaths(t, filepath.Join(t.TempDir(), "missing.gpg"), keyringPath)

	keyring, err := loadKeyring()
	if err != nil {
		t.Fatalf("loadKeyring() error = %v", err)
	}
	if len(keyring) != 1 {
		t.Fatalf("len(keyring) = %d, want 1", len(keyring))
	}
}

func newTestEntity(t *testing.T) *openpgp.Entity {
	t.Helper()

	entity, err := openpgp.NewEntity("updex test", "", "test@example.com", nil)
	if err != nil {
		t.Fatalf("NewEntity() error = %v", err)
	}
	return entity
}

func writeTestKeyring(t *testing.T, entity *openpgp.Entity, armored bool) string {
	t.Helper()

	var keyring bytes.Buffer
	var writer io.Writer = &keyring
	var armorWriter io.WriteCloser

	if armored {
		var err error
		armorWriter, err = armor.Encode(&keyring, openpgp.PublicKeyType, nil)
		if err != nil {
			t.Fatalf("armor.Encode() error = %v", err)
		}
		writer = armorWriter
	}

	if err := entity.Serialize(writer); err != nil {
		t.Fatalf("Serialize() error = %v", err)
	}
	if armorWriter != nil {
		if err := armorWriter.Close(); err != nil {
			t.Fatalf("armor writer Close() error = %v", err)
		}
	}

	path := filepath.Join(t.TempDir(), "keyring.gpg")
	if err := os.WriteFile(path, keyring.Bytes(), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}

func setTestKeyringPaths(t *testing.T, paths ...string) {
	t.Helper()

	original := keyringPaths
	keyringPaths = paths
	t.Cleanup(func() {
		keyringPaths = original
	})
}
