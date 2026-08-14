BEGIN;

CREATE TABLE scheduler_job_runs
(
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,

    job_key     TEXT        NOT NULL,
    run_source  TEXT        NOT NULL,

    status      TEXT        NOT NULL,

    summary     JSONB       NOT NULL DEFAULT '{}'::jsonb,
    message     JSONB       NOT NULL DEFAULT '[]'::jsonb,
    error       TEXT        NOT NULL DEFAULT '',

    started_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ
);

CREATE INDEX scheduler_job_runs_job_key_started_idx
    ON scheduler_job_runs (job_key, started_at DESC);

CREATE INDEX scheduler_job_runs_status_started_idx
    ON scheduler_job_runs (status, started_at DESC);

COMMIT;
