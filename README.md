## Capsule Cache

A lightweight, memory-quota-aware HTTP output cache middleware for Go web frameworks like Gin, Chi, and Fiber. This package addresses the challenge of safely deploying in-memory caches in containerized environments (like Kubernetes) by enforcing a strict memory limit.

---

### Key Features

*   **Hard Memory Limit (Quota):** Prevents Out-of-Memory (OOM) issues in containerized environments by evicting Least Recently Used (LRU) items when a configurable MB limit is hit. This ensures stability in memory-constrained setups.
*   **Framework Agnostic:** Implemented as a standard `net/http` middleware, ensuring compatibility with all major Go routers (Gin, Chi, Fiber via their adapter methods).
*   **Stale-While-Revalidate (SWR):** Serves stale content instantly while triggering a non-blocking background refresh to update the cache entry.
*   **Time-to-Live (TTL):** Standard freshness control for cached entries.
*   **Pluggable Backend:** Ships with a memory-safe LRU store and an interface for easy integration with external services like Redis.
*   **Flexible Key Strategy:** Supports cache keys based on URL, headers, and request body hash for varied API use cases (e.g., translation services, API throttling).

---

### Installation

```bash
go get github.com/spdeepak/capsulecache
```

---

### Usage

The core of the package is the `NewCacheMiddleware` function, which requires a `Store` implementation and a `Config`.

#### 1. Setup the Cache Store (In-Memory LRU with Quota)

The example below sets a hard memory limit of **100 Megabytes (MB)**.

```go
package main

import (
    "log"
    "net/http"
    "time"
    
    "github.com/go-chi/chi/v5"
    "github.com/spdeepak/capsulecache"
)

// Global store for access in other handlers (e.g., for manual invalidation)
var store cache.CacheStore 

func main() {
    // Initialize the In-Memory Cache Store with a hard limit of 100 MB
    const maxMemoryMB = 100
    store = cache.NewInMemoryQuotaLRU(maxMemoryMB)
    defer store.Close()

    // Configure the Middleware
    cfg := &cache.Config{
        DefaultTTL: 1 * time.Minute,
        DefaultSWR: 15 * time.Second, // Allow stale content for 15s while refreshing
    }

    // Initialize the Middleware
    cacheMiddleware := cache.NewCacheMiddleware(store, cfg)
    
    // Setup Chi Router
    r := chi.NewRouter()

    // Apply the cache middleware to a specific route group or handler
    r.Group(func(r chi.Router) {
        r.Use(cacheMiddleware) // Apply cache to handlers in this group

        r.Get("/api/products/{id}", productHandler)
        r.Get("/public/data", publicDataHandler)
    })
    
    // Non-cached route
    r.Post("/api/products", createProductHandler)

    log.Println("Server starting on :8080")
    http.ListenAndServe(":8080", r)
}

// Example Handler
func publicDataHandler(w http.ResponseWriter, r *http.Request) {
    // This expensive operation will only run once per cache TTL
    time.Sleep(500 * time.Millisecond) 
    w.Write([]byte("Cached response: " + time.Now().Format(time.RFC3339)))
}
```

#### 2. Integration with Frameworks

| Framework | Integration Method | Example |
| :--- | :--- | :--- |
| **Chi** | Standard `r.Use(middleware)` | See example above (`r.Use(cacheMiddleware)`) |
| **Gin** | Use `gin.WrapH()` | `r.GET("/path", gin.WrapH(cacheMiddleware(http.HandlerFunc(myHandler))))` |
| **Fiber** | Use the standard `net/http` adapter (e.g., `adaptor.HTTPHandlerFunc`) | `app.Use(adaptor.HTTPHandlerFunc(cacheMiddleware(http.HandlerFunc(myHandler))))` |

---

### Cache Invalidation

For compliance or immediate content updates, the `Delete` method on the `CacheStore` can be used to manually purge an entry.

```go
// Example: Purging a cache entry after a POST request
func createProductHandler(w http.ResponseWriter, r *http.Request) {
    // ... logic to create product ...
    
    productID := "123" // Assume product ID is known
    productKey := "GET:/api/products/" + productID // Must match the KeyGenerator logic

    // Invalidate the cached GET response for the product
    if err := store.Delete(productKey); err != nil {
        log.Printf("Failed to purge cache for key %s: %v", productKey, err)
    }

    w.WriteHeader(http.StatusCreated)
}
```

---

### Custom Cache Key Strategy

You can define a custom function to include specific headers (e.g., `Accept-Language` for translation services) in the cache key.

```go
// Example: Caching based on URL AND Accept-Language header
func KeyWithLanguage(r *http.Request) string {
    // Assuming DefaultKeyGenerator is also exported from capsulecache/cache
    baseKey := cache.DefaultKeyGenerator(r) 

    lang := r.Header.Get("Accept-Language")
    if lang != "" {
        // Simple extraction for the primary language tag (e.g., "en-US,en;q=0.9" -> "en-US")
        if len(lang) > 5 { 
            lang = lang[:5] 
        }
        return baseKey + ":lang=" + lang
    }
    return baseKey
}

// In main():
// cfg := &cache.Config{
//     DefaultTTL: 1 * time.Minute,
//     KeyGenerator: KeyWithLanguage,
// }
```