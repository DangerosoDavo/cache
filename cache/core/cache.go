package core

import (
	"context"
	"errors"
	"time"
)

// Metadata carries auxiliary information about a cached entry.
type Metadata struct {
	TTL     time.Duration
	Version int64
	Format  string
	Headers map[string]string
}

// MessageType identifies the kind of pub/sub message emitted by a cache backend.
type MessageType string

const (
	// MessageTypeUpdate indicates the underlying value has been updated.
	MessageTypeUpdate MessageType = "update"
	// MessageTypeInvalidate indicates the value has been removed or expired.
	MessageTypeInvalidate MessageType = "invalidate"
)

// Message represents an update notification from the cache backend.
type Message struct {
	Key     string
	Type    MessageType
	Version int64
	Format  string
}

// ErrNotFound indicates the key does not exist in the cache backend.
var ErrNotFound = errors.New("core: cache miss")

// Subscription provides a stream of cache change notifications.
type Subscription interface {
	Channel() <-chan Message
	Close() error
}

// Cache abstracts operations required by the runtime manager regardless of backend.
type Cache interface {
	Set(ctx context.Context, key string, payload Payload, meta Metadata) error
	Get(ctx context.Context, key string) (Payload, Metadata, error)
	Delete(ctx context.Context, key string) error
	Publish(ctx context.Context, key string, msg Message) error
	Subscribe(ctx context.Context, key string) (Subscription, error)
}
