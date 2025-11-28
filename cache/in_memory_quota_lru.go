package cache

import (
	"container/list"
	"sync"
)

// lruEntry links the cache key and the entry to the list element.
type lruEntry struct {
	key   string
	size  int64
	value *ResponseCacheEntry
}

// InMemoryQuotaLRU implements Store with a hard memory limit and LRU policy.
type InMemoryQuotaLRU struct {
	mutex sync.RWMutex
	// Doubly linked list for LRU order
	lru   *list.List
	cache map[string]*list.Element
	// Hard memory limit in bytes
	maxBytes int64
	// Current total size of all stored items
	currentBytes int64
}

// NewInMemoryQuotaLRU creates a new InMemoryQuotaLRU cache.
// maxMB is the memory limit in megabytes.
func NewInMemoryQuotaLRU(maxMB int) Store {
	return &InMemoryQuotaLRU{
		lru:      list.New(),
		cache:    make(map[string]*list.Element),
		maxBytes: int64(maxMB) * 1024 * 1024,
	}
}

// Get retrieves an entry and moves it to the front of the list (MRU).
func (lru *InMemoryQuotaLRU) Get(key string) (*ResponseCacheEntry, bool) {
	// Step 1: Read lock to check existence
	lru.mutex.RLock()
	element, ok := lru.cache[key]
	lru.mutex.RUnlock()
	if !ok {
		return nil, false
	}

	// Step 2: Only lock for moving element to front
	lru.mutex.Lock()
	lru.lru.MoveToFront(element)
	lru.mutex.Unlock()

	return element.Value.(*lruEntry).value, true
}

// Set adds or updates an entry, triggering eviction if the hard memory limit is hit.
func (lru *InMemoryQuotaLRU) Set(key string, entry *ResponseCacheEntry) error {
	// compute before acquiring lock
	itemSize := entry.Size()

	lru.mutex.Lock()
	defer lru.mutex.Unlock()

	if element, ok := lru.cache[key]; ok {
		oldEntry := element.Value.(*lruEntry)
		lru.currentBytes -= oldEntry.size
		oldEntry.size = itemSize
		oldEntry.value = entry
		lru.currentBytes += itemSize
		lru.lru.MoveToFront(element)
	} else {
		newEntry := &lruEntry{key: key, size: itemSize, value: entry}
		element := lru.lru.PushFront(newEntry)
		lru.cache[key] = element
		lru.currentBytes += itemSize
	}

	// Eviction
	for lru.currentBytes > lru.maxBytes {
		lruElement := lru.lru.Back()
		if lruElement == nil {
			break
		}
		evictedEntry := lru.lru.Remove(lruElement).(*lruEntry)
		delete(lru.cache, evictedEntry.key)
		lru.currentBytes -= evictedEntry.size
	}
	return nil
}

// Delete removes an entry from the cache.
func (lru *InMemoryQuotaLRU) Delete(key string) error {
	lru.mutex.Lock()
	defer lru.mutex.Unlock()

	if element, ok := lru.cache[key]; ok {
		evictedEntry := lru.lru.Remove(element).(*lruEntry)
		delete(lru.cache, evictedEntry.key)
		lru.currentBytes -= evictedEntry.size
	}
	return nil
}

// Close is a no-op for in-memory, but required by the interface.
func (lru *InMemoryQuotaLRU) Close() error {
	return nil
}
