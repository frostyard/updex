package manifest

import (
	"bytes"
	"context"
	"crypto"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"

	"github.com/frostyard/updex/internal/retry"
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

	if err := verifySignature(t.Context(), server.Client(), server.URL, content, singleAttempt()); err != nil {
		t.Fatalf("verifySignature() error = %v", err)
	}

	err := verifySignature(t.Context(), server.Client(), server.URL, []byte("tampered manifest"), singleAttempt())
	if err == nil || !strings.Contains(err.Error(), "invalid signature") {
		t.Fatalf("verifySignature() error = %v, want invalid signature", err)
	}
}

func TestVerifySignatureRejectsInsecureMessageHash(t *testing.T) {
	content := []byte("0123456789abcdef  image.raw\n")
	entity := newTestEntity(t)
	setTestKeyringPaths(t, writeTestKeyring(t, entity, true))
	randomizeSignature := false

	tests := []struct {
		name      string
		config    *packet.Config
		wantError string
	}{
		{
			name: "SHA-1 rejected",
			config: &packet.Config{
				DefaultHash:                           crypto.SHA1,
				RejectMessageHashAlgorithms:           map[crypto.Hash]bool{},
				NonDeterministicSignaturesViaNotation: &randomizeSignature,
			},
			wantError: "rejected signature message hash algorithm: SHA-1",
		},
		{
			name: "SHA-256 accepted",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var signature bytes.Buffer
			if err := detachSignForHashPolicyTest(&signature, entity, bytes.NewReader(content), tt.config); err != nil {
				t.Fatalf("DetachSign() error = %v", err)
			}

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write(signature.Bytes())
			}))
			defer server.Close()

			err := verifySignature(t.Context(), server.Client(), server.URL, content, singleAttempt())
			if tt.wantError == "" {
				if err != nil {
					t.Fatalf("verifySignature() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("verifySignature() error = %v, want %q", err, tt.wantError)
			}
		})
	}
}

// TestVerifySignatureRejectsInsecureHashBehindDecoyPacket pins the digest
// floor against a multi-packet detached signature: a decoy SHA-256 packet
// from an entity absent from the keyring, followed by a genuine SHA-1 packet
// from the trusted entity. Verification skips the decoy (unknown issuer) and
// would otherwise accept the SHA-1 packet, so checking only the first packet
// is a complete bypass of the policy.
func TestVerifySignatureRejectsInsecureHashBehindDecoyPacket(t *testing.T) {
	content := []byte("0123456789abcdef  image.raw\n")
	trusted := newTestEntity(t)
	untrusted := newTestEntity(t)
	setTestKeyringPaths(t, writeTestKeyring(t, trusted, true))
	randomizeSignature := false

	var signature bytes.Buffer
	if err := openpgp.DetachSign(&signature, untrusted, bytes.NewReader(content), nil); err != nil {
		t.Fatalf("DetachSign(decoy) error = %v", err)
	}
	sha1Config := &packet.Config{
		DefaultHash:                           crypto.SHA1,
		RejectMessageHashAlgorithms:           map[crypto.Hash]bool{},
		NonDeterministicSignaturesViaNotation: &randomizeSignature,
	}
	if err := detachSignForHashPolicyTest(&signature, trusted, bytes.NewReader(content), sha1Config); err != nil {
		t.Fatalf("DetachSign(trusted SHA-1) error = %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(signature.Bytes())
	}))
	defer server.Close()

	err := verifySignature(t.Context(), server.Client(), server.URL, content, singleAttempt())
	if err == nil || !strings.Contains(err.Error(), "rejected signature message hash algorithm: SHA-1") {
		t.Fatalf("verifySignature() error = %v, want rejected SHA-1 message hash", err)
	}
}

