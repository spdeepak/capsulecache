package capsulecache

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strings"
	"time"
)

// Config holds the middleware settings.
type Config struct {
	// DefaultTTL is how long a cached entry is considered fresh.
	DefaultTTL time.Duration
	// DefaultSWR is the stale-while-revalidate window.
	DefaultSWR   time.Duration
	KeyGenerator func(*http.Request) string
	// ShouldCache decides whether a response with given status code should be cached.
	ShouldCache func(statusCode int) bool
	// MaxBodyBytes - do not cache bodies larger than this.
	MaxBodyBytes int64
	// StripHeaders removes headers before storing (hop-by-hop etc).
	StripHeaders func(http.Header) http.Header
}

// DefaultConfig provides defaults.
var DefaultConfig = &Config{
	DefaultTTL:   5 * time.Minute,
	DefaultSWR:   1 * time.Minute,
	KeyGenerator: DefaultKeyGenerator,
	ShouldCache: func(statusCode int) bool {
		// Only cache successful responses by default
		return statusCode >= http.StatusOK && statusCode < http.StatusMultipleChoices
	},
	StripHeaders: stripHopByHop,
}

// DefaultKeyGenerator creates a simple cache key (Method:Path).
func DefaultKeyGenerator(r *http.Request) string {
	return "cache:" + r.Method + ":" + r.URL.Path
}

// AdvancedKeyGenerator includes specified headers and body hash (for POST/PUT).
func AdvancedKeyGenerator(r *http.Request, headersToInclude []string) string {
	// 1. Base Key
	key := r.Method + ":" + r.URL.Path

	// 2. Add Headers
	for _, header := range headersToInclude {
		if val := r.Header.Get(header); val != "" {
			key += ":" + header + "=" + val
		}
	}

	// 3. Add Body Hash (for safe caching of idempotent requests like POST to a search endpoint)
	if r.ContentLength > 0 && (r.Method == http.MethodPost || r.Method == http.MethodPut) {
		var buf bytes.Buffer
		// TeeReader allows reading the body while writing it to buf
		tee := io.TeeReader(r.Body, &buf)
		bodyBytes, _ := io.ReadAll(tee)
		r.Body.Close()
		r.Body = io.NopCloser(&buf) // restore body for downstream

		hash := sha256.Sum256(bodyBytes)
		key += ":body_hash=" + hex.EncodeToString(hash[:])
	}

	return key
}

func stripHopByHop(header http.Header) http.Header {
	// Clone so caller can mutate safely.
	headerClone := header.Clone()

	for _, k := range []string{
		"Connection", "Proxy-Connection", "Keep-Alive",
		"Proxy-Authenticate", "Proxy-Authorization", "TE",
		"Trailer", "Transfer-Encoding", "Upgrade",
	} {
		headerClone.Del(k)
	}
	// Also remove hop-by-hop values referenced by Connection header
	if conn := header.Get("Connection"); conn != "" {
		for _, token := range strings.Split(conn, ",") {
			token = strings.TrimSpace(token)
			if token != "" {
				headerClone.Del(token)
			}
		}
	}
	return headerClone
}
