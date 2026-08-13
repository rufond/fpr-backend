BEGIN;

-- FPR initial PostgreSQL schema.
--
-- Principles:
-- - single-fund application: no funds table and no fund_id columns;
-- - PostgreSQL stores structure and durable state;
-- - business validation lives in Go, so domain CHECK/regex constraints are intentionally absent;
-- - migrations do not seed mutable current fund data;
-- - normal public runtime reads are served from RAM state built from this durable data.

CREATE TABLE instruments (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,

    asset_type TEXT NOT NULL,
    isin TEXT NOT NULL UNIQUE,

    name TEXT NOT NULL,
    issuer TEXT,
    ticker TEXT,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);


CREATE TABLE bond_details (
    instrument_id BIGINT PRIMARY KEY
        REFERENCES instruments (id)
        ON DELETE CASCADE,

    nominal_value NUMERIC,
    nominal_currency TEXT,

    maturity_date DATE,
    coupon_rate_percent NUMERIC,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);


-- Canonical official daily history from the management company page.
-- Initial backfill may insert missing dates of any age.
-- For already stored dates the application may update values only inside the
-- fresh seven-calendar-day correction window relative to the latest source date.
-- Older source mismatches are diagnostics and do not overwrite stored history.
CREATE TABLE fund_daily_values (
    as_of_date DATE PRIMARY KEY,

    calculated_unit_value_usd NUMERIC NOT NULL,
    nav_usd NUMERIC NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);


-- Immutable/versioned snapshots of the current management company page.
-- The same as_of_date may have multiple versions when the management company changes current data.
CREATE TABLE fund_snapshots (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,

    as_of_date DATE NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,

    source_hash TEXT NOT NULL,

    calculated_unit_value_usd NUMERIC NOT NULL,
    nav_usd NUMERIC NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (as_of_date, source_hash)
);

CREATE INDEX fund_snapshots_latest_idx
    ON fund_snapshots (
        as_of_date DESC,
        observed_at DESC,
        id DESC
    );


-- Exact rows from the management company "Структура активов" table.
-- For current securities instrument_id and quantity are required by Go validation.
-- For cash/claims/etc. they are intentionally NULL.
CREATE TABLE fund_snapshot_assets (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,

    snapshot_id BIGINT NOT NULL
        REFERENCES fund_snapshots (id)
        ON DELETE CASCADE,

    row_no SMALLINT NOT NULL,

    source_name TEXT NOT NULL,
    source_type TEXT NOT NULL,

    instrument_id BIGINT
        REFERENCES instruments (id),

    currency TEXT,
    quantity NUMERIC,

    asset_share_percent NUMERIC NOT NULL,
    asset_share_upper_bound BOOLEAN NOT NULL DEFAULT false,

    UNIQUE (snapshot_id, row_no)
);

CREATE INDEX fund_snapshot_assets_instrument_idx
    ON fund_snapshot_assets (instrument_id)
    WHERE instrument_id IS NOT NULL;


-- More precise category shares published by the management company separately from rounded detail rows.
CREATE TABLE fund_snapshot_categories (
    snapshot_id BIGINT NOT NULL
        REFERENCES fund_snapshots (id)
        ON DELETE CASCADE,

    row_no SMALLINT NOT NULL,

    source_name TEXT NOT NULL,
    asset_share_percent NUMERIC NOT NULL,

    PRIMARY KEY (snapshot_id, row_no)
);


-- Confirmed number of fund units from periodic official PDF reports.
CREATE TABLE fund_unit_counts (
    as_of_date DATE PRIMARY KEY,

    units_outstanding NUMERIC NOT NULL,

    source_url TEXT NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);


-- Provider-specific market-price mapping.
CREATE TABLE instrument_price_sources (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,

    instrument_id BIGINT NOT NULL
        REFERENCES instruments (id)
        ON DELETE CASCADE,

    provider TEXT NOT NULL,
    provider_symbol TEXT NOT NULL,

    enabled BOOLEAN NOT NULL DEFAULT true,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (instrument_id, provider)
);


