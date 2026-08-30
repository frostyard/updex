package catalog

import (
	"context"
	"encoding/json"
	"errors"
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
// CachedList call fetch live. SDK callers should inject a cache dir via
// updex.RuntimePaths rather than mutating this variable; it remains for
// compatibility wrappers and tests that have not yet migrated.
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
	// WriteErr is non-nil when the listing was fetched or served
	// successfully but persisting it to the local cache failed. The
	// listing itself is always valid; callers with a Progress reporter
	// should surface this rather than silently discarding it.
	WriteErr error
}

// CachedListIn is List with a local TTL+ETag cache, using an explicit
// cacheDir instead of the package-global CacheDir. An empty cacheDir
// disables caching (every call fetches live). This is the explicit-dir
// variant of CachedList and is what SDK internals use.
func CachedListIn(ctx context.Context, client *http.Client, repo Repo, opts CachedListOptions, cacheDir string) ([]string, CacheResult, error) {
	if !repoNamePattern.MatchString(repo.Name) {
		return nil, CacheResult{}, fmt.Errorf("invalid catalog name %q (allowed: [a-zA-Z0-9_-]+)", repo.Name)
	}

	ttl := opts.TTL
	if ttl <= 0 {
		ttl = DefaultListCacheTTL
	}

	path := listCachePathIn(repo, cacheDir)
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
		if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, CacheResult{}, err
		}
		if entry != nil {
			return entry.Names, CacheResult{FromCache: true, Stale: true, Age: time.Since(entry.FetchedAt)}, nil
		}
		return nil, CacheResult{}, err
	}

	if notModified {
		if entry == nil {
			return nil, CacheResult{}, fmt.Errorf("catalog %q returned 304 to an unconditional request", repo.Name)
		}
		entry.FetchedAt = time.Now()
		var writeErr error
		if err := saveListCache(path, entry); err != nil {
			writeErr = fmt.Errorf("failed to write list cache for %q at %s: %w", repo.Name, path, err)
		}
		return entry.Names, CacheResult{FromCache: true, Age: 0, WriteErr: writeErr}, nil
	}

	var writeErr error
	if err := saveListCache(path, &listCacheEntry{
		ListURL:   repo.ListURL,
		ETag:      newETag,
		FetchedAt: time.Now(),
		Names:     names,
	}); err != nil {
		writeErr = fmt.Errorf("failed to write list cache for %q at %s: %w", repo.Name, path, err)
	}
	return names, CacheResult{WriteErr: writeErr}, nil
}

// CachedList is List with a local TTL+ETag cache. Within the TTL the
// cached listing is served with zero network traffic. After expiry the
// listing is revalidated with If-None-Match: GitHub's 304 response costs
// no API rate limit and only bumps the cache timestamp. When a live fetch
// fails but an expired entry exists, the stale listing is served with
// Stale set so callers can warn. Cache reads and writes are best-effort:
// corrupt entries are treated as misses and write failures are reported
// via CacheResult.WriteErr rather than failing the list operation.
func CachedList(ctx context.Context, client *http.Client, repo Repo, opts CachedListOptions) ([]string, CacheResult, error) {
	return CachedListIn(ctx, client, repo, opts, CacheDir)
}

// listCachePathIn returns the cache file path for a repo's listing using the
// given cacheDir, or "" when cacheDir is empty (caching disabled).
func listCachePathIn(repo Repo, cacheDir string) string {
	if cacheDir == "" {
		return ""
	}
	return filepath.Join(cacheDir, "list-"+repo.Name+".json")
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
// data and a failed write only costs a future refetch. The caller treats
// a non-nil error as observability only, never as cause to fail the list
// operation.
func saveListCache(path string, entry *listCacheEntry) error {
	if path == "" {
		return nil
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
