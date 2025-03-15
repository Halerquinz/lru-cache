package lrucache

import "time"

// Cache is the interface for the LRU cache
type Cache interface {
	// Get retrieves the value associated with the given key
	Get(key any) any

	// Put adds a key-value pair to the cache
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

	// InitializeCapacity sets the initial capacity of the cache
	InitializeCapacity int

	// Pin indicates whether the cache entries should be pinned (not evicted)
	Pin bool

	// RemoveFunc is a callback function that is called when an entry is removed from the cache
	RemoveFunc RemoveFunc

	// TineNow is a function that returns the current time, used for TTL calculations
	TineNow func() time.Time
}

// RemoveFunc is a callback function type for removed cache entries
type RemoveFunc func(any)

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
