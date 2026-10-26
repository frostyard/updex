package updex

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/frostyard/updex/v2/internal/testutil"
	"github.com/frostyard/updex/v2/sysext"
)

// timeoutNetError is a net.Error whose Timeout reports true, as the HTTP
// client's timeout error does.
type timeoutNetError struct{}

func (timeoutNetError) Error() string   { return "i/o timeout" }
func (timeoutNetError) Timeout() bool   { return true }
func (timeoutNetError) Temporary() bool { return true }

// dialError builds the error the net package returns for a failed dial to
// host, with cause as its reason.
func dialError(host string, cause error) error {
	return &net.OpError{Op: "dial", Net: "tcp", Addr: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}, Err: cause}
}

func TestIsUnreachable(t *testing.T) {
	dnsFailure := &net.OpError{
		Op:  "dial",
		Net: "tcp",
		Err: &net.DNSError{Err: "no such host", Name: "unreachable.invalid", IsNotFound: true},
	}
	dialRefused := dialError("127.0.0.1:1", syscall.ECONNREFUSED)
	urlWrappedDial := &url.Error{Op: "Get", URL: "http://unreachable.invalid/SHA256SUMS", Err: dialRefused}
	deeplyWrappedDNS := fmt.Errorf("one or more components failed to update: %w",
		fmt.Errorf("feature/component: %w",
			fmt.Errorf("failed to get available versions: %w",
				fmt.Errorf("failed to fetch manifest: %w", dnsFailure))))

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil error", err: nil, want: false},
		{name: "DNS error", err: &net.DNSError{Err: "no such host", Name: "unreachable.invalid", IsNotFound: true}, want: true},
		{name: "dial OpError with ECONNREFUSED", err: dialRefused, want: true},
		{name: "ENETUNREACH", err: dialError("192.0.2.1:443", syscall.ENETUNREACH), want: true},
		{name: "EHOSTUNREACH", err: dialError("192.0.2.1:443", syscall.EHOSTUNREACH), want: true},
		{name: "ENETDOWN", err: dialError("192.0.2.1:443", syscall.ENETDOWN), want: true},
		{name: "ETIMEDOUT", err: dialError("192.0.2.1:443", &os.SyscallError{Syscall: "connect", Err: syscall.ETIMEDOUT}), want: true},
		{name: "real dial shape with ECONNREFUSED", err: dialError("127.0.0.1:1", &os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED}), want: true},
		{name: "dial OpError with EMFILE (local)", err: dialError("192.0.2.1:443", &os.SyscallError{Syscall: "socket", Err: syscall.EMFILE}), want: false},
		{name: "dial OpError with EACCES (local)", err: dialError("192.0.2.1:443", &os.SyscallError{Syscall: "connect", Err: syscall.EACCES}), want: false},
		{name: "dial OpError with ENOBUFS (local)", err: dialError("192.0.2.1:443", syscall.ENOBUFS), want: false},
		{name: "timeout net.Error", err: timeoutNetError{}, want: true},
		{name: "url.Error wrapping a dial error", err: urlWrappedDial, want: true},
		{name: "several wraps around a DNS error", err: deeplyWrappedDNS, want: true},
		{name: "context.Canceled", err: context.Canceled, want: false},
		{name: "url.Error wrapping context.Canceled", err: &url.Error{Op: "Get", URL: "http://unreachable.invalid/SHA256SUMS", Err: context.Canceled}, want: false},
		{name: "x509 error", err: x509.UnknownAuthorityError{}, want: false},
		{name: "hash mismatch", err: errors.New("hash mismatch: expected a, got b"), want: false},
		{name: "HTTP status error", err: errors.New("manifest fetch failed with status: 404 Not Found"), want: false},
		{name: "signature failure", err: fmt.Errorf("signature verification failed: %w", errors.New("bad signature")), want: false},
		{name: "plain error", err: errors.New("no versions available"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isUnreachable(tt.err); got != tt.want {
				t.Errorf("isUnreachable(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// roundTripFunc adapts a function to http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestSourceTrackerTransportRecordsAnsweredAndAttempted(t *testing.T) {
	response := &http.Response{StatusCode: http.StatusNotFound, Body: http.NoBody}
	transport := sourceTrackerTransport{base: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response, nil
	})}

	ctx, tracker := withSourceTracker(t.Context())
	req := httptest.NewRequest(http.MethodGet, "http://unreachable.invalid/SHA256SUMS", nil).WithContext(ctx)
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	if resp != response {
		t.Errorf("RoundTrip() response = %p, want the base response %p", resp, response)
	}
	if !tracker.attempted.Load() {
		t.Error("attempted = false after a request")
	}
	if !tracker.answered.Load() {
		t.Error("answered = false after a response, also a 404")
	}
}

func TestSourceTrackerTransportRecordsNoAnswer(t *testing.T) {
	baseErr := dialError("unreachable.invalid:80", syscall.ECONNREFUSED)
	transport := sourceTrackerTransport{base: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, baseErr
	})}

	ctx, tracker := withSourceTracker(t.Context())
	req := httptest.NewRequest(http.MethodGet, "http://unreachable.invalid/SHA256SUMS", nil).WithContext(ctx)
	if _, err := transport.RoundTrip(req); !errors.Is(err, baseErr) {
		t.Fatalf("RoundTrip() error = %v, want the base error", err)
	}
	if !tracker.attempted.Load() {
		t.Error("attempted = false after a failed request")
	}
	if tracker.answered.Load() {
		t.Error("answered = true although no response arrived")
	}
}

