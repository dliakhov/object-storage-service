package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	blobsSubdir   = "blobs"
	objectsSubdir = "objects"
)

type bucketLock struct {
	mu sync.RWMutex
}

// FileStore is a thread-safe file-backed Store with per-bucket locking
// and per-bucket content deduplication.
type FileStore struct {
	root    string
	mu      sync.RWMutex
	buckets map[string]*bucketLock
}

// NewFileStore returns a FileStore rooted at root, creating the directory if needed.
// root is resolved to an absolute path so that confine checks are reliable.
// Any stale temp files or orphaned blobs left by a prior crash are removed at startup.
func NewFileStore(root string) (*FileStore, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve storage root: %w", err)
	}
	if err := os.MkdirAll(abs, 0o750); err != nil {
		return nil, fmt.Errorf("create storage root: %w", err)
	}
	cleanupStale(abs)
	return &FileStore{root: abs, buckets: make(map[string]*bucketLock)}, nil
}

// isValidNameChar reports whether c is in the allowed set [a-zA-Z0-9._-].
func isValidNameChar(c rune) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') || c == '.' || c == '-' || c == '_'
}

// validateName rejects names that are empty, ".", "..", or contain characters
// outside [a-zA-Z0-9._-], preventing path traversal attacks.
func validateName(s string) error {
	if s == "" || s == "." || s == ".." {
		return InvalidInputError{Msg: fmt.Sprintf("invalid name %q", s)}
	}
	for _, c := range s {
		if !isValidNameChar(c) {
			return InvalidInputError{Msg: fmt.Sprintf("name %q contains invalid character %q", s, c)}
		}
	}
	return nil
}

// confine asserts that path is inside f.root — defence-in-depth against logic errors.
// Since validateName already blocks traversal characters, this should never fire in practice.
func (f *FileStore) confine(path string) error {
	root := filepath.Clean(f.root) + string(os.PathSeparator)
	if !strings.HasPrefix(filepath.Clean(path)+string(os.PathSeparator), root) {
		return fmt.Errorf("constructed path escapes storage root")
	}
	return nil
}

func (f *FileStore) getOrCreateBucketLock(name string) *bucketLock {
	f.mu.RLock()
	bl := f.buckets[name]
	f.mu.RUnlock()
	if bl != nil {
		return bl
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if bl = f.buckets[name]; bl == nil {
		bl = &bucketLock{}
		f.buckets[name] = bl
	}
	return bl
}

// lookupBucketLock returns the existing bucketLock for name, or nil.
// It never inserts a new map entry, so it is safe to call for non-existent buckets.
func (f *FileStore) lookupBucketLock(name string) *bucketLock {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.buckets[name]
}

// requireBucketLock returns the lock for a bucket that must already exist.
// If the in-memory map has no entry (e.g. after a restart) it checks disk before
// creating one, preventing unbounded map growth from reads on non-existent buckets.
func (f *FileStore) requireBucketLock(name string) *bucketLock {
	if bl := f.lookupBucketLock(name); bl != nil {
		return bl
	}
	if _, statErr := os.Stat(filepath.Join(f.root, name)); errors.Is(statErr, os.ErrNotExist) {
		return nil
	}
	return f.getOrCreateBucketLock(name)
}

func (f *FileStore) blobPath(bucket, hash string) string {
	return filepath.Join(f.root, bucket, blobsSubdir, hash)
}

func (f *FileStore) objectPath(bucket, objectID string) string {
	return filepath.Join(f.root, bucket, objectsSubdir, objectID)
}

// writeFileAtomic writes data to path via a temp-file + rename to prevent partial writes.
// The rename is atomic on POSIX filesystems (Linux, macOS).
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()

	_, writeErr := tmp.Write(data)
	closeErr := tmp.Close()
	if writeErr != nil {
		removeErr := os.Remove(tmpName) //nolint:gosec // G703: tmpName is from os.CreateTemp, not user-controlled
		return errors.Join(fmt.Errorf("write temp file: %w", writeErr), closeErr, removeErr)
	}
	if closeErr != nil {
		removeErr := os.Remove(tmpName) //nolint:gosec // G703: tmpName is from os.CreateTemp, not user-controlled
		return errors.Join(fmt.Errorf("close temp file: %w", closeErr), removeErr)
	}
	if chmodErr := os.Chmod(tmpName, 0o600); chmodErr != nil { //nolint:gosec // G703: tmpName is from os.CreateTemp, not user-controlled
		removeErr := os.Remove(tmpName) //nolint:gosec // G703: tmpName is from os.CreateTemp, not user-controlled
		return errors.Join(fmt.Errorf("chmod temp file: %w", chmodErr), removeErr)
	}
	if renameErr := os.Rename(tmpName, path); renameErr != nil { //nolint:gosec // G703: tmpName is from os.CreateTemp, not user-controlled
		removeErr := os.Remove(tmpName) //nolint:gosec // G703: tmpName is from os.CreateTemp, not user-controlled
		return errors.Join(fmt.Errorf("rename to target: %w", renameErr), removeErr)
	}
	return nil
}

