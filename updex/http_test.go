package updex

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/frostyard/updex/catalog"
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

func TestNewClientPreservesSuppliedHTTPClient(t *testing.T) {
	custom := &http.Client{}
	if got := NewClient(ClientConfig{HTTPClient: custom}).httpClient; got != custom {
		t.Error("NewClient replaced caller-supplied HTTPClient")
	}
}
