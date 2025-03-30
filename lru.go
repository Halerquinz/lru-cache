package lrucache

import (
	"container/list"
	"errors"
	"sync"
	"time"
)

var ErrCacheFull = errors.New("cache is full")

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

type entryImpl struct {
	key        any
	value      any
	createTime time.Time
	refCount   int
}

func (e *entryImpl) Key() any { return e.key }

func (e *entryImpl) Value() any { return e.value }

func (e *entryImpl) CreateTime() time.Time { return e.createTime }

type iteratorImpl struct {
	lru        *lru
	nextItem   *list.Element
	createTime time.Time
}

func (it *iteratorImpl) Close() { it.lru.mu.Unlock() }

func (it *iteratorImpl) HasNext() bool { return it.nextItem != nil }

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

func (c *lru) Put(key any, value any) any {
	if c.pin {
		panic("Can not put if pin")
	}
	val, _ := c.putInternal(key, value, true)
	return val
}

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

func (c *lru) Delete(key any) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.evictExpiredItems()
	element := c.byKey[key]
	if element != nil {
		c.deleteInternal(element)
	}
}

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

func (c *lru) Size() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.evictExpiredItems()
	return len(c.byKey)
}

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

func (c *lru) isCacheFull() bool {
	return len(c.byKey) > c.maxSize
}

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

func (c *lru) deleteInternal(element *list.Element) {
	entry := c.byAccess.Remove(element).(*entryImpl)
	delete(c.byKey, entry.key)
	c.updateSizeOnDelete(entry.key)
}

func (c *lru) updateSizeOnAdd(key any, valueSize uint64) {
	c.currSize += uint64(valueSize)
	c.sizeByKey[key] = valueSize
}

func (c *lru) updateSizeOnDelete(key any) {
	c.currSize -= uint64(c.sizeByKey[key])
	delete(c.sizeByKey, key)
}

func (c *lru) isEntryExpired(entry *entryImpl, currentTime time.Time) bool {
	// entry.createTime must not be Zero because the default value of time.Time is Zero.
	return entry.refCount == 0 && !entry.createTime.IsZero() && currentTime.After(entry.createTime.Add(c.ttl))
}
