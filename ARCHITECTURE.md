# Entity Cache Package Architecture

## 1. Overview
The goal is to ship a reusable Go package that accepts user-defined structs, respects struct tags, and persists their data to remote caches. Redis is the primary backend, but the design keeps backends modular so other systems (Memcached, in-memory, distributed caches) can be added later. The package also maintains live, hydrated instances of registered structs by listening for pub/sub notifications and applying remote changes automatically.

## 2. Core Objectives
- Accept any struct and honor `cache` tags to exclude or rename fields before persistence.
- Serialize payloads to JSON by default, with optional binary serialization interfaces.
- Wrap cache backends behind a common interface; implement Redis first.
- Keep in-memory objects synchronized by subscribing to backend update events.
- Provide ergonomic APIs for registering, updating, and invalidating cached entries.
- Offer hooks for extensibility (custom serializers, field transformers, TTL overrides).

## 3. High-Level Architecture
### 3.1 Modules
- `cache/core`
  - `Cache` interface: `Set`, `Get`, `Delete`, `Subscribe`.
  - `Serializer` interface: `Serialize`, `Deserialize`, with metadata header support.
  - `TagConfig` and `FieldMapper`: inspect struct tags, compute cacheable field metadata.
  - `Hydrator`: apply cached payloads back onto struct instances through reflection.
- `cache/backends/redis`
  - Implementation of `Cache` using `github.com/redis/go-redis/v9`.
  - Namespace management, TTL handling, and key formatting helpers.
  - Pub/sub publisher and subscriber utilities for update channels.
- `cache/runtime`
  - `Manager`: orchestrates registration, updates, invalidation, and subscriptions.
  - `ObjectHandle`: tracks registered object pointers, metadata, callbacks, versioning.
  - `Registry`: thread-safe map of cache key to object handles.

### 3.2 Control Flow
1. User calls `Manager.Register(ctx, key, pointer, opts...)`.
2. `FieldMapper` inspects tags, filters fields, and builds metadata for serialization.
3. Current struct state is serialized via `Serializer` and written to the backend `Set`.
4. `Manager` starts/joins a subscription for the key prefix on the backend `Subscribe`.
5. Other writers call `Manager.Update` → new serialized payload → backend write → publish.
6. Subscribers receive update event, fetch latest payload, deserialize, and hydrate all registered handles for that key.
7. `Manager.Invalidate` removes the entry and broadcasts invalidation so all handles can react.

## 4. Struct Tag Semantics
- `cache:"-"` skips a field entirely.
- `cache:"name"` overrides the serialized field name.
- Optional `cache_ttl:"15m"` to override item TTL for specific fields (applied only when supported by backend).
- Nested structs are processed recursively; options allow shallow copy when deep hydration not desired.
- Provide hook interface for custom per-field encoders (e.g., encryption).
- Parse tags once during registration and store metadata for reuse.

## 5. Serialization Layer
- Default `JSONSerializer` uses `encoding/json` but ensures deterministic ordering if possible (marshal to map, sort keys before encoding when needed).
- Binary serializer interface is defined, but the v0 release ships without a built-in implementation; `encoding/gob` support is a follow-up once JSON path is stable.
- `Serializer` interface includes metadata header so payloads can carry `{version, format}` information.
- Allow users to inject custom serializer implementations (protobuf, flatbuffers, msgpack).
- JSON serializer maps structs to canonical JSON using struct metadata (cache tags respected, nested structs handled recursively) and supports deserializing back into structs to prepare for runtime hydration.

## 6. Cache Backend Abstraction
- `Cache` interface:
  ```go
  type Cache interface {
      Set(ctx context.Context, key string, data []byte, meta Metadata) error
      Get(ctx context.Context, key string) ([]byte, Metadata, error)
      Delete(ctx context.Context, key string) error
      Subscribe(ctx context.Context, topic string) (Subscription, error)
      Publish(ctx context.Context, topic string, msg Message) error
  }
  ```