func TestSourceTrackerTransportWithoutTrackerChangesNothing(t *testing.T) {
	baseErr := errors.New("base failure")
	transport := sourceTrackerTransport{base: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, baseErr
	})}

	req := httptest.NewRequest(http.MethodGet, "http://unreachable.invalid/SHA256SUMS", nil)
	if _, err := transport.RoundTrip(req); !errors.Is(err, baseErr) {
		t.Fatalf("RoundTrip() error = %v, want the base error", err)
	}
}

type idleCloser struct {
	http.RoundTripper
	closed bool
}

func (c *idleCloser) CloseIdleConnections() { c.closed = true }

// The wrapper forwards CloseIdleConnections, so http.Client still reaches the
// connection pool of the transport it wraps.
func TestSourceTrackerTransportForwardsCloseIdleConnections(t *testing.T) {
	base := &idleCloser{RoundTripper: http.DefaultTransport}
	client := &http.Client{Transport: sourceTrackerTransport{base: base}}
	client.CloseIdleConnections()
	if !base.closed {
		t.Error("CloseIdleConnections did not reach the wrapped transport")
	}
	// A base without the method is fine.
	sourceTrackerTransport{base: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, nil })}.CloseIdleConnections()
}

// unreachableHost is the source host the fake transport cannot reach.
const unreachableHost = "unreachable.invalid"

// unreachableTransport fails every request to unreachableHost with a
// non-transient DNS error, so the retry layer does not retry it and the test
// stays fast. Every other request goes to delegate.
type unreachableTransport struct {
	delegate http.RoundTripper
}

func (t unreachableTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host == unreachableHost {
		return nil, &net.OpError{
			Op:  "dial",
			Net: "tcp",
			Err: &net.DNSError{Err: "no such host", Name: unreachableHost, IsNotFound: true},
		}
	}
	return t.delegate.RoundTrip(req)
}

// unreachableSourceURL returns a source URL the fake transport cannot reach,
// unique per name so that sources do not share a manifest cache entry.
func unreachableSourceURL(name string) string {
	return "http://" + unreachableHost + "/" + name
}

// updateFixture prepares a definitions directory with enabled features whose
// transfers point at the given sources, a staging directory, and a client that
// cannot reach unreachableHost.
func updateFixture(t *testing.T, sources map[string]string, configure func(*ClientConfig)) *Client {
	t.Helper()

	configDir := t.TempDir()
	targetDir := t.TempDir()

	for component, source := range sources {
		feature := featureForComponent(component)
		createFeatureTransferFile(t, configDir, component, feature, source)
		createFeatureFile(t, configDir, feature, true)
	}
	updateTransferTargetPath(t, configDir, targetDir)

	cfg := ClientConfig{
		Definitions:  configDir,
		SysextRunner: &sysext.MockRunner{},
		HTTPClient:   &http.Client{Transport: unreachableTransport{delegate: http.DefaultTransport}},
		Paths: RuntimePaths{
			SysextLinkDir:     t.TempDir(),
			SysextHierarchies: []string{t.TempDir()},
			RunExtensionsDir:  t.TempDir(),
			LockPath:          filepath.Join(t.TempDir(), "lock"),
			RollbackDir:       t.TempDir(),
		},
	}
	if configure != nil {
		configure(&cfg)
	}
	return NewClient(cfg)
}

// localDialFailureTransport fails every request with a dial error whose cause
// is local (too many open files), not the network.
type localDialFailureTransport struct{}

