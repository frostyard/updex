package retry

import (
	"context"
	"errors"
	"io"
	"net"
	"syscall"
	"time"
)

// Config controls bounded retry behavior.
type Config struct {
	MaxAttempts int
	BaseDelay   time.Duration
}

// DefaultConfig retries an operation up to three total attempts with a
// one-second exponential backoff.
var DefaultConfig = Config{MaxAttempts: 3, BaseDelay: time.Second}

// Notify is called before sleeping for a retry attempt.
type Notify func(attempt, maxAttempts int, reason error)

type transientError struct {
	err error
}

func (e transientError) Error() string {
	return e.err.Error()
}

func (e transientError) Unwrap() error {
	return e.err
}

// Transient marks err as retryable.
func Transient(err error) error {
	if err == nil {
		return nil
	}
	if IsTransient(err) {
		return err
	}
	return transientError{err: err}
}

// IsTransient reports whether err has been marked retryable.
func IsTransient(err error) bool {
	var transient transientError
	return errors.As(err, &transient)
}

// TransientIfNetwork marks err retryable only when it looks like a transient
// network or response-body read failure.
func TransientIfNetwork(err error) error {
	if err == nil {
		return nil
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return Transient(err)
	}

	switch {
	case errors.Is(err, syscall.ECONNRESET),
		errors.Is(err, syscall.ECONNREFUSED),
		errors.Is(err, syscall.ECONNABORTED),
		errors.Is(err, syscall.EPIPE),
		errors.Is(err, io.EOF),
		errors.Is(err, io.ErrUnexpectedEOF):
		return Transient(err)
	default:
		return err
	}
}

// Do runs fn until it succeeds, returns a non-transient error, exhausts the
// configured attempts, or ctx is canceled while waiting for the next attempt.
func Do(ctx context.Context, cfg Config, notify Notify, fn func() error) error {
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = DefaultConfig.MaxAttempts
	}
	if cfg.BaseDelay <= 0 {
		cfg.BaseDelay = DefaultConfig.BaseDelay
	}

	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		err := fn()
		if err == nil {
			return nil
		}
		if !IsTransient(err) || attempt == cfg.MaxAttempts {
			return err
		}

		if notify != nil {
			notify(attempt+1, cfg.MaxAttempts, err)
		}

		delay := cfg.BaseDelay * (1 << (attempt - 1))
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}

	return nil
}
