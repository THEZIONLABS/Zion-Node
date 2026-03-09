# Contributing to Zion Node

Thank you for your interest in contributing to Zion Node! This document provides guidelines for contributing.

## Getting Started

1. **Fork** the repository
2. **Clone** your fork locally
3. **Create a branch** for your change: `git checkout -b feature/my-change`
4. **Make your changes** and add tests
5. **Run tests**: `make test`
6. **Commit** with a descriptive message
7. **Push** your branch and open a Pull Request

## Development Setup

### Prerequisites

- Go 1.21+
- Docker (for running agent containers)
- Linux or macOS (Windows via WSL2)

### Building

```bash
make build
```

### Running Tests

```bash
# Unit tests
make test

# With race detector
make test-race

# Lint
make lint
```

### Project Structure

```
cmd/zion-node/     Main entry point
internal/
  agent/           Agent container lifecycle management
  config/          Configuration loading and validation
  crypto/          Wallet and signature verification
  daemon/          Main daemon loop (heartbeat, command processing)
  hub/             Hub API client
  http/            HTTP client utilities
  logger/          Structured logging
  snapshot/        Agent state snapshot management
  tui/             Terminal UI dashboard
pkg/types/         Shared type definitions
```

## Code Style

- Follow standard Go conventions (`gofmt`, `go vet`)
- Use `golangci-lint` for additional checks
- Write table-driven tests where appropriate
- Use interface-based design for testability
- Wrap errors with context: `fmt.Errorf("doing X: %w", err)`

## Commit Messages

Use [Conventional Commits](https://www.conventionalcommits.org/):

```
feat(agent): add snapshot restore support
fix(hub): handle connection timeout during heartbeat
docs: update configuration reference
test(daemon): add command signing edge cases
```

## Pull Request Process

1. Ensure all tests pass
2. Update documentation if your change affects user-facing behavior
3. Add test coverage for new functionality
4. Keep PRs focused — one logical change per PR

## Reporting Issues

- Use GitHub Issues for bug reports and feature requests
- Include reproduction steps, expected/actual behavior, and environment details
- Check existing issues before creating a new one

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
