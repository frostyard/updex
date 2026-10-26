package updex

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/frostyard/updex/v2/catalog"
)

func TestDefaultHTTPClientRejectsRedirectDowngrade(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("catalog conf"))
	}))
	defer plain.Close()

	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+r.URL.Path, http.StatusFound)
	}))
	defer secure.Close()

	client := NewClient(ClientConfig{})
	client.httpClient.Transport = secure.Client().Transport

	_, err := catalog.FetchConf(t.Context(), client.httpClient, catalog.Repo{
		Name:    "test",
		SiteURL: secure.URL,
	}, "zoxide")
	if err == nil {
		t.Fatal("FetchConf() error = nil")
	}
	if !strings.Contains(err.Error(), "redirect downgrade") {
		t.Errorf("FetchConf() error = %q, want redirect downgrade", err)
	}
}

func TestDefaultHTTPClientAllowsSecureRedirect(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/zoxide/zoxide.conf" {
			http.Redirect(w, r, server.URL+"/final", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("catalog conf"))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{})
	client.httpClient.Transport = server.Client().Transport

	data, err := catalog.FetchConf(t.Context(), client.httpClient, catalog.Repo{
		Name:    "test",
		SiteURL: server.URL,
	}, "zoxide")
	if err != nil {
		t.Fatalf("FetchConf() error = %v", err)
	}
	if string(data) != "catalog conf" {
		t.Errorf("FetchConf() = %q, want catalog conf", data)
	}
}

func TestDefaultHTTPClientKeepsRedirectLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.URL.Path, http.StatusFound)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{})
	if _, err := client.httpClient.Get(server.URL); err == nil {
		t.Fatal("Get() error = nil")
	} else if !strings.Contains(err.Error(), "stopped after 10 redirects") {
		t.Errorf("Get() error = %q, want redirect limit", err)
	}
}

func TestNewClientDefaultKeepsTenMinuteTimeout(t *testing.T) {
	got := NewClient(ClientConfig{}).httpClient.Timeout
	if got != 10*time.Minute {
		t.Errorf("httpClient.Timeout = %v, want 10m", got)
	}
}

func TestNewClientCopiesSuppliedHTTPClient(t *testing.T) {
	custom := &http.Client{Timeout: 3 * time.Second}
	got := NewClient(ClientConfig{HTTPClient: custom}).httpClient
	if got == custom {
		t.Error("NewClient reused the caller-supplied HTTPClient instead of wrapping a copy")
	}
	if got.Timeout != 3*time.Second {
		t.Errorf("httpClient.Timeout = %v, want the supplied 3s", got.Timeout)
	}
	if custom.Transport != nil {
		t.Error("NewClient changed the caller-supplied client's Transport")
	}
	transport, ok := got.Transport.(sourceTrackerTransport)
	if !ok {
		t.Fatalf("httpClient.Transport = %T, want sourceTrackerTransport", got.Transport)
	}
	if transport.base != http.DefaultTransport {
		t.Errorf("wrapped transport = %T, want http.DefaultTransport", transport.base)
	}
}

// A caller-supplied transport is the base of the copy's wrapper, so updex keeps
// the transport's TLS settings and connection pool.
func TestNewClientWrapsSuppliedTransport(t *testing.T) {
	base := &http.Transport{}
	custom := &http.Client{Transport: base}
	got := NewClient(ClientConfig{HTTPClient: custom}).httpClient
	transport, ok := got.Transport.(sourceTrackerTransport)
	if !ok {
		t.Fatalf("httpClient.Transport = %T, want sourceTrackerTransport", got.Transport)
	}
	if transport.base != base {
		t.Errorf("wrapped transport = %T, want the supplied transport", transport.base)
	}
}