func (localDialFailureTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, dialError("192.0.2.1:80", &os.SyscallError{Syscall: "socket", Err: syscall.EMFILE})
}

// A dial that fails for a local reason, on every source, is a host failure,
// not an offline host: the run must not be a skip.
func TestUpdateFeatures_LocalDialFailureIsNotSkipped(t *testing.T) {
	client := updateFixture(t, map[string]string{
		"testext":  unreachableSourceURL("a"),
		"othertex": unreachableSourceURL("b"),
	}, func(cfg *ClientConfig) {
		cfg.HTTPClient = &http.Client{Transport: localDialFailureTransport{}}
	})

	_, err := client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{NoRefresh: true})
	if err == nil {
		t.Fatal("UpdateFeatures() error = nil, want the dial failure")
	}
	if errors.Is(err, ErrSourcesUnreachable) {
		t.Errorf("UpdateFeatures() error = %v, want no ErrSourcesUnreachable for EMFILE", err)
	}
}

// cancelOnlyTransport blocks every request until CancelRequest is called, as
// an old transport that ignores the request context does.
type cancelOnlyTransport struct {
	cancelled chan struct{}
}

func (t cancelOnlyTransport) RoundTrip(*http.Request) (*http.Response, error) {
	<-t.cancelled
	return nil, errors.New("request cancelled")
}

func (t cancelOnlyTransport) CancelRequest(*http.Request) {
	close(t.cancelled)
}

// http.Client cancels a transport that is not an *http.Transport through
// CancelRequest when its Timeout expires. The wrapper forwards it, so such a
// transport still ends at the client timeout.
func TestSourceTrackerTransportForwardsCancelRequest(t *testing.T) {
	base := cancelOnlyTransport{cancelled: make(chan struct{})}
	client := NewClient(ClientConfig{HTTPClient: &http.Client{Transport: base, Timeout: 50 * time.Millisecond}}).httpClient

	done := make(chan error, 1)
	go func() {
		resp, err := client.Get("http://unreachable.invalid/SHA256SUMS")
		if resp != nil {
			_ = resp.Body.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Error("Get() error = nil, want the timeout")
		}
	case <-time.After(5 * time.Second):
		close(base.cancelled)
		t.Fatal("the client timeout did not reach the wrapped transport's CancelRequest")
	}
}

// An *http.Transport is cancelled through the request context, so the wrapper
// does not call its deprecated CancelRequest.
func TestSourceTrackerTransportCancelRequestSkipsHTTPTransport(t *testing.T) {
	sourceTrackerTransport{base: &http.Transport{}}.CancelRequest(httptest.NewRequest(http.MethodGet, "http://example.invalid/", nil))
	sourceTrackerTransport{base: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, nil })}.CancelRequest(httptest.NewRequest(http.MethodGet, "http://example.invalid/", nil))
}

// featureForComponent returns the feature name updateFixture gave a component.
func featureForComponent(component string) string { return "feature-" + component }

// TestUpdateFeatures_AllSourcesUnreachableIsErrSourcesUnreachable covers the
// rule of ADR-0018: two enabled features, both sources unreachable, so the run
// wraps ErrSourcesUnreachable and still reports every transfer error.
func TestUpdateFeatures_AllSourcesUnreachableIsErrSourcesUnreachable(t *testing.T) {
	client := updateFixture(t, map[string]string{
		"testext":  unreachableSourceURL("a"),
		"othertex": unreachableSourceURL("b"),
	}, nil)

	results, err := client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{NoRefresh: true})
	if !errors.Is(err, ErrSourcesUnreachable) {
		t.Fatalf("UpdateFeatures() error = %v, want ErrSourcesUnreachable", err)
	}
	if !strings.Contains(err.Error(), "no update source is reachable") {
		t.Errorf("error text %q does not name the unreachable sources", err)
	}
	for _, component := range []string{"testext", "othertex"} {
		if !strings.Contains(err.Error(), component) {
			t.Errorf("error %q does not keep the transfer error of %s", err, component)
		}
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2 (the results stay unchanged)", len(results))
	}
	for _, fr := range results {
		if len(fr.Results) != 1 || fr.Results[0].Error == "" {
			t.Errorf("feature %s results = %+v, want one failed transfer", fr.Feature, fr.Results)
		}
	}
}

