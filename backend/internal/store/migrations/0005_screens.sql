-- Screens: a list of companies (e.g. the S&P 500) scanned without the AI,
-- to find candidates worth a full report. Fundamentals come first (SEC, for
-- every company), then prices (Tiingo, only for the strongest businesses).
CREATE TABLE screens (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    universe    text        NOT NULL,                  -- sp500, watchlist, custom
    status      text        NOT NULL DEFAULT 'scanning', -- scanning, pricing, done, cancelled, failed
    params      jsonb       NOT NULL,                  -- thresholds and the price cap
    members     jsonb       NOT NULL,                  -- [{ticker, name, sector}], so a scan can resume
    error       text        NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz
);

-- One row per company scanned, its numbers as JSON.
CREATE TABLE screen_results (
    screen_id  uuid        NOT NULL REFERENCES screens (id) ON DELETE CASCADE,
    ticker     text        NOT NULL,
    result     jsonb       NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (screen_id, ticker)
);
