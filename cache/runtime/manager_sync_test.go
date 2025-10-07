package runtime

import (
	"context"
	"testing"
	"time"

	miniredis "github.com/alicebob/miniredis/v2"
	redis "github.com/redis/go-redis/v9"

	redisbackend "github.com/entitycache/entitycache/cache/backends/redis"
)

func TestManagerHydratesOnRemoteUpdate(t *testing.T) {
	backend, shutdown := newRuntimeRedisBackend(t)
	defer shutdown()

	ctx := context.Background()

	managerA, err := NewManager(backend, WithNamespace("sync"))
	if err != nil {
		t.Fatalf("NewManager (A) returned error: %v", err)
	}
	defer managerA.Close()

	managerB, err := NewManager(backend, WithNamespace("sync"))
	if err != nil {
		t.Fatalf("NewManager (B) returned error: %v", err)
	}
	defer managerB.Close()

	created := time.Now().UTC().Truncate(time.Second)

	updates := make(chan struct{}, 1)
	userA := sampleUser{ID: "user-42", Name: "Initial", Created: created}
	_, err = managerA.Register(ctx, "user:42", &userA, WithOnUpdate(func() {
		select {
		case updates <- struct{}{}:
		default:
		}
	}))
	if err != nil {
		t.Fatalf("managerA.Register returned error: %v", err)
	}

	userB := sampleUser{ID: "user-42", Name: "Initial", Created: created}
	if _, err := managerB.Register(ctx, "user:42", &userB); err != nil {
		t.Fatalf("managerB.Register returned error: %v", err)
	}

	userB.Name = "Remote"
	userB.Count = 99
	userB.Address = sampleAddress{Street: "42 Sync St", City: "Gopher City"}
	if err := managerB.Update(ctx, "user:42", &userB); err != nil {
		t.Fatalf("managerB.Update returned error: %v", err)
	}

	select {
	case <-updates:
	case <-time.After(2 * time.Second):
		t.Fatalf("did not receive remote update notification")
	}

	if userA.Name != "Remote" {
		t.Fatalf("expected userA.Name to update, got %s", userA.Name)
	}
	if userA.Count != 99 {
		t.Fatalf("expected userA.Count=99, got %d", userA.Count)
	}
	if userA.Address.City != "Gopher City" {
		t.Fatalf("expected userA address city updated, got %s", userA.Address.City)
	}
}

func TestManagerHandlesRemoteInvalidate(t *testing.T) {
	backend, shutdown := newRuntimeRedisBackend(t)
	defer shutdown()

	ctx := context.Background()

	managerA, err := NewManager(backend)
	if err != nil {
		t.Fatalf("NewManager (A) returned error: %v", err)
	}
	defer managerA.Close()

	managerB, err := NewManager(backend)
	if err != nil {
		t.Fatalf("NewManager (B) returned error: %v", err)
	}
	defer managerB.Close()

	invalidated := make(chan struct{}, 1)
	userA := sampleUser{ID: "remote", Name: "Subscriber"}
	_, err = managerA.Register(ctx, "remote", &userA, WithOnInvalidate(func() {
		select {
		case invalidated <- struct{}{}:
		default:
		}
	}))
	if err != nil {
		t.Fatalf("managerA.Register returned error: %v", err)
	}

	userB := sampleUser{ID: "remote", Name: "Publisher"}
	if _, err := managerB.Register(ctx, "remote", &userB); err != nil {
		t.Fatalf("managerB.Register returned error: %v", err)
	}

	if err := managerB.Invalidate(ctx, "remote"); err != nil {
		t.Fatalf("managerB.Invalidate returned error: %v", err)
	}

	select {
	case <-invalidated:
	case <-time.After(2 * time.Second):
		t.Fatalf("did not receive remote invalidation notification")
	}
}

func newRuntimeRedisBackend(t *testing.T) (*redisbackend.Backend, func()) {
	t.Helper()

	srv, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}

	client := redis.NewClient(&redis.Options{Addr: srv.Addr()})

	backend, err := redisbackend.NewBackend(client)
	if err != nil {
		t.Fatalf("NewBackend failed: %v", err)
	}

	return backend, func() {
		_ = client.Close()
		srv.Close()
	}
}
