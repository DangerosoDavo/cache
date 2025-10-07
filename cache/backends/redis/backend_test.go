package redisbackend

import (
	"context"
	"testing"
	"time"

	miniredis "github.com/alicebob/miniredis/v2"
	redis "github.com/redis/go-redis/v9"

	"github.com/entitycache/entitycache/cache/core"
)

func TestBackendSetGet(t *testing.T) {
	backend, shutdown := newTestBackend(t)
	defer shutdown()

	ctx := context.Background()
	payload := core.Payload{Format: core.FormatJSON, Data: []byte(`{"id":"1"}`)}

	err := backend.Set(ctx, "user:1", payload, core.Metadata{
		TTL:     5 * time.Second,
		Version: 3,
		Format:  payload.Format,
	})
	if err != nil {
		t.Fatalf("Set returned error: %v", err)
	}

	gotPayload, gotMeta, err := backend.Get(ctx, "user:1")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}

	if string(gotPayload.Data) != string(payload.Data) {
		t.Fatalf("expected payload %s, got %s", payload.Data, gotPayload.Data)
	}
	if gotMeta.Version != 3 {
		t.Fatalf("expected version 3, got %d", gotMeta.Version)
	}
	if gotMeta.Format != payload.Format {
		t.Fatalf("expected format %s, got %s", payload.Format, gotMeta.Format)
	}
	if gotMeta.TTL <= 0 || gotMeta.TTL > 5*time.Second {
		t.Fatalf("expected TTL within range, got %v", gotMeta.TTL)
	}
}

func TestBackendGetMissing(t *testing.T) {
	backend, shutdown := newTestBackend(t)
	defer shutdown()

	_, _, err := backend.Get(context.Background(), "missing")
	if err == nil || err != core.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestBackendDelete(t *testing.T) {
	backend, shutdown := newTestBackend(t)
	defer shutdown()

	ctx := context.Background()
	payload := core.Payload{Format: core.FormatJSON, Data: []byte(`{"id":2}`)}
	if err := backend.Set(ctx, "user:2", payload, core.Metadata{}); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}

	if err := backend.Delete(ctx, "user:2"); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}

	if _, _, err := backend.Get(ctx, "user:2"); err == nil || err != core.ErrNotFound {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestBackendPublishSubscribe(t *testing.T) {
	backend, shutdown := newTestBackend(t)
	defer shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	sub, err := backend.Subscribe(ctx, "user:3")
	if err != nil {
		t.Fatalf("Subscribe returned error: %v", err)
	}
	defer sub.Close()

	msg := core.Message{
		Key:     "user:3",
		Type:    core.MessageTypeUpdate,
		Version: 10,
		Format:  core.FormatJSON,
	}

	if err := backend.Publish(ctx, "user:3", msg); err != nil {
		t.Fatalf("Publish returned error: %v", err)
	}

	select {
	case received := <-sub.Channel():
		if received.Key != msg.Key || received.Type != msg.Type || received.Version != msg.Version {
			t.Fatalf("unexpected message %#v", received)
		}
	case <-ctx.Done():
		t.Fatalf("timed out waiting for message")
	}
}

func newTestBackend(t *testing.T) (*Backend, func()) {
	t.Helper()

	srv, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}

	client := redis.NewClient(&redis.Options{
		Addr: srv.Addr(),
	})

	backend, err := NewBackend(client)
	if err != nil {
		t.Fatalf("NewBackend failed: %v", err)
	}

	return backend, func() {
		_ = client.Close()
		srv.Close()
	}
}
