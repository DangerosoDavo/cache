# Entity Cache Usage Guide

This document highlights the main workflows for integrating the cache runtime into your application.

## Setup

```go
client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
backend, _ := redisbackend.NewBackend(client)
manager, _ := runtime.NewManager(
    backend,
    runtime.WithNamespace("profiles"),
    runtime.WithDefaultTTL(15*time.Minute),
)
defer manager.Close()
```

## Registering an Object

```go
type Profile struct {
    ID    string `cache:"id"`
    Name  string
    Email string `cache:"email"`
}

profile := Profile{ID: "user-1", Name: "Example"}
handle, err := manager.Register(ctx, profile.ID, &profile,
    runtime.WithOnUpdate(func() { log.Printf("updated: %#v", profile) }),
    runtime.WithOnInvalidate(func() { log.Printf("invalidated %s", profile.ID) }),
)
if err != nil {
    // handle error
}
```

## Updating and Invalidating

```go
profile.Name = "Updated"
_ = manager.Update(ctx, profile.ID, &profile)

// Later, remove the entry
_ = manager.Invalidate(ctx, profile.ID)
```

## Logging

The manager uses a logger with the signature `Printf(string, ...any)`. By default it uses the standard library logger, but you can supply your own:

```go
manager, _ := runtime.NewManager(backend, runtime.WithLogger(myLogger))
```

`myLogger` can be any type that implements `Printf` (e.g., `*log.Logger`, zap's sugar logger, etc.).

## Defaults & Configuration Tips

- Namespace defaults to empty, so keys map directly to the values you pass to `Register`. Use `WithNamespace` when you need logical grouping or key separation.
- TTL defaults to zero (no expiration). Apply `WithDefaultTTL` for a global policy and `WithRegisterTTL` for per-object overrides.
- JSON is the default serializer through `core.NewJSONSerializer`. Inject your own via `WithSerializer` if you need a different format.
- Logging emits through Go's standard logger unless `WithLogger` is provided.
- Prefer setting callbacks when registering (`WithOnUpdate`, `WithOnInvalidate`) to ensure they are active before the first remote update arrives.

### Redis Client Recommendations

- Create a dedicated `go-redis` client for the cache layer; share it across managers instead of instantiating one per request.
- Size the pool according to expected concurrency (`PoolSize` ~10–20 per CPU core is a common starting point) and set `MinIdleConns` if you expect bursty load.
- Enable retries for transient network issues by configuring `MaxRetries` plus `MinRetryBackoff` / `MaxRetryBackoff`.
- Keep command timeouts (`ReadTimeout`, `WriteTimeout`) modest (e.g., a few hundred milliseconds) so stalled operations fail fast and surface through the logger.
