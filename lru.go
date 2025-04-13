package lrucache

import (
	"container/list"
	"errors"
	"sync"
	"time"
)

// ErrCacheFull is returned when the cache is full and cannot accept more entries
var ErrCacheFull = errors.New("cache is full")

// lru implements a Least Recently Used cache
type lru struct {
	mu            sync.Mutex
	byKey         map[any]*list.Element
	byAccess      *list.List
	sizeByKey     map[any]uint64
	currSize      uint64
	maxSize       int
	pin           bool
	ttl           time.Duration
	activelyEvict bool
	TimeNow       func() time.Time
}

// entryImpl represents a cache entry implementation
type entryImpl struct {
	key        any
	value      any
	createTime time.Time
	refCount   int
}

// Key returns the key of the cache entry
func (e *entryImpl) Key() any { return e.key }

// Value returns the value of the cache entry
func (e *entryImpl) Value() any { return e.value }

// CreateTime returns the creation time of the cache entry
func (e *entryImpl) CreateTime() time.Time { return e.createTime }

// iteratorImpl implements the Iterator interface for the LRU cache
type iteratorImpl struct {
	lru        *lru
	nextItem   *list.Element
	createTime time.Time
}

// Close releases the lock on the LRU cache
func (it *iteratorImpl) Close() { it.lru.mu.Unlock() }

// HasNext checks if there are more entries to iterate over
func (it *iteratorImpl) HasNext() bool { return it.nextItem != nil }

// Next returns the next cache entry
func (it *iteratorImpl) Next() Entry {
	if it.nextItem == nil {
		panic("LRU cache iterator Next called when there is no next item")
	}

	entry := it.nextItem.Value.(*entryImpl)
	it.nextItem = it.nextItem.Next()
	// make a copy of the entry to avoid race condition when concurrent access
	entry = &entryImpl{
		key:        entry.key,
		value:      entry.value,
		createTime: entry.createTime,
	}
	it.prepareNext()
	return entry
}

// prepareNext prepares the next valid item for iteration
func (it *iteratorImpl) prepareNext() {
	for it.nextItem != nil {
		entry := it.nextItem.Value.(*entryImpl)
		if it.lru.isEntryExpired(entry, it.createTime) {
			nextItem := it.nextItem.Next()
			it.lru.deleteInternal(it.nextItem)
			it.nextItem = nextItem
		} else {
			return
		}
	}
}

// Iterator returns an iterator for the cache entries
func (c *lru) Iterator() Iterator {
	c.mu.Lock()
	iterator := &iteratorImpl{
		lru:        c,
		nextItem:   c.byAccess.Front(),
		createTime: c.TimeNow(),
	}
	iterator.prepareNext()
	return iterator
}

// New creates a new LRU cache with the given options
func New(opts *Options) Cache {
	if opts == nil {
		opts = &Options{}
	}
	if opts.TimeNow == nil {
		opts.TimeNow = time.Now
	}
	return &lru{
		byAccess:      list.New(),
		byKey:         make(map[any]*list.Element, opts.InitialCapacity),
		sizeByKey:     make(map[any]uint64, opts.InitialCapacity),
		ttl:           opts.TTL,
		maxSize:       opts.MaxSize,
		pin:           opts.Pin,
		TimeNow:       opts.TimeNow,
		activelyEvict: opts.ActivelyEvict,
	}
}

// Get retrieves the value associated with the given key
func (c *lru) Get(key any) any {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.evictExpiredItems()

	element := c.byKey[key]
	if element == nil {
		return nil
	}

	entry := element.Value.(*entryImpl)
	if c.isEntryExpired(entry, c.TimeNow()) {
		c.deleteInternal(element)
		return nil
	}
	if c.pin {
		entry.refCount++
	}

	c.byAccess.MoveToFront(element)
	return entry.value
}

// Put adds a key-value pair to the cache and returns the previous value if it existed
func (c *lru) Put(key any, value any) any {
	if c.pin {
		panic("Can not put if pin")
	}
	val, _ := c.putInternal(key, value, true)
	return val
}

// PutIfNotExist adds a key-value pair to the cache if the key does not already exist
func (c *lru) PutIfNotExist(key any, value any) (any, error) {
	existing, err := c.putInternal(key, value, false)
	if err != nil {
		return nil, err
	}
	if existing == nil { // put new value success
		return value, nil
	}
	return existing, nil
}

