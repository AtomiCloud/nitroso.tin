# Epoch Prober — Design Spec

Replace the detect-then-react polling pipeline (tin `poller` → helium pollee Jobs →
LIVE pub/sub → `differ` → reserver) with a **probe-by-reserving** fleet: short-lived
Go Jobs, spawned per epoch, that call KTMB `Reserve` directly for every demanded slot.
A successful probe *is* a hold. This document is the design contract and the
acceptance checklist, in the style of `RECOVERY.md`.

Status: **PROPOSED** (not implemented). Baseline: tin `origin/main` (v1.48.0),
zinc v1.45.0, helium v1.20.0.

## 1. Problem

Today, seat acquisition is a four-stage relay:

1. tin `poller` (Deployment ×2) spawns helium pollee Jobs every minute
   (`lib/poller/poller.go`, `lib/poller/job.go`): ~12 concurrent pods × 1 CPU / 1 Gi
   on raichu (42-stream cap / 15 per shard × ~2 overlapping generations × 2 poller
   replicas each spawning independently).
2. Each pollee polls KTMB search every 10 ms and publishes availability snapshots to
   LIVE redis pub/sub (`helium src/lib/poller.ts` → `ktmb:schedule:<dir>:<date>`).
3. The reserver's `differ` subscribes, diffs snapshots against a cache, and emits
   deltas — with a 600 ms normalizer window that can eat rapid flips
   (`lib/reserver/reserver.go:128-136`).
4. The reserver reacts to a delta by *then* calling KTMB `Reserve`
   (`lib/reserver/reserver.go:280`).

Costs and defects of this shape:

- **Race window**: between a pollee *seeing* a seat and the reserver *reserving* it,
  any external buyer can take it. The relay adds pub/sub, diff, channel-hop, and
  normalizer latency to that window.
- **Fleet cost**: ~13–16 always-on pods (~12+ CPU, ~12 Gi) for detection alone, plus
  a dedicated LIVE redis, plus a multi-account login pool (`loginer`, `lib/pool`)
  whose only consumer is the pollee fleet.
- **Cross-repo coupling**: the poller pins a helium image tag in tin settings
  (`config/app/settings.*.yaml` → `poller.pollee.version`), currently `1.9.2` — a
  tag that predates helium's `multi-watch` command (first shipped in v1.14.0). The
  checked-in pin is broken; production only works if an `ATOMI_POLLER__POLLEE__VERSION`
  env override exists in the tin Infisical secret. Three artifacts (tin settings,
  helium chart ConfigMap, helium image CLI) must stay mutually compatible.
- **Lossy channel**: pub/sub has no persistence; differ re-subscribes on every count
  update and drops in-flight messages during the swap (`lib/reserver/differ.go:42-60`).

## 2. Core idea

KTMB's `Reserve` (`lib/ktmb/ktmb.go:161`) is an atomic *check-and-acquire*: it either
returns a hold (`BookingData`) or a failure. Probing with `Reserve` collapses
detect → publish → diff → react into **one call** and eliminates the race entirely:
seeing the seat and owning the seat become the same event.

The prober fleet is spawned per **epoch** (fixed-length cycle), sized by demand,
and expires by deadline:

```
 tin-spawner (Deployment; renamed poller, minus helium)
   every EPOCH_MINUTES (epoch E = floor(unix / (EPOCH_MINUTES*60))):
     read MAIN "{ps}:count" → filterPoller window → cap at maxStreams slots
     create Job "tin-prober-<E>"                ← deterministic name (idempotent)
       image: nitroso-tin (same image as every module)
       cmd: /app/nitroso-tin prober
            -d '[{dir,date,time,needed}, ...]'   (all slots + hold budgets)
            -i <epoch seconds>                   (probe deadline)
       backoffLimit: 0, TTLSecondsAfterFinished, ownerRef → spawner pod
   (ONE Job per epoch: there is ONE funded account (§5), and throughput is capped
    per-account, so sharding cannot add throughput — it only adds accounts. The
    sharding mechanics stay in the code path (shardTargets, shardSize: 0 = one
    chunk) as the future multi-account scale-out lever — §5.)

 tin-prober Job (Go, all slots)
   reuse the cached purchasing-account session (never force re-login — §5)
   seed SearchData/TripData per slot (from ktmb:store cache, else fetch)
   goroutine per slot: paced ktmb.Reserve loop until deadline or budget met
     ├─ Status=true → hold acquired → LPUSH STREAM "reserver" ReserveDto  (unchanged)
     ├─ sold-out message → sleep(pace), retry
     ├─ stale-data message → refresh SearchData/TripData once, retry
     └─ rate-limit / session failure → backoff, then bail (next epoch is a clean slate)
   at deadline: exit 0; emit per-slot tally lines + write epoch tally to MAIN

 buyer, recoverer, cdc, enricher*, zinc: unchanged contracts
 (* enricher optionally folded into the prober later — §9 phase 4)
```

