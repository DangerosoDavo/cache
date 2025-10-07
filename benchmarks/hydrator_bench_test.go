package benchmarks

import (
	"context"
	"testing"
	"time"

	"github.com/entitycache/entitycache/cache/core"
	"github.com/entitycache/entitycache/cache/runtime"
)

type hydratorBenchUser struct {
	ID        string
	Name      string
	Email     string
	CreatedAt time.Time
	Tags      []string
}

func newHydratorUser() hydratorBenchUser {
	return hydratorBenchUser{
		ID:        "user-456",
		Name:      "Hydration",
		Email:     "hydration@example.com",
		CreatedAt: time.Now().UTC(),
		Tags:      []string{"delta", "epsilon", "zeta"},
	}
}

func BenchmarkHydrationRoundTrip(b *testing.B) {
	serializer := core.NewJSONSerializer()
	user := newHydratorUser()
	meta, err := core.GetStructMetadata(user)
	if err != nil {
		b.Fatalf("metadata error: %v", err)
	}

	ctx := context.Background()

	backend := newMockBackend()
	manager, err := runtime.NewManager(backend)
	if err != nil {
		b.Fatalf("NewManager error: %v", err)
	}
	defer manager.Close()

	handleUser := newHydratorUser()
	if _, err := manager.Register(ctx, "hydration:user", &handleUser); err != nil {
		b.Fatalf("register error: %v", err)
	}

	payload, err := serializer.Serialize(ctx, meta, user)
	if err != nil {
		b.Fatalf("serialize error: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// simulate publish/update cycle without hitting Redis
		backend.pushMessage(runtimeMessage{
			key:     "hydration:user",
			msg:     core.Message{Key: "hydration:user", Type: core.MessageTypeUpdate, Version: int64(i + 1), Format: payload.Format},
			payload: payload,
			meta:    core.Metadata{Version: int64(i + 1), Format: payload.Format},
		})
		handleUser = newHydratorUser()
	}
}

// minimal mock backend for benchmarks

type runtimeMessage struct {
	key     string
	msg     core.Message
	payload core.Payload
	meta    core.Metadata
}

type mockBenchmarkBackend struct {
	messages chan runtimeMessage
}

func newMockBackend() *mockBenchmarkBackend {
	return &mockBenchmarkBackend{messages: make(chan runtimeMessage, 1024)}
}

func (m *mockBenchmarkBackend) pushMessage(msg runtimeMessage) {
	select {
	case m.messages <- msg:
	default:
	}
}

func (m *mockBenchmarkBackend) Set(ctx context.Context, key string, payload core.Payload, meta core.Metadata) error {
	return nil
}

func (m *mockBenchmarkBackend) Get(ctx context.Context, key string) (core.Payload, core.Metadata, error) {
	select {
	case msg := <-m.messages:
		return msg.payload, msg.meta, nil
	default:
		return core.Payload{}, core.Metadata{}, core.ErrNotFound
	}
}

func (m *mockBenchmarkBackend) Delete(ctx context.Context, key string) error {
	return nil
}

func (m *mockBenchmarkBackend) Publish(ctx context.Context, key string, msg core.Message) error {
	return nil
}

func (m *mockBenchmarkBackend) Subscribe(ctx context.Context, key string) (core.Subscription, error) {
	ch := make(chan core.Message)
	go func() {
		for {
			select {
			case <-ctx.Done():
				close(ch)
				return
			case msg := <-m.messages:
				if msg.key == key {
					ch <- msg.msg
				}
			}
		}
	}()

	return &mockSubscription{ch: ch}, nil
}

type mockSubscription struct {
	ch chan core.Message
}

func (s *mockSubscription) Channel() <-chan core.Message { return s.ch }
func (s *mockSubscription) Close() error                 { return nil }
