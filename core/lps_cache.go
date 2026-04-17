package core

import "sync"

type LPSCache struct {
	mu    sync.RWMutex
	cache map[string][]int
}

func NewLPSCache() *LPSCache {
	return &LPSCache{
		cache: make(map[string][]int),
	}
}

func (c *LPSCache) Get(pattern []byte) []int {
	key := string(pattern)

	c.mu.RLock()
	lps, ok := c.cache[key]
	c.mu.RUnlock()

	// happy path
	if ok {
		return lps
	}

	// cache miss
	c.mu.Lock()
	defer c.mu.Unlock()

	// double check
	lps, ok = c.cache[key]
	if ok {
		return lps
	}

	lps = buildLPS(pattern)
	c.cache[key] = lps
	return lps
}

func buildLPS(pattern []byte) []int {
	m := len(pattern)
	lps := make([]int, m)
	length := 0

	for i := 1; i < m; i++ {
		for length > 0 && pattern[i] != pattern[length] {
			length = lps[length-1]
		}
		if pattern[i] == pattern[length] {
			length++
		}
		lps[i] = length
	}
	return lps
}
