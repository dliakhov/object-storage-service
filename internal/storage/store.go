package storage

import "context"

// NotFoundError is returned by Get and Delete when the object does not exist.
// Defined as a struct type (not a var) to satisfy gochecknoglobals.
type NotFoundError struct{}

func (NotFoundError) Error() string { return "object not found" }

// InvalidInputError is returned by Put, Get, and Delete when a bucket or objectID name is invalid.
type InvalidInputError struct{ Msg string }

func (e InvalidInputError) Error() string { return e.Msg }

// Store is the persistence abstraction for objects organized by buckets.
type Store interface {
	Put(ctx context.Context, bucket, objectID string, data []byte) error
	Get(ctx context.Context, bucket, objectID string) ([]byte, error)
	Delete(ctx context.Context, bucket, objectID string) error
}
