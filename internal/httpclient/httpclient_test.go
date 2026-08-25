package httpclient

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewSetsTimeout(t *testing.T) {
	got := New(5 * time.Second).Timeout
	if got != 5*time.Second {
		t.Errorf("Timeout = %v, want 5s", got)
	}
}

func TestNewRejectsRedirectDowngrade(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer plain.Close()

	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+r.URL.Path, http.StatusFound)
	}))
	defer secure.Close()

	client := New(time.Minute)
	client.Transport = secure.Client().Transport

	_, err := client.Get(secure.URL)
	if err == nil {
		t.Fatal("Get() error = nil, want redirect downgrade error")
	}
	if !strings.Contains(err.Error(), "redirect downgrade") {
		t.Errorf("Get() error = %q, want redirect downgrade", err)
	}
}

func TestNewAllowsSameSchemeRedirect(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, server.URL+"/final", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("final content"))
	}))
	defer server.Close()

	client := New(time.Minute)
	client.Transport = server.Client().Transport

	resp, err := client.Get(server.URL + "/start")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want 200", resp.StatusCode)
	}
}

func TestNewLimitsRedirectsToTen(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.URL.Path, http.StatusFound)
	}))
	defer server.Close()

	client := New(time.Minute)
	_, err := client.Get(server.URL)
	if err == nil {
		t.Fatal("Get() error = nil, want redirect limit error")
	}
	if !strings.Contains(err.Error(), "stopped after 10 redirects") {
		t.Errorf("Get() error = %q, want redirect limit", err)
	}
}

func TestNewPreservesCustomClientRedirectFollowing(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer plain.Close()

	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+r.URL.Path, http.StatusFound)
	}))
	defer secure.Close()

	custom := secure.Client()
	resp, err := custom.Get(secure.URL)
	if err != nil {
		t.Fatalf("Get() error = %v, want a caller-supplied client to keep following the downgrade redirect", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want 200", resp.StatusCode)
	}
}
