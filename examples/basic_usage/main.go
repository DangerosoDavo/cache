package main

import (
	"context"
	"fmt"
	"log"
	"time"

	redis "github.com/redis/go-redis/v9"

	redisbackend "github.com/entitycache/entitycache/cache/backends/redis"
	"github.com/entitycache/entitycache/cache/runtime"
)

type Profile struct {
	ID        string `cache:"id"`
	Name      string
	Email     string    `cache:"email"`
	UpdatedAt time.Time `cache:"updated_at"`
}

func main() {
	ctx := context.Background()

	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	backend, err := redisbackend.NewBackend(client)
	if err != nil {
		log.Fatalf("create backend: %v", err)
	}

	manager, err := runtime.NewManager(backend, runtime.WithNamespace("profiles"), runtime.WithDefaultTTL(15*time.Minute))
	if err != nil {
		log.Fatalf("create manager: %v", err)
	}
	defer manager.Close()

	profile := Profile{ID: "u-100", Name: "Alice", Email: "alice@example.com", UpdatedAt: time.Now()}

	_, err = manager.Register(ctx, profile.ID, &profile,
		runtime.WithOnUpdate(func() {
			fmt.Printf("profile %s updated: %#v\n", profile.ID, profile)
		}),
		runtime.WithOnInvalidate(func() {
			fmt.Printf("profile %s invalidated\n", profile.ID)
		}),
	)
	if err != nil {
		log.Fatalf("register: %v", err)
	}

	profile.Name = "Alice Example"
	profile.UpdatedAt = time.Now()
	if err := manager.Update(ctx, profile.ID, &profile); err != nil {
		log.Fatalf("update: %v", err)
	}

	// Later on, when the profile should be purged:
	if err := manager.Invalidate(ctx, profile.ID); err != nil {
		log.Fatalf("invalidate: %v", err)
	}
}
