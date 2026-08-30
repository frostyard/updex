package catalog

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// withCacheDir points CacheDir at a fresh temp directory for the test.
func withCacheDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	original := CacheDir
	CacheDir = dir
	t.Cleanup(func() { CacheDir = original })
	return dir
}

// listServer serves a contents-API listing with ETag support and counts
// requests and 304 responses.
type listServer struct {
	*httptest.Server
	requests    atomic.Int64
	notModified atomic.Int64
	etag        atomic.Value // string
	body        atomic.Value // string
}

func newListServer(t *testing.T) *listServer {
	t.Helper()
	s := &listServer{}
	s.etag.Store(`"v1"`)
	s.body.Store(`[{"name": "zoxide", "type": "dir"}, {"name": "btop", "type": "dir"}]`)
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.requests.Add(1)
		etag := s.etag.Load().(string)
		if r.Header.Get("If-None-Match") == etag {
			s.notModified.Add(1)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		_, _ = w.Write([]byte(s.body.Load().(string)))
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *listServer) repo() Repo {
	return Repo{
		Name:          "fedora",
		SiteURL:       s.URL,
		ListURL:       s.URL,
		Component:     "catalog-fedora",
		AllowInsecure: true,
	}
}

func TestCachedList_ServesFromCacheWithinTTL(t *testing.T) {
	withCacheDir(t)
	server := newListServer(t)
	repo := server.repo()

	names, res, err := CachedList(t.Context(), server.Client(), repo, CachedListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.FromCache {
		t.Error("first call should be live")
	}
	if len(names) != 2 || names[0] != "btop" {
		t.Fatalf("unexpected names: %v", names)
	}

	// Second call within the TTL: served from cache, zero network.
	names, res, err = CachedList(t.Context(), server.Client(), repo, CachedListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.FromCache || res.Stale {
		t.Errorf("expected fresh cache hit, got %+v", res)
	}
	if len(names) != 2 {
		t.Fatalf("unexpected cached names: %v", names)
	}
	if got := server.requests.Load(); got != 1 {
		t.Errorf("expected 1 request, got %d", got)
	}
}

func TestCachedList_ETagRevalidationAfterExpiry(t *testing.T) {
	dir := withCacheDir(t)
	server := newListServer(t)
	repo := server.repo()

	if _, _, err := CachedList(t.Context(), server.Client(), repo, CachedListOptions{}); err != nil {
		t.Fatal(err)
	}

	// Age the cache entry past the TTL.
	backdateCache(t, dir, repo, 2*time.Hour)

	names, res, err := CachedList(t.Context(), server.Client(), repo, CachedListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.FromCache || res.Stale {
		t.Errorf("expected revalidated cache hit, got %+v", res)
	}
	if len(names) != 2 {
		t.Fatalf("unexpected names after 304: %v", names)
	}
	if got := server.notModified.Load(); got != 1 {
		t.Errorf("expected 1 conditional 304, got %d", got)
	}

	// The revalidation refreshed the timestamp: next call is a pure cache
	// hit with no further request.
	before := server.requests.Load()
	if _, res, err = CachedList(t.Context(), server.Client(), repo, CachedListOptions{}); err != nil || !res.FromCache {
		t.Fatalf("expected cache hit after revalidation (err %v, res %+v)", err, res)
	}
	if got := server.requests.Load(); got != before {
		t.Errorf("expected no new request, got %d -> %d", before, got)
	}
}

func TestCachedList_ChangedContentAfterExpiry(t *testing.T) {
	dir := withCacheDir(t)
	server := newListServer(t)
	repo := server.repo()

	if _, _, err := CachedList(t.Context(), server.Client(), repo, CachedListOptions{}); err != nil {
		t.Fatal(err)
	}
	backdateCache(t, dir, repo, 2*time.Hour)

	server.etag.Store(`"v2"`)
	server.body.Store(`[{"name": "fastfetch", "type": "dir"}]`)

	names, res, err := CachedList(t.Context(), server.Client(), repo, CachedListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.FromCache {
		t.Errorf("expected live result for changed content, got %+v", res)
	}
	if len(names) != 1 || names[0] != "fastfetch" {
		t.Fatalf("unexpected names: %v", names)
	}
}

func TestCachedList_NoCacheAlwaysFetches(t *testing.T) {
	withCacheDir(t)
	server := newListServer(t)
	repo := server.repo()

	for range 2 {
		_, res, err := CachedList(t.Context(), server.Client(), repo, CachedListOptions{NoCache: true})
		if err != nil {
			t.Fatal(err)
		}
		if res.FromCache {
			t.Errorf("NoCache result served from cache: %+v", res)
		}
	}
	if got := server.requests.Load(); got != 2 {
		t.Errorf("expected 2 requests with NoCache, got %d", got)
	}

	// NoCache still rewrote the cache: a normal call now hits it.
	if _, res, err := CachedList(t.Context(), server.Client(), repo, CachedListOptions{}); err != nil || !res.FromCache {
		t.Errorf("expected cache hit after NoCache rewrite (err %v, res %+v)", err, res)
	}
}

func TestCachedList_ListURLChangeInvalidates(t *testing.T) {
	withCacheDir(t)
	server := newListServer(t)
	repo := server.repo()

	if _, _, err := CachedList(t.Context(), server.Client(), repo, CachedListOptions{}); err != nil {
		t.Fatal(err)
	}

	// Same repo name, different ListURL: the handler serves every path, so
	// the fetch succeeds — the assertion is that the old entry is not
	// reused even though a fresh cache file exists under this repo name.
	changed := repo
	changed.ListURL = server.URL + "/other"
	_, res, err := CachedList(t.Context(), server.Client(), changed, CachedListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.FromCache {
		t.Errorf("cache entry for old ListURL was reused: %+v", res)
	}
}

func TestCachedList_CorruptCacheRefetches(t *testing.T) {
	dir := withCacheDir(t)
	server := newListServer(t)
	repo := server.repo()

	if _, _, err := CachedList(t.Context(), server.Client(), repo, CachedListOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "list-fedora.json"), []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}

	_, res, err := CachedList(t.Context(), server.Client(), repo, CachedListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.FromCache {
		t.Errorf("corrupt cache should be a miss: %+v", res)
	}
}

func TestCachedList_StaleFallbackOnFetchError(t *testing.T) {
	dir := withCacheDir(t)
	server := newListServer(t)
	repo := server.repo()

	if _, _, err := CachedList(t.Context(), server.Client(), repo, CachedListOptions{}); err != nil {
		t.Fatal(err)
	}
	backdateCache(t, dir, repo, 2*time.Hour)
	server.Close() // live fetch now fails

	names, res, err := CachedList(t.Context(), server.Client(), repo, CachedListOptions{})
	if err != nil {
		t.Fatalf("expected stale fallback, got error: %v", err)
	}
	if !res.FromCache || !res.Stale {
		t.Errorf("expected stale cache result, got %+v", res)
	}
	if res.Age < 2*time.Hour {
		t.Errorf("expected age >= 2h, got %s", res.Age)
	}
	if len(names) != 2 {
		t.Fatalf("unexpected stale names: %v", names)
	}
}

func TestCachedList_CancelledContextDoesNotServeStale(t *testing.T) {
	dir := withCacheDir(t)
	server := newListServer(t)
	repo := server.repo()

	if _, _, err := CachedList(t.Context(), server.Client(), repo, CachedListOptions{}); err != nil {
		t.Fatal(err)
	}
	backdateCache(t, dir, repo, 2*time.Hour)

	// A cancelled context is the caller aborting, not an unreachable
	// catalog: it must surface rather than masquerade as a stale hit.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, res, err := CachedList(ctx, server.Client(), repo, CachedListOptions{})
	if err == nil {
		t.Fatalf("expected context error, got success (%+v)", res)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if res.FromCache || res.Stale {
		t.Errorf("expected no cache result on cancellation, got %+v", res)
	}
}

func TestCachedList_InvalidRepoName(t *testing.T) {
	withCacheDir(t)
	server := newListServer(t)

	// CachedList is public API: a hand-built Repo must not be able to
	// steer the cache filename outside CacheDir.
	repo := server.repo()
	repo.Name = "../../outside"

	if _, _, err := CachedList(t.Context(), server.Client(), repo, CachedListOptions{}); err == nil {
		t.Fatal("expected error for traversal-shaped repo name")
	}
	if got := server.requests.Load(); got != 0 {
		t.Errorf("expected no request for invalid repo name, got %d", got)
	}
}

func TestCachedList_FetchErrorWithoutCache(t *testing.T) {
	withCacheDir(t)
	server := newListServer(t)
	repo := server.repo()
	server.Close()

	if _, _, err := CachedList(t.Context(), server.Client(), repo, CachedListOptions{}); err == nil {
		t.Fatal("expected error when fetch fails with no cache")
	}
}

func TestCachedList_DisabledCacheDir(t *testing.T) {
	original := CacheDir
	CacheDir = ""
	t.Cleanup(func() { CacheDir = original })

	server := newListServer(t)
	repo := server.repo()

	for range 2 {
		_, res, err := CachedList(t.Context(), server.Client(), repo, CachedListOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if res.FromCache {
			t.Errorf("caching disabled but served from cache: %+v", res)
		}
	}
	if got := server.requests.Load(); got != 2 {
		t.Errorf("expected 2 live requests, got %d", got)
	}
}

func TestSaveListCache_ReturnsErrorOnWriteFailure(t *testing.T) {
	dir := t.TempDir()
	blocked := filepath.Join(dir, "blocked")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0644); err != nil {
		t.Fatal(err)
	}
	// blocked is a file, so MkdirAll(filepath.Dir(path), ...) below it fails
	// regardless of the effective user's permissions.
	path := filepath.Join(blocked, "list-fedora.json")

	err := saveListCache(path, &listCacheEntry{
		ListURL:   "https://example.com",
		FetchedAt: time.Now(),
		Names:     []string{"a"},
	})
	if err == nil {
		t.Fatal("expected error when the cache directory path is blocked by a file")
	}
}

func TestCachedListIn_WriteFailureIsLoggedNotFatal(t *testing.T) {
	dir := t.TempDir()
	blocked := filepath.Join(dir, "blocked")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	original := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(original) })

	server := newListServer(t)
	repo := server.repo()

	names, res, err := CachedListIn(t.Context(), server.Client(), repo, CachedListOptions{}, blocked)
	if err != nil {
		t.Fatalf("expected the list operation to succeed despite the cache write failure, got: %v", err)
	}
	if len(names) == 0 {
		t.Fatal("expected a non-empty listing despite the cache write failure")
	}
	if res.FromCache {
		t.Errorf("expected a live result, got %+v", res)
	}
	if !strings.Contains(buf.String(), "failed to write list cache") {
		t.Errorf("expected the cache write failure to be logged, got log output: %q", buf.String())
	}
}

// backdateCache rewrites a cache entry's FetchedAt to age it past the TTL.
func backdateCache(t *testing.T, dir string, repo Repo, age time.Duration) {
	t.Helper()
	path := filepath.Join(dir, "list-"+repo.Name+".json")
	entry := loadListCache(path, repo)
	if entry == nil {
		t.Fatalf("no cache entry at %s", path)
	}
	entry.FetchedAt = time.Now().Add(-age)
	saveListCache(path, entry)
}