// A feature with one reachable and one unreachable source is not an all-
// unreachable run: the reachable source answered, and the other transfer of
// the feature failed only as collateral (errNotLinked).
func TestUpdateFeatures_OneReachableSourceIsNotSkipped(t *testing.T) {
	extContent := []byte("fake extension content")
	server := testutil.NewTestServer(t, testutil.TestServerFiles{
		Files:   map[string]string{"testext_1.0.0.raw": hashContent(extContent)},
		Content: map[string][]byte{"testext_1.0.0.raw": extContent},
	})
	defer server.Close()

	configDir := t.TempDir()
	targetDir := t.TempDir()
	createFeatureTransferFile(t, configDir, "testext", "testfeature", server.URL)
	createFeatureTransferFile(t, configDir, "othertex", "testfeature", unreachableSourceURL("mixed"))
	createFeatureFile(t, configDir, "testfeature", true)
	updateTransferTargetPath(t, configDir, targetDir)

	client := NewClient(ClientConfig{
		Definitions:  configDir,
		SysextRunner: &sysext.MockRunner{},
		HTTPClient:   &http.Client{Transport: unreachableTransport{delegate: http.DefaultTransport}},
		Paths: RuntimePaths{
			SysextLinkDir:     t.TempDir(),
			SysextHierarchies: []string{t.TempDir()},
			RunExtensionsDir:  t.TempDir(),
			LockPath:          filepath.Join(t.TempDir(), "lock"),
			RollbackDir:       t.TempDir(),
		},
	})

	_, err := client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{NoRefresh: true})
	if err == nil {
		t.Fatal("UpdateFeatures() error = nil, want the failed transfer")
	}
	if errors.Is(err, ErrSourcesUnreachable) {
		t.Errorf("UpdateFeatures() error = %v: a reachable source answered, so this is not an offline run", err)
	}
	if !strings.Contains(err.Error(), "not linked: another transfer of feature testfeature failed") {
		t.Errorf("error %q does not report the collateral failure of the second transfer", err)
	}
}

// A source that answers with an HTTP error status is reachable: the run is a
// failure, not a skip, also when the other source is unreachable.
func TestUpdateFeatures_AnsweringErrorStatusIsNotSkipped(t *testing.T) {
	server := testutil.NewErrorServer(t, http.StatusNotFound)
	defer server.Close()

	client := updateFixture(t, map[string]string{
		"testext":  server.URL,
		"othertex": unreachableSourceURL("status"),
	}, nil)

	_, err := client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{NoRefresh: true})
	if err == nil {
		t.Fatal("UpdateFeatures() error = nil, want the 404 failure")
	}
	if errors.Is(err, ErrSourcesUnreachable) {
		t.Errorf("UpdateFeatures() error = %v: the 404 source answered, so this is not an offline run", err)
	}
}

// A verification failure of a reachable source is a failure, not a skip: the
// source answered, so the run must keep exiting non-zero.
func TestUpdateFeatures_VerificationFailureIsNotSkipped(t *testing.T) {
	extContent := []byte("fake extension content")
	otherContent := []byte("different content, so the hash does not match")

	tests := []struct {
		name        string
		files       testutil.TestServerFiles
		verify      bool
		wantInError string
	}{
		{
			name: "hash mismatch",
			files: testutil.TestServerFiles{
				Files:   map[string]string{"testext_1.0.0.raw": hashContent(otherContent)},
				Content: map[string][]byte{"testext_1.0.0.raw": extContent},
			},
			wantInError: "hash mismatch",
		},
		{
			name: "missing signature",
			files: testutil.TestServerFiles{
				Files:   map[string]string{"testext_1.0.0.raw": hashContent(extContent)},
				Content: map[string][]byte{"testext_1.0.0.raw": extContent},
			},
			verify:      true,
			wantInError: "signature",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := testutil.NewTestServer(t, tt.files)
			defer server.Close()

			client := updateFixture(t, map[string]string{"testext": server.URL}, func(cfg *ClientConfig) {
				cfg.Verify = tt.verify
			})

			_, err := client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{NoRefresh: true})
			if err == nil {
				t.Fatalf("UpdateFeatures() error = nil, want the %s failure", tt.name)
			}
			if errors.Is(err, ErrSourcesUnreachable) {
				t.Errorf("UpdateFeatures() error = %v: the source answered, so this is not an offline run", err)
			}
			if !strings.Contains(err.Error(), tt.wantInError) {
				t.Errorf("error %q does not name %q", err, tt.wantInError)
			}
		})
	}
}

