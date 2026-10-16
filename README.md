# TickerLens

[![CI](https://github.com/YLaguna506/tickerlens/actions/workflows/ci.yml/badge.svg)](https://github.com/YLaguna506/tickerlens/actions/workflows/ci.yml)

Enter a US ticker and TickerLens runs a multi-stage research pipeline (data ingestion → metrics → AI analysts → verdict). It then renders a report with a **Sell / Pass / Hold / Watch / Buy** verdict.

> **Educational project. Not financial advice.** Nothing here is a recommendation to buy or sell any security.

**Status:** working end to end. Open http://localhost:3000, type a ticker, and watch the report build live: business overview and quality from the latest 10-K, financial overview, lifecycle phase, valuation against the company's own history, and a verdict with its reasoning.

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

Data comes from free sources: SEC EDGAR (financials and filings), Tiingo (prices) and Alpha Vantage (EPS estimates). The LLM runs through [OpenRouter](https://openrouter.ai) (free models work) or OpenAI.

## How the AI agents stay honest

Each report runs three agents in parallel on the company's latest 10-K (Items 1 and 7):

| Agent | Produces |
|---|---|
| Business overview | What the company does, how it makes money, who pays and why |
| Business quality | Revenue predictability, pricing power, recession resistance, competitive position, each with a justification |
| Segments | Revenue by segment and by region, copied from the filing |

Every answer goes through checks before it's accepted:

1. **Schema:** the reply must match a strict JSON schema.
2. **Evidence:** every judgment cites a quote, and the quote must appear **word for word** in the filing (30 words max).
3. **Number guard:** every number in the prose must match a computed metric or a number inside the agent's own quotes. An invented figure fails.
4. **Segments:** each figure must appear in a quote, and the total must roughly match reported revenue.

A failed check gets one repair attempt, in which the agent is told exactly what was wrong. If that fails too, the section is left out and the report is marked partial. Scores are computed in Go from the agents' answers, not by the model.

`task eval` runs the agents on 10 companies and checks their answers against expected results (for example, that Visa's pricing power is "Easily").

## How the verdict is decided

The verdict is computed in Go from the scores. The LLM only writes the summary afterwards.

1. **Business score (B):** the weighted average of business quality (40%), growth (30%) and financial health (30%). If one is missing, the others are reweighted.
2. **Valuation score (V):** where today's multiple sits in its own 5-year history.
3. **Rules,** in order (first match wins):

| Verdict | Rule | Meaning |
|---|---|---|
| **Sell** | B < 2.5, or B < 3.0 and V ≤ 2 | Weak business, or a mediocre one that is expensive |
| **Buy** | B ≥ 4.0 and V ≥ 4, or B ≥ 3.5 and V = 5 | Strong business at an attractive price |
| **Watch** | B ≥ 4.0 | Great business; wait for a better price |
| **Hold** | B ≥ 3.0 and V ≥ 3 | Solid business at a fair price |
| **Pass** | anything else | Not compelling at this price |

Every report shows the rule that matched, the weighted components and a **confidence** level that drops for each gap (no AI analysis, a fallback valuation metric, missing data). All thresholds and weights live in [`config/scoring.yaml`](config/scoring.yaml): edit the file and restart the API (`docker compose restart api`) to tune the model. The file's version is stored on every report.

Example: Visa scores B 4.5 (quality 4.5, growth 4, health 5) and V 2, so its verdict is **Watch**.

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

### Request a report over the API

```bash
curl -X POST http://localhost:8080/api/reports -H "Content-Type: application/json" -d '{"ticker": "MSFT"}'
```

This returns `{"id": "...", "status": "queued"}` and runs the report in the background. To watch it live, open the event stream (Server-Sent Events):

```bash
curl -N http://localhost:8080/api/reports/<id>/events
```

Each stage sends events (`stage.started`, `section.ready`, `section.failed`, `job.done`), and `GET /api/reports/<id>` returns the report, even partway through. If a data source fails, the rest of the report still finishes and is marked `partial`. A report requested again within 12 hours is returned instantly; send `"refresh": true` to rebuild it.

| Endpoint | Purpose |
|---|---|
| `GET /api/tickers?q=micro` | Search companies by ticker or name |
| `POST /api/reports` | Start a report (`{"ticker": "V", "refresh": false}`) |
| `GET /api/reports/{id}` | Report status and JSON |
| `GET /api/reports/{id}/events` | Live progress (SSE) |
| `GET /api/reports?ticker=V` | Report history for a ticker |

### Fetch data from the command line

With the stack running (`task up`) and keys in `.env`:

```bash
task cli -- fetch V
```

This prints normalized annual, quarterly and trailing-twelve-month financials from SEC filings, along with which XBRL concept each number came from. It also prints a price summary and the last 8 quarters of EPS versus analyst estimates. Add `-json` for the full data, or `-no-cache` to run without Postgres.

```bash
task cli -- metrics V
```

This prints the Financial overview:

- profitability, growth (1/3/5/10-year CAGRs), financial health, valuation multiples and shareholder yields;
- the company's lifecycle phase, with its three checks (growing? profitable? returning capital?);
- growth, health and valuation scores (1–5), each with its reasons;
- valuation bands: today's multiple against its own 5-year history, translated into cheap and expensive share prices.

Every metric in the `-json` output lists its inputs, period and formula.

Responses are cached in Postgres (SEC data and prices for a day, earnings for a week). Alpha Vantage's 25-requests-per-day free quota is tracked so it is never exceeded.

**How the financials are built:** each value is the most recently filed figure for its period, so restatements win. Quarters that companies only report as year-to-date totals are derived by subtraction (for example, Q4 = full year − nine months), and the source says so. Values that can't be found or derived are left empty, never guessed. Companies with several share classes (such as Visa) have no plain EPS in SEC's company-facts feed. For those, the per-class figures are read from the filings' XBRL documents.

**How valuation works:** per-share figures are converted to today's share count using the split history, so a 10-for-1 split doesn't distort P/E. Each day's historical multiple only uses results that were public by then. A company's phase picks the multiple: P/S for fast-growing companies, P/FCF while margins expand, P/E once profits are returned to shareholders.

Test fixtures for Visa, Apple and Snowflake are recorded with `task fixtures`. Tests check them against numbers from the companies' filings.

## Repository layout

```
backend/   Go module: API, workers, CLI
agents/    Python package: AI analyst agents
web/       React + Vite frontend
```

## License

[MIT](LICENSE)
