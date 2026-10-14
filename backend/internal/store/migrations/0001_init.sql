-- Raw provider responses (SEC, Tiingo, Alpha Vantage), keyed by request.
CREATE TABLE http_cache (
    key        text        PRIMARY KEY,
    body       bytea       NOT NULL,
    fetched_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL
);

-- Requests made per provider per day, for providers with a daily quota.
CREATE TABLE provider_usage (
    provider text NOT NULL,
    day      date NOT NULL,
    count    int  NOT NULL,
    PRIMARY KEY (provider, day)
);