// isBlobOrphan returns true if no object file in objDir references hash.
// Temp files (.tmp-*) are skipped. On any read error the entry is skipped
// (conservative: assume not orphan).
func isBlobOrphan(objDir, hash string) bool {
	entries, err := os.ReadDir(objDir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".tmp-") {
			continue
		}
		refPath := filepath.Join(objDir, entry.Name())
		refHash, err := os.ReadFile(refPath) //nolint:gosec // G304: refPath is within a validated, confined directory
		if err != nil {
			continue
		}
		if string(refHash) == hash {
			return false
		}
	}
	return true
}

// cleanupStale removes leftover .tmp-* files and orphaned blobs from all existing buckets.
// Called at startup to recover from crashes during writes.
func cleanupStale(root string) {
	buckets, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, b := range buckets {
		if !b.IsDir() {
			continue
		}
		bucket := b.Name()
		for _, sub := range []string{blobsSubdir, objectsSubdir} {
			dir := filepath.Join(root, bucket, sub)
			entries, dirErr := os.ReadDir(dir)
			if dirErr != nil {
				continue
			}
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), ".tmp-") {
					if rmErr := os.Remove(filepath.Join(dir, entry.Name())); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
						_ = rmErr // best-effort startup cleanup; failures are non-fatal
					}
				}
			}
		}
		blobDir := filepath.Join(root, bucket, blobsSubdir)
		objDir := filepath.Join(root, bucket, objectsSubdir)
		blobs, blobsErr := os.ReadDir(blobDir)
		if blobsErr != nil {
			continue
		}
		for _, blob := range blobs {
			if blob.IsDir() || strings.HasPrefix(blob.Name(), ".tmp-") {
				continue
			}
			if isBlobOrphan(objDir, blob.Name()) {
				if rmErr := os.Remove(filepath.Join(blobDir, blob.Name())); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
					_ = rmErr // best-effort startup cleanup; failures are non-fatal
				}
			}
		}
	}
}

// Put stores data under objectID in the given bucket.
// If objectID already exists, its content is replaced.
// Identical content within the same bucket is stored only once.
func (f *FileStore) Put(ctx context.Context, bucket, objectID string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateName(bucket); err != nil {
		return fmt.Errorf("invalid bucket: %w", err)
	}
	if err := validateName(objectID); err != nil {
		return fmt.Errorf("invalid objectID: %w", err)
	}

	hash := hashOf(data)
	bl := f.getOrCreateBucketLock(bucket)
	bl.mu.Lock()
	defer bl.mu.Unlock()

	blobDir := filepath.Join(f.root, bucket, blobsSubdir)
	objDir := filepath.Join(f.root, bucket, objectsSubdir)
	if err := f.confine(blobDir); err != nil {
		return err
	}
	if err := os.MkdirAll(blobDir, 0o750); err != nil {
		return fmt.Errorf("create blobs dir: %w", err)
	}
	if err := os.MkdirAll(objDir, 0o750); err != nil {
		return fmt.Errorf("create objects dir: %w", err)
	}

	// Write the blob only if not already present (content deduplication).
	blobFile := f.blobPath(bucket, hash)
	if _, statErr := os.Stat(blobFile); errors.Is(statErr, os.ErrNotExist) {
		if werr := writeFileAtomic(blobFile, data); werr != nil {
			return fmt.Errorf("write blob: %w", werr)
		}
	} else if statErr != nil {
		return fmt.Errorf("check blob: %w", statErr)
	}

	// Read current ref to detect no-op and enable old-blob cleanup on overwrite.
	objFile := f.objectPath(bucket, objectID)
	if err := f.confine(objFile); err != nil {
		return err
	}
	var oldHash string
	if prevRef, err := os.ReadFile(objFile); err == nil { //nolint:gosec // G304: objFile is within a validated, confined path
		oldHash = string(prevRef)
	}

	if oldHash == hash {
		return nil // already stored with this exact content
	}

	if err := writeFileAtomic(objFile, []byte(hash)); err != nil {
		return fmt.Errorf("write object ref: %w", err)
	}

	// Best-effort cleanup of the old blob when the overwrite orphans it.
	// Not returned on failure: Put has already committed; a leaked blob is only cosmetic.
	if oldHash != "" && isBlobOrphan(objDir, oldHash) {
		oldBlobFile := f.blobPath(bucket, oldHash)
		if f.confine(oldBlobFile) == nil {
			if rmErr := os.Remove(oldBlobFile); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
				_ = rmErr // intentionally swallowed — see comment above
			}
		}
	}

	return nil
}

