package executor

import (
	"strings"
	"testing"
)

func TestTheCacheReturnsWhatWasStored(t *testing.T) {
	c := NewMemoryCache(1024)
	c.Put("a", []byte("module"))

	got, ok := c.Get("a")
	if !ok {
		t.Fatal("stored module was not found")
	}
	if string(got) != "module" {
		t.Errorf("got %q", got)
	}
	if _, ok := c.Get("b"); ok {
		t.Error("found a module that was never stored")
	}
}

func TestTheCacheEvictsTheLeastRecentlyUsed(t *testing.T) {
	c := NewMemoryCache(30)

	c.Put("a", []byte(strings.Repeat("x", 10)))
	c.Put("b", []byte(strings.Repeat("x", 10)))

	// Touching "a" makes "b" the least recently used.
	c.Get("a")

	c.Put("c", []byte(strings.Repeat("x", 10)))
	c.Put("d", []byte(strings.Repeat("x", 10)))

	if _, ok := c.Get("b"); ok {
		t.Error("the least recently used entry survived eviction")
	}
	if _, ok := c.Get("d"); !ok {
		t.Error("the newest entry was evicted")
	}
	if c.Bytes() > 30 {
		t.Errorf("cache holds %d bytes, over its 30 byte limit", c.Bytes())
	}
}

// One oversized module must not empty the cache for everybody else.
func TestAModuleLargerThanTheBudgetIsRefused(t *testing.T) {
	c := NewMemoryCache(100)
	c.Put("small", []byte(strings.Repeat("x", 50)))
	c.Put("huge", []byte(strings.Repeat("x", 500)))

	if _, ok := c.Get("huge"); ok {
		t.Error("a module larger than the whole budget was stored")
	}
	if _, ok := c.Get("small"); !ok {
		t.Error("storing an oversized module evicted a valid entry")
	}
}

func TestReplacingAnEntryDoesNotDoubleCountItsBytes(t *testing.T) {
	c := NewMemoryCache(1000)
	c.Put("a", []byte(strings.Repeat("x", 100)))
	c.Put("a", []byte(strings.Repeat("x", 200)))

	if c.Len() != 1 {
		t.Errorf("entries = %d, want 1", c.Len())
	}
	if c.Bytes() != 200 {
		t.Errorf("bytes = %d, want 200", c.Bytes())
	}
}
