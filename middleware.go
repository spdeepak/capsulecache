package capsulecache

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/spdeepak/capsulecache/cache"
	"golang.org/x/sync/singleflight"
)

// NewCacheMiddleware returns middleware that caches GET/HEAD responses with quotas,
// SWR (Stale while revalidate) refreshes and sensible defaults.
// The middleware uses singleflight to avoid thundering-herd during SWR refreshes.
func NewCacheMiddleware(store cache.Store, cfg *Config) func(next http.Handler) http.Handler {
	if cfg == nil {
		cfg = DefaultConfig
	}

	// This ensures that when there are multiple requests for the same expensive operation at the same time
	// The operation runs only once, and all requests get the same result.
	var singleFlight singleflight.Group

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
			// Only cache GET/HEAD requests
			if request.Method != http.MethodGet && request.Method != http.MethodHead {
				next.ServeHTTP(responseWriter, request)
				return
			}

			// Generate a key for a given request
			cacheKey := cfg.KeyGenerator(request)
			if cacheKey == "" {
				// fallback to not caching if key generation fails
				next.ServeHTTP(responseWriter, request)
				return
			}

			// Check if response is already cached
			responseCacheEntry, cacheHit := store.Get(cacheKey)
			if cacheHit && responseCacheEntry != nil && !responseCacheEntry.IsRotten() {
				// Serve cached response -> must set headers BEFORE WriteHeader
				// Start with a fresh header map so we don't mutate stored headers
				for headerKey, headerValues := range responseCacheEntry.Headers {
					for _, headerValue := range headerValues {
						responseWriter.Header().Add(headerKey, headerValue)
					}
				}
				responseWriter.Header().Set("X-Cache-Status", "HIT")
				if responseCacheEntry.IsStale() {
					responseWriter.Header().Set("X-Cache-Stale", "YES")
					// trigger background SWR refresh via singleflight since the cache is stale
					go func(ctx context.Context, cacheKey string, req *http.Request) {
						// singleflight ensures only one background refresh for this cacheKey
						_, _, _ = singleFlight.Do(cacheKey, func() (interface{}, error) {
							// create a fresh request clone with background context
							reqClone := req.Clone(context.Background())

							// Use a recorder that discards writes (we only want capture for cache)
							// Using a dummy response writer that implements minimal interface.
							dw := &discardResponseWriter{header: make(http.Header)}
							rec := NewResponseRecorder(dw, cfg.MaxBodyBytes)

							defer func() {
								// recover handler panics in refresh to avoid crashing goroutine
								_ = recover()
							}()

							next.ServeHTTP(rec, reqClone)

							// If the response is cacheable, cache it
							if cfg.ShouldCache(rec.StatusCode()) && !rec.capReached {
								response := &cache.ResponseCacheEntry{
									StatusCode: rec.StatusCode(),
									Headers:    cfg.StripHeaders(rec.Header().Clone()),
									Body:       append([]byte(nil), rec.Body()...), // copy
									CreatedAt:  time.Now(),
									TTL:        cfg.DefaultTTL,
									SWR:        cfg.DefaultSWR,
								}
								err := store.Set(cacheKey, response)
								if err != nil {
									slog.Error("Failed to cache response during single flight", slog.Any("cacheKey", cacheKey), slog.Any("error", err.Error()), slog.Any("request", request))
								}
							}
							return nil, nil
						})
					}(request.Context(), cacheKey, request)
				} else {
					responseWriter.Header().Set("X-Cache-Stale", "NO")
				}

				// Write the cached response to client
				responseWriter.WriteHeader(responseCacheEntry.StatusCode)
				if len(responseCacheEntry.Body) > 0 {
					_, _ = responseWriter.Write(responseCacheEntry.Body)
				}
				return
			} else if !cacheHit {
				responseWriter.Header().Set("X-Cache-Status", "MISS")
			}

			// Cache miss/rotten path: execute handler and capture response
			// Protect against panics so we can still flush or at least return a 500
			responseRecorder := NewResponseRecorder(responseWriter, cfg.MaxBodyBytes)
			defer func() {
				if p := recover(); p != nil {
					// Best-effort: return 500 if nothing was written and avoid crashing the server
					if !responseRecorder.written {
						http.Error(responseWriter, "internal server error", http.StatusInternalServerError)
					}
				}
			}()

			// Actually call next handler and let responseRecorder capture
			next.ServeHTTP(responseRecorder, request)

			// Determine if we can/should cache
			status := responseRecorder.StatusCode()
			// If body exceeded cap, do not cache
			if cfg.ShouldCache(status) && !responseRecorder.capReached {
				// Clone headers and strip hop-by-hop
				hdrCopy := responseRecorder.Header().Clone()
				clean := cfg.StripHeaders(hdrCopy)

				newEntry := &cache.ResponseCacheEntry{
					StatusCode: status,
					Headers:    clean,
					Body:       append([]byte(nil), responseRecorder.Body()...), // copy
					CreatedAt:  time.Now(),
					TTL:        cfg.DefaultTTL,
					SWR:        cfg.DefaultSWR,
				}
				// Store asynchronously so we don't block the client
				go func(cacheKey string, newEntry *cache.ResponseCacheEntry) {
					// recover to avoid uncaught goroutine panic
					defer func() { _ = recover() }()
					err := store.Set(cacheKey, newEntry)
					if err != nil {
						slog.Error("Failed to cache response", slog.Any("cacheKey", cacheKey), slog.Any("error", err.Error()), slog.Any("request", request))
					}
				}(cacheKey, newEntry)
			}

			// Finally, flush recorded response to the client
			responseRecorder.Flush()
		})
	}
}

// Utility: discardResponseWriter
// minimal http.ResponseWriter used in background refresh where we don't want to send to client.
type discardResponseWriter struct {
	header http.Header
}

func (d *discardResponseWriter) Header() http.Header {
	return d.header
}

func (d *discardResponseWriter) Write(b []byte) (int, error) {
	// Discard
	return len(b), nil
}

func (d *discardResponseWriter) WriteHeader(statusCode int) {
	// no-op
}
