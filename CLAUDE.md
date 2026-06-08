# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Is

**Nitroso** is a KTMB (Malaysian railway) train-ticket booking automation platform for
Singapore↔Malaysia routes. It is composed of four sibling services under
`platforms/nitroso/`:

- **zinc** — .NET 8 backend API (bookings, payments, wallets). The system of record.
- **tin** — _this repo_. Go CLI that orchestrates the automated booking pipeline.
- **helium** — Bun/TypeScript CLI that scrapes/searches KTMB for ticket availability
  (run by `tin`'s poller as Kubernetes Jobs).
- **argon** — SvelteKit frontend (user-facing booking UI).

`tin` polls `zinc` for demand, drives the KTMB API directly to reserve and buy tickets,
and reports completed bookings back to `zinc`.

> **Task runner:** this repo uses **`pls`** (a rename of [go-task](https://taskfile.dev);
> `pls --version` reports "Task version 3.x"). It reads `Taskfile.yaml`. All commands below
> are `pls <target>`. `pls setup` runs automatically on `direnv` load via `.envrc`.

> **Note (README):** `README.MD` in this repo is a stale scaffolding template (it describes an
> unrelated "Sulfone Boron / CyanPrint" project) — ignore it. This file is the source of truth.

> **📐 Deep dive:** For how the booking pipeline actually works — the demand-vs-supply
> sniping model, the reserver matching loop, Redis topology, and open design questions — see
> **[`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)**. Read it before changing pipeline
> behavior or designing a new sniper; the per-stage summaries below are the map, that doc is
> the territory.

> **🎯 Sniper strategy:** Competitive intel (Kamba/Cleon), the rate-limit reverse-engineering
> plan, and the ranked macro roadmap (no-proxy path, dual mobile/web API, budgeted polling,
> detection avoidance) live in **[`docs/SNIPER-STRATEGY.md`](docs/SNIPER-STRATEGY.md)**. The
> lynchpin is **Phase 0**: instrument `lib/ktmb/http.go` so the rate limiter is observable
> before any redesign.

## Common Commands

### Build & Development

- `pls build` - Build the binary. **Output is `bin/nitroso-zinc`**, not `nitroso-tin` —
  the `Taskfile.yaml` `SERVICE` var is still `zinc`, a scaffolding leftover from when this
  repo was copied from `zinc`. The app's real identity is `service: tin` (see `config/app/settings.yaml`).
- `pls setup` - Setup the repository (runs `scripts/local/secrets.sh`, then `go mod tidy`)
- `pls run -- <command>` - Run with secrets loaded (`infisical run --env=<landscape>`)
- `pls dev -- <command>` - Run with full infrastructure (manages k3d cluster + Tilt)
- `pls dev:watch -- <command>` - Same as `dev` but with `air` hot reload

`<command>` is one of the CLI subcommands below (e.g. `pls run -- cdc`).

### Other Utilities

- `pls sdk:gen` - Regenerate the `zinc` SDK (`lib/zinc/main.go`) via `oapi-codegen`.
  **Requires a running `zinc` API** — it curls `zinc`'s Swagger endpoint.
- `pls process-proxy -- <args>` - Split a webshare proxy list into `helium.proxy.txt`
  (first 300) and `tin.proxy.txt` (rest)
- `pls update-proxy` - Full proxy sync: pull from webshare → split → upload to Infisical
  per landscape (`ATOMI_KTMB__PROXY` for tin, `ATOMI_APP__SEARCHER__PROXY` for helium)
- `pls helm:latest` - Latest helm-chart dependency versions; `pls util:*` for OCI versions

### Infrastructure

Helm tasks are nested per-landscape: `pls helm:<landscape>:<action>`
(e.g. `pls helm:lapras:install`, `pls helm:raichu:sync`).

- `pls helm:<landscape>:install | remove | sync | update | build | template | debug`
- `pls tear:*` - Tear down k3d clusters (dev / test / ci)
- `pls stop:*` - `tilt down` (dev / test)

Helm landscapes: **lapras, tauros, pinsir, absol, raichu, pichu, pikachu** (7).

## Architecture Overview

`tin` is a single Go binary (`urfave/cli`) with multiple subcommands. Each subcommand is a
long-running pipeline stage; they communicate through Redis streams/queues and share state
in `zinc` + the KTMB API. Shared runtime state lives in the `State` struct (`cmds/state.go`),
built once in `main.go` and passed to every command.

### CLI Subcommands (9 total)

The booking pipeline (stages 1→6):

1. **cdc** (`lib/cdc`) — Pipeline entry point. Polls `zinc` for current booking counts,
   stores them in Redis key `nitroso.tin:count`, and emits a signal on the **`Update`**
   stream to wake downstream stages.
2. **poller** (`lib/poller`) — Triggered by cron (`@every 1m`), the `Update` stream, or
   manual resend. Creates **helium** pollee **Kubernetes Jobs** to scrape availability for
   pollable dates (>30min away + within closing window, <6 months out). Runs with
   `jobRbac` permissions to create Jobs.
3. **enricher** (`lib/enricher`) — Logs into KTMB, fetches trip/station metadata
   (`SearchStations`/`Trip`), stores an **encrypted `FindStore`** in Redis, emits to the
   **`Enrich`** stream. Triggered at `@midnight`, a random 2–4h interval, or a CDC signal.
4. **reserver** (`lib/reserver`) — Runs as a **StatefulSet** (pod name injected as
   `ATOMI_RESERVER__GROUP`). Three parallel goroutines: **CountSyncer** (filters reservable
   dates), **Differ** (PubSub delta computation), **LoginSyncer** (keeps KTMB session fresh).
   Performs concurrent reservations and pushes encrypted `ReserveDto`s onto the **`Reserver`**
   queue. Has separate **normal vs maintenance** concurrency/attempt tuning.
5. **buyer** (`lib/buyer`) — Consumes the `Reserver` queue. For each item: fetches/marks the
   booking in `zinc`, then drives KTMB `BookStart → SetPassenger → Pay → Complete →
PrintTicket`, and reports the completed ticket back to `zinc`. Releases on a 404.
6. **terminator** (`lib/terminator`) — Consumes a separate termination queue; looks up the
   booking in KTMB, fetches the refund policy, and refunds the ticket.

Operational / utility subcommands:

- **manual-buy** — Manually complete a booking: uploads a ticket file to `zinc`
  `POST /api/v{version}/booking/complete/{id}`. Args: `<ticketPath> <bookingNo> <ticketNo> <bookingId>`.
- **resend** — Pushes an empty string onto the Redis `buyqueue` (legacy/debug).
- **decrypt** — Decrypts a `FindStore` value using the `ENCRYPTION_KEY` env var (debug utility).

### Redis Topology (`lib/otelredis`)

- **Streams/queues:** `Update` (CDC → poller/enricher/reserver), `Enrich`
  (enricher → reserver), `Reserver` (reserver → buyer), plus a terminator queue.
- **Caches:** `MAIN` (zinc maincache), `LIVE` (tin livecache), `STREAM` (zinc streamcache) —
  keys are **case-sensitive uppercase**.
- Messages are wrapped in a `{message, context}` JSON envelope so OpenTelemetry trace
  context propagates across queue boundaries.

### Key Libraries & Systems

- **lib/auth** — Descope M2M credential provider. Exchanges an access key for a cached JWT;
  exposes `RequestEditor()` for Bearer-auth header injection on outbound HTTP.
- **lib/ktmb** — Hand-written HTTP client for the KTMB API (login, search, trip, reserve,
  book, pay, ticket). Supports a proxy pool (random pick from a `;`-separated list).
- **lib/zinc** — **Generated** `oapi-codegen` client for `zinc`'s OpenAPI spec
  (`lib/zinc/main.go`, large generated file — regenerate with `pls sdk:gen`, never hand-edit).
- **lib/encryptor** — AES-GCM symmetric encryption (`nonce:ciphertext` hex). Note there are
  **three distinct keys** in use: `FindStore`, KTMB `LoginRes`, and `ReserveDto`.
- **lib/session** — KTMB login with encrypted session caching.
- **lib/count** — Time-window filtering for pollable / reservable dates.
- **system/telemetry** — Full OpenTelemetry: metrics, traces (stdout / OTLP gRPC+HTTP), and
  Zerolog logging with trace/span-ID injection. Resource attributes carry
  landscape/platform/service/module/version.

### Configuration Management

- Loader: `system/config/loader.go`; model (`RootConfig`): `system/config/model.go`.
- **Hierarchical merge:** base `config/app/settings.yaml` is loaded first, then
  `config/app/settings.{LANDSCAPE}.yaml` is merged on top (landscape overrides base).
- **Env-var override:** Viper `AutomaticEnv` with prefix `atomi` and `.`→`__` replacer
  (e.g. `app.landscape` → `ATOMI_APP__LANDSCAPE`, `ktmb.proxy` → `ATOMI_KTMB__PROXY`).
- **Env vars:** `LANDSCAPE` (default `lapras`), `BASE_CONFIG` (default `./config/app`),
  `ENCRYPTION_KEY` (used by the `decrypt` command).
- **App-config landscapes** (settings files present): `lapras` (default), `pichu`, `pikachu`,
  `raichu`. (The Helm layer defines more landscapes — see Infrastructure.)
- `RootConfig` sections: `Cache`, `App`, `Otel`, `Cdc`, `Stream`, `Auth`, `Poller`,
  `Reserver`, `Encryptor`, `Enricher`, `Ktmb`, `Buyer`, `Terminator`, `Buffer`.

### Infrastructure

- **Helm:** `infra/root_chart/` depends on `infra/consumer_chart/` (a generic `golang-chart`
  aliased once per module: cdc, poller, terminator, enricher, reserver, buyer). External
  deps: redis (bitnami), `sulfoxide-bromine`, and `zinc`/`helium` root-charts.
- **Per-module specialization:** `poller` gets `jobRbac` (create K8s Jobs); `reserver` is a
  `StatefulSet`; the rest are `Deployment`s. ArgoCD sync-waves order bromine → livecache → modules.
- **Images:** single multi-stage `infra/Dockerfile` (Alpine, `CGO_ENABLED=0`), built for
  `linux/amd64` + `linux/arm64` via `docker buildx` in CI, pushed to GHCR.
- **Release:** semantic-release (conventional commits) bumps `Chart.yaml` appVersion and
  pushes the OCI chart (`scripts/ci/publish.sh`, config in `atomi_release.yaml`).

### Development Environment

- **Nix flake** + **direnv** (`.envrc` runs `pls setup`). Tooling is grouped in `nix/env.nix`
  (go, infisical, helm, kubectl, k3d, tilt, mirrord, air, oapi-codegen, linters, …).
  Three shells: `default`, `ci`, `releaser`.
- **Tilt** (`Tiltfile`, `config/tilt/dev.Tiltfile`, `config/tilt/util.Tiltfile`): builds a
  Docker image per module with `air` hot reload (`infra/dev.Dockerfile`). Modes: `cluster`
  (full `tilt up`) vs `local` (`mirrord` proxy into the cluster).
- **Local k3d** clusters: `lapras` and `tauros` (`infra/k3d.*.yaml`).
- Go **1.21.4**. Pre-commit hooks via `nix/pre-commit.nix`.

## Key Development Notes

- All work runs in a `.envrc` (nix) directory — use `direnv exec . <command>` for tool calls.
- Configuration is loaded once at startup and passed to every component via `State`.
- Every component is fully instrumented (OTel traces/metrics + Zerolog).
- Redis is the backbone for both caching and inter-stage stream/queue communication.
- The pipeline is event-driven: `cdc` emits `Update` → fans out to poller/enricher/reserver →
  `buyer` finalizes via `zinc` + KTMB → `terminator` handles refunds.
