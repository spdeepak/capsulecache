package cache

import (
	"net/http"
	"time"
)

// Store defines the contract for all storage backends.
type Store interface {
	Get(key string) (*ResponseCacheEntry, bool)
	Set(key string, entry *ResponseCacheEntry) error
	Delete(key string) error
	Close() error // For graceful shutdown/cleanup
}

// ResponseCacheEntry holds the complete HTTP response data and caching metadata.
type ResponseCacheEntry struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
	CreatedAt  time.Time
	TTL        time.Duration // Time-to-Live (Freshness)
	SWR        time.Duration // Stale-While-Revalidate window
}

// Size returns the estimated memory footprint in bytes.
func (e *ResponseCacheEntry) Size() int64 {
	// Simple size estimation: Body length + headers map/string overhead
	bodySize := int64(len(e.Body))
	// Heuristic: ~30 bytes per header key/value pair overhead
	headerSize := int64(len(e.Headers) * 30)
	return bodySize + headerSize
}

// IsStale checks if the entry is past its TTL.
func (e *ResponseCacheEntry) IsStale() bool {
	return time.Since(e.CreatedAt) > e.TTL
}

// IsRotten checks if the entry is past its Stale-While-Revalidate window.
func (e *ResponseCacheEntry) IsRotten() bool {
	return time.Since(e.CreatedAt) > (e.TTL + e.SWR)
}
