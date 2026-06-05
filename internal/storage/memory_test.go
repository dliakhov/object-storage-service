package storage_test

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"

	"github.com/dliakhov/object-storage-service/internal/storage"
)

func newStore(t *testing.T) *storage.MemoryStore {
	t.Helper()
	return storage.NewMemoryStore()
}

func isNotFound(err error) bool {
	var nf storage.NotFoundError
	return errors.As(err, &nf)
}

func TestMemoryStore_PutGet(t *testing.T) {
	t.Parallel()

	s := newStore(t)
	if err := s.Put(context.Background(), "b", "o", []byte("hello")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := s.Get(context.Background(), "b", "o")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
}

func TestMemoryStore_GetNotFound(t *testing.T) {
	t.Parallel()

	s := newStore(t)
	_, err := s.Get(context.Background(), "b", "missing")
	if !isNotFound(err) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestMemoryStore_GetMissingBucket(t *testing.T) {
	t.Parallel()

	s := newStore(t)
	_, err := s.Get(context.Background(), "no-such-bucket", "o")
	if !isNotFound(err) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestMemoryStore_DeleteThenGet(t *testing.T) {
	t.Parallel()

	s := newStore(t)
	if err := s.Put(context.Background(), "b", "o", []byte("data")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Delete(context.Background(), "b", "o"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err := s.Get(context.Background(), "b", "o")
	if !isNotFound(err) {
		t.Errorf("expected ErrNotFound after Delete, got %v", err)
	}
}

func TestMemoryStore_DeleteNotFound(t *testing.T) {
	t.Parallel()

	s := newStore(t)
	err := s.Delete(context.Background(), "b", "missing")
	if !isNotFound(err) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestMemoryStore_DeleteMissingBucket(t *testing.T) {
	t.Parallel()

	s := newStore(t)
	err := s.Delete(context.Background(), "no-such-bucket", "o")
	if !isNotFound(err) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestMemoryStore_Deduplication(t *testing.T) {
	t.Parallel()

	s := newStore(t)
	content := []byte("shared content")

	if err := s.Put(context.Background(), "b", "a", content); err != nil {
		t.Fatalf("Put a: %v", err)
	}
	if err := s.Put(context.Background(), "b", "b", content); err != nil {
		t.Fatalf("Put b: %v", err)
	}

	// Delete one; the other must still be accessible.
	if err := s.Delete(context.Background(), "b", "a"); err != nil {
		t.Fatalf("Delete a: %v", err)
	}

	got, err := s.Get(context.Background(), "b", "b")
	if err != nil {
		t.Fatalf("Get b after Delete a: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("got %q, want %q", got, content)
	}
}

func TestMemoryStore_DeduplicationBothDeleted(t *testing.T) {
	t.Parallel()

	s := newStore(t)
	content := []byte("shared")

	if err := s.Put(context.Background(), "b", "a", content); err != nil {
		t.Fatalf("Put a: %v", err)
	}
	if err := s.Put(context.Background(), "b", "b", content); err != nil {
		t.Fatalf("Put b: %v", err)
	}
	if err := s.Delete(context.Background(), "b", "a"); err != nil {
		t.Fatalf("Delete a: %v", err)
	}
	if err := s.Delete(context.Background(), "b", "b"); err != nil {
		t.Fatalf("Delete b: %v", err)
	}

	if _, err := s.Get(context.Background(), "b", "a"); !isNotFound(err) {
		t.Errorf("expected ErrNotFound for a, got %v", err)
	}
	if _, err := s.Get(context.Background(), "b", "b"); !isNotFound(err) {
		t.Errorf("expected ErrNotFound for b, got %v", err)
	}
}

func TestMemoryStore_Overwrite(t *testing.T) {
	t.Parallel()

	s := newStore(t)
	if err := s.Put(context.Background(), "b", "o", []byte("v1")); err != nil {
		t.Fatalf("Put v1: %v", err)
	}
	if err := s.Put(context.Background(), "b", "o", []byte("v2")); err != nil {
		t.Fatalf("Put v2: %v", err)
	}

	got, err := s.Get(context.Background(), "b", "o")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != "v2" {
		t.Errorf("got %q, want %q", got, "v2")
	}
}

func TestMemoryStore_OverwriteDedup(t *testing.T) {
	t.Parallel()

	// objectA and objectB share content. Re-put objectA with new content.
	// objectB must still return the original content.
	s := newStore(t)
	if err := s.Put(context.Background(), "b", "a", []byte("shared")); err != nil {
		t.Fatalf("Put a: %v", err)
	}
	if err := s.Put(context.Background(), "b", "b", []byte("shared")); err != nil {
		t.Fatalf("Put b: %v", err)
	}
	if err := s.Put(context.Background(), "b", "a", []byte("new content")); err != nil {
		t.Fatalf("Put a overwrite: %v", err)
	}

	gotA, err := s.Get(context.Background(), "b", "a")
	if err != nil {
		t.Fatalf("Get a: %v", err)
	}
	if string(gotA) != "new content" {
		t.Errorf("a: got %q, want %q", gotA, "new content")
	}

	gotB, err := s.Get(context.Background(), "b", "b")
	if err != nil {
		t.Fatalf("Get b: %v", err)
	}
	if string(gotB) != "shared" {
		t.Errorf("b: got %q, want %q", gotB, "shared")
	}
}

func TestMemoryStore_CrossBucketIsolation(t *testing.T) {
	t.Parallel()

	s := newStore(t)
	if err := s.Put(context.Background(), "bucket-a", "o", []byte("in a")); err != nil {
		t.Fatalf("Put bucket-a: %v", err)
	}

	_, err := s.Get(context.Background(), "bucket-b", "o")
	if !isNotFound(err) {
		t.Errorf("expected ErrNotFound for object in different bucket, got %v", err)
	}
}

func TestMemoryStore_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	s := newStore(t)
	const goroutines = 50

	// Exercise per-object locking: each goroutine operates on its own objectID
	// so Puts and Deletes on different objects run without contending on entry.mu.
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
		go func(n int) {
			defer wg.Done()
			id := "obj-" + strconv.Itoa(n)
			_ = s.Put(context.Background(), "b", id, []byte("data-"+strconv.Itoa(n)))
			_, _ = s.Get(context.Background(), "b", id)
			if n%2 == 0 {
				_ = s.Delete(context.Background(), "b", id)
			}
		}(i)
	}
	wg.Wait()

	// Exercise shared-hash contention: concurrent Puts of identical content
	// contend on blobsMu but not on entry.mu.
	wg.Add(goroutines)
	for i := range goroutines {
		go func(n int) {
			defer wg.Done()
			id := "shared-" + strconv.Itoa(n)
			_ = s.Put(context.Background(), "b", id, []byte("same content"))
			_, _ = s.Get(context.Background(), "b", id)
		}(i)
	}
	wg.Wait()
}

// TestMemoryStore_ConcurrentPutsDistinctObjects verifies that concurrent Puts
// to different objectIDs within the same bucket all complete correctly: after
// all goroutines finish, every object is readable and returns its own data.
func TestMemoryStore_ConcurrentPutsDistinctObjects(t *testing.T) {
	t.Parallel()

	const n = 100
	s := newStore(t)

	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func(id int) {
			defer wg.Done()
			_ = s.Put(context.Background(), "b", "obj-"+strconv.Itoa(id), []byte("data-"+strconv.Itoa(id)))
		}(i)
	}
	wg.Wait()

	for i := range n {
		key := "obj-" + strconv.Itoa(i)
		got, err := s.Get(context.Background(), "b", key)
		if err != nil {
			t.Errorf("Get %s: %v", key, err)
			continue
		}
		want := "data-" + strconv.Itoa(i)
		if string(got) != want {
			t.Errorf("Get %s: got %q, want %q", key, got, want)
		}
	}
}

// TestMemoryStore_ConcurrentGets verifies that concurrent Gets of the same
// object all return the same consistent data — no torn reads.
func TestMemoryStore_ConcurrentGets(t *testing.T) {
	t.Parallel()

	const n = 100
	s := newStore(t)
	if err := s.Put(context.Background(), "b", "obj", []byte("content")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Each goroutine stores its result at a unique index; no mutex needed since
	// no two goroutines share an index. wg.Wait() synchronizes before reads.
	results := make([]string, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func(idx int) {
			defer wg.Done()
			got, err := s.Get(context.Background(), "b", "obj")
			if err != nil {
				return
			}
			results[idx] = string(got)
		}(i)
	}
	wg.Wait()

	for i, got := range results {
		if got != "content" {
			t.Errorf("goroutine %d: got %q, want %q", i, got, "content")
		}
	}
}

// TestMemoryStore_ConcurrentDeduplication verifies that deduplication refcounts
// remain correct under concurrent Puts of identical content: the blob must
// survive as long as at least one objectID references it.
func TestMemoryStore_ConcurrentDeduplication(t *testing.T) {
	t.Parallel()

	const n = 50
	s := newStore(t)
	content := []byte("shared content")

	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func(id int) {
			defer wg.Done()
			_ = s.Put(context.Background(), "b", "obj-"+strconv.Itoa(id), content)
		}(i)
	}
	wg.Wait()

	// Delete all but the last; the blob must not be freed until refcount hits 0.
	for i := range n - 1 {
		if err := s.Delete(context.Background(), "b", "obj-"+strconv.Itoa(i)); err != nil {
			t.Fatalf("Delete obj-%d: %v", i, err)
		}
	}

	got, err := s.Get(context.Background(), "b", "obj-"+strconv.Itoa(n-1))
	if err != nil {
		t.Fatalf("Get last object after %d deletes: %v", n-1, err)
	}
	if string(got) != string(content) {
		t.Errorf("got %q, want %q", got, content)
	}
}

// TestMemoryStore_ConcurrentPutsSameObject verifies that when many goroutines
// race to Put different data to the same objectID, the store remains consistent:
// exactly one value wins, it matches one of the inputs, and a single Delete
// cleans up with no ghost references left behind.
func TestMemoryStore_ConcurrentPutsSameObject(t *testing.T) {
	t.Parallel()

	const n = 50
	s := newStore(t)

	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func(id int) {
			defer wg.Done()
			_ = s.Put(context.Background(), "b", "obj", []byte("data-"+strconv.Itoa(id)))
		}(i)
	}
	wg.Wait()

	got, err := s.Get(context.Background(), "b", "obj")
	if err != nil {
		t.Fatalf("Get after concurrent puts: %v", err)
	}
	valid := false
	for i := range n {
		if string(got) == "data-"+strconv.Itoa(i) {
			valid = true
			break
		}
	}
	if !valid {
		t.Errorf("data %q does not match any of the %d put values", got, n)
	}

	// A single Delete must remove it cleanly; a second Delete must return
	// NotFoundError, proving no ghost reference remains in the refcount.
	if err := s.Delete(context.Background(), "b", "obj"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(context.Background(), "b", "obj"); !isNotFound(err) {
		t.Errorf("expected NotFoundError after delete, got %v", err)
	}
	if err := s.Delete(context.Background(), "b", "obj"); !isNotFound(err) {
		t.Errorf("expected NotFoundError on second delete (no ghost ref), got %v", err)
	}
}

// TestMemoryStore_ConcurrentPutDelete verifies that interleaved concurrent Puts
// and Deletes on the same objectID leave the store in one of two consistent
// states: the object either exists with a valid value or does not exist at all.
func TestMemoryStore_ConcurrentPutDelete(t *testing.T) {
	t.Parallel()

	const n = 50
	s := newStore(t)
	if err := s.Put(context.Background(), "b", "obj", []byte("initial")); err != nil {
		t.Fatalf("Put initial: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(n * 2)
	for i := range n {
		go func(id int) {
			defer wg.Done()
			_ = s.Put(context.Background(), "b", "obj", []byte("data-"+strconv.Itoa(id)))
		}(i)
		go func() {
			defer wg.Done()
			_ = s.Delete(context.Background(), "b", "obj")
		}()
	}
	wg.Wait()

	got, err := s.Get(context.Background(), "b", "obj")
	if err != nil {
		// Object does not exist: a Delete ran last. This is a valid end state.
		if !isNotFound(err) {
			t.Errorf("unexpected error from Get: %v", err)
		}
		return
	}

	// Object exists: its data must be one of the values that was Put.
	valid := string(got) == "initial"
	for i := range n {
		if string(got) == "data-"+strconv.Itoa(i) {
			valid = true
			break
		}
	}
	if !valid {
		t.Errorf("data %q is not a valid put value", got)
	}

	// One Delete must clean it up completely.
	if err := s.Delete(context.Background(), "b", "obj"); err != nil {
		t.Fatalf("Delete after all ops: %v", err)
	}
	if _, err := s.Get(context.Background(), "b", "obj"); !isNotFound(err) {
		t.Errorf("expected NotFoundError after final delete, got %v", err)
	}
}

// TestMemoryStore_ConcurrentDistinctBuckets verifies that concurrent Puts to
// different buckets under the same objectID do not cross-contaminate: each
// bucket independently stores only what was Put into it.
func TestMemoryStore_ConcurrentDistinctBuckets(t *testing.T) {
	t.Parallel()

	const n = 50
	s := newStore(t)

	var wg sync.WaitGroup
	wg.Add(n * 2)
	for range n {
		go func() {
			defer wg.Done()
			_ = s.Put(context.Background(), "bucket-a", "obj", []byte("content-a"))
		}()
		go func() {
			defer wg.Done()
			_ = s.Put(context.Background(), "bucket-b", "obj", []byte("content-b"))
		}()
	}
	wg.Wait()

	gotA, err := s.Get(context.Background(), "bucket-a", "obj")
	if err != nil || string(gotA) != "content-a" {
		t.Errorf("bucket-a/obj: got %q, %v", gotA, err)
	}
	gotB, err := s.Get(context.Background(), "bucket-b", "obj")
	if err != nil || string(gotB) != "content-b" {
		t.Errorf("bucket-b/obj: got %q, %v", gotB, err)
	}
}
