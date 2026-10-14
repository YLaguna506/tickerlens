# TickerLens

[![CI](https://github.com/YLaguna506/tickerlens/actions/workflows/ci.yml/badge.svg)](https://github.com/YLaguna506/tickerlens/actions/workflows/ci.yml)

Enter a US ticker and TickerLens runs a multi-stage research pipeline (data ingestion → metrics → AI analysts → verdict). It then renders a report with a **Sell / Pass / Hold / Watch / Buy** verdict.

> **Educational project. Not financial advice.** Nothing here is a recommendation to buy or sell any security.

**Status:** early development. The repo skeleton, local infrastructure and CI are in place; the data pipeline comes next.

## Design principles

- **Numbers are computed, never generated.** Every figure shown comes from source data (SEC filings, price history) and is calculated in Go. AI agents only interpret and explain, and must cite filing text.
- **Message-driven pipeline.** Stages communicate through a durable queue, so a crashed worker resumes where it left off.
- **Swappable providers.** Data sources, the queue and the LLM sit behind interfaces.

## Stack

| Layer | Tech |
|---|---|
| API, pipeline workers, metrics | Go |
| AI agents | Python, FastAPI, Pydantic |
| Queue | NATS JetStream |
| Storage and cache | PostgreSQL, Redis |
| Frontend | React, Vite, TypeScript |
| Tracing | OpenTelemetry, Jaeger |

Data comes from free sources: SEC EDGAR (financials and filings), Tiingo (prices) and Alpha Vantage (EPS estimates).

## Getting started

### Prerequisites

- [Docker Desktop](https://www.docker.com/products/docker-desktop/)
- [Task](https://taskfile.dev/installation/) (`winget install Task.Task` on Windows)

For running tests and linters outside Docker you also need:

- [Go](https://go.dev/dl/) 1.27+ and [golangci-lint](https://golangci-lint.run/welcome/install/) v2
- [uv](https://docs.astral.sh/uv/) (installs Python 3.12 for you)
- [Node.js](https://nodejs.org/) 24 LTS

### Run it

```bash
task up
```

The first run creates `.env` from `.env.example`. Add your API keys there. Then open:

| Service | URL |
|---|---|
| Web app | http://localhost:3000 |
| API health | http://localhost:8080/healthz |
| Agents health | http://localhost:8000/healthz |
| Jaeger (traces) | http://localhost:16686 |
| NATS monitoring | http://localhost:8222 |

Stop everything with `task down`.

### Develop

```bash
task setup   # install Python and web dependencies
task test    # Go, Python and web checks
task lint    # golangci-lint, ruff + mypy, eslint
```

Run `task --list` to see every task.

## Repository layout

```
backend/   Go module: API, workers, CLI
agents/    Python package: AI analyst agents
web/       React + Vite frontend
```

## License

[MIT](LICENSE)
