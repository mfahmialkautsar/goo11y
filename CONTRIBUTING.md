# Contributing

## Getting Started

1. Fork the repository and create your branch from `main`.
2. Install Go 1.23 or later.
3. Run `go mod tidy` to ensure dependencies are in sync.

## Testing

1. Start the Grafana MLTP stack by running the composite action `.github/actions/start-mltp-stack` or replicating the steps from that action locally.
2. Run `go test ./...`.
3. Run `go vet ./...` before submitting changes.

## Code Style

- Keep changes small and focused.
- Add unit tests for new functionality.
- Avoid introducing dependencies unless necessary for the feature.

## Submitting Changes

1. Ensure the CI, Report Card, OpenSSF Scorecard, OpenSSF Best Practices, and Fuzzing workflows succeed for your branch.
2. Open a pull request with a clear description of the change and testing performed.
