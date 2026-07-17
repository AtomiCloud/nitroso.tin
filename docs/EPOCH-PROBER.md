# Epoch Prober — Refactor

Refactor the detect-then-react polling pipeline (tin `poller` → helium pollee Jobs →
LIVE pub/sub → `differ` → reserver) into a **probe-by-reserving** fleet: short-lived
Go Jobs, spawned every minute, that call KTMB `Reserve` directly for every demanded
slot. A successful probe _is_ a hold. This is a refactor of the seat-acquisition
pipeline, not a new product: every external contract is preserved — the single
funded account, the `reserver` stream + `ReserveDto` shape, zinc's `Pending → Buying`
ownership boundary, the recoverer's refund verdict — only the detect→reserve
mechanism is replaced (and ~1,500 lines + 2 services deleted with it). This document
is the refactor contract and acceptance checklist, in the style of `RECOVERY.md`.

Status: **PHASE 1 IMPLEMENTED — validation and landscape cutover pending** (§9).
The load-bearing assumption — KTMB rate-limits per IP, so per-request X-Real-IP
rotation supports the multi-job fleet (§10.2) — is confirmed. Baseline: tin
`origin/main` (v1.48.0), zinc v1.45.0, helium v1.20.0.

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
4. The reserver reacts to a delta by _then_ calling KTMB `Reserve`
   (`lib/reserver/reserver.go:280`).

Costs and defects of this shape:

- **Race window**: between a pollee _seeing_ a seat and the reserver _reserving_ it,
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

KTMB's `Reserve` (`lib/ktmb/ktmb.go:161`) is an atomic _check-and-acquire_: it either
returns a hold (`BookingData`) or a failure. Probing with `Reserve` collapses
detect → publish → diff → react into **one call** and eliminates the race entirely:
seeing the seat and owning the seat become the same event.

The prober fleet is a **multi-job batch spawned every epoch** (1-minute tick), sized
by demand along two axes — _breadth_ (slots per Job) and _fanout_ (Jobs per slot
group) — and each Job expires by its own deadline:

```text
 tin-spawner (Deployment; absorbs the poller AND the enricher)
   every EPOCH_MINUTES (epoch E = floor(unix / (EPOCH_MINUTES*60)); default 1 min):
     1. read MAIN "{ps}:count" → filterPoller window → ALL demanded slots (NO cap),
        each carrying `needed` (Pending tickets) — §7 hold budget
     2. ensure session valid; enrich missing/stale slots ONLY (StationsAll →
        SearchStations → Trip — the enricher's loop, lib/enricher/client.go:30),
        concurrently, with X-Real-IP rotation
     3. write MAIN "ktmb:userData" + "ktmb:store" (encrypted, as today —
        lib/enricher/enricher.go:232-256)
     4. shard ALL slots into breadth groups of slotsPerJob (default 500):
          breadth = ceil(slots / slotsPerJob)
        fan out `fanout` (default 1) copies of each group:
          JOBS THIS EPOCH = breadth × fanout
        create each Job "tin-prober-<E>-<shard>-<f>"   ← deterministic (idempotent; 409 = ok)
          image: nitroso-tin (same image as every module)
          cmd: /app/nitroso-tin prober
               -d '[{dir,date,time,needed}, ...]'      (THIS shard's slots ONLY)
               -i <jobMinutes × 60>                    (probe deadline; default 120 s)
          backoffLimit: 0, TTLSecondsAfterFinished: 300, no ephemeral ownerRef
   (e.g. 3000 slots, slotsPerJob=500, fanout=1 → 6 Jobs/epoch; fanout=2 → 12.
    breadth = coverage (every slot gets a dedicated prober); fanout = aggression
    (each slot group hammered by N Jobs = N× the Reserve attempts/slot). Jobs live
    jobMinutes (2) > epoch (1), so ~2 overlapping generations are alive at once →
    ~12 (fanout 1) / ~24 (fanout 2) Jobs steady-state. ALL generations REUSE the one
    cached session (§5); only a fresh LOGIN invalidates, and the prober never logs in.
    Session/trip blobs are passed BY REFERENCE via the MAIN store, never in Job args —
    args are plaintext in the pod spec, and the current poller leaks pool tokens that
    way (lib/poller/job.go:134); the funded account's session must not repeat that.)

 tin-prober Job (Go, one shard's slots; cache/session consumer)
   read session + SearchData/TripData from MAIN; never logs in and never mutates
     the shared cache, but may self-seed or refresh one missing/stale slot in memory
     through the context-aware enrichment path (§7)
   goroutine per slot: ktmb.Reserve loop until deadline or budget met
     (paceMs default 0 = unthrottled; X-Real-IP rotated per request — §7)
     ├─ Status=true → hold acquired → LPUSH STREAM "reserver" ReserveDto  (unchanged)
     ├─ sold-out message → sleep(paceMs, default 0), retry
     ├─ stale-data message → refresh SearchData/TripData once, retry
     └─ session failure → bail epoch (next epoch is a clean slate; rate-limits ignored)
   at deadline: exit 0; emit per-slot tally lines + write Job tally to MAIN

 buyer, recoverer, cdc, enricher*, zinc: unchanged contracts
 (* enricher may be folded into the prober after Phase 3 — §9)
```

