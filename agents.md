# Agents Working Agreement

## Mission
- Deliver a reusable Go cache package that ingests structs, respects cache tags, supports pluggable serializers, and stays synchronized through backend pub/sub (Redis first).
- Maintain alignment with the architecture in `ARCHITECTURE.md`, refining it as implementation details emerge.

## Collaboration Principles
- Keep `ARCHITECTURE.md` and this document current as decisions change.
- Favor incremental, test-backed development; each phase should be independently verifiable.
- Surface uncertainties early (versioning model, hydration concurrency, serializer choices) and record resolutions here.
- Prefer interfaces and option patterns that enable future backends without refactors.

## Working Rhythm
1. Confirm goals and constraints before coding; document assumptions.
2. Break down work into phases (see Plan) and update status after each milestone.
3. Add tests alongside features; run integration tests against Redis before closing related tasks.
4. Review API ergonomics and documentation prior to release.

## Current Plan Snapshot
- Phase 0 – Prep: Requirements sign-off, serializer/backends decisions.
- Phase 1 – Core metadata & serialization.
- Phase 2 – Runtime foundations.
- Phase 3 – Redis backend.
- Phase 4 – Synchronization & versioning.
- Phase 5 – API polish & options.
- Phase 6 – Validation & delivery.

## Phase 1 Tasks
- [x] Finalize `Serializer` interface signature and implement default JSON path with deterministic encoding helper.
- [x] Build tag parsing and metadata cache covering exclusions, renames, and nested structs.
- [x] Define core structures (`FieldDescriptor`, `StructMetadata`) plus helper APIs for future hydration logic.
- [x] Stand up initial `cache/core` package layout with serializer and metadata modules.
- [x] Create unit tests for tag parsing and JSON round trips to lock correctness before integrating runtime components.

### Phase 1 Follow-ups
- [x] Decide on error reporting strategy for serialization/hydration edge cases (log and continue, surface via logger).
- [ ] Document JSON serializer usage and expectations for callers ahead of exposing public API.

## Phase 2 Tasks
- [x] Design runtime registry structures (`objectRegistry`, `objectHandle`) capturing versioning and callbacks.
- [x] Implement `Manager` scaffolding with `Register`, `Update`, and `Invalidate` signatures (logic stubbed for now).
- [x] Define backend-facing interfaces for pub/sub subscription handles and message envelopes.
- [x] Add smoke tests covering registration metadata usage and manager lifecycle without backend wiring.
- [x] Update documentation with runtime architecture details and note remaining Phase 2 work.

## Phase 3 Tasks
- [x] Establish Redis connection configuration (client options, pooled connection, context handling).
- [x] Implement Redis backend `Set`, `Get`, `Delete` with namespace + TTL behavior matching manager expectations.
- [x] Build pub/sub helper that subscribes to key channels, relays `core.Message` instances, and manages reconnection.
- [x] Provide integration test scaffolding (mock or embedded Redis) to validate cache round trips and message flow.
- [x] Document Redis backend design decisions, including version propagation via publish payloads.

## Phase 4 Tasks
- [x] Decide on hydration error handling strategy (log, propagate, or callback) and document outcome.
- [x] Implement subscription loop within `Manager` that consumes backend messages, fetches latest payload, and hydrates registered handles.
- [x] Extend runtime with sequential dispatcher to honor per-key ordering guarantees.
- [x] Add tests covering hydration on update/invalidation, including version mismatch handling.
- [x] Wire Redis backend into integration tests to validate end-to-end update propagation.

## Phase 5 Tasks
- [x] Evaluate public API surface; shortlist ergonomics improvements (option patterns, context usage, error types).
- [x] Add convenience helpers (e.g., register-time callbacks, logger injection) where they improve adoption.
- [x] Produce user-facing documentation snippets and examples for registration, updates, invalidation, and callbacks.
- [x] Review configuration story (options, TTL overrides, namespaces) and ensure defaults are intuitive.
- [x] Document logging expectations and customization via `WithLogger`.

## Phase 6 Tasks
- [x] Define CI strategy (miniredis unit tests vs. optional Redis container integration).
- [x] Add benchmark suite for serialization and hydration hot paths.
- [x] Prepare release notes / changelog summarizing API surface and configuration guidance.
- [x] Finalize README with quickstart, links to `docs/USAGE.md`, and example snippets.
- [x] Ensure repository metadata (module path, license) is complete for tagging v0.1.0.

## Tracking Decisions & Open Items
- Decisions:
  - Phase 0: Ship with JSON serializer only; expose interfaces so additional formats (e.g., gob) can plug in later.
  - Phase 0: Use monotonic version counters per key to detect stale updates.
  - Phase 0: Execute `OnUpdate` callbacks sequentially per key to give users deterministic ordering.
  - Phase 4: Hydration errors are logged and processing continues so callers can decide how to react via injected logger.
  - Phase 5: Keep defaults minimal—no namespace, no TTL, JSON serializer, std logger—with documentation steering users to options when needed.
- Open Questions:
  - Redis connection policy specifics (pool sizing, retry backoff).

## Next Update
- Document Redis connection policy once established and outline connection pooling/backoff guidance.
- Revisit configuration defaults after review of option ergonomics.
- Identify remaining Phase 6 validation items (CI strategy, additional benchmarks) before release.
