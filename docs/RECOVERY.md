# Duplicate-Passport Recovery — Spec

This is the specification for the duplicate-passport recovery pipeline that spans
three services: **helium** (scraper), **zinc** (.NET system of record / wallet), and
**tin** (this repo — Go booking automation). It is both the design contract and the
acceptance checklist. The money invariants in §3 are load-bearing: ~SGD 26K flows
through the wallet, so every recovery decision must be provably non-lossy.

## 1. Problem

When `tin`'s buyer drives a KTMB purchase, the purchase can succeed while the follow-up
capture fails (pod dies, `PrintTicket` fails, or the `zinc` `complete` call fails). The
booking is then stuck in `Buying`, the user's money is held in `BookingReserve`, and any
re-buy for the same passport+date+time hits KTMB's **"duplicate passport"** rejection.

Historically a helium `reverter` CronJob (every 5 min) "fixed" stuck `Buying` bookings by
reverting them to `Pending` — with no age check and via an **unguarded** zinc `revert`
endpoint that reversed no money. Racing the buyer's ~10–30s purchase window, it could:

- knock a just-`Completed` booking back to `Pending` while the `BookingComplete` ledger
  row (reserve → collected) already existed → corrupted booking; later auto-refunded from
  an already-emptied reserve, and
- re-expose an in-flight booking as `Pending` demand → double reservation → double buy →
  KTMB "duplicate passport".

**Fix strategy:** remove `revert` everywhere; detect conflicts; park bookings in a new
`Recovering` state; resolve them from a durable, reconciled recovery loop; never guess with
money.

## 2. Booking state machine (zinc)

```text
Pending ─▶ Buying ─▶ Completed
              │
              ▼
          Recovering (6) ─▶ Completed              (false duplicate: our uncaptured ticket)
                         ─▶ Duplicate (7, terminal) (true duplicate: full refund, = Cancel flow)

any non-terminal ─▶ RequireManualIntervention (8)   (status-only parking; no money move)
```

- `BookStatus`: `Pending=0, Buying=1, Completed=2, Cancelled=3, Refunded=4, Terminated=5,
Recovering=6, Duplicate=7, RequireManualIntervention=8`. Stored as smallint; string in the
  API. **No DB migration** (bare smallint, no converter/check constraint).
- `revert` endpoint + `RevertBuying` service method are **deleted**.
- `TransactionType.BookingDuplicate=12` (appended to preserve stored values).
- New endpoints (all `AdminOrTin`): `POST recovering/{id}`, `POST duplicate/{id}`,
  `POST manual-intervention/{id}`.
- Booking search gains a `PassportNumber` filter so tin resolves passport+date+time → booking
  id without direct DB access.

### Transition guards (all inside the RepeatableRead transaction)

| Transition               | Guard                                                                                | Money                                                        |
| ------------------------ | ------------------------------------------------------------------------------------ | ------------------------------------------------------------ |
| `Recovering(id)`         | status `== Buying`                                                                   | none (status only)                                           |
| `Duplicate(id)`          | status ∈ {`Recovering`, `RequireManualIntervention`} **and** `BookingNumber == null` | full refund BookingReserve → Usable + `BookingDuplicate` txn |
| `ManualIntervention(id)` | status non-terminal                                                                  | none                                                         |
| `Complete(id)`           | status ∈ {`Pending`, `Buying`, `Recovering`} **and** `BookingNumber == null`         | collect BookingReserve → BunnyBooker + `BookingComplete` txn |
| `RevertBuying`           | —                                                                                    | **removed**                                                  |

## 3. Money invariants (MUST hold)

1. **Once-only collect.** A booking's reserve is collected at most once. Enforced by
   `Complete`'s `BookingNumber == null` guard (`BookingNumber` is written only by a committed
   `Complete`, in the same transaction as the collect). `BookingNumber` set ⟺ reserve collected.
2. **Once-only refund.** A booking's reserve is refunded at most once. `Duplicate`/`Cancel`/
   `Refund` are terminal and mutually exclusive by status guards; `Duplicate` additionally
   refuses a booking that already collected (`BookingNumber` set).
3. **No refund of a paid ticket.** The recoverer marks `Duplicate` (refund) only after a
   **conclusive** scan of our KTMB account that did not find our ticket, or when the found
   ticket is already claimed by a different `Completed` zinc booking. An _inconclusive_ scan
   (empty/blank list, mutated pagination) must retry, never refund.
4. **No double buy.** A re-buy that succeeds stashes its ticket identifiers so a later
   complete-failure force-completes deterministically instead of re-buying. A re-buy that
   conflicts spends no money (KTMB rejects pre-payment; the reservation is released).