Deleted permanently (~1,500 lines + 2 services' worth of coupling):

| Component                    | Files                                                                              |
| ---------------------------- | ---------------------------------------------------------------------------------- |
| tin poller's helium coupling | `lib/poller/job.go` helium Job spec, `poller.pollee.*` settings                    |
| tin loginer + pool           | `cmds/loginer.go`, `lib/pool/pool.go` (no probing pool needed at N=1; §5)          |
| tin differ + LIVE plumbing   | `lib/reserver/differ.go`, delta/deferred paths in `lib/reserver/reserver.go`       |
| LIVE redis                   | `livecache` chart dependency (`infra/root_chart/values.yaml`), `cache.LIVE` config |
| helium watch/multi-watch     | `src/lib/{watcher,streamer,poller}.ts`, `cli.ts` commands, pollee ConfigMap/secret |
| the 1.9.2 pin problem        | no spawned foreign images ⇒ no cross-repo version pin at all                       |

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
   (zinc `Domain/Booking/Service.cs` `Buying` guard — only a `Pending` booking can
   enter `Buying`, so a second/stale hold for an already-Buying/Booked booking is
   rejected, not double-applied). Duplicate or stale holds are absorbed by: buyer
   404-no-demand → release (`lib/buyer/client.go:183-191`), and hold expiry on KTMB's
   side. Nothing in this design bypasses that. Verified: zero zinc changes needed.
3. **The recoverer's "conclusively absent ⇒ Duplicate (refund)" verdict must scan
   EVERY account that can hold tickets.** At N=1 (this design) that is the single
   purchasing account — the deployed recoverer (`lib/recoverer/scan.go`) is already
   correct, no change needed. Scaling to N funded accounts makes the multi-account
   scan (§6) a **blocking prerequisite** — an absent-from-one-account scan is not
   conclusive.
4. **One KTMB session per account; the prober only ever REUSES it.** A fresh login
   invalidates the previous session (KTMB constraint, documented in helium `cli.ts`
   login error text). The prober SHARES the purchasing account with the enricher
   (and recoverer/terminator), all via the same cached session (`login-session`,
   `lib/session/client.go`). The prober must NEVER force a re-login while other
   modules hold it (§5). **Overlapping Job generations are safe**: they all READ the
   same cached token — concurrent readers of one session do not invalidate each
   other; only a fresh LOGIN does.
5. **The reserver queue contract is frozen.** ReserveDto shape
   (`lib/reserver/reserver.go:47-54`) and the `stream.reserver` LIST are unchanged, so
   the buyer needs zero changes.

## 4. Epoch mechanics

- **Epoch = spawn tick**: `prober.epochMinutes` (default **1**). A fresh batch of Jobs
  is created every minute. The current pipeline's effective cadence is already ≥ 1 min
  (poller cron) with 2-min pollee lifetimes; 1-min ticks with continuous in-tick
  probing strictly dominate it on detection latency.
- **Job lifetime ≠ epoch**: `prober.jobMinutes` (default **2**) is each Job's deadline
  (`-i` = jobMinutes × 60). Because deadline (2) > epoch (1), Job generations
  **overlap by design** — ~2 ticks alive at once. This is safe per §3.4: overlapping
  Jobs all reuse the one cached session; concurrent readers don't conflict.
- **Multi-job fleet per epoch (breadth × fanout)**: the spawner shards **ALL**
  demanded slots (no `maxStreams` truncation) into `breadth = ceil(slots /
slotsPerJob)` groups (`prober.slotsPerJob`, default 500), then creates `fanout`
  (`prober.fanout`, default 1) copies of each group.
  **Jobs per epoch = breadth × fanout.** Example: 3000 slots, `slotsPerJob=500`,
  `fanout=1` → 6 Jobs; `fanout=2` → 12. The old `shardTargets` chunking
  (`lib/poller/job.go:73-86`) now does real work — breadth within one account —
  rather than sitting idle for a future multi-account case (§5.1).
  - **breadth = coverage**: every slot gets a dedicated prober (one Job can't
    meaningfully hammer thousands of slots within a tick).
  - **fanout = aggression**: each slot group is hit by N Jobs → N× concurrent
    `Reserve` attempts per slot → higher win probability on a seat the instant it
    releases.
- **Deterministic Job names**: `tin-prober-<E>-<shard>-<f>`. Creation treats HTTP 409
  `AlreadyExists` as success. This makes the spawner **idempotent**: two spawner
  replicas (or a tick overlapping a redeploy) cannot double-spawn. This fixes the
  current design's quirk where each poller replica spawns an independent fleet with
  random `xid` names (`lib/poller/job.go:144-146`).
- **TTLSecondsAfterFinished**: 300 (long enough to scrape logs; short enough to keep
  the namespace clean). `backoffLimit: 0` — a crashed prober is NOT retried by k8s;
  the next tick replaces it (crash-loops burn accounts, clean ticks don't).
  `activeDeadlineSeconds = jobMinutes × 60 + 125` is the hard pod lifetime: probing
  stops at `jobMinutes`, then the pod retains enough cleanup grace for a 60-second
  Reserve plus a 60-second compensating Cancel. KTMB requests are context-aware
  and also have a 60-second HTTP-client timeout.
- **No spawner-Pod OwnerReference**: Jobs must survive a spawner restart or outage.
  Deterministic names prevent duplicates and the 300-second completion TTL handles
  cleanup without coupling Job lifetime to an ephemeral Deployment Pod.
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
    Overlapping Job generations and fanout siblings all read the same token safely.
    All cache-miss login callers (including the legacy enricher during Phase 1)
    share the Redis `login-session:login-lock`, so only one KTMB login can create
    the replacement token.
  - On a session-invalid error, the prober bails the epoch and writes the durable
    token's SHA-256 fingerprint into the `{ps}:prober:session-dead` Redis set; it
    never deletes or refreshes the shared token. The single spawner consumes all
    pending fingerprints next tick and atomically deletes the cache only when one
    matches the exact encrypted token it read. Late failures from overlapping
    old-token Jobs cannot delete a newer healthy session or overwrite a current-token
    failure. A matching dead token is re-established through the existing cache-miss
    `session.Login`.
- **Funding**: size the account for maximum _concurrent_ holds across all live
  generations, not one epoch: `overlap = ceil(jobMinutes / epochMinutes)`. For
  uniform demand, use `breadth × fanout × needed × overlap`. For heterogeneous
  slots, use `sum(target.needed) × fanout × overlap`.
  Add every unreleased hold already in immediate-cancel or durable-release backlog,
  then multiply the total hold budget by the maximum ticket price. This is an
  operational runbook requirement, not code. The §3.1 revert path is the backstop
  for a drained wallet, not the plan.
- **No rate cap — throughput scales with Jobs**: the prober has **no `maxRps`**
  (`paceMs` defaults to **0 ms**, §7). Every goroutine fires `Reserve` as fast as the
  pooled HTTP client allows, relying on per-request **X-Real-IP rotation** to defeat
  KTMB's per-IP rate limiter, exactly as the enricher already does
  (`system/config/model.go:165-168`: rotation "defeats the rate limiter"). Because
  the per-IP limiter is defeated, **more Jobs DO add throughput** — breadth covers
  more slots, fanout adds per-slot aggression. The throughput ceiling is per-account
  session-concurrency tolerance + HTTP RTT + X-Real-IP effectiveness, NOT an
  artificial RPS cap. The per-IP assumption is verified (§10.2); were KTMB ever
  found to rate-limit per _account/session_, all Jobs share one session and could
  trip an account-level lock that X-Real-IP cannot evade — that is the one case to
  raise `paceMs` above 0 or add funded accounts (§5.1).

### 5.1 Future scale-out (N funded accounts) — documented, NOT built

Within one account, breadth + fanout already scale the fleet. Add funded accounts
only if one account's session can't sustain the probe concurrency, or if §10.2 finds
account-level limiting:

1. Fund more accounts; add `prober.accounts` JSON (`ATOMI_PROBER__ACCOUNTS`, the
   `ATOMI_POOL__LOGINS` pattern from `system/config/model.go`).
2. Assign each breadth-shard to an account round-robin (account = `shard % N`),
   keeping `fanout` within-account. Refuse configs where concurrent shards > accounts.
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

One goroutine per slot. Each Job handles up to `slotsPerJob` slots; with `fanout` F,
each slot group is probed by F concurrent Jobs (F× goroutines per slot). Each goroutine:

```text
seed: searchData/tripData := ktmb:store[slot]        // enricher cache, if present
      else StationsAll → SearchStations → Trip        // self-seed (3 calls, ~4 s)
loop until deadline or holds == needed:
  res := ktmb.Reserve(userData, appInfo, searchData, tripData)
  if res.Status:            // HOLD ACQUIRED
      LPUSH reserver ReserveDto{UserData, BookingData, dir, date, time}
      holds++
      continue              // more demand? keep probing
  classify(res.Messages):
      soldOutPatterns   → sleep(paceMs, default 0); continue // the normal case
      staleDataPatterns → refresh searchData/tripData once (held-searcher
                          pattern, cf. helium mobile_held_schedule_searcher.ts);
                          retry immediately; second failure → treat as error
      sessionPatterns   → bail epoch (session is SHARED — never force re-login, §5)
      else              → errBackoff++; exponential sleep; bail slot after M
```

- **Pacing — default 0 ms, no `maxRps`**: `prober.paceMs` is **configurable but
  defaults to 0 ms** (keep it at 0–1 ms; it exists only as an emergency brake, not
  a tuned limit). At the default every slot goroutine fires `Reserve` back-to-back
  as fast as the pooled HTTP client allows. The pollee's 10 ms cadence and the
  prior 1000 ms-per-slot throttle are gone; `maxRps` is removed entirely. This is
  viable because of per-request **X-Real-IP rotation**: the prober reuses
  `lib/ktmb`, whose `HttpClient.applyRealIP` (`lib/ktmb/http.go:42-44`, invoked on
  every `Send`/`SendWith`/`BinarySendWith`) stamps a fresh `randomPublicIP()` into
  the `X-Real-IP` header on **every** request — inherited for free, zero new code,
  identical to how the enricher, reserver, and recoverer already evade the per-IP
  rate limiter today. (If network-layer source rotation is also wanted, tin's
  optional `;`-separated proxy list with per-request random selection,
  `lib/ktmb/http.go:98-103`, is a separate knob — not required for X-Real-IP defeat.)
- **Maintenance window**: KTMB is down 23:00–00:15 SGT; seats release at ~00:15.
  Port the reserver's maintenance clock (`lib/reserver/reserver.go:249-278`):
  probers whose Job spans 00:14:55 block until `maintenanceOver`, then resume firing
  at full (uncapped) speed — no `burstRps`/`burstSeconds`, just the same no-throttle
  loop the instant the window opens. The spawner also aligns a tick to ~00:14 so a
  fresh batch with fresh sessions is in place before the release moment.
- **Hold budget**: stop probing a slot after `needed` holds this Job. `needed`
  self-corrects across ticks via CDC (holds → buys → Pending count drops → next
  tick's `-d` shrinks). With fanout F and overlapping ticks, up to ~(F × in-flight)
  extra holds per slot can land before CDC catches up; these are absorbed by §3.2
  (buyer 404→release + hold expiry + zinc Buying guard). Tighter coordination via a
  shared per-slot redis decrement is possible later if over-hold volume warrants it.
- **Failed delivery/release safety**: if a hold cannot be enqueued, or a dry-run
  hold cannot be cancelled immediately, persist its encrypted `ReserveDto` in
  MAIN `{ps}:prober:release`. Every Job retries these cancellations before probing
  and removes an entry only after KTMB confirms success. `releaseFailed` is tallied
  and logged loudly; session and booking blobs never appear in plaintext. Cleanup
  fetches and attempts at most 10 entries within 5 seconds per Job so the queue
  cannot starve new probing. Unreadable encrypted entries are removed with a loud
  error, and KTMB responses matching `releaseTerminalPatterns` (expired/not-found)
  are removed as conclusively no longer releasable.
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
- **Epoch tally in MAIN redis**: each Job LPUSHes its tally to
  `{ps}:prober:tally:<E>` (a list), `EXPIRE` 24 h. Because generations overlap,
  the spawner aggregates E only after `ceil(jobMinutes/epochMinutes) + 1` ticks,
  when every Job has crossed its deadline and flushed its tally:
  - zero-polls across the whole batch ⇒ log loud error (KTMB down, session dead, or
    patterns broken);
  - rate-limit / 4xx-throttle responses are tallied for visibility but NOT acted
    on — probing continues uncapped (X-Real-IP rotation is the mitigation, §7);
  - the spawner stores every expected deterministic Job name in
    `{ps}:prober:expected:<E>` and logs the exact missing names when only part of a
    fleet reports; spawn normally (ticks self-heal).
- Prometheus stays out of scope for v1; logs + redis tally are sufficient and
  match the fleet's existing observability posture.

## 9. Migration plan (each phase independently shippable / revertible)

- **Phase 0 — validate (§10)** on pichu by enabling only `spawner.enabled` while
  `prober.dryRun: true` remains set. Inspect the spawned `tin-prober-*` Job logs for
  verbatim `Unclassified KTMB Reserve response` messages and the per-slot tallies;
  use those messages to tighten the response patterns before enabling queue pushes.
  (The rate-limit axis, §10.2, is already verified.)
- **Phase 1 — build behind a mode flag.** `reserver.mode: delta | probe` is NOT
  needed — instead the spawner and prober ship as new modules while the old
  pipeline keeps running untouched. New: `cmds/spawner.go`, `cmds/prober.go`,
  `lib/prober/*`, chart alias `spawner` (jobRbac: create, like poller), settings
  blocks. The old poller stays enabled; the prober runs in **dry-run** mode
  (probe, log, immediately cancel any acquired hold, and DO NOT push ReserveDto)
  on pichu to measure hit-rate safely.
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
2. **Rate-limit axis — ✅ VERIFIED**: KTMB rate-limits by **IP**, so per-request
   X-Real-IP rotation supports the multi-job fleet; no account/session-level lock
   observed under uncapped probing with rotating X-Real-IP. This closes the
   load-bearing assumption behind the breadth × fanout scaling model (§4, §5).
   (Were account-level limiting ever observed, raising `paceMs` or adding funded
   accounts §5.1 is the fallback; there is no `maxRps` to seed.)
3. **Hold TTL and per-account concurrent-hold cap**: reserve, don't buy, measure
   expiry; try k concurrent holds → seeds hold budget and funding math.
4. **Failed-probe side effects**: confirm repeated sold-out reserves don't flag the
   account (soak: 1 h at production pace on pichu account).
5. **Stale-data wording**: let a seeded SearchData age past maintenance, probe,
   record the error text → seeds `staleDataPatterns`.

## 11. Failure semantics (delta vs today)

| Failure                               | Today (pollee relay)                                                    | Epoch prober                                                                                                                                               |
| ------------------------------------- | ----------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Detector pod dies                     | pollee gap ≤ 2 min (next poller cron)                                   | one Job death: its slot group uncovered ≤1 tick (≤60 s) unless fanout>1 siblings keep probing; next tick replaces it. k8s does not retry (backoffLimit 0)  |
| Redis pub/sub loss                    | delta silently lost; next snapshot ≤ 10 ms later                        | N/A — channel deleted                                                                                                                                      |
| Seat appears, external buyer races us | relay latency window (pub/sub+diff+600 ms normalizer)                   | race window = 0 (probe IS the acquisition)                                                                                                                 |
| KTMB rate-limits detection            | pollee account burned (disposable pool)                                 | X-Real-IP rotation defeats per-IP limiting (verified §10.2); prober does NOT throttle — rate-limit responses are tallied (§8) but ignored                  |
| Session invalidated mid-cycle         | pollee stream dies silently until Job end                               | bail epoch (shared session, §5); overlapping Jobs/fanout siblings keep going on the cached token until the next needer re-establishes via cache-miss login |
| Demand doubles                        | fixed fleet until cap; silently truncated at 42 streams                 | next tick spawns more breadth-Jobs (`ceil(slots/slotsPerJob)` grows); no truncation; loudly logged. `fanout` stays a manual aggression knob                |
| Spawner down                          | no new pollees; reserver still reacts to stale LIVE (nothing publishes) | no new batches; in-flight Jobs (≤2 min) keep probing to deadline, then holds wind down; bookings stay Pending (safe, visible in tally silence)             |
| Duplicate holds (boundary/overlap)    | possible via differ replays                                             | fanout>1 + overlapping ticks raise dup frequency; absorbed by buyer 404→release + hold expiry + zinc Buying guard (§3.2)                                   |

## 12. Cost delta (raichu, steady state)

Steady-state Job count = `breadth × fanout × ceil(jobMinutes/epochMinutes)`.
Example, 3000 demanded slots, `slotsPerJob=500`, `epochMinutes=1`, `jobMinutes=2`:
fanout 1 → ~12 Jobs; fanout 2 → ~24 Jobs. Each Job ≈ 250 mCPU / 128 Mi.

|                        | Today                                                | Epoch prober                                                                                          |
| ---------------------- | ---------------------------------------------------- | ----------------------------------------------------------------------------------------------------- |
| Detection pods         | ~12 × (1 CPU / 1 Gi) pollees + 2 pollers + 1 loginer | 1 spawner + ~12–24 short-lived prober Jobs (≈ 3–6 CPU / 1.5–3 Gi total), scaling with demand × fanout |
| Redis                  | MAIN + STREAM + **LIVE**                             | MAIN + STREAM                                                                                         |
| Accounts               | N disposable (pool) + 1 funded                       | the same 1 funded account, pool deleted                                                               |
| Cross-repo pins        | tin↔helium image/config/CLI triple                  | none                                                                                                  |
| Detection→hold latency | poll + pub/sub + diff + normalizer + reserve         | one Reserve RTT                                                                                       |
