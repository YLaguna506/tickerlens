-- Backtests: the verdict without the AI, run as of past dates on data filed
-- by then, against how each stock did over the following years.
CREATE TABLE backtests (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    universe    text        NOT NULL,                   -- sp500, watchlist, custom
    status      text        NOT NULL DEFAULT 'running', -- running, done, cancelled, failed
    params      jsonb       NOT NULL,                   -- as-of dates, scoring version
    members     jsonb       NOT NULL,                   -- [{ticker, name, sector}]
    error       text        NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz
);

-- One row per company: its observations (one per as-of date) as JSON.
CREATE TABLE backtest_results (
    backtest_id uuid        NOT NULL REFERENCES backtests (id) ON DELETE CASCADE,
    ticker      text        NOT NULL,
    result      jsonb       NOT NULL,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (backtest_id, ticker)
);
