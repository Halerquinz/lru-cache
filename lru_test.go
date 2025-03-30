package lrucache

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestLRU(t *testing.T) {
	cache := New(&Options{MaxSize: 5})

	cache.Put("A", "A")
	assert.Equal(t, "A", cache.Get("A"))
	assert.Nil(t, cache.Get("B"))
	assert.Equal(t, 1, cache.Size())

	cache.Put("B", "B")
	cache.Put("C", "C")
	cache.Put("D", "D")

	assert.Equal(t, "B", cache.Get("B"))
	assert.Equal(t, "C", cache.Get("C"))
	assert.Equal(t, "D", cache.Get("D"))
	assert.Equal(t, 4, cache.Size())

	cache.Put("A", "A2")
	assert.Equal(t, "A2", cache.Get("A"))
	cache.Put("E", "E")
	assert.Equal(t, "E", cache.Get("E"))
	assert.Equal(t, 5, cache.Size())

	cache.Put("F", "F")
	assert.Nil(t, cache.Get("B")) // B is evict why cache full size
	assert.Equal(t, "F", cache.Get("F"))
	assert.Equal(t, 5, cache.Size())

	cache.Get("C")
	cache.Put("G", "G")
	assert.Nil(t, cache.Get("D")) // D is lru
	assert.Equal(t, 5, cache.Size())

	cache.Delete("A")
	assert.Nil(t, cache.Get("A"))
	assert.Equal(t, 4, cache.Size())
}

type keyType struct {
	dumStr string
	dumInt int
}

func TestGeneric(t *testing.T) {
	key := keyType{
		dumStr: "random string",
		dumInt: 36,
	}
	value := "random value"

	cache := New(&Options{MaxSize: 5})
	cache.Put(key, value)

	assert.Equal(t, value, cache.Get(key))
	assert.Equal(t, value, cache.Get(keyType{
		dumStr: "random string",
		dumInt: 36,
	}))
	assert.Nil(t, cache.Get(keyType{
		dumStr: "other random string",
		dumInt: 36,
	}))

	cache.Put(&key, value)

	assert.Equal(t, value, cache.Get(&key))
	assert.Nil(t, cache.Get(&keyType{
		dumStr: "random string",
		dumInt: 36,
	}))
}

type simulatedClock struct {
	sync.Mutex
	currTime time.Time
}

func (c *simulatedClock) Now() time.Time {
	c.Lock()
	defer c.Unlock()
	if c.currTime.IsZero() {
		return time.Now()
	}
	return c.currTime
}

func (c *simulatedClock) Elapse(d time.Duration) time.Time {
	c.Lock()
	defer c.Unlock()
	c.currTime = c.currTime.Add(d)
	return c.currTime
}

func TestLRUWithTTL(t *testing.T) {
	clock := &simulatedClock{
		currTime: time.Now(),
	}
	cache := New(&Options{
		MaxSize: 5,
		TTL:     time.Hour * 2,
		TimeNow: clock.Now,
	})

	cache.Put("A", "A")
	assert.Equal(t, "A", cache.Get("A"))
	assert.Equal(t, 1, cache.Size())

	clock.Elapse(time.Hour)
	assert.Equal(t, "A", cache.Get("A"))
	assert.Equal(t, 1, cache.Size())

	clock.Elapse(time.Hour + time.Millisecond)
	assert.Nil(t, cache.Get("A"))
	assert.Equal(t, 0, cache.Size())
}

func TestIterator(t *testing.T) {
	cache := New(&Options{MaxSize: 5})
	expected := map[string]string{
		"A": "A",
		"B": "B",
		"C": "C",
		"D": "D",
		"E": "E",
	}

	for k, v := range expected {
		cache.Put(k, v)
	}

	actual := map[string]string{}
	// approach 1
	it := cache.Iterator()
	for it.HasNext() {
		entry := it.Next()
		actual[entry.Key().(string)] = entry.Value().(string)
	}
	it.Close()
	assert.Equal(t, expected, actual)

	// approach 2
	it = cache.Iterator()
	for range len(expected) {
		entry := it.Next()
		actual[entry.Key().(string)] = entry.Value().(string)
	}
	it.Close()
	assert.Equal(t, expected, actual)
}

func TestLRUCacheConcurrentAccess(t *testing.T) {
	cache := New(&Options{MaxSize: 5})
	values := map[string]string{
		"A": "A",
		"B": "B",
		"C": "C",
		"D": "D",
		"E": "E",
	}

	for k, v := range values {
		cache.Put(k, v)
	}
}
