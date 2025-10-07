# Continuous Integration

## Default Workflow

- Runs on GitHub Actions for `push` and `pull_request` targeting `main`.
- Uses Go 1.22 via `actions/setup-go` with module cache enabled.
- Executes `go test ./...` with explicit `GOCACHE`/`GOMODCACHE` paths to keep artifacts inside the workspace.
- miniredis-backed tests run as part of the default suite; no external Redis dependency is required.

## Future Enhancements

- Add an optional integration job that spins up a Redis service container and runs targeted end-to-end tests behind a workflow dispatch flag.
- Publish benchmark results (once implemented) to GitHub Actions artifacts for performance tracking.
