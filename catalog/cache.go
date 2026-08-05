package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// DefaultListCacheTTL is how long a cached listing is served without any
// network traffic. After expiry the listing is revalidated with a
// conditional request (see CachedList) rather than unconditionally
// refetched.
var DefaultListCacheTTL = 60 * time.Minute

// CacheDir is the directory listing caches are stored in, defaulting to
// <user cache dir>/updex (~/.cache/updex). Empty — including when the
// user cache dir cannot be resolved — disables caching, making every
// CachedList call fetch live. Overridable in tests.
var CacheDir = defaultCacheDir()

func defaultCacheDir() string {
	base, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(base, "updex")
}

// listCacheEntry is the on-disk cache format for one repo's listing.
type listCacheEntry struct {
	// ListURL invalidates the entry when the repo's configured ListURL
	// changes under the same repo name.
	ListURL   string    `json:"list_url"`
	ETag      string    `json:"etag,omitempty"`
	FetchedAt time.Time `json:"fetched_at"`
	Names     []string  `json:"names"`
}

// CachedListOptions configures CachedList.
type CachedListOptions struct {
	// TTL is how long a cached listing is served without network traffic.
	// Zero means DefaultListCacheTTL.
	TTL time.Duration

	// NoCache bypasses the cache entirely: always fetch live (the result
	// still rewrites the cache).
	NoCache bool
}

// CacheResult describes where a CachedList listing came from.
type CacheResult struct {
	// FromCache is true when the names were served from the local cache
	// (fresh, revalidated via ETag, or stale fallback).
	FromCache bool
	// Stale is true when a live fetch failed and an expired cache entry
	// was served instead.
	Stale bool
	// Age is the age of the served cache entry; zero for live results.
	Age time.Duration
}

// CachedList is List with a local TTL+ETag cache. Within the TTL the
// cached listing is served with zero network traffic. After expiry the
// listing is revalidated with If-None-Match: GitHub's 304 response costs
// no API rate limit and only bumps the cache timestamp. When a live fetch
// fails but an expired entry exists, the stale listing is served with
// Stale set so callers can warn. Cache reads and writes are best-effort:
// corrupt entries are treated as misses and write failures are ignored.
func CachedList(ctx context.Context, client *http.Client, repo Repo, opts CachedListOptions) ([]string, CacheResult, error) {
	ttl := opts.TTL
	if ttl <= 0 {
		ttl = DefaultListCacheTTL
	}

	path := listCachePath(repo)
	var entry *listCacheEntry
	if !opts.NoCache && path != "" {
		entry = loadListCache(path, repo)
	}

	if entry != nil {
		if age := time.Since(entry.FetchedAt); age >= 0 && age < ttl {
			return entry.Names, CacheResult{FromCache: true, Age: age}, nil
		}
	}

	etag := ""
	if entry != nil {
		etag = entry.ETag
	}

	names, newETag, notModified, err := fetchList(ctx, client, repo, etag)
	if err != nil {
		if entry != nil {
			return entry.Names, CacheResult{FromCache: true, Stale: true, Age: time.Since(entry.FetchedAt)}, nil
		}
		return nil, CacheResult{}, err
	}

	if notModified {
		// A 304 can only happen when we sent an ETag, i.e. entry != nil;
		// the guard protects against a server 304ing unconditionally.
		if entry == nil {
			return nil, CacheResult{}, fmt.Errorf("catalog %q returned 304 to an unconditional request", repo.Name)
		}
		entry.FetchedAt = time.Now()
		saveListCache(path, entry)
		return entry.Names, CacheResult{FromCache: true, Age: 0}, nil
	}

	saveListCache(path, &listCacheEntry{
		ListURL:   repo.ListURL,
		ETag:      newETag,
		FetchedAt: time.Now(),
		Names:     names,
	})
	return names, CacheResult{}, nil
}

// listCachePath returns the cache file path for a repo's listing, or ""
// when caching is disabled. Repo names are charset-validated at load time,
// so they are safe as filename components.
func listCachePath(repo Repo) string {
	if CacheDir == "" {
		return ""
	}
	return filepath.Join(CacheDir, "list-"+repo.Name+".json")
}

// loadListCache reads and validates a cache entry, returning nil for any
// miss: missing or unreadable file, unparseable JSON, or a ListURL that no
// longer matches the repo's configuration.
func loadListCache(path string, repo Repo) *listCacheEntry {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var entry listCacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil
	}
	if entry.ListURL != repo.ListURL || entry.FetchedAt.IsZero() {
		return nil
	}
	return &entry
}

// saveListCache writes a cache entry, best-effort: the listing is public
// data and a failed write only costs a future refetch.
func saveListCache(path string, entry *listCacheEntry) {
	if path == "" {
		return
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0644)
}
