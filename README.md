# Entity Cache

Reusable Go cache manager that serializes structs, stores them in remote caches (Redis first), and keeps in-memory objects synchronized via pub/sub hydration.

## Quick Start

```bash
go get github.com/entitycache/entitycache
```

```go
client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
backend, _ := redisbackend.NewBackend(client)
manager, _ := runtime.NewManager(
    backend,
    runtime.WithNamespace("profiles"),
    runtime.WithDefaultTTL(15*time.Minute),
)
defer manager.Close()

profile := Profile{ID: "user-1", Name: "Example"}
_, _ = manager.Register(ctx, profile.ID, &profile,
    runtime.WithOnUpdate(func() { log.Printf("updated: %#v", profile) }),
    runtime.WithOnInvalidate(func() { log.Printf("invalidated %s", profile.ID) }),
)

profile.Name = "Updated"
_ = manager.Update(ctx, profile.ID, &profile)

_ = manager.Invalidate(ctx, profile.ID)
```

See [`docs/USAGE.md`](docs/USAGE.md) for more scenarios and configuration tips.

## Benchmarks

Benchmarks live under `benchmarks/` and can be run with:

```bash
go test -bench=. ./benchmarks
```

## CI

GitHub Actions workflow runs `go test ./...` on pushes and pull requests. See [`docs/CI.md`](docs/CI.md).

## License

MIT
