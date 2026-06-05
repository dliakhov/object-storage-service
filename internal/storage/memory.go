package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
)

type objectEntry struct {
	mu   sync.RWMutex
	hash string // "" means this object is not stored
}

type bucketState struct {
	indexMu sync.RWMutex
	objects map[string]*objectEntry

	blobsMu  sync.RWMutex
	blobs    map[string][]byte
	refcount map[string]int
}

// MemoryStore is a thread-safe in-memory Store with per-object locking
// and per-bucket content deduplication.
type MemoryStore struct {
	mu      sync.RWMutex
	buckets map[string]*bucketState
}

// NewMemoryStore returns an initialized MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{buckets: make(map[string]*bucketState)}
}

// getOrCreateBucket returns the bucket for name, creating it if needed.
func (m *MemoryStore) getOrCreateBucket(name string) *bucketState {
	m.mu.RLock()
	b := m.buckets[name]
	m.mu.RUnlock()
	if b != nil {
		return b
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if b = m.buckets[name]; b == nil {
		b = &bucketState{
			objects:  make(map[string]*objectEntry),
			blobs:    make(map[string][]byte),
			refcount: make(map[string]int),
		}
		m.buckets[name] = b
	}
	return b
}

// getOrCreateEntry returns the objectEntry for objectID in b, creating it if needed.
func (b *bucketState) getOrCreateEntry(objectID string) *objectEntry {
	b.indexMu.RLock()
	e := b.objects[objectID]
	b.indexMu.RUnlock()
	if e != nil {
		return e
	}

	b.indexMu.Lock()
	defer b.indexMu.Unlock()
	if e = b.objects[objectID]; e == nil {
		e = &objectEntry{}
		b.objects[objectID] = e
	}
	return e
}

func hashOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Put stores data under objectID in the given bucket.
// If objectID already exists, its content is replaced.
// Identical content within the same bucket is stored only once.
func (m *MemoryStore) Put(_ context.Context, bucket, objectID string, data []byte) error {
	hash := hashOf(data)
	b := m.getOrCreateBucket(bucket)
	e := b.getOrCreateEntry(objectID)

	e.mu.Lock()
	oldHash := e.hash
	if oldHash == hash {
		e.mu.Unlock()
		return nil
	}
	e.hash = hash

	b.blobsMu.Lock()
	if oldHash != "" {
		b.refcount[oldHash]--
		if b.refcount[oldHash] == 0 {
			delete(b.blobs, oldHash)
			delete(b.refcount, oldHash)
		}
	}
	b.refcount[hash]++
	if b.refcount[hash] == 1 {
		blob := make([]byte, len(data))
		copy(blob, data)
		b.blobs[hash] = blob
	}
	b.blobsMu.Unlock()

	e.mu.Unlock()

	return nil
}

// Get retrieves the data stored under objectID in the given bucket.
// Returns NotFoundError if the bucket or objectID does not exist.
func (m *MemoryStore) Get(_ context.Context, bucket, objectID string) ([]byte, error) {
	m.mu.RLock()
	b := m.buckets[bucket]
	m.mu.RUnlock()
	if b == nil {
		return nil, NotFoundError{}
	}

	b.indexMu.RLock()
	e := b.objects[objectID]
	b.indexMu.RUnlock()
	if e == nil {
		return nil, NotFoundError{}
	}

	e.mu.RLock()
	hash := e.hash
	if hash == "" {
		e.mu.RUnlock()
		return nil, NotFoundError{}
	}

	b.blobsMu.RLock()
	data := b.blobs[hash]
	b.blobsMu.RUnlock()

	e.mu.RUnlock()

	return data, nil
}

// Delete removes objectID from the given bucket.
// Returns NotFoundError if the bucket or objectID does not exist.
// The underlying blob is freed when no other objectID in the bucket references it.
func (m *MemoryStore) Delete(_ context.Context, bucket, objectID string) error {
	m.mu.RLock()
	b := m.buckets[bucket]
	m.mu.RUnlock()
	if b == nil {
		return NotFoundError{}
	}

	b.indexMu.RLock()
	e := b.objects[objectID]
	b.indexMu.RUnlock()
	if e == nil {
		return NotFoundError{}
	}

	e.mu.Lock()
	hash := e.hash
	if hash == "" {
		e.mu.Unlock()
		return NotFoundError{}
	}
	e.hash = ""

	b.blobsMu.Lock()
	b.refcount[hash]--
	if b.refcount[hash] == 0 {
		delete(b.blobs, hash)
		delete(b.refcount, hash)
	}
	b.blobsMu.Unlock()

	e.mu.Unlock()

	return nil
}