5. **No stranded money.** A booking with money held in `BookingReserve` is never left in a
   state no path can resolve: `Recovering` is reconciled every cycle; `Buying` is covered by
   the queue item and the manual `recover` command; `RequireManualIntervention` is resolved by
   a human. Auto-refund (`RefundList`) only touches `Pending`, so it can never refund a
   `Recovering`/`Duplicate`/parked booking.
6. **Atomicity.** Every zinc money move + ledger row + status write commits or rolls back
   together (`ITransactionManager.Start`, RepeatableRead). Status-only recovery transitions
   also run inside the transaction so a stale guard-read cannot overwrite a concurrently
   committed terminal status.

## 4. Buyer parking behaviour (tin `lib/buyer`)

The buyer no longer crash-loops on a conflict. On a KTMB `SetPassenger` rejection matching the
configurable `buyer.conflictPatterns` (default `["duplicate passport"]`, raw messages logged
verbatim to pin the real text):

- **ConflictError** (pre-purchase duplicate): park the booking, release the KTMB reservation.
- **PurchasedError** (`Complete` succeeded but `PrintTicket` failed): the money is spent — park
  **with** the KTMB `bookingNo`/`ticketNo`, never release.
- **zinc complete failure** after a successful buy: retry with backoff (`buyer.completeRetries`);
  on persistent failure, park **with** identifiers.

`park()` ordering (durability-critical):

1. Push the encrypted `RecoverDto` onto the `recover` queue **with retry** — this is the sole
   durable store of a captured ticket's identifiers.
2. Transition `Buying → Recovering` **with retry** (the recoverer drives this itself if it
   fails, since a queued record exists).
3. Optionally release the reservation (never on the purchased path).

## 5. Recovery decision tree (tin `lib/recoverer`)

Runs as a **single-replica** `Deployment`, `robfig/cron`: `Drain` the queue every 15m
(`drainCron`, the fast path), and `Sweep` zinc for reconciliation hourly (`sweepCron`; each
sweep drains first). Per booking (`ProcessItem`):

1. Fetch from zinc. Not found → drop. Terminal/parked status → drop.
2. **Legacy corruption**: `BookingNo` set but status ≠ `Completed` → `manual-intervention`
   (reserve already collected; force-completing would double-collect).