- `Metadata` carries TTL, version, and serializer identifiers.
- `Subscription` wraps a typed message channel plus a `Close` method so backends can control lifecycle.
- Redis backend responsibilities:
  - Manage connection pool and context cancellation.
  - Use namespaced keys (`namespace:key`) and consistent channel names (`namespace:update:key`).
  - Persist payloads + metadata in Redis hashes (`data`, `format`, `version`) and honor TTL via `EXPIRE`.
  - Handle reconnection logic for pub/sub subscriptions with channel prefix `entitycache::` (configurable).
  - Encode version numbers within stored metadata and publish wire messages `{key,type,version,format}` for subscribers.
  - Connection guidance: prefer a dedicated go-redis client per service, cap `PoolSize` based on workload (e.g., 10–20 per CPU), enable `MinIdleConns` for steady traffic, and configure retry/backoff (`MaxRetries`, `MinRetryBackoff`, `MaxRetryBackoff`) to mask transient network hiccups.

## 7. Runtime Synchronization
- `Registry` keeps `map[string][]*ObjectHandle` with RWMutex protection.
- `ObjectHandle` stores:
  - Pointer to the original struct.
  - Metadata (field indices, setter functions).
  - Current version (monotonic counter) and optional user callbacks (`OnUpdate`, `OnInvalidate`).
- Manager scaffolding emits canonical cache keys (optionally namespaced), persists serialized snapshots, and bumps registry version counters on every register/update operation.
- Subscription goroutine:
  - Reads from backend channel.
  - For each message: ensure version is newer → call backend `Get` → deserialize → hydrate handles.
  - Apply hydration on a dedicated worker pool to avoid blocking subscription loop.
  - Dispatch `OnUpdate` callbacks sequentially per key to guarantee deterministic ordering for user code.
  - Log and continue on hydration/serialization errors so a single failure does not stall other subscribers.
- Hydration uses reflection to set exported fields only; optionally support private field updates via unsafe, gated behind configuration.

## 8. Public API Sketch
```go
type Manager struct {
    backend    Cache
    serializer Serializer
}

func NewManager(backend Cache, opts ...Option) *Manager
func (m *Manager) Register(ctx context.Context, key string, obj any, opts ...RegisterOption) (Handle, error)
func (m *Manager) Update(ctx context.Context, key string, obj any) error
func (m *Manager) Invalidate(ctx context.Context, key string) error

type Handle interface {
    Unregister()
    OnUpdate(func())
    OnInvalidate(func())
}
```

### Options
- `WithSerializer(Serializer)`
- `WithNamespace(string)`
- `WithDefaultTTL(time.Duration)`
- `WithLogger(Logger)` to integrate with host logging frameworks.
- `RegisterOption` helpers: `WithRegisterTTL(duration)`, `WithOnUpdate(func())`, `WithOnInvalidate(func())`.

Default behavior: namespace is empty (keys pass through unchanged), TTL is unlimited unless set, and the JSON serializer with standard logging is used when no custom options are supplied.

## 9. Testing Strategy
- Unit tests:
  - Tag parsing and metadata generation across nested structs.
  - Serialization and deserialization round trips (JSON and gob).
  - Hydrator updating struct fields, including skipped and renamed fields.
- Integration tests:
  - Start Redis via testcontainers (guarded by build tags).
  - Ensure multiple handles on different goroutines receive updates in order.
  - Validate pub/sub reconnection and version conflict handling.
  - Local backend tests use `miniredis` for fast, serverless validation of hash storage + pub/sub.
- Benchmarks:
  - Serialize/deserialize throughput.
  - Hydration performance for large structs.

## 10. Roadmap
1. Finalize API and option ergonomics.
2. Implement core metadata extraction, serializer abstraction, and hydration logic.
3. Build Redis backend with pub/sub support and version tracking.
4. Add runtime registry, handle management, and subscription loop.
5. Develop comprehensive test suite and CI configuration.
6. Document usage examples, best practices, and extension points.
7. Explore additional backends (in-memory LRU, Memcached) and advanced features (cache warmers, conflict resolution hooks).

## 11. Open Considerations
- Decide on hydration error handling (log and continue vs. propagate).
- Evaluate need for background refresh timers or expiration callbacks.
- Assess whether to support partial field updates (delta writes) in future iterations.
