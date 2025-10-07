package runtime

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/entitycache/entitycache/cache/core"
)

type sampleAddress struct {
	Street string
	City   string
}

type sampleUser struct {
	ID      string
	Name    string
	Count   int
	Created time.Time
	Address sampleAddress
	Ignore  string `cache:"-"`
}

func TestManagerRegisterAndUnregister(t *testing.T) {
	backend := newMockBackend()
	manager, err := NewManager(backend, WithNamespace("ns"))
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}
	defer manager.Close()

	ctx := context.Background()
	user := sampleUser{ID: "123", Name: "Test", Created: time.Now()}

	handle, err := manager.Register(ctx, "user:123", &user)
	if err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	if got := len(backend.setCalls); got != 1 {
		t.Fatalf("expected 1 set call, got %d", got)
	}
	call := backend.setCalls[0]
	if call.key != "ns:user:123" {
		t.Fatalf("expected namespaced key ns:user:123, got %s", call.key)
	}
	if call.meta.Version != 1 {
		t.Fatalf("expected version 1, got %d", call.meta.Version)
	}
	if call.payload.Format != core.FormatJSON {
		t.Fatalf("expected JSON format, got %s", call.payload.Format)
	}

	if manager.registry.size() != 1 {
		t.Fatalf("expected registry size 1, got %d", manager.registry.size())
	}

	handle.Unregister()
	if manager.registry.size() != 0 {
		t.Fatalf("expected registry to be empty after unregister, got %d", manager.registry.size())
	}
}

func TestManagerRegisterRequiresPointer(t *testing.T) {
	manager, err := NewManager(newMockBackend())
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}
	defer manager.Close()

	ctx := context.Background()
	user := sampleUser{ID: "123"}

	if _, err := manager.Register(ctx, "user:123", user); !errors.Is(err, ErrNilTarget) {
		t.Fatalf("expected ErrNilTarget, got %v", err)
	}
}

func TestManagerUpdateIncrementsVersion(t *testing.T) {
	backend := newMockBackend()
	manager, err := NewManager(backend)
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}
	defer manager.Close()

	ctx := context.Background()
	user := sampleUser{ID: "123", Name: "One"}
	if _, err := manager.Register(ctx, "user:123", &user); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	user.Count = 42
	if err := manager.Update(ctx, "user:123", &user); err != nil {
		t.Fatalf("Update returned error: %v", err)
	}

	if got := len(backend.setCalls); got != 2 {
		t.Fatalf("expected 2 set calls, got %d", got)
	}

	call := backend.setCalls[1]
	if call.meta.Version != 2 {
		t.Fatalf("expected version 2 on update, got %d", call.meta.Version)
	}
}

func TestManagerInvalidate(t *testing.T) {
	backend := newMockBackend()
	manager, err := NewManager(backend)
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}
	defer manager.Close()

	ctx := context.Background()
	user := sampleUser{ID: "123", Name: "Two"}
	handle, err := manager.Register(ctx, "user:123", &user)
	if err != nil {
		t.Fatalf("Register returned error: %v", err)
	}

	notified := make(chan struct{}, 1)
	handle.OnInvalidate(func() {
		notified <- struct{}{}
	})

	if err := manager.Invalidate(ctx, "user:123"); err != nil {
		t.Fatalf("Invalidate returned error: %v", err)
	}

	select {
	case <-notified:
	case <-time.After(time.Second):
		t.Fatalf("expected invalidate callback to be invoked")
	}

	if len(backend.deleteKeys) != 1 || backend.deleteKeys[0] != "user:123" {
		t.Fatalf("expected delete call for user:123, got %v", backend.deleteKeys)
	}
	if manager.registry.size() != 0 {
		t.Fatalf("expected registry empty after invalidate, got %d", manager.registry.size())
	}
}

func TestManagerInvalidateUnknownKey(t *testing.T) {
	backend := newMockBackend()
	manager, err := NewManager(backend)
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}
	defer manager.Close()

	if err := manager.Invalidate(context.Background(), "missing"); !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("expected ErrUnknownKey, got %v", err)
	}
}

type setCall struct {
	key     string
	payload core.Payload
	meta    core.Metadata
}

type mockBackend struct {
	mu         sync.Mutex
	setCalls   []setCall
	deleteKeys []string
}

func newMockBackend() *mockBackend {
	return &mockBackend{}
}

func (m *mockBackend) Set(ctx context.Context, key string, payload core.Payload, meta core.Metadata) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.setCalls = append(m.setCalls, setCall{key: key, payload: payload, meta: meta})
	return nil
}

func (m *mockBackend) Get(ctx context.Context, key string) (core.Payload, core.Metadata, error) {
	return core.Payload{}, core.Metadata{}, errors.New("not implemented")
}

func (m *mockBackend) Delete(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleteKeys = append(m.deleteKeys, key)
	return nil
}

func (m *mockBackend) Publish(ctx context.Context, key string, msg core.Message) error {
	return nil
}

func (m *mockBackend) Subscribe(ctx context.Context, key string) (core.Subscription, error) {
	return &mockSubscription{ch: make(chan core.Message)}, nil
}

type mockSubscription struct {
	ch chan core.Message
}

func (s *mockSubscription) Channel() <-chan core.Message {
	return s.ch
}

func (s *mockSubscription) Close() error {
	close(s.ch)
	return nil
}
