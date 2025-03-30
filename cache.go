package lrucache

import "time"

// Cache is the interface for the LRU cache
type Cache interface {
	// Get retrieves the value associated with the given key
	Get(key any) any

	// Put adds a key-value pair to the cache return value
	Put(key any, value any) any

	// PutIfNotExist adds a key-value pair to the cache if the key does not already exist
	PutIfNotExist(key any, value any) (any, error)

	// Delete removes the entry associated with the given key
	Delete(key any)

	// Release releases the entry associated with the given key
	Release(key any)

	// Iterator returns the iterator of the cache
	Iterator() Iterator

	// Size returns the number of entries in the cache
	Size() int
}

// Options represents the configuration options for the LRU cache
type Options struct {
	// TTL specifies the time-to-live duration for cache entries
	TTL time.Duration

	// InitialCapacity sets the initial capacity of the cache
	InitialCapacity int

	// Pin indicates whether the cache entries should be pinned (not evicted)
	Pin bool

	// MaxSize defines the maximum number of entries the cache can hold
	MaxSize int

	// SizeFunc is a function to calculate the size of a cache item
	SizeFunc GetCacheItemSizeFunc

	// ActivelyEvict determines whether the cache should actively evict entries
	ActivelyEvict bool

	// TimeNow is a function that returns the current time, used for TTL calculations
	TimeNow func() time.Time
}

type GetCacheItemSizeFunc func(any) uint64

// Iterator is the interface for iterating over cache entries
type Iterator interface {
	// Close closes the iterator
	Close()

	// HasNext checks if there are more entries to iterate over
	HasNext() bool

	// Next returns the next cache entry
	Next() Entry
}

// Entry represents a single cache entry
type Entry interface {
	// Key returns the key of the cache entry
	Key() any

	// Value returns the value of the cache entry
	Value() any

	// CreateTime returns the creation time of the cache entry
	CreateTime() time.Time
}