// A client whose transport is replaced after construction records no attempt,
// so the rule fails closed: such a run is never a skip.
func TestUpdateFeatures_ReplacedTransportIsNotSkipped(t *testing.T) {
	client := updateFixture(t, map[string]string{"testext": unreachableSourceURL("replaced")}, nil)
	// Remove the tracker wrapper, as code that replaces the transport after
	// NewClient does.
	client.httpClient.Transport = unreachableTransport{delegate: http.DefaultTransport}

	_, err := client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{NoRefresh: true})
	if err == nil {
		t.Fatal("UpdateFeatures() error = nil, want the unreachable source's error")
	}
	if errors.Is(err, ErrSourcesUnreachable) {
		t.Errorf("UpdateFeatures() error = %v: without the tracker the rule must fail closed", err)
	}
}

// A cancelled run is never a skip, even when every source is unreachable.
func TestUpdateFeatures_CancelledRunIsNotSkipped(t *testing.T) {
	client := updateFixture(t, map[string]string{
		"testext":  unreachableSourceURL("cancelled-a"),
		"othertex": unreachableSourceURL("cancelled-b"),
	}, nil)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := client.UpdateFeatures(ctx, UpdateFeaturesOptions{NoRefresh: true})
	if err == nil {
		t.Fatal("UpdateFeatures() error = nil, want the cancelled run's error")
	}
	if errors.Is(err, ErrSourcesUnreachable) {
		t.Errorf("UpdateFeatures() error = %v: a cancelled run is never a skip", err)
	}
}

// A failing refresh keeps the run a failure, even when no source was
// reachable.
func TestUpdateFeatures_RefreshFailureIsNotSkipped(t *testing.T) {
	runner := &sysext.MockRunner{RefreshErr: errors.New("refresh failed")}
	client := updateFixture(t, map[string]string{"testext": unreachableSourceURL("refresh")}, func(cfg *ClientConfig) {
		cfg.SysextRunner = runner
	})

	_, err := client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{})
	if err == nil {
		t.Fatal("UpdateFeatures() error = nil, want the refresh failure")
	}
	if !runner.RefreshCalled {
		t.Error("Refresh() was not called")
	}
	if errors.Is(err, ErrSourcesUnreachable) {
		t.Errorf("UpdateFeatures() error = %v: a refresh failure is never a skip", err)
	}
}

// A run with no errors keeps returning a nil error.
func TestUpdateFeatures_ReachableSourcesReturnNoError(t *testing.T) {
	extContent := []byte("fake extension content")
	server := testutil.NewTestServer(t, testutil.TestServerFiles{
		Files:   map[string]string{"testext_1.0.0.raw": hashContent(extContent)},
		Content: map[string][]byte{"testext_1.0.0.raw": extContent},
	})
	defer server.Close()

	client := updateFixture(t, map[string]string{"testext": server.URL}, nil)
	results, err := client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{NoRefresh: true})
	if err != nil {
		t.Fatalf("UpdateFeatures() error = %v, want nil", err)
	}
	if len(results) != 1 || len(results[0].Results) != 1 || !results[0].Results[0].Downloaded {
		t.Fatalf("results = %+v, want the downloaded transfer", results)
	}
}

// TestUpdateFeatures_RefusedLoopbackPortIsUnreachable runs the rule against a
// real network error: a closed loopback port refuses the connection. It is the
// only test that pays the retry backoff (3 attempts, 1s and 2s).
func TestUpdateFeatures_RefusedLoopbackPortIsUnreachable(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	source := "http://" + listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	client := updateFixture(t, map[string]string{"testext": source}, nil)
	_, err = client.UpdateFeatures(t.Context(), UpdateFeaturesOptions{NoRefresh: true})
	if !errors.Is(err, ErrSourcesUnreachable) {
		t.Fatalf("UpdateFeatures() error = %v, want ErrSourcesUnreachable", err)
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("error %q does not keep the dial error", err)
	}
}

// A mocked transport and the tracker must not race under the race detector:
// the tracker is per operation, and its fields are atomics.
func TestSourceTrackerIsConcurrencySafe(t *testing.T) {
	ctx, tracker := withSourceTracker(t.Context())
	transport := sourceTrackerTransport{base: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
	})}

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			req := httptest.NewRequest(http.MethodGet, "http://unreachable.invalid/SHA256SUMS", nil).WithContext(ctx)
			if _, err := transport.RoundTrip(req); err != nil {
				t.Errorf("RoundTrip() error = %v", err)
			}
		})
	}
	wg.Wait()
	if !tracker.attempted.Load() || !tracker.answered.Load() {
		t.Errorf("tracker = %+v, want attempted and answered", tracker)
	}
}
