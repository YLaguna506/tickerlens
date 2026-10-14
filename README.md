# TickerLens

[![CI](https://github.com/YLaguna506/tickerlens/actions/workflows/ci.yml/badge.svg)](https://github.com/YLaguna506/tickerlens/actions/workflows/ci.yml)

Enter a US ticker and TickerLens runs a multi-stage research pipeline (data ingestion → metrics → AI analysts → verdict). It then renders a report with a **Sell / Pass / Hold / Watch / Buy** verdict.

> **Educational project. Not financial advice.** Nothing here is a recommendation to buy or sell any security.

**Status:** early development. The data layer works: SEC financials, Tiingo prices and Alpha Vantage EPS estimates, available from a CLI. Metrics and scoring come next.

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

### Fetch data from the command line

With the stack running (`task up`) and keys in `.env`:

```bash
task cli -- fetch V
```

This prints normalized annual, quarterly and trailing-twelve-month financials from SEC filings, along with which XBRL concept each number came from. It also prints a 5-year price summary and the last 8 quarters of EPS versus analyst estimates. Add `-json` for the full data, or `-no-cache` to run without Postgres.

Responses are cached in Postgres (SEC data and prices for a day, earnings for a week). Alpha Vantage's 25-requests-per-day free quota is tracked so it is never exceeded.

**How the financials are built:** each value is the most recently filed figure for its period, so restatements win. Quarters that companies only report as year-to-date totals are derived by subtraction (for example, Q4 = full year − nine months), and the source says so. Values that can't be found or derived are left empty, never guessed. Test fixtures for Visa, Apple and Snowflake are recorded with `go run ./tools/fixturegen` (in `backend/`) and checked against numbers from the companies' filings.

## Repository layout

```
backend/   Go module: API, workers, CLI
agents/    Python package: AI analyst agents
web/       React + Vite frontend
```

## License

[MIT](LICENSE)