// Get retrieves the data stored under objectID in the given bucket.
// Returns NotFoundError if the bucket or objectID does not exist.
// Returns an error if the stored blob fails its SHA-256 integrity check.
func (f *FileStore) Get(ctx context.Context, bucket, objectID string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateName(bucket); err != nil {
		return nil, fmt.Errorf("invalid bucket: %w", err)
	}
	if err := validateName(objectID); err != nil {
		return nil, fmt.Errorf("invalid objectID: %w", err)
	}

	bl := f.requireBucketLock(bucket)
	if bl == nil {
		return nil, NotFoundError{}
	}
	bl.mu.RLock()
	defer bl.mu.RUnlock()

	objFile := f.objectPath(bucket, objectID)
	if err := f.confine(objFile); err != nil {
		return nil, err
	}

	hashBytes, err := os.ReadFile(objFile) //nolint:gosec // G304: objFile is within a validated, confined path
	if errors.Is(err, os.ErrNotExist) {
		return nil, NotFoundError{}
	}
	if err != nil {
		return nil, fmt.Errorf("read object ref: %w", err)
	}

	hash := string(hashBytes)
	blobFile := f.blobPath(bucket, hash)
	if err := f.confine(blobFile); err != nil {
		return nil, fmt.Errorf("blob path integrity: %w", err)
	}
	data, err := os.ReadFile(blobFile) //nolint:gosec // G304: blobFile is within a validated, confined path
	if err != nil {
		return nil, fmt.Errorf("read blob: %w", err)
	}

	if hashOf(data) != hash {
		return nil, fmt.Errorf("blob integrity check failed for %s/%s", bucket, objectID)
	}

	return data, nil
}

// Delete removes objectID from the given bucket.
// Returns NotFoundError if the bucket or objectID does not exist.
// The underlying blob is freed when no other objectID in the bucket references it.
func (f *FileStore) Delete(ctx context.Context, bucket, objectID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateName(bucket); err != nil {
		return fmt.Errorf("invalid bucket: %w", err)
	}
	if err := validateName(objectID); err != nil {
		return fmt.Errorf("invalid objectID: %w", err)
	}

	bl := f.requireBucketLock(bucket)
	if bl == nil {
		return NotFoundError{}
	}
	bl.mu.Lock()
	defer bl.mu.Unlock()

	objFile := f.objectPath(bucket, objectID)
	if err := f.confine(objFile); err != nil {
		return err
	}

	hashBytes, err := os.ReadFile(objFile) //nolint:gosec // G304: objFile is within a validated, confined path
	if errors.Is(err, os.ErrNotExist) {
		return NotFoundError{}
	}
	if err != nil {
		return fmt.Errorf("read object ref: %w", err)
	}
	hash := string(hashBytes)

	if err := os.Remove(objFile); err != nil {
		return fmt.Errorf("remove object ref: %w", err)
	}

	objDir := filepath.Join(f.root, bucket, objectsSubdir)
	if isBlobOrphan(objDir, hash) {
		blobFile := f.blobPath(bucket, hash)
		if err := f.confine(blobFile); err != nil {
			return fmt.Errorf("blob path integrity: %w", err)
		}
		if err := os.Remove(blobFile); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove blob: %w", err)
		}
	}
	return nil
}
