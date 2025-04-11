package ttlmap

import (
	"fmt"
	"sync"
	"time"
)

type cleanerState string

const (
	stopped    cleanerState = "stopped"
	running                 = "running"
	processing              = "processing"
)

type cacheItem[V any] struct {
	data V
	ttl  time.Time
}

type cache[K comparable, V any] struct {
	data   map[K]cacheItem[V]
	mu     sync.RWMutex
	stopCh chan struct{}

	cs   cleanerState
	csMu sync.Mutex
}

func NewCache[T comparable, V any]() *cache[T, V] {
	return &cache[T, V]{
		data: make(map[T]cacheItem[V]),
		cs:   stopped,
	}
}

func NewCacheWithCleaner[T comparable, V any](t int) *cache[T, V] {
	c := NewCache[T, V]()
	_ = c.RunCleaner(t)
	return c
}

func (c *cache[T, V]) Set(key T, data V, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	ttlVal := time.Now().Add(ttl)
	if ttl == 0 {
		ttlVal = time.Time{}
	}

	c.data[key] = cacheItem[V]{
		data: data,
		ttl:  ttlVal,
	}
}

func (c *cache[T, V]) Delete(key T) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.data, key)
}

func (c *cache[T, V]) Get(key T) (value V, ok bool) {
	var zero V
	isExpired := false

	c.mu.RLock()

	defer func() {
		if isExpired {
			c.deleteExpired(key)
		}
	}()

	defer c.mu.RUnlock()

	item, ok := c.data[key]
	if !ok {
		return zero, false
	}

	if !item.ttl.IsZero() && item.ttl.Before(time.Now()) {
		isExpired = true
		return zero, false
	}

	return item.data, true
}

func (c *cache[T, V]) RunCleaner(t int) error {
	c.csMu.Lock()
	defer c.csMu.Unlock()

	if c.cs == running || c.cs == processing {
		return fmt.Errorf(`cleaner has state "%s"`, c.cs)
	}

	c.stopCh = make(chan struct{})
	c.cs = running

	ticker := time.NewTicker(time.Duration(t) * time.Second)

	go func() {
		for {
			select {
			case <-c.stopCh:
				c.csMu.Lock()
				c.cs = stopped
				c.csMu.Unlock()
				return
			case <-ticker.C:
				c.mu.Lock()
				for key, item := range c.data {
					if !item.ttl.IsZero() && item.ttl.Before(time.Now()) {
						delete(c.data, key)
					}
				}
				c.mu.Unlock()
			}
		}
	}()

	return nil
}

func (c *cache[T, V]) StopCleaner() error {
	c.csMu.Lock()
	defer c.csMu.Unlock()

	if c.cs == stopped || c.cs == processing {
		return fmt.Errorf(`cleaner has state "%s"`, c.cs)
	}

	c.cs = processing
	close(c.stopCh)
	return nil
}

func (c *cache[T, V]) deleteExpired(key T) {
	c.mu.Lock()
	defer c.mu.Unlock()

	item, ok := c.data[key]
	if !ok {
		return
	}

	if item.ttl.Before(time.Now()) {
		delete(c.data, key)
	}
}
