# Agent Guide

## Project Purpose

This repository is an early Go-based data ingestion foundation for a low-frequency A-share quantitative research tool.

Current scope:

- Fetch stock metadata from Tushare.
- Fetch yearly daily bar data.
- Persist raw market data as Parquet under `data/raw`.
- Initialize a SQLite metadata/factor database under `data/meta`.

The current implementation is intentionally small and is not yet a production-grade research data pipeline.

## Repository Layout

- `main.go`: single executable containing configuration, Tushare HTTP client, stock filtering, daily data ingestion, Parquet writing, and SQLite initialization.
- `go.mod`, `go.sum`: Go module dependencies.
- `data/raw`: generated raw market data. Do not treat generated files here as source.
- `data/meta`: generated SQLite database and other metadata. Do not treat generated files here as source.

## Local Commands

Use these commands from the repository root:

```bash
go test ./...
go vet ./...
go run .
```

`go run .` requires a valid Tushare token before it can fetch data.

## Current Data Flow

1. `main` checks the Tushare token placeholder.
2. `initSQLite` creates `data/meta/quant.db` and a `daily_factors` table.
3. `fetchStockList` calls `stock_basic`, keeps listed A-share main-board stocks, and excludes ChiNext, STAR Market, and other boards by code prefix.
4. `fetchDailyByYear` loops through each configured year and each stock code, then writes one Parquet file per year to `data/raw/daily/<year>.parquet`.
5. The stock list is written to `data/raw/stocks.parquet`.

## Known Issues

- `pro_bar` is currently called through the generic HTTP endpoint. Tushare documents `pro_bar` as an SDK-level integrated API whose adjusted-price logic is not directly available through HTTP. For Go ingestion, replace this with HTTP-supported endpoints such as `daily` plus `adj_factor`, or another documented HTTP-compatible adjusted-factor endpoint.
- The Tushare token is hard-coded in `main.go`. Move it to an environment variable or config file before real use. Do not commit real tokens.
- The HTTP client uses `http.Post` without a timeout, retry policy, or context cancellation. A network stall can hang the whole run.
- `json.Marshal`, stock-list Parquet creation, Parquet writes, and close operations have ignored errors in some paths. Silent failure can leave missing or corrupt files.
- Field lookups assume every requested field is present. Missing fields currently fall back to index `0`, which can silently map the wrong value.
- `stock_basic` only fetches currently listed stocks. Backtests over historical periods will have survivorship bias unless delisted and formerly listed securities are included.
- The current stock/year loop is slow and hard to resume. Tushare recommends date-based full-market pulls for the `daily` endpoint; use a resumable calendar/date partition before scaling.
- One full year of all stocks is accumulated in memory before writing. Stream rows or write smaller partitions if memory becomes an issue.
- A failed run writes directly to final Parquet paths. Prefer writing to a temporary file and atomically renaming after successful close.
- `EndYear` is inclusive. If it points to the current calendar year, the resulting Parquet file is partial until the year finishes or an incremental refresh process is added.

## Recommended Next Changes

1. Replace direct HTTP `pro_bar` calls with `daily` plus `adj_factor` and compute adjusted OHLC consistently.
2. Read configuration from environment variables or flags: token, start date, end date, output directory, market universe, and adjustment mode.
3. Add a reusable Tushare client with timeout, retry with backoff, response validation, and per-endpoint rate limiting.
4. Validate response fields before converting rows.
5. Make output writes atomic and record an ingestion manifest for every partition.
6. Add tests for response parsing, field validation, stock filtering, and adjusted-price calculation.

## Agent Working Rules

- Do not assume generated data under `data/` is reliable unless the ingestion manifest or row counts verify it.
- Preserve the user's market-universe decision unless explicitly asked to broaden it. The current universe is A-share main-board stocks only.
- Be careful with backtest assumptions: avoid survivorship bias, look-ahead bias, and mixing adjusted and unadjusted prices.
- Before changing any Tushare endpoint behavior, check current Tushare documentation because limits, fields, and access rules may change.
- Prefer small, testable packages over adding more logic to `main.go` as this evolves.
- Never store real API tokens in source files, docs, test fixtures, or generated logs.

## External References Checked

- Tushare HTTP protocol documentation: https://tushare.pro/document/1?doc_id=40
- Tushare `pro_bar` documentation: https://tushare.pro/document/1?doc_id=109
- Tushare `daily` documentation: https://tushare.pro/document/2?doc_id=27
- Tushare `adj_factor` documentation: https://tushare.pro/document/2?doc_id=28