func detachSignForHashPolicyTest(w io.Writer, entity *openpgp.Entity, content io.Reader, config *packet.Config) error {
	if config == nil || config.DefaultHash != crypto.SHA1 {
		return openpgp.DetachSign(w, entity, content, config)
	}

	issuerKeyID := entity.PrimaryKey.KeyId
	signature := &packet.Signature{
		Version:           entity.PrimaryKey.Version,
		SigType:           packet.SigTypeBinary,
		PubKeyAlgo:        entity.PrimaryKey.PubKeyAlgo,
		Hash:              crypto.SHA1,
		CreationTime:      time.Now(),
		IssuerKeyId:       &issuerKeyID,
		IssuerFingerprint: entity.PrimaryKey.Fingerprint,
	}
	hash, err := signature.PrepareSign(config)
	if err != nil {
		return err
	}
	if _, err := io.Copy(hash, content); err != nil {
		return err
	}
	if err := signature.Sign(hash, entity.PrivateKey, config); err != nil {
		return err
	}
	return signature.Serialize(w)
}

// singleAttempt returns retry settings that never retry, so tests of
// non-retry behavior fail fast instead of sleeping through backoff.
func singleAttempt() retrySettings {
	return retrySettings{cfg: retry.Config{MaxAttempts: 1, BaseDelay: time.Millisecond}}
}

// signedManifest returns manifest content, its detached signature, and
// installs a keyring that trusts the signing entity for the test.
func signedManifest(t *testing.T) (content, signature []byte) {
	t.Helper()

	content = []byte(validManifestContent())
	entity := newTestEntity(t)

	var sig bytes.Buffer
	if err := openpgp.DetachSign(&sig, entity, bytes.NewReader(content), nil); err != nil {
		t.Fatalf("DetachSign() error = %v", err)
	}

	setTestKeyringPaths(t, writeTestKeyring(t, entity, true))
	return content, sig.Bytes()
}

// signatureServer serves content at /SHA256SUMS and delegates /SHA256SUMS.gpg
// to sig, counting signature requests.
func signatureServer(t *testing.T, content []byte, sig func(w http.ResponseWriter, hit int32)) (*httptest.Server, *atomic.Int32) {
	t.Helper()

	var sigRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/SHA256SUMS":
			_, _ = w.Write(content)
		case "/SHA256SUMS.gpg":
			sig(w, sigRequests.Add(1))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server, &sigRequests
}

func TestFetchRetriesSignatureServerErrorThenSucceeds(t *testing.T) {
	content, signature := signedManifest(t)
	server, sigRequests := signatureServer(t, content, func(w http.ResponseWriter, hit int32) {
		if hit == 1 {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write(signature)
	})

	type notification struct {
		attempt, maxAttempts int
		reason               error
	}
	var notifications []notification
	notify := func(attempt, maxAttempts int, reason error) {
		notifications = append(notifications, notification{attempt, maxAttempts, reason})
	}

	m, err := Fetch(t.Context(), server.Client(), server.URL, true, WithRetryConfig(3, time.Millisecond), WithRetryNotify(notify))
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if got := m.Files["file.raw"]; got != testManifestHash() {
		t.Fatalf("Files[file.raw] = %q, want %q", got, testManifestHash())
	}
	if sigRequests.Load() != 2 {
		t.Fatalf("signature requests = %d, want 2", sigRequests.Load())
	}
	if len(notifications) != 1 {
		t.Fatalf("notifications = %d, want 1", len(notifications))
	}
	if n := notifications[0]; n.attempt != 2 || n.maxAttempts != 3 {
		t.Fatalf("notification = attempt %d of %d, want 2 of 3", n.attempt, n.maxAttempts)
	}
	if !strings.Contains(notifications[0].reason.Error(), "signature fetch failed with status: 503") {
		t.Fatalf("notification reason = %v, want signature 503", notifications[0].reason)
	}
}

func TestFetchDoesNotRetrySignatureNotFound(t *testing.T) {
	content, _ := signedManifest(t)
	server, sigRequests := signatureServer(t, content, func(w http.ResponseWriter, _ int32) {
		http.Error(w, "missing", http.StatusNotFound)
	})

	_, err := Fetch(t.Context(), server.Client(), server.URL, true, WithRetryConfig(3, time.Millisecond))
	if err == nil || !strings.Contains(err.Error(), "signature verification failed: signature fetch failed with status: 404") {
		t.Fatalf("Fetch() error = %v, want signature 404", err)
	}
	if sigRequests.Load() != 1 {
		t.Fatalf("signature requests = %d, want 1", sigRequests.Load())
	}
}

func TestFetchDoesNotRetryInvalidSignature(t *testing.T) {
	content, _ := signedManifest(t)
	server, sigRequests := signatureServer(t, content, func(w http.ResponseWriter, _ int32) {
		_, _ = w.Write([]byte("not a signature"))
	})

	_, err := Fetch(t.Context(), server.Client(), server.URL, true, WithRetryConfig(3, time.Millisecond))
	if err == nil || !strings.Contains(err.Error(), "signature verification failed: invalid signature") {
		t.Fatalf("Fetch() error = %v, want invalid signature", err)
	}
	if sigRequests.Load() != 1 {
		t.Fatalf("signature requests = %d, want 1", sigRequests.Load())
	}
}

func TestFetchSignatureRetriesExhausted(t *testing.T) {
	content, _ := signedManifest(t)
	server, sigRequests := signatureServer(t, content, func(w http.ResponseWriter, _ int32) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	})

	_, err := Fetch(t.Context(), server.Client(), server.URL, true, WithRetryConfig(3, time.Millisecond))
	if err == nil || !strings.Contains(err.Error(), "signature fetch failed with status: 503") {
		t.Fatalf("Fetch() error = %v, want signature 503", err)
	}
	if sigRequests.Load() != 3 {
		t.Fatalf("signature requests = %d, want 3", sigRequests.Load())
	}
}

