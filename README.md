# ArbitrageBot

High-frequency cryptocurrency arbitrage bot.

## Quick Start

```bash
# Build
make build

# Run
make run

# Run in dry-run mode (paper trading)
make run-dry
```

## Configuration

Copy example config and adjust:

```bash
cp configs/config.example.yaml configs/config.yaml
```

### Config Structure

| Section        | Required | Description                                      |
|----------------|----------|--------------------------------------------------|
| `app`          | Yes      | Application name, environment, log level         |
| `exchanges`    | Yes      | Exchange settings (fees, rate limits, websocket) |
| `pairs`        | Yes      | Trading pairs list                               |
| `orderbook`    | No       | Orderbook cache settings and Redis config        |
| `arbitrage`    | No       | Cross-exchange and triangular arbitrage params   |
| `execution`    | No       | Timeout and retry settings                       |
| `risk`         | No       | Position limits, loss limits, kill-switch        |
| `notification` | No       | Telegram alerts configuration                    |
| `metrics`      | No       | Prometheus metrics endpoint                      |
| `server`       | No       | HTTP server settings                             |

Optional sections can be omitted entirely. If not specified, they will be `nil` in the config.

### Environment Variables

Config values can be overridden with environment variables using `ARBITRAGE_` prefix:

```bash
export ARBITRAGE_APP_LOG_LEVEL=debug
export ARBITRAGE_SERVER_HTTP_PORT=9000
```

## CLI Flags

```bash
./arbitragebot --config configs/config.yaml      # specify config path
./arbitragebot --dry-run                         # paper trading mode
```

## Testing

Tests are located in the `tests/` directory:

```bash
make test                        # Run all tests
go test ./tests/config/...       # Run config tests only
go test -v ./tests/...           # Verbose output
```

## Make Targets

```bash
make build            # Build binary
make run              # Build and run
make run-dry          # Run in dry-run mode
make test             # Run tests
make test-integration # Run integration tests
make lint             # Run linter
make clean            # Remove build artifacts
make docker-build     # Build Docker image
make docker-run       # Run with Docker Compose
```
