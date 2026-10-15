-- One row per report request. The report itself is stored as JSON and filled
-- in as stages finish, so a running report can be shown partially.
CREATE TABLE reports (
    id              uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    ticker          text        NOT NULL,
    company_name    text        NOT NULL DEFAULT '',
    status          text        NOT NULL DEFAULT 'queued', -- queued, running, done, partial, failed
    stage           text        NOT NULL DEFAULT 'queued', -- current pipeline stage
    error           text        NOT NULL DEFAULT '',
    failed_sections text[]      NOT NULL DEFAULT '{}',
    -- Latest filing accession + latest price date: two reports with the same
    -- key were built from the same data.
    cache_key       text        NOT NULL DEFAULT '',
    reused_from     uuid,
    body            jsonb,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    finished_at     timestamptz
);

CREATE INDEX reports_ticker_created ON reports (ticker, created_at DESC);
CREATE INDEX reports_unfinished ON reports (status) WHERE status IN ('queued', 'running');
