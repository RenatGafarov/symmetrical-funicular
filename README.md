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

Exchange credentials (never commit to repo).

Variable names are formed as `{EXCHANGE_NAME}_API_KEY` and `{EXCHANGE_NAME}_API_SECRET`, where `{EXCHANGE_NAME}` is the uppercase name of the exchange from `config.yaml`:

```bash
# For exchanges.binance in config.yaml:
export BINANCE_API_KEY=your_api_key
export BINANCE_API_SECRET=your_api_secret

# For exchanges.bybit in config.yaml:
export BYBIT_API_KEY=your_api_key
export BYBIT_API_SECRET=your_api_secret
```

Or use `.env` file (recommended for local development):

```bash
cp .env.example .env
# Edit .env with your real API keys
source .env
```

Telegram notifications:

```bash
export TELEGRAM_BOT_TOKEN=your_bot_token
export TELEGRAM_CHAT_ID=your_chat_id
```

Redis (if not using defaults):

```bash
export REDIS_ADDR=localhost:6379
export REDIS_PASSWORD=your_password
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