-- Last durable normalized valuation price for a price source.
-- unit_value is normalized so it can be multiplied by the position quantity.
CREATE TABLE instrument_prices (
    price_source_id BIGINT PRIMARY KEY
        REFERENCES instrument_price_sources (id)
        ON DELETE CASCADE,

    unit_value NUMERIC NOT NULL,
    currency TEXT NOT NULL,

    priced_at TIMESTAMPTZ NOT NULL,
    fetched_at TIMESTAMPTZ NOT NULL
);


-- Short intraday history for UI mini-charts.
-- Retention is controlled by Go/scheduler; initial target is about 48 hours.
CREATE TABLE instrument_price_points (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,

    price_source_id BIGINT NOT NULL
        REFERENCES instrument_price_sources (id)
        ON DELETE CASCADE,

    unit_value NUMERIC NOT NULL,
    currency TEXT NOT NULL,

    priced_at TIMESTAMPTZ NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX instrument_price_points_history_idx
    ON instrument_price_points (
        price_source_id,
        observed_at DESC
    );


-- Permanent daily market-price series when useful.
-- Initially this is primarily the MOEX history of the fund unit (БСИП).
CREATE TABLE instrument_daily_prices (
    price_source_id BIGINT NOT NULL
        REFERENCES instrument_price_sources (id)
        ON DELETE CASCADE,

    price_date DATE NOT NULL,

    unit_value NUMERIC NOT NULL,
    currency TEXT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    PRIMARY KEY (price_source_id, price_date)
);


-- Current FX rates needed by live valuation.
-- Historical FX is not part of the initial schema; each baseline persists the
-- exact FX rate used by that baseline calculation.
CREATE TABLE fx_rates (
    base_currency TEXT NOT NULL,
    quote_currency TEXT NOT NULL,

    provider TEXT NOT NULL,
    rate NUMERIC NOT NULL,

    priced_at TIMESTAMPTZ NOT NULL,
    fetched_at TIMESTAMPTZ NOT NULL,

    PRIMARY KEY (base_currency, quote_currency)
);


-- Market baseline for an official snapshot asset.
-- This is derived FPR data, not a fact published by the management company.
-- It lets live valuation survive restart without reconstructing old quotes.
CREATE TABLE fund_snapshot_price_baselines (
    asset_id BIGINT PRIMARY KEY
        REFERENCES fund_snapshot_assets (id)
        ON DELETE CASCADE,

    price_source_id BIGINT NOT NULL
        REFERENCES instrument_price_sources (id),

    unit_value NUMERIC NOT NULL,
    currency TEXT NOT NULL,
    priced_at TIMESTAMPTZ NOT NULL,

    fx_rate_to_usd NUMERIC NOT NULL,
    fx_provider TEXT NOT NULL,
    fx_priced_at TIMESTAMPTZ NOT NULL,

    market_value_usd NUMERIC NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);


-- Short history of calculated live fund value for UI mini-charts.
CREATE TABLE fund_value_points (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,

    snapshot_id BIGINT NOT NULL
        REFERENCES fund_snapshots (id)
        ON DELETE CASCADE,

    observed_at TIMESTAMPTZ NOT NULL,

    estimated_nav_usd NUMERIC NOT NULL,
    estimated_calculated_unit_value_usd NUMERIC NOT NULL,

    live_delta_usd NUMERIC NOT NULL,
    live_coverage_percent NUMERIC NOT NULL
);

CREATE INDEX fund_value_points_history_idx
    ON fund_value_points (observed_at DESC);


-- Bond-specific current market metrics for UI and valuation diagnostics.
-- instrument_prices.unit_value remains the normalized common valuation value;
-- clean/accrued/YTM stay bond-specific here.
CREATE TABLE bond_market_data (
    instrument_id BIGINT PRIMARY KEY
        REFERENCES instruments (id)
        ON DELETE CASCADE,

    price_source_id BIGINT NOT NULL
        REFERENCES instrument_price_sources (id)
        ON DELETE CASCADE,

    currency TEXT NOT NULL,

    clean_unit_value NUMERIC,
    accrued_coupon_unit_value NUMERIC,
    yield_to_maturity_percent NUMERIC,

    priced_at TIMESTAMPTZ NOT NULL,
    fetched_at TIMESTAMPTZ NOT NULL
);

COMMIT;