Deleted permanently (~1,500 lines + 2 services' worth of coupling):

| Component | Files |
|---|---|
| tin poller's helium coupling | `lib/poller/job.go` helium Job spec, `poller.pollee.*` settings |
| tin loginer + pool | `cmds/loginer.go`, `lib/pool/pool.go` (no probing pool needed at N=1; §5) |
| tin differ + LIVE plumbing | `lib/reserver/differ.go`, delta/deferred paths in `lib/reserver/reserver.go` |
| LIVE redis | `livecache` chart dependency (`infra/root_chart/values.yaml`), `cache.LIVE` config |
| helium watch/multi-watch | `src/lib/{watcher,streamer,poller}.ts`, `cli.ts` commands, pollee ConfigMap/secret |
| the 1.9.2 pin problem | no spawned foreign images ⇒ no cross-repo version pin at all |

Helium shrinks to three CronJobs (scheduler, refunder, reverter).

## 3. Money and correctness invariants (load-bearing)

These are inherited from the existing system and MUST survive the redesign:

1. **A hold is paid for by the account that made it.** `ReserveDto.UserData` flows to
   the buyer, which drives BookStart→Pay on that session (`lib/buyer/client.go:156`,
   `lib/buyer/buyer.go:104`). There is exactly **ONE funded account** (the enricher /
   purchasing account, `enricher.email`) — all probing runs on it. An unfunded
   account's buys would fail at Pay with "wallet balance is insufficient" → buyer
   `RevertError` → booking recycled to Pending (`lib/buyer/errors.go:24`, zinc
   guarded revert) — safe but wasteful, which is why probing MUST NOT run on
   unfunded pool-style accounts.
2. **Zinc's guarded `Pending → Buying` remains the ownership boundary**
   (zinc `Domain/Booking/Service.cs` Buying guard). Duplicate or stale holds are
   absorbed by: buyer 404-no-demand → release (`lib/buyer/client.go:183-191`), and
   hold expiry on KTMB's side. Nothing in this design bypasses that.
3. **The recoverer's "conclusively absent ⇒ Duplicate (refund)" verdict must scan
   EVERY account that can hold tickets.** At N=1 (this design) that is the single
   purchasing account — the deployed recoverer (`lib/recoverer/scan.go`) is already
   correct, no change needed. Scaling to N funded accounts makes the multi-account
   scan (§6) a **blocking prerequisite** — an absent-from-one-account scan is not
   conclusive.
4. **One KTMB session per account.** A fresh login invalidates the previous session
   (KTMB constraint, documented in helium `cli.ts` login error text). The prober
   SHARES the purchasing account with the enricher (and recoverer/terminator), all
   via the same cached session (`login-session`, `lib/session/client.go`) — so the
   prober must only ever REUSE the cache, never force a re-login while other
   modules hold it (§5). Non-overlapping epochs keep prober generations exclusive.
5. **The reserver queue contract is frozen.** ReserveDto shape
   (`lib/reserver/reserver.go:47-54`) and the `stream.reserver` LIST are unchanged, so
   the buyer needs zero changes.

## 4. Epoch mechanics

- **Epoch length**: `prober.epochMinutes` (default **5**). Long enough to amortize
  login + SearchData seeding (one search+trip per slot per epoch), short enough for
  demand elasticity. The current pipeline's effective cadence is already ≥ 1 min
  (poller cron) with 2-min pollee lifetimes; 5-min epochs with continuous in-epoch
  probing strictly dominates it on detection latency.
- **Deterministic Job names**: `tin-prober-<epoch>`. Creation treats HTTP 409
  `AlreadyExists` as success. This makes the spawner **idempotent**: two spawner
  replicas (or a cron overlapping a redeploy) cannot double-spawn. This fixes
  the current design's quirk where each poller replica spawns an independent fleet
  with random `xid` names (`lib/poller/job.go:144-146`).
- **Deadline = epoch length** (`-i` = epochMinutes × 60). No overlap between
  prober generations ⇒ the §3.4 session-exclusivity invariant holds by
  construction. Cost: a fleet-boot gap (~1–2 s for a Go static binary + session
  reuse) at each epoch boundary. Accepted; the maintenance-window burst (§7) covers
  the one moment where boundary timing matters.
- **TTLSecondsAfterFinished**: 300 (long enough to scrape logs; short enough to keep
  the namespace clean). `backoffLimit: 0` — a crashed prober is NOT retried by k8s;
  the next epoch replaces it (crash-loops burn accounts, clean epochs don't).
- **OwnerReference → spawner pod**, as today (`lib/poller/job.go:158-169`): a deleted
  spawner GCs its in-flight Jobs.
- **One Job per epoch** (`shardSize: 0` — `shardTargets` at `lib/poller/job.go:73-86`
  already returns one chunk for 0). All demanded slots ride in one `-d` payload,
  capped at `maxStreams` (default 42) sorted earliest-date-first
  (`lib/poller/poller.go:106-123`). Sharding stays in the code path solely as the
  future multi-account lever (§5); at one account it adds pods, not throughput.
- **Demand snapshot**: reuse `count.Client.GetPollerCount` window filter
  (`lib/count/client.go:33-39`, `[now+closing, now+6mo]`). Each slot's entry carries
  `needed` (tickets still Pending) so the prober can stop early (§7 hold budget).

## 5. The single funded account (probing identity)

There is **exactly one funded KTMB account** in the system: the purchasing account
(`enricher.email` / `enricher.password` in settings). The prober runs on it. No
probing pool exists — `pool.logins` / `lib/pool` / `loginer` are deleted, not
replaced.

- **Session sharing**: the purchasing account's session is shared by the enricher,
  recoverer, terminator, and now the prober — all through the same encrypted cache
  key (`ktmb.loginKey` = `login-session`, `lib/session/client.go:31-94`). KTMB
  permits one session per account, so an uncoordinated re-login by any module
  invalidates everyone else's session. Rules:
  - The prober only ever **reuses** the cached session; it never force re-logins.
  - On a session-invalid error, the prober bails the epoch (logged in the tally);
    the existing `session.Login` cache-miss path re-establishes the session on the
    next module that needs it. (Today nothing refreshes a *stale but present*
    cache entry either — this design does not regress that; a shared
    delete-then-relogin helper is a possible later hardening.)
- **Funding**: the account must hold e-wallet balance ≥ (max holds per epoch ×
  ticket price). Operational runbook item, not code. The §3.1 revert path is the
  backstop for a drained wallet, not the plan.
- **Rate budget**: ALL probe pacing (§7) is per-THIS-account. This is the system's
  hard throughput ceiling and the honest scaling limit: more replicas or shards
  cannot add throughput — only more funded accounts can.

### 5.1 Future scale-out (N funded accounts) — documented, NOT built

If demand ever outgrows one account's rate budget:

1. Fund more accounts; add `prober.accounts` JSON (`ATOMI_PROBER__ACCOUNTS`, the
   `ATOMI_POOL__LOGINS` pattern from `system/config/model.go`).
2. Set `shardSize` > 0: the spawner shards slots across Jobs
   `tin-prober-<epoch>-<k>`, shard k using account `k % N` exclusively —
   §3.4 session exclusivity per shard by construction. Refuse configs where
   concurrent shards > accounts.
3. **Blocking prerequisite — recoverer multi-account scan**: `findTicket`
   (`lib/recoverer/scan.go`) becomes `findTicketAcross(accounts, ...)`; "conclusively
   absent ⇒ Duplicate (refund)" requires a CONCLUSIVE scan of EVERY funded account —
   any single inconclusive scan makes the whole verdict inconclusive ⇒ retry, never
   refund. Extend `scan_test.go` with the multi-account matrix
   (found-on-2nd-account, inconclusive-on-3rd-blocks-refund).
4. Per-account session cache keys (`prober:session:<email>`) replace the shared
   `login-session` for probing shards.

## 6. Recoverer

**No change at N=1.** The deployed recoverer (origin/main v1.48) already scans the
single purchasing account, which is the only account that can hold tickets in this
design. The multi-account scan is specified in §5.1 and becomes mandatory only when
a second funded account is added.

## 7. Prober internals

One goroutine per slot (≤ maxStreams = 42 in the single Job), each running:

```
seed: searchData/tripData := ktmb:store[slot]        // enricher cache, if present
      else StationsAll → SearchStations → Trip        // self-seed (3 calls, ~4 s)
loop until deadline or holds == needed:
  res := ktmb.Reserve(userData, appInfo, searchData, tripData)
  if res.Status:            // HOLD ACQUIRED
      LPUSH reserver ReserveDto{UserData, BookingData, dir, date, time}
      holds++
      continue              // more demand? keep probing
  classify(res.Messages):
      soldOutPatterns   → sleep(pace); continue          // the normal case
      staleDataPatterns → refresh searchData/tripData once (held-searcher
                          pattern, cf. helium mobile_held_schedule_searcher.ts);
                          retry immediately; second failure → treat as error
      sessionPatterns   → bail epoch (session is SHARED — never force re-login, §5)
      else              → errBackoff++; exponential sleep; bail slot after M
```

- **Pacing**: `prober.paceMs` per slot (default **1000 ms** — deliberately 100×
  gentler than the pollee's 10 ms, because probes run on funded, non-disposable
  accounts). Global per-Job token bucket `prober.maxRps` (default 5) caps the sum
  across slots. Both config, tuned by §10's validation.
- **Maintenance window**: KTMB is down 23:00–00:15 SGT; seats release at ~00:15.
  Port the reserver's maintenance clock (`lib/reserver/reserver.go:249-278`):
  probers whose epoch spans 00:14:55 block until `maintenanceOver`, then burst
  (`prober.burstRps`, default 20, for `prober.burstSeconds`, default 60). The
  spawner also aligns an epoch boundary to 00:14 so a fresh fleet with fresh
  sessions is in place before the release moment.
- **Hold budget**: stop probing a slot after `needed` holds this epoch. `needed`
  self-corrects across epochs via CDC (holds → buys → Pending count drops → next
  epoch's `-d` shrinks). Over-holding due to boundary raciness is absorbed by §3.2.
- **Warm pool**: construct `ktmb.New` with `WarmConfig{PoolSize: ~slot count, ...}`
  (`lib/ktmb/warmpool.go`) so TLS/DNS are pre-warmed at Job start — the mechanism
  already exists for the reserver, the pollee never had it.
- **Error patterns** (`soldOutPatterns`, `staleDataPatterns`, `sessionPatterns`)
  live in settings like the buyer's `conflictPatterns`/`revertPatterns`
  (`config/app/settings.yaml`), matched case-insensitively on KTMB's verbatim
  `messages` array — KTMB's wording is the API contract (cf. the "Duplicated
  passport" lesson: KTMB says "Duplicat**ed**"; substring patterns must match
  reality, not paraphrase). Populated by §10 validation.

## 8. Tallies and observability

- **Per-slot flat log line at exit** (LogQL-friendly, one line per slot — the shape
  helium's `feat/multiwatch-poll-summary` branch established):
  `{"slot":"JToW:26-12-2026:08:30", "polls":213, "holds":2, "soldOut":209,
  "stale":1, "errors":1, "rateLimited":0}` + one totals line per Job.
- **Epoch tally in MAIN redis**: `SET {ps}:prober:tally:<epoch> <json>`,
  `EXPIRE` 24 h. The spawner reads epoch E−1's tally before spawning E:
  - zero-polls ⇒ log loud error (KTMB down, session dead, or patterns broken);
  - `rateLimited > threshold` ⇒ spawn E with a `paceMs` multiplier (simple
    feedback control, config-capped);
  - tally missing ⇒ prober died; spawn normally (epochs self-heal).
- Prometheus stays out of scope for v1; logs + redis tally are sufficient and
  match the fleet's existing observability posture.

## 9. Migration plan (each phase independently shippable / revertible)

- **Phase 0 — validate (§10)** on pichu with a throwaway `prober validate` run.
- **Phase 1 — build behind a mode flag.** `reserver.mode: delta | probe` is NOT
  needed — instead the spawner and prober ship as new modules while the old
  pipeline keeps running untouched. New: `cmds/spawner.go`, `cmds/prober.go`,
  `lib/prober/*`, chart alias `spawner` (jobRbac: create, like poller), settings
  blocks. The old poller stays enabled; the prober runs in **dry-run** mode
  (probe, log, DO NOT push ReserveDto) on pichu to measure hit-rate safely.
- **Phase 2 — cut over per landscape.** Enable prober push + disable poller (helm:
  `poller.enabled: false`, `spawner.enabled: true`) on pichu → pikachu → raichu,
  watching fill-rate vs the tally baseline across ≥ 1 full maintenance-window
  cycle each. The differ can stay running (it just sees a silent LIVE channel);
  disable `loginer` with the poller. **Rollback = flip the two helm booleans.**
- **Phase 3 — delete** (only after raichu is stable ≥ 1 week): `lib/poller` helium
  spec, `lib/pool`, `cmds/loginer.go`, `lib/reserver/differ.go` + delta paths,
  LIVE cache chart + config, helium watch/multi-watch/streamer/poller + pollee
  ConfigMap/secret, `poller.pollee` settings (and the 1.9.2 pin with them).
  Optionally fold the enricher into the prober (self-seeding §7 already covers
  cache-miss; the store then becomes a warm-start optimization only).

## 10. Phase-0 validation checklist (unknowns the repos cannot answer)

1. **Sold-out `Reserve` wording**: exact `messages` text when a slot has no seats
   → seeds `soldOutPatterns`. Method: probe a known-full historical peak slot.
2. **Rate tolerance**: max sustained `Reserve` RPS per account before throttling /
   captcha / lockout → seeds `paceMs`/`maxRps`. Probe a far-future empty slot at
   escalating rates; watch for error-shape changes.
3. **Hold TTL and per-account concurrent-hold cap**: reserve, don't buy, measure
   expiry; try k concurrent holds → seeds hold budget and funding math.
4. **Failed-probe side effects**: confirm repeated sold-out reserves don't flag the
   account (soak: 1 h at production pace on pichu account).
5. **Stale-data wording**: let a seeded SearchData age past maintenance, probe,
   record the error text → seeds `staleDataPatterns`.

## 11. Failure semantics (delta vs today)

| Failure | Today (pollee relay) | Epoch prober |
|---|---|---|
| Detector pod dies | pollee gap ≤ 2 min (next poller cron) | probe gap ≤ 1 epoch; k8s does not retry (backoffLimit 0) |
| Redis pub/sub loss | delta silently lost; next snapshot ≤ 10 ms later | N/A — channel deleted |
| Seat appears, external buyer races us | relay latency window (pub/sub+diff+600 ms normalizer) | race window = 0 (probe IS the acquisition) |
| KTMB rate-limits detection | pollee account burned (disposable pool) | prober backs off; account is funded — §10.2 budget prevents, tally feedback (§8) reacts |
| Session invalidated mid-cycle | pollee stream dies silently until Job end | bail epoch (shared session, §5); next needer re-establishes via cache-miss login |
| Demand doubles | fixed fleet until cap; silently truncated at 42 streams | next epoch spawns proportionally; same cap, same earliest-first policy, loudly logged |
| Spawner down | no new pollees; reserver still reacts to stale LIVE (nothing publishes) | no new probers; holds stop; bookings stay Pending (safe, visible in tally silence) |
| Duplicate holds (boundary/overlap) | possible via differ replays | possible at epoch boundary; absorbed by buyer 404→release + hold expiry (§3.2) |

## 12. Cost delta (raichu, steady state)

| | Today | Epoch prober |
|---|---|---|
| Detection pods | ~12 × (1 CPU / 1 Gi) pollees + 2 pollers + 1 loginer | 1 prober Job (250m / 128 Mi) + 1 spawner |
| Redis | MAIN + STREAM + **LIVE** | MAIN + STREAM |
| Accounts | N disposable (pool) + 1 funded | the same 1 funded account, pool deleted |
| Cross-repo pins | tin↔helium image/config/CLI triple | none |
| Detection→hold latency | poll + pub/sub + diff + normalizer + reserve | one Reserve RTT |
