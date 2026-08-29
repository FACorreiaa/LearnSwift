package executor

import (
	"container/list"
	"sync"
)

// MemoryCache holds compiled modules in memory, evicting the least recently
// used once a byte budget is reached.
//
// Bounded by bytes rather than entries because the two SDKs differ by fifty
// times in output size: an entry count that is generous for embedded modules
// (38 KB) is ruinous for full-SDK ones (7.7 MB).
//
// In-process, so it is per-replica and empty after a deploy. That is a real
// weakening of the cache-hit rate and it is accepted here: a miss costs one
// compile, the correctness is unaffected, and a shared cache means an object
// store on the request path. If hit rates ever justify it, the Cache interface
// is the seam to put S3 behind.
type MemoryCache struct {
	mu      sync.Mutex
	entries map[string]*list.Element
	order   *list.List // front is most recently used
	bytes   int
	limit   int
}

type cacheEntry struct {
	key    string
	module []byte
}

// DefaultCacheBytes holds roughly a hundred embedded modules, or thirty
// full-SDK ones.
const DefaultCacheBytes = 256 << 20

func NewMemoryCache(limit int) *MemoryCache {
	if limit <= 0 {
		limit = DefaultCacheBytes
	}
	return &MemoryCache{
		entries: make(map[string]*list.Element),
		order:   list.New(),
		limit:   limit,
	}
}

func (c *MemoryCache) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	c.order.MoveToFront(el)
	return el.Value.(*cacheEntry).module, true
}

func (c *MemoryCache) Put(key string, module []byte) {
	// A module larger than the whole budget would evict everything and then
	// not fit. Refusing it keeps one pathological submission from emptying the
	// cache for everybody else.
	if len(module) > c.limit {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if el, ok := c.entries[key]; ok {
		c.bytes -= len(el.Value.(*cacheEntry).module)
		el.Value.(*cacheEntry).module = module
		c.bytes += len(module)
		c.order.MoveToFront(el)
		return
	}

	c.entries[key] = c.order.PushFront(&cacheEntry{key: key, module: module})
	c.bytes += len(module)

	for c.bytes > c.limit {
		oldest := c.order.Back()
		if oldest == nil {
			break
		}
		entry := oldest.Value.(*cacheEntry)
		c.order.Remove(oldest)
		delete(c.entries, entry.key)
		c.bytes -= len(entry.module)
	}
}

// Len and Bytes exist for tests and for a future metric.
func (c *MemoryCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

func (c *MemoryCache) Bytes() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.bytes
}
