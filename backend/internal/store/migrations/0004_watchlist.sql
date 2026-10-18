-- Tickers the user follows. The watchlist page shows each one's latest
-- report: verdict, price and where the price sits in its valuation bands.
CREATE TABLE watchlist (
    ticker       text        PRIMARY KEY,
    company_name text        NOT NULL DEFAULT '',
    added_at     timestamptz NOT NULL DEFAULT now()
);
