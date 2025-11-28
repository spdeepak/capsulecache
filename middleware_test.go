package capsulecache

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	cache2 "github.com/spdeepak/capsulecache/cache"
)

func TestCacheMiddleware(t *testing.T) {

	mux := http.NewServeMux()
	mux.HandleFunc("/", Handler)

	cache := cache2.NewInMemoryQuotaLRU(2)
	config := Config{
		DefaultTTL:   1 * time.Second,
		DefaultSWR:   1500 * time.Millisecond,
		KeyGenerator: DefaultKeyGenerator,
		ShouldCache:  func(statusCode int) bool { return statusCode == http.StatusOK },
		MaxBodyBytes: 1024,
		StripHeaders: stripHopByHop,
	}
	client := NewCacheMiddleware(cache, &config)(mux)

	req := httptest.NewRequest("GET", "/", nil)
	r1 := httptest.NewRecorder()
	client.ServeHTTP(r1, req)

	if r1.Header().Get("X-Cache-Status") != "MISS" {
		t.Fatalf("expected MISS, got %s", r1.Header().Get("X-Cache-Status"))
	}
	if r1.Body.String() != "fresh" {
		t.Fatalf("unexpected body: %s", r1.Body.String())
	}

	//Sleep for some time so that the cache gets populated
	time.Sleep(10 * time.Millisecond)
	r2 := httptest.NewRecorder()
	client.ServeHTTP(r2, req)

	if r2.Header().Get("X-Cache-Status") != "HIT" {
		t.Fatalf("expected HIT, got %s", r2.Header().Get("X-Cache-Status"))
	}
	if r2.Body.String() != "fresh" {
		t.Fatalf("unexpected body: %s", r2.Body.String())
	}
}

func Handler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("fresh"))
}
