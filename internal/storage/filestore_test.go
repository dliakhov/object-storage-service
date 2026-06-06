package storage_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/dliakhov/object-storage-service/internal/storage"
)

func newFileStore(t *testing.T) *storage.FileStore {
	t.Helper()
	dir := t.TempDir()
	s, err := storage.NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	return s
}

// blobHash returns the hex-encoded SHA-256 of data, matching the internal hash used by FileStore.
func blobHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestFileStore_PutGet(t *testing.T) {
	t.Parallel()

	s := newFileStore(t)
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

func TestFileStore_GetNotFound(t *testing.T) {
	t.Parallel()

	s := newFileStore(t)
	_, err := s.Get(context.Background(), "b", "missing")
	if !isNotFound(err) {
		t.Errorf("expected NotFoundError, got %v", err)
	}
}

func TestFileStore_GetMissingBucket(t *testing.T) {
	t.Parallel()

	s := newFileStore(t)
	_, err := s.Get(context.Background(), "no-such-bucket", "o")
	if !isNotFound(err) {
		t.Errorf("expected NotFoundError, got %v", err)
	}
}

func TestFileStore_DeleteThenGet(t *testing.T) {
	t.Parallel()

	s := newFileStore(t)
	if err := s.Put(context.Background(), "b", "o", []byte("data")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Delete(context.Background(), "b", "o"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err := s.Get(context.Background(), "b", "o")
	if !isNotFound(err) {
		t.Errorf("expected NotFoundError after Delete, got %v", err)
	}
}

func TestFileStore_DeleteNotFound(t *testing.T) {
	t.Parallel()

	s := newFileStore(t)
	err := s.Delete(context.Background(), "b", "missing")
	if !isNotFound(err) {
		t.Errorf("expected NotFoundError, got %v", err)
	}
}

func TestFileStore_DeleteMissingBucket(t *testing.T) {
	t.Parallel()

	s := newFileStore(t)
	err := s.Delete(context.Background(), "no-such-bucket", "o")
	if !isNotFound(err) {
		t.Errorf("expected NotFoundError, got %v", err)
	}
}

func TestFileStore_Deduplication(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	s, err := storage.NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	content := []byte("shared content")

	if err := s.Put(context.Background(), "b", "a", content); err != nil {
		t.Fatalf("Put a: %v", err)
	}
	if err := s.Put(context.Background(), "b", "b", content); err != nil {
		t.Fatalf("Put b: %v", err)
	}

	// One blob file on disk (deduplication).
	blobDir := filepath.Join(dir, "b", "blobs")
	entries, rerr := os.ReadDir(blobDir)
	if rerr != nil {
		t.Fatalf("ReadDir blobs: %v", rerr)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 blob file, got %d", len(entries))
	}

	// Delete one; the other must still be accessible.
	if err := s.Delete(context.Background(), "b", "a"); err != nil {
		t.Fatalf("Delete a: %v", err)
	}

	// Blob file must still exist (b still references it).
	entries, rerr = os.ReadDir(blobDir)
	if rerr != nil {
		t.Fatalf("ReadDir blobs after delete a: %v", rerr)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 blob file after deleting a, got %d", len(entries))
	}

	got, err := s.Get(context.Background(), "b", "b")
	if err != nil {
		t.Fatalf("Get b after Delete a: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("got %q, want %q", got, content)
	}
}

func TestFileStore_DeduplicationBothDeleted(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	s, err := storage.NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
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

	// Blob file must be removed once all refs are gone.
	blobDir := filepath.Join(dir, "b", "blobs")
	entries, rerr := os.ReadDir(blobDir)
	if rerr != nil {
		t.Fatalf("ReadDir blobs: %v", rerr)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 blob files after both deleted, got %d", len(entries))
	}

	if _, err := s.Get(context.Background(), "b", "a"); !isNotFound(err) {
		t.Errorf("expected NotFoundError for a, got %v", err)
	}
	if _, err := s.Get(context.Background(), "b", "b"); !isNotFound(err) {
		t.Errorf("expected NotFoundError for b, got %v", err)
	}
}

func TestFileStore_Overwrite(t *testing.T) {
	t.Parallel()

	s := newFileStore(t)
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

func TestFileStore_OverwriteDedup(t *testing.T) {
	t.Parallel()

	// objectA and objectB share content. Re-put objectA with new content.
	// objectB must still return the original content.
	dir := t.TempDir()
	s, err := storage.NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	if err := s.Put(context.Background(), "b", "a", []byte("shared")); err != nil {
		t.Fatalf("Put a: %v", err)
	}
	if err := s.Put(context.Background(), "b", "b", []byte("shared")); err != nil {
		t.Fatalf("Put b: %v", err)
	}
	if err := s.Put(context.Background(), "b", "a", []byte("new content")); err != nil {
		t.Fatalf("Put a overwrite: %v", err)
	}

	// Old "shared" blob must still exist because b still references it.
	sharedHash := blobHash([]byte("shared"))
	if _, statErr := os.Stat(filepath.Join(dir, "b", "blobs", sharedHash)); statErr != nil {
		t.Errorf("shared blob should still exist: %v", statErr)
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

func TestFileStore_OverwriteOrphanBlobCleaned(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	s, err := storage.NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	if err := s.Put(context.Background(), "b", "o", []byte("v1")); err != nil {
		t.Fatalf("Put v1: %v", err)
	}
	if err := s.Put(context.Background(), "b", "o", []byte("v2")); err != nil {
		t.Fatalf("Put v2: %v", err)
	}

	// v1 blob must be cleaned up because no object references it anymore.
	v1Hash := blobHash([]byte("v1"))
	if _, statErr := os.Stat(filepath.Join(dir, "b", "blobs", v1Hash)); !os.IsNotExist(statErr) {
		t.Errorf("v1 blob should be removed after overwrite, stat error: %v", statErr)
	}
}

func TestFileStore_CrossBucketIsolation(t *testing.T) {
	t.Parallel()

	s := newFileStore(t)
	if err := s.Put(context.Background(), "bucket-a", "o", []byte("in a")); err != nil {
		t.Fatalf("Put bucket-a: %v", err)
	}

	_, err := s.Get(context.Background(), "bucket-b", "o")
	if !isNotFound(err) {
		t.Errorf("expected NotFoundError for object in different bucket, got %v", err)
	}
}

func TestFileStore_PathTraversalRejected(t *testing.T) {
	t.Parallel()

	s := newFileStore(t)

	cases := []struct {
		bucket   string
		objectID string
	}{
		{"../evil", "o"},
		{"b", "../evil"},
		{"..", "o"},
		{"b", ".."},
		{"b/sub", "o"},
		{"b", "o/sub"},
		{"", "o"},
		{"b", ""},
	}
	for _, tc := range cases {
		if err := s.Put(context.Background(), tc.bucket, tc.objectID, []byte("x")); err == nil {
			t.Errorf("Put(%q, %q): expected error for traversal input, got nil", tc.bucket, tc.objectID)
		}
		if _, err := s.Get(context.Background(), tc.bucket, tc.objectID); err == nil {
			t.Errorf("Get(%q, %q): expected error for traversal input, got nil", tc.bucket, tc.objectID)
		}
		if err := s.Delete(context.Background(), tc.bucket, tc.objectID); err == nil {
			t.Errorf("Delete(%q, %q): expected error for traversal input, got nil", tc.bucket, tc.objectID)
		}
	}
}

func TestFileStore_BlobIntegrityCheck(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	s, err := storage.NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}

	content := []byte("integrity test")
	if err := s.Put(context.Background(), "b", "o", content); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Directly corrupt the blob file on disk.
	hash := blobHash(content)
	blobPath := filepath.Join(dir, "b", "blobs", hash)
	if writeErr := os.WriteFile(blobPath, []byte("corrupted data"), 0o600); writeErr != nil {
		t.Fatalf("corrupt blob: %v", writeErr)
	}

	_, err = s.Get(context.Background(), "b", "o")
	if err == nil {
		t.Error("Get: expected integrity error, got nil")
	}
	if isNotFound(err) {
		t.Errorf("Get: expected integrity error, not NotFoundError, got %v", err)
	}
}

func TestFileStore_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	s := newFileStore(t)
	const goroutines = 50

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
}

func TestFileStore_ConcurrentPutsDistinctObjects(t *testing.T) {
	t.Parallel()

	const n = 100
	s := newFileStore(t)

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

func TestFileStore_ConcurrentGets(t *testing.T) {
	t.Parallel()

	const n = 100
	s := newFileStore(t)
	if err := s.Put(context.Background(), "b", "obj", []byte("content")); err != nil {
		t.Fatalf("Put: %v", err)
	}

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

func TestFileStore_ConcurrentDeduplication(t *testing.T) {
	t.Parallel()

	const n = 50
	s := newFileStore(t)
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

func TestFileStore_ConcurrentPutsSameObject(t *testing.T) {
	t.Parallel()

	const n = 50
	s := newFileStore(t)

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

func TestFileStore_ConcurrentPutDelete(t *testing.T) {
	t.Parallel()

	const n = 50
	s := newFileStore(t)
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
		if !isNotFound(err) {
			t.Errorf("unexpected error from Get: %v", err)
		}
		return
	}

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

	if err := s.Delete(context.Background(), "b", "obj"); err != nil {
		t.Fatalf("Delete after all ops: %v", err)
	}
	if _, err := s.Get(context.Background(), "b", "obj"); !isNotFound(err) {
		t.Errorf("expected NotFoundError after final delete, got %v", err)
	}
}

func TestFileStore_ConcurrentDistinctBuckets(t *testing.T) {
	t.Parallel()

	const n = 50
	s := newFileStore(t)

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

func TestFileStore_PersistsAcrossRestart(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	s1, err := storage.NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	if err := s1.Put(context.Background(), "b", "o", []byte("persistent")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Second instance simulates a restart — the in-memory bucket lock map starts empty.
	s2, err := storage.NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore restart: %v", err)
	}

	got, err := s2.Get(context.Background(), "b", "o")
	if err != nil {
		t.Fatalf("Get after restart: %v", err)
	}
	if string(got) != "persistent" {
		t.Errorf("got %q, want %q", got, "persistent")
	}

	if err := s2.Delete(context.Background(), "b", "o"); err != nil {
		t.Fatalf("Delete after restart: %v", err)
	}
	if _, err := s2.Get(context.Background(), "b", "o"); !isNotFound(err) {
		t.Errorf("expected NotFoundError after delete, got %v", err)
	}
}

func TestFileStore_StartupCleansOrphanedBlobs(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	blobDir := filepath.Join(dir, "b", "blobs")
	if err := os.MkdirAll(blobDir, 0o750); err != nil {
		t.Fatalf("MkdirAll blobs: %v", err)
	}
	objDir := filepath.Join(dir, "b", "objects")
	if err := os.MkdirAll(objDir, 0o750); err != nil {
		t.Fatalf("MkdirAll objects: %v", err)
	}

	// Simulate a crash-stranded blob: blob file with no object ref.
	orphanContent := []byte("orphaned content")
	orphanBlob := filepath.Join(blobDir, blobHash(orphanContent))
	if err := os.WriteFile(orphanBlob, orphanContent, 0o600); err != nil {
		t.Fatalf("WriteFile orphan: %v", err)
	}

	if _, err := storage.NewFileStore(dir); err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}

	if _, statErr := os.Stat(orphanBlob); !os.IsNotExist(statErr) {
		t.Errorf("orphaned blob should be removed at startup, stat error: %v", statErr)
	}
}

func TestFileStore_StartupCleansTempFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	for _, sub := range []string{"blobs", "objects"} {
		subDir := filepath.Join(dir, "b", sub)
		if err := os.MkdirAll(subDir, 0o750); err != nil {
			t.Fatalf("MkdirAll %s: %v", sub, err)
		}
		tmpFile := filepath.Join(subDir, ".tmp-leftover")
		if err := os.WriteFile(tmpFile, []byte("stale"), 0o600); err != nil {
			t.Fatalf("WriteFile .tmp-: %v", err)
		}
	}

	if _, err := storage.NewFileStore(dir); err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}

	for _, sub := range []string{"blobs", "objects"} {
		tmpFile := filepath.Join(dir, "b", sub, ".tmp-leftover")
		if _, statErr := os.Stat(tmpFile); !os.IsNotExist(statErr) {
			t.Errorf(".tmp-* file in %s should be removed at startup", sub)
		}
	}
}

func TestFileStore_Check(t *testing.T) {
	t.Parallel()

	t.Run("valid root returns nil", func(t *testing.T) {
		t.Parallel()
		s := newFileStore(t)
		if err := s.Check(context.Background()); err != nil {
			t.Errorf("Check: %v", err)
		}
	})

	t.Run("removed root returns error", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		s, err := storage.NewFileStore(dir)
		if err != nil {
			t.Fatalf("NewFileStore: %v", err)
		}
		if err := os.RemoveAll(dir); err != nil {
			t.Fatalf("RemoveAll: %v", err)
		}
		if err := s.Check(context.Background()); err == nil {
			t.Error("expected error for missing root, got nil")
		}
	})
}

func TestFileStore_InvalidInputErrorType(t *testing.T) {
	t.Parallel()

	s := newFileStore(t)

	cases := []struct {
		bucket   string
		objectID string
	}{
		{"../evil", "o"},
		{"b", "../evil"},
		{"bad~name", "o"},
		{"b", "bad~name"},
	}
	for _, tc := range cases {
		putErr := s.Put(context.Background(), tc.bucket, tc.objectID, []byte("x"))
		var inputErr storage.InvalidInputError
		if !errors.As(putErr, &inputErr) {
			t.Errorf("Put(%q, %q): expected InvalidInputError, got %T: %v", tc.bucket, tc.objectID, putErr, putErr)
		}

		_, getErr := s.Get(context.Background(), tc.bucket, tc.objectID)
		if !errors.As(getErr, &inputErr) {
			t.Errorf("Get(%q, %q): expected InvalidInputError, got %T: %v", tc.bucket, tc.objectID, getErr, getErr)
		}

		delErr := s.Delete(context.Background(), tc.bucket, tc.objectID)
		if !errors.As(delErr, &inputErr) {
			t.Errorf("Delete(%q, %q): expected InvalidInputError, got %T: %v", tc.bucket, tc.objectID, delErr, delErr)
		}
	}
}