3. If status `Buying` → drive `markRecovering` (covers a buyer whose transition didn't land).
4. **Deterministic**: `RecoverDto` carries `BookingNo`/`TicketNo` → `PrintTicket` →
   `complete/{id}` (force complete).
5. Departure already past (upcoming-list can't verify) or unparseable → `manual-intervention`.
6. **Scan** KTMB `UpcomingShuttleList` (binary-search the page whose datetime range spans the
   target, then scan every contiguous page sharing the target datetime; passport match):
   - found + unclaimed → force complete;
   - found + claimed by another `Completed` booking → `duplicate` (refund);
   - inconclusive (empty in-range page, mutated list) → **retry** (never refund);
   - not found → **re-buy probe** (reserve + buy):
     - success → complete (stash identifiers first);
     - `ConflictError` → release probe, **strict re-scan** (empty list = inconclusive → retry);
       found → force complete; conclusively not ours → `duplicate` (another channel);
     - `PurchasedError` → requeue deterministically with identifiers.
7. Item fails `maxAttempts` cycles → `manual-intervention`.

### Durability model (queue + sweep)

- The `recover` queue is a **fast-path hint** carrying the buyer's deterministic identifiers.
  zinc's `Recovering` status is the **durable source of truth**.
- `Drain` pops with a destructive RPop and records handled BookingIds.
- `Sweep` re-derives every `Recovering` booking from zinc and re-processes it, **skipping**
  bookings that were handled this cycle _or_ still have a live queue item
  (`queuedBookingIds` unions the queue contents into the skip set). Because `park()` pushes the
  queue item before the `Recovering` transition, any booking the sweep sees as `Recovering`
  with a live item is captured — leaving it for the deterministic drain path.
- **Single replica only.** The destructive-pop drain + concurrent sweep dedup assume one
  process. Enforced by Helm `replicaCount: 1` and no HPA; documented on the `recoverer.Client`.

### Manual recovery

`tin recover <passport> <date dd-MM-yyyy> <time HH:mm:ss> <direction JToW|WToJ>`: search zinc (PassportNumber filter),
interactively confirm each candidate, transition to `Recovering`, run the same classification.
This is the tool for bookings already stuck when the pipeline ships.

## 6. Per-repo change surface

- **helium** (`feat/remove-reverter`): delete `src/lib/reverter.ts`, the `reverter` CLI command
  - DI, the `reverter` chart alias + all `values.*.yaml` blocks + `Chart.lock`, tilt wiring,
    regenerated README.
- **zinc** (`feat/duplicate-recovery`): §2 states/endpoints/txn/search + guards; remove `revert`;
  unit tests for the mappers (all enum values) and the `DuplicateBooking` generator.
- **tin** (`feat/duplicate-recoverer`): `lib/buyer` conflict handling (§4), `lib/recoverer`
  (§5), `recoverer`/`recover` commands, `RecovererConfig` + `buyer.completeRetries`/
  `conflictPatterns`, regenerated `lib/zinc` SDK, `recoverer` helm alias, unit tests for the
  conflict matcher and the scanner (bisection, page-spanning, inconclusive/strict).
- **argon** (`feat/recovery-statuses`, base branch `pichu` — the active argon branch, NOT
  `main`): the SvelteKit frontend must stop calling the removed `revert` endpoint and render
  the three new statuses without crashing (§6.1).

### 6.1 argon frontend (SvelteKit)

The frontend types booking `status` as a plain nullable string (no enum), so the new values
compile fine but the status→color lookup map throws at render if a value is missing. Required
changes:

1. **Regenerate the API SDK from the updated zinc** — `task sdk-gen` (runs
   `scripts/local/sdk_gen.sh v1` against a live zinc at `API_URL`, default
   `http://127.0.0.1:9002`). This DROPS the now-dead `vBookingRevertCreate`, ADDS the
   `recovering`/`duplicate`/`manual-intervention` endpoints, and ADDS `PassportNumber` to the
   booking-search query. NEVER hand-edit `src/lib/api/core/*` (generated).
2. **`src/routes/bookings/book_status.ts`** — add `Recovering`, `Duplicate`,
   `RequireManualIntervention` to `BOOKING_STATUS` (value/label/color). REQUIRED: the map is
   indexed as `BOOKING_STATUS[status].color` in `BookingRow.svelte`, `Booking.svelte`, and
   `bookings/+page.svelte`; an unmapped status throws `Cannot read properties of undefined`.
   Adding entries also populates the status filter dropdown automatically.
3. **`src/lib/components/entities/Bookings/BookingRow.svelte`** — remove the admin "Buying
   (Click to revert)" button, the `revertBuying()` function, the `reverting` var, the `session`
   const, and the now-unused imports (`page`, `toResult`, `api`, `toast`, `invalidateAll`);
   render just the plain status badge.
4. **i18n `src/lib/i18n/locales/{en,ms,zh}.json`** — add `status.booking.Recovering`,
   `status.booking.Duplicate`, `status.booking.RequireManualIntervention`, and
   `status.transactionType.BookingDuplicate` in ALL THREE locales; remove the now-unused
   `bookingActions.row.{revertSuccess,revertError,clickToRevert}` keys.
5. **`src/lib/components/entities/Bookings/Booking.svelte`** — add `Duplicate` to the
   terminal-state list (`["Cancelled", "Refunded", "Terminated"]`) so a duplicated booking
   shows the terminal card.

Not in scope: exposing the new admin recovery actions or a passport-search UI — those are
tin/CLI-driven. argon only needs to stop calling the removed endpoint and render the new states.

## 7. Deploy sequencing

1. **helium** first — removing the reverter cron stops new corruption immediately.
2. **zinc** — new endpoints are additive; `revert` removal is safe once helium's reverter is gone.
3. **tin** — regenerate the SDK from deployed zinc, then deploy. Run §5 manual recovery for the
   currently-stuck bookings.
4. **argon** — regenerate its SDK from deployed zinc and deploy; safe any time after zinc since
   it only drops a call and adds display strings.

## 8. Acceptance checklist

- [ ] Money invariants §3.1–§3.6 hold on every reachable path.
- [ ] Every `BookStatus`/`TransactionType` switch/mapper/validator extended with the new values.
- [ ] No residual **live** `revert`/`Reverter`/`RevertBuying` reference in any repo's code, infra,
      or CI (call site, DI wiring, chart alias, values block, pipeline step). Regenerated-away SDK
      stubs and design docs that describe the removal — **this document included** — are exempt:
      they name the removed elements only to explain what was deleted and why, so a grep gate should
      exclude `docs/RECOVERY.md` and the canonical spec.
- [ ] Buyer never crash-loops on a conflict; a captured ticket is never dropped or released.
- [ ] Recoverer never refunds on an inconclusive scan and never double-buys.
- [ ] No booking with held money is left unresolvable by any path.
- [ ] Recoverer is single-replica (helm) and says so in code.
- [ ] go.mod stays at the pinned Go version; SDK regeneration introduces no toolchain bump.
- [ ] argon renders all three new statuses without a runtime crash and no longer calls the
      removed `revert` endpoint; `npm run check` passes.
- [ ] All four PRs green in CI.