func TestVerifySignatureMissingKeyring(t *testing.T) {
	setTestKeyringPaths(t, filepath.Join(t.TempDir(), "missing.gpg"))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("signature"))
	}))
	defer server.Close()

	err := verifySignature(t.Context(), server.Client(), server.URL, []byte("manifest"), singleAttempt())
	if err == nil || !strings.Contains(err.Error(), "failed to load keyring") {
		t.Fatalf("verifySignature() error = %v, want missing keyring error", err)
	}
}

func TestVerifySignatureHTTPFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	err := verifySignature(t.Context(), server.Client(), server.URL, []byte("manifest"), singleAttempt())
	if err == nil || !strings.Contains(err.Error(), "503 Service Unavailable") {
		t.Fatalf("verifySignature() error = %v, want HTTP status error", err)
	}
}

func TestVerifySignatureCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := verifySignature(ctx, http.DefaultClient, "http://example.invalid/signature", []byte("manifest"), singleAttempt())
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

	original := KeyringPaths
	KeyringPaths = paths
	t.Cleanup(func() {
		KeyringPaths = original
	})
}

// TestFetchRecordsVerified pins Manifest.Verified: false when the caller did
// not request verification (even though a valid signature is available), and
// true only after CheckDetachedSignature succeeded. Callers that cache
// manifests rely on this so a transfer requiring verification never consumes
// a manifest that was fetched without it.
func TestFetchRecordsVerified(t *testing.T) {
	content, signature := signedManifest(t)
	server, sigRequests := signatureServer(t, content, func(w http.ResponseWriter, _ int32) {
		_, _ = w.Write(signature)
	})

	unverified, err := Fetch(t.Context(), server.Client(), server.URL, false, WithRetryConfig(1, time.Millisecond))
	if err != nil {
		t.Fatalf("Fetch(verify=false) error = %v", err)
	}
	if unverified.Verified {
		t.Fatal("Fetch(verify=false) must not report Verified=true")
	}
	if sigRequests.Load() != 0 {
		t.Fatalf("verify=false fetched the signature %d time(s), want 0", sigRequests.Load())
	}

	verified, err := Fetch(t.Context(), server.Client(), server.URL, true, WithRetryConfig(1, time.Millisecond))
	if err != nil {
		t.Fatalf("Fetch(verify=true) error = %v", err)
	}
	if !verified.Verified {
		t.Fatal("Fetch(verify=true) with a valid signature must report Verified=true")
	}
	if sigRequests.Load() != 1 {
		t.Fatalf("verify=true fetched the signature %d time(s), want 1", sigRequests.Load())
	}
}
