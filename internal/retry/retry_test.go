package retry

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/url"
	"syscall"
	"testing"
	"time"
)

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func TestDoSucceedsAfterTransientFailures(t *testing.T) {
	var calls int
	var notified []int
	err := Do(t.Context(), Config{MaxAttempts: 3, BaseDelay: time.Millisecond}, func(attempt, maxAttempts int, reason error) {
		if maxAttempts != 3 {
			t.Errorf("maxAttempts = %d, want 3", maxAttempts)
		}
		if !IsTransient(reason) {
			t.Errorf("notify reason is not transient: %v", reason)
		}
		notified = append(notified, attempt)
	}, func() error {
		calls++
		if calls < 3 {
			return Transient(fmt.Errorf("temporary failure %d", calls))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
	if fmt.Sprint(notified) != "[2 3]" {
		t.Fatalf("notified attempts = %v, want [2 3]", notified)
	}
}

func TestDoStopsAfterMaxAttempts(t *testing.T) {
	var calls int
	var notifications int
	err := Do(t.Context(), Config{MaxAttempts: 3, BaseDelay: time.Millisecond}, func(int, int, error) {
		notifications++
	}, func() error {
		calls++
		return Transient(errors.New("temporary failure"))
	})
	if err == nil {
		t.Fatal("Do() error = nil, want error")
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
	if notifications != 2 {
		t.Fatalf("notifications = %d, want 2", notifications)
	}
}

func TestDoStopsOnNonTransientError(t *testing.T) {
	var calls int
	var notifications int
	wantErr := errors.New("permanent failure")
	err := Do(t.Context(), Config{MaxAttempts: 3, BaseDelay: time.Millisecond}, func(int, int, error) {
		notifications++
	}, func() error {
		calls++
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Do() error = %v, want %v", err, wantErr)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
	if notifications != 0 {
		t.Fatalf("notifications = %d, want 0", notifications)
	}
}

func TestDoContextCanceledDuringBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := Do(ctx, Config{MaxAttempts: 3, BaseDelay: time.Hour}, nil, func() error {
		return Transient(errors.New("temporary failure"))
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Do() error = %v, want context.Canceled", err)
	}
}

func TestTransientIfNetwork(t *testing.T) {
	certErr := &tls.CertificateVerificationError{
		UnverifiedCertificates: []*x509.Certificate{{}},
		Err:                    errors.New("bad certificate"),
	}

	tests := []struct {
		name      string
		err       error
		transient bool
	}{
		{name: "timeout", err: timeoutErr{}, transient: true},
		{name: "connection reset", err: syscall.ECONNRESET, transient: true},
		{name: "connection refused", err: syscall.ECONNREFUSED, transient: true},
		{name: "unexpected eof", err: io.ErrUnexpectedEOF, transient: true},
		{name: "certificate verification", err: certErr, transient: false},
		{name: "unsupported protocol", err: &url.Error{Op: "Get", URL: "bogus://example", Err: errors.New("unsupported protocol scheme")}, transient: false},
		{name: "plain error", err: errors.New("plain error"), transient: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := TransientIfNetwork(tt.err)
			if IsTransient(err) != tt.transient {
				t.Fatalf("IsTransient(%v) = %v, want %v", err, IsTransient(err), tt.transient)
			}
		})
	}
}
