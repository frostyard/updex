package updex

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync/atomic"
	"syscall"
)

// ErrSourcesUnreachable reports that a run failed only because no update
// source answered. UpdateFeatures wraps it when every failed transfer failed
// with a network error and no HTTP request of the run got a response, so the
// caller can treat an offline host by itself (the scheduled timer skips the
// run instead of failing the unit, ADR-0018). Every other failure, including a
// source that answered with an error status or a verification failure, keeps
// the error of today.
var ErrSourcesUnreachable = errors.New("no update source is reachable")

// sourceTracker records what the HTTP requests of one operation saw: whether
// any request was attempted at all, and whether any request got an HTTP
// response, whatever its status. A transport wrapper reads it from the
// request context (see sourceTrackerTransport).
type sourceTracker struct {
	attempted atomic.Bool
	answered  atomic.Bool
}

// sourceTrackerKey is the context key of a run's sourceTracker. It is an
// unexported struct type, so no other package can collide with it.
type sourceTrackerKey struct{}

// withSourceTracker returns ctx carrying a new sourceTracker, and the tracker.
func withSourceTracker(ctx context.Context) (context.Context, *sourceTracker) {
	tracker := &sourceTracker{}
	return context.WithValue(ctx, sourceTrackerKey{}, tracker), tracker
}

// sourceTrackerFrom returns the tracker ctx carries, or nil when it carries
// none (a request outside UpdateFeatures).
func sourceTrackerFrom(ctx context.Context) *sourceTracker {
	tracker, _ := ctx.Value(sourceTrackerKey{}).(*sourceTracker)
	return tracker
}

// sourceTrackerTransport records each request in the sourceTracker of the
// request context, then delegates to the wrapped transport. A request whose
// context carries no tracker changes nothing. The HTTP client keeps its
// Timeout, CheckRedirect and Jar; only the transport is wrapped.
type sourceTrackerTransport struct {
	base http.RoundTripper
}

// RoundTrip implements http.RoundTripper.
func (t sourceTrackerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	tracker := sourceTrackerFrom(req.Context())
	if tracker == nil {
		return t.base.RoundTrip(req)
	}
	tracker.attempted.Store(true)
	resp, err := t.base.RoundTrip(req)
	if resp != nil {
		// Any response counts, also a 404 or a 500: the source answered.
		tracker.answered.Store(true)
	}
	return resp, err
}

// CloseIdleConnections forwards to the wrapped transport, so that
// http.Client.CloseIdleConnections still reaches its connection pool.
func (t sourceTrackerTransport) CloseIdleConnections() {
	if closer, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

// CancelRequest forwards the legacy cancellation that http.Client still uses
// when its Timeout expires and the transport is not an *http.Transport. An
// *http.Transport is cancelled through the request context, as it was before
// the wrapper, so the call is not forwarded to it.
func (t sourceTrackerTransport) CancelRequest(req *http.Request) {
	if _, ok := t.base.(*http.Transport); ok {
		return
	}
	if canceler, ok := t.base.(interface{ CancelRequest(*http.Request) }); ok {
		canceler.CancelRequest(req)
	}
}

// isUnreachable reports whether err means that a source could not be reached
// at all: a DNS failure, a connection refused, an unreachable host or
// network, a network that is down, or a timeout that expired before a
// response arrived. A dial error counts only through one of these causes, so
// a local failure such as EMFILE or EACCES is not unreachable. TLS and x509
// errors, HTTP status errors, verification failures, disk errors and a
// cancelled context are not unreachable either: the source answered, or the
// host or the run, not the network, ended the request.
func isUnreachable(err error) bool {
	if err == nil {
		return false
	}
	// A cancelled run is never a skip, whatever the request error wraps.
	if errors.Is(err, context.Canceled) {
		return false
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	// A failed dial counts only by its cause: a dial can also fail for a
	// local reason (EMFILE, EACCES, ENOBUFS), and that must fail the run.
	switch {
	case errors.Is(err, syscall.ECONNREFUSED),
		errors.Is(err, syscall.EHOSTUNREACH),
		errors.Is(err, syscall.ENETUNREACH),
		errors.Is(err, syscall.ENETDOWN),
		errors.Is(err, syscall.ETIMEDOUT):
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		// The client timeout expired while it waited for a response; rule 2
		// (no response arrived) is checked separately.
		return true
	}
	return false
}

// everySourceUnreachable reports whether a failed run failed only because no
// source answered: there is at least one transfer error, every one of them is
// unreachable, no HTTP request of the run got a response, the run's own
// context is still live, and no refresh failed. hardErrs are the run's
// transfer errors except the collateral errNotLinked that the other transfers
// of a failed feature report; a feature always reports at least one error of
// its own, so a non-empty hardErrs is rule 1.
func everySourceUnreachable(ctx context.Context, tracker *sourceTracker, hardErrs []error, refreshErr error) bool {
	if len(hardErrs) == 0 || tracker == nil {
		return false
	}
	// A request was attempted and none got a response. A client whose
	// transport was replaced after NewClient records no attempt, so it can
	// never turn a failure into a skip (fail closed).
	if !tracker.attempted.Load() || tracker.answered.Load() {
		return false
	}
	if ctx.Err() != nil || refreshErr != nil {
		return false
	}
	for _, err := range hardErrs {
		if !isUnreachable(err) {
			return false
		}
	}
	return true
}
