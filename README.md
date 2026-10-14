# TickerLens

[![CI](https://github.com/YLaguna506/tickerlens/actions/workflows/ci.yml/badge.svg)](https://github.com/YLaguna506/tickerlens/actions/workflows/ci.yml)

Enter a US ticker and TickerLens runs a multi-stage research pipeline (data ingestion → metrics → AI analysts → verdict). It then renders a report with a **Sell / Pass / Hold / Watch / Buy** verdict.

> **Educational project. Not financial advice.** Nothing here is a recommendation to buy or sell any security.

**Status:** early development. The repo skeleton, Docker setup and CI are in place; the data pipeline comes next.

## Design principles

- **Numbers are computed, never generated.** Every figure shown comes from source data (SEC filings, price history) and is calculated in Go. AI agents only interpret and explain, and must cite filing text.
- **Simple by design.** One Go process runs the API and the report pipeline. It calls a small Python service for the AI analysis and stores everything in Postgres. There's no queue or cache server to run.
- **Swappable providers.** Data sources and the LLM sit behind interfaces.

## Stack

| Layer | Tech |
|---|---|
| API, report pipeline, metrics | Go |
| AI agents (HTTP service) | Python, FastAPI, Pydantic |
| Storage and cache | PostgreSQL |
| Frontend | React, Vite, TypeScript |

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
