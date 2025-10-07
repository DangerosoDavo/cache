# Changelog

## v0.1.0 (Unreleased)

- Core metadata extraction with struct-tag exclusions, TTL overrides, and nested struct support.
- Deterministic JSON serializer with recursive hydration and canonical output.
- Runtime manager for registration, update, invalidate flows plus automatic hydration via pub/sub.
- Redis backend (hash storage + pub/sub wiring) with configurable channel prefix.
- Register-time options: TTL overrides, update/invalidate callbacks, logger injection.
- Example app (`examples/basic_usage`) and usage guide (`docs/USAGE.md`).
- CI pipeline running `go test ./...` and benchmarks covering serialization/hydration paths.
