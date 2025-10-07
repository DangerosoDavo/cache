package redisbackend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	redis "github.com/redis/go-redis/v9"

	"github.com/entitycache/entitycache/cache/core"
)

const (
	fieldData    = "data"
	fieldFormat  = "format"
	fieldVersion = "version"

	defaultChannelPrefix = "entitycache::"
)

// Option configures backend behavior.
type Option func(*config)

type config struct {
	channelPrefix string
}

// WithChannelPrefix overrides the prefix used for pub/sub channels.
func WithChannelPrefix(prefix string) Option {
	return func(cfg *config) {
		cfg.channelPrefix = prefix
	}
}

// Backend provides Redis-backed Cache implementation.
type Backend struct {
	client        redis.UniversalClient
	channelPrefix string
}

// NewBackend constructs a backend around an existing redis client.
func NewBackend(client redis.UniversalClient, opts ...Option) (*Backend, error) {
	if client == nil {
		return nil, errors.New("redisbackend: client is nil")
	}

	cfg := config{
		channelPrefix: defaultChannelPrefix,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	return &Backend{
		client:        client,
		channelPrefix: cfg.channelPrefix,
	}, nil
}

// NewBackendWithOptions creates a Redis client using go-redis options and wraps it with Backend.
func NewBackendWithOptions(options *redis.Options, opts ...Option) (*Backend, error) {
	if options == nil {
		return nil, errors.New("redisbackend: redis options are required")
	}
	client := redis.NewClient(options)
	return NewBackend(client, opts...)
}

// Set stores the payload and metadata in Redis.
func (b *Backend) Set(ctx context.Context, key string, payload core.Payload, meta core.Metadata) error {
	fields := map[string]any{
		fieldData:    payload.Data,
		fieldFormat:  payload.Format,
		fieldVersion: strconv.FormatInt(meta.Version, 10),
	}
	if err := b.client.HSet(ctx, key, fields).Err(); err != nil {
		return err
	}

	if meta.TTL > 0 {
		if err := b.client.Expire(ctx, key, meta.TTL).Err(); err != nil {
			return err
		}
	}
	return nil
}

// Get retrieves the payload and metadata from Redis.
func (b *Backend) Get(ctx context.Context, key string) (core.Payload, core.Metadata, error) {
	result, err := b.client.HGetAll(ctx, key).Result()
	if err != nil {
		return core.Payload{}, core.Metadata{}, err
	}
	if len(result) == 0 {
		return core.Payload{}, core.Metadata{}, core.ErrNotFound
	}

	meta := core.Metadata{}
	payload := core.Payload{}

	if data, ok := result[fieldData]; ok {
		payload.Data = []byte(data)
	}
	if format, ok := result[fieldFormat]; ok {
		payload.Format = format
		meta.Format = format
	}
	if versionStr, ok := result[fieldVersion]; ok {
		if version, err := strconv.ParseInt(versionStr, 10, 64); err == nil {
			meta.Version = version
		}
	}

	ttl, err := b.client.TTL(ctx, key).Result()
	if err == nil && ttl > 0 {
		meta.TTL = ttl
	}

	return payload, meta, nil
}

// Delete removes a key from Redis.
func (b *Backend) Delete(ctx context.Context, key string) error {
	return b.client.Del(ctx, key).Err()
}

// Publish sends a message to subscribers about an update/invalidation.
func (b *Backend) Publish(ctx context.Context, key string, msg core.Message) error {
	wire := wireMessage{
		Key:     key,
		Type:    string(msg.Type),
		Version: msg.Version,
		Format:  msg.Format,
	}
	payload, err := json.Marshal(wire)
	if err != nil {
		return err
	}

	return b.client.Publish(ctx, b.channelName(key), payload).Err()
}

// Subscribe listens for messages on the key-specific channel.
func (b *Backend) Subscribe(ctx context.Context, key string) (core.Subscription, error) {
	channel := b.channelName(key)
	pubsub := b.client.Subscribe(ctx, channel)

	subCtx, cancel := context.WithCancel(ctx)
	sub := &redisSubscription{
		pubsub: pubsub,
		ch:     make(chan core.Message),
		cancel: cancel,
	}

	go sub.forward(subCtx)
	return sub, nil
}

func (b *Backend) channelName(key string) string {
	if strings.Contains(key, " ") {
		key = strings.ReplaceAll(key, " ", "_")
	}
	return fmt.Sprintf("%s%s", b.channelPrefix, key)
}

type wireMessage struct {
	Key     string `json:"key"`
	Type    string `json:"type"`
	Version int64  `json:"version"`
	Format  string `json:"format,omitempty"`
}

type redisSubscription struct {
	pubsub *redis.PubSub
	ch     chan core.Message

	cancel    context.CancelFunc
	closeOnce sync.Once
}

func (s *redisSubscription) Channel() <-chan core.Message {
	return s.ch
}

func (s *redisSubscription) Close() error {
	var err error
	s.closeOnce.Do(func() {
		s.cancel()
		err = s.pubsub.Close()
		close(s.ch)
	})
	return err
}

func (s *redisSubscription) forward(ctx context.Context) {
	ch := s.pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			var wire wireMessage
			if err := json.Unmarshal([]byte(msg.Payload), &wire); err != nil {
				continue
			}
			s.ch <- core.Message{
				Key:     wire.Key,
				Type:    core.MessageType(wire.Type),
				Version: wire.Version,
				Format:  wire.Format,
			}
		}
	}
}

// Client exposes the underlying redis client.
func (b *Backend) Client() redis.UniversalClient {
	return b.client
}
