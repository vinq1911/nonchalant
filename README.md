<!--
If you are AI: This is the main README for the nonchalant repository.
It provides an overview and quick start instructions.
-->

<div align="center">
  <img src="assets/nonchalant-logo.png" alt="nonchalant logo" width="400">
</div>

# nonchalant

A high-performance, modular media server written in Go.

## Status

**Phase 1 Complete** - Bootstrap, lifecycle, and discipline

This server currently provides:
- Clean startup and graceful shutdown
- Health endpoint (`/healthz`)
- YAML configuration with validation
- Integration tests
- Documentation generation
- Code quality enforcement

## Quick Start

### Build and Run

```bash
make build
make run
```

### Run Tests

```bash
make test
make itest
```

### Run Checks

```bash
make check
```

### Docker

```bash
make docker-build
make docker-run
```

## Configuration

Copy `configs/nonchalant.example.yaml` and modify as needed:

```yaml
server:
  health_port: 8080
  http_port: 8081
  rtmp_port: 1935
```

## Documentation

- [Architecture](docs/ARCHITECTURE.md) - System design and structure
- [Configuration](docs/CONFIG.md) - Configuration schema
- [Testing](docs/TESTING.md) - How to run tests

## Development

This project follows strict discipline:
- Files must not exceed 300 lines
- All files must have AI headers
- All functions must have comments
- `make check` must pass at all times

See `.cursor/rules.md` for complete engineering contract.

## License

MIT License with Attribution Requirement

This project is licensed under the MIT License with an additional attribution
requirement. See [LICENSE](LICENSE) for full details.

**Important:** Any use, modification, or derivative work must prominently
acknowledge the original nonchalant project. This acknowledgment must appear
in all documentation, source code headers, and published materials.