// Delete removes the entry associated with the given key
func (c *lru) Delete(key any) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.evictExpiredItems()
	element := c.byKey[key]
	if element != nil {
		c.deleteInternal(element)
	}
}

// Release decrements the reference count for the entry associated with the given key
func (c *lru) Release(key any) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.evictExpiredItems()
	element, ok := c.byKey[key]
	if !ok {
		return
	}
	entry := element.Value.(*entryImpl)
	entry.refCount--
}

// Size returns the number of entries in the cache
func (c *lru) Size() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.evictExpiredItems()
	return len(c.byKey)
}

// putInternal handles the internal logic for adding or updating cache entries
func (c *lru) putInternal(key any, value any, allowUpdate bool) (any, error) {
	valueSize := uint64(0)
	c.mu.Lock()
	defer c.mu.Unlock()

	c.evictExpiredItems()

	element := c.byKey[key]
	if element != nil {
		entry := element.Value.(*entryImpl)
		if c.isEntryExpired(entry, c.TimeNow()) {
			c.deleteInternal(element)
		} else {
			existing := entry.value
			if allowUpdate {
				for c.isCacheFull() {
					oldest := c.byAccess.Back().Value.(*entryImpl)
					if oldest.refCount > 0 {
						// Cache is full with pinned elements
						// so don't update
						return existing, ErrCacheFull
					}
					c.deleteInternal(c.byAccess.Back())
				}
				c.updateSizeOnAdd(key, valueSize)
				c.updateSizeOnDelete(key)

				entry.value = value
				if c.ttl != 0 {
					entry.createTime = c.TimeNow()
				}
			}
			c.byAccess.MoveToFront(element)
			if c.pin {
				entry.refCount++
			}
			return existing, nil
		}
	}

	entry := &entryImpl{
		key:   key,
		value: value,
	}
	if c.ttl != 0 {
		entry.createTime = c.TimeNow()
	}
	if c.pin {
		entry.refCount++
	}
	c.byKey[key] = c.byAccess.PushFront(entry)
	c.updateSizeOnAdd(key, valueSize)
	if c.isCacheFull() {
		oldest := c.byAccess.Back().Value.(*entryImpl)
		if oldest.refCount > 0 {
			// Cache is full with pinned elements
			// so revert the insertion
			c.deleteInternal(c.byAccess.Front())
			return nil, ErrCacheFull
		}
		c.deleteInternal(c.byAccess.Back())
	}

	return nil, nil
}

// isCacheFull checks if the cache has reached its maximum size
func (c *lru) isCacheFull() bool {
	return len(c.byKey) > c.maxSize
}

// evictExpiredItems removes expired entries from the cache
func (c *lru) evictExpiredItems() {
	if !c.activelyEvict {
		return
	}

	now := c.TimeNow()
	for element := c.byAccess.Back(); element != nil; element = c.byAccess.Back() {
		if !c.isEntryExpired(element.Value.(*entryImpl), now) {
			break // list is sorted by age ttl from front to back so we can stop soon.
		}
		c.deleteInternal(element)
	}
}

// deleteInternal removes an element from the cache
func (c *lru) deleteInternal(element *list.Element) {
	entry := c.byAccess.Remove(element).(*entryImpl)
	delete(c.byKey, entry.key)
	c.updateSizeOnDelete(entry.key)
}

// updateSizeOnAdd updates the cache size when adding an entry
func (c *lru) updateSizeOnAdd(key any, valueSize uint64) {
	c.currSize += uint64(valueSize)
	c.sizeByKey[key] = valueSize
}

// updateSizeOnDelete updates the cache size when deleting an entry
func (c *lru) updateSizeOnDelete(key any) {
	c.currSize -= uint64(c.sizeByKey[key])
	delete(c.sizeByKey, key)
}

// isEntryExpired checks if a cache entry has expired
func (c *lru) isEntryExpired(entry *entryImpl, currentTime time.Time) bool {
	// entry.createTime must not be zero because the default value of time.Time is zero when the cache is not configured with TTL.
	return entry.refCount == 0 && !entry.createTime.IsZero() && currentTime.After(entry.createTime.Add(c.ttl))
}
