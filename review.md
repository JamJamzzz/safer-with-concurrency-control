# SAFER-CC V1 — Final Review

This document is the final, consolidated review of SAFER-CC V1: a strict
two-phase-locking (2PL) concurrency-control extension built on top of the
original CS161 SAFER secure file-system project. It supersedes the
per-phase completion reports in scope (this is the whole-system picture)
but does not repeat their full detail — see `docs/concurrency-control.md`
for the complete design rationale, phase by phase.

## 1. Project architecture

SAFER-CC keeps the original SAFER cryptographic architecture completely
intact — encrypted/MACed datastore objects, capability-based sharing via
`AccessBox`/`AccessBoxStructure`, signed `FileStatus` for revocation
detection, epoch-based key rotation. On top of that, it adds three new,
clearly separated layers:

- **`client/lockmanager`** — a generic, standalone, in-process
  shared/exclusive lock manager (fair FIFO queueing, no upgrades, no
  reentrancy, explicit `TxnID` ownership). Knows nothing about SAFER.
- **Metadata versioning** — an independent `Metadata.Version` counter,
  orthogonal to `Metadata.EpochID`, tracking logical content generations
  separately from authorization/revocation generations.
- **Strict-2PL integration** — every public SAFER operation
  (`StoreFile`, `AppendToFile`, `LoadFile`, `CreateInvitation`,
  `AcceptInvitation`, `RevokeAccess`) is now one transaction: it allocates
  a `TxnID`, acquires Namespace and File locks in a fixed order, revalidates
  authorization state after the File lock is held, performs its work, and
  releases everything via one deferred `guard.ReleaseAll()`.
- **Storage-engine latches** (`datastoreMu`/`keystoreMu`) — a separate,
  lower-level fix, unrelated to 2PL, protecting `userlib`'s raw
  (unsynchronized) Go maps from concurrent-map crashes.

## 2. Original concurrency failures (Phase 1 audit)

The pre-SAFER-CC implementation had no concurrency control at all. The
Phase 1 audit identified, and this project subsequently fixed or
explicitly documented, races including:

- **Lost updates on append**: two concurrent `AppendToFile` calls could
  both read the same stale `Metadata`, both compute `ChunkCount+1`, and
  the second's `DatastoreSet` on the metadata UUID would silently discard
  the first's chunk — even though both calls returned success. **Measured
  in Phase 6** (Workload A, NoCC strategy): up to 3,849 lost successful
  appends across repeated trials, growing with worker count.
- **Same-name create race**: two concurrent `StoreFile` calls for a
  not-yet-existing filename could both see "doesn't exist" and both
  publish a namespace entry, silently orphaning one file's objects.
- **Load vs. overwrite**: `LoadFile` walking a chunk chain with no lock
  could observe a partially-deleted chain mid-overwrite.
- **Accept vs. Revoke TOCTOU**: `AcceptInvitation` validated an `AccessBox`
  once, unlocked, then installed a `NamespaceEntry` from that snapshot —
  a concurrent `RevokeAccess` could commit in the gap, handing the
  recipient a namespace entry that looked installed but pointed at
  already-revoked capability state.
- **Concurrent `CreateInvitation`/`RevokeAccess`** both read-modify-wrote
  `AccessBoxStructure.RecipientBoxes` from stale snapshots, silently
  dropping recipients or resurrecting revoked ones under the old
  last-writer-wins pattern.

Full details: the Phase 1 audit is preserved verbatim in this
conversation's history and summarized in `docs/concurrency-control.md`.

## 3. Final lock/resource model

Two logical resource classes, both keyed unambiguously:

- **Namespace**`(username, filename)`, keyed by `canonicalNamespaceIdentity`
  — netstring-style length-prefixed encoding (`len(u):u len(f):f`),
  provably collision-free (unlike the original `username+"-"+filename`
  concatenation, which collided on inputs like `("a-b","c")` vs.
  `("a","b-c")`).
- **File**`(FileID)`, keyed by the UUID string, never by `MetadataUUID`,
  `ChunkUUID`, `AccessBoxUUID`, or filename — the one identifier every
  authorized user of a file agrees on.

Final operation matrix (unchanged from the Phase 4/5 design, frozen for
Phase 6 as instructed):

```text
LoadFile:              S(namespace) -> S(file)
AppendToFile:           S(namespace) -> X(file)
StoreFile (create):     X(namespace) -> X(new file)
StoreFile (overwrite):  X(namespace) -> X(file)
CreateInvitation:       S(namespace) -> X(file)
AcceptInvitation:       X(namespace) -> S(file)
RevokeAccess:           S(namespace) -> X(file)
```

Namespace is always acquired before File, with no exception across any of
the seven paths — this fixed ordering is what prevents deadlock (no
wait-for-graph detection is implemented or needed).

## 4. Strict-2PL semantics

Each public operation is exactly one transaction:

```text
allocate TxnID (CAS-based; permanently exhausted once math.MaxUint64
                is reached, never recycles)
↓
construct LockGuard, defer guard.ReleaseAll()
↓
acquire Namespace lock
↓
load/authenticate NamespaceEntry
↓
acquire File lock
↓
revalidate AccessBox/FileStatus fresh, under the File lock
↓
perform reads/mutation
↓
return (success or error) — ReleaseAll runs regardless
```

No lock is ever released and later reacquired within one transaction; no
helper function beneath the public API boundary acquires or releases a
lock itself. `resolveFile` (the old, single-shot, unlocked
namespace+file resolution) survives only as a test/inspection
convenience — no production operation calls it.

## 5. Version vs. Epoch

Two independent counters in `Metadata`:

| Field | Meaning | Changes on |
|---|---|---|
| `Version` | logical content generation | `StoreFile` (create=1), `AppendToFile` (non-empty content, +1), `StoreFile` overwrite (+1) |
| `EpochID` | authorization/revocation generation | `RevokeAccess` only |

`RevokeAccess` copies `Version` verbatim into the rotated `Metadata` —
authorization changing is not a content mutation. This was true from
Phase 3 onward; Phase 4/5's locking is what makes it *safe*: because
`AppendToFile`/overwrite/`RevokeAccess` all require File X on the same
resource, there is never a race between "what Version a concurrent
mutation just published" and "what Version `RevokeAccess` copies forward."

## 6. Lock vs. latch

Two unrelated mechanisms, deliberately not conflated:

- **`saferLockManager`** (transaction locks): S/X, per logical resource,
  may be held across an entire operation, provides SAFER's serializability
  guarantees.
- **`datastoreMu`/`keystoreMu`** (storage latches): plain `sync.RWMutex`,
  held only for the duration of one raw `userlib.Datastore*`/`Keystore*`
  call, exist only because `userlib`'s maps are not goroutine-safe (Go's
  runtime crashes the whole process on concurrent map access, independent
  of SAFER's own logic). Discovered when Phase 4's first real concurrent
  test crashed the process. All production access goes through five thin
  wrappers (`datastoreGet/Set/Delete`, `keystoreGet/Set`); audited clean
  (zero unwrapped production call sites).

## 7. Test evidence

54 white-box specs in `client/*_test.go` (Ginkgo) plus 15 standalone
`lockmanager` tests plus the full pre-existing black-box `client_test`
suite, covering every phase:

- **LockManager** (Phase 2): S/S coexistence, X blocks S/X, independent
  resources don't conflict, writer fairness (a queued X is never bypassed
  by later S requests), batched-reader granting, `ReleaseAll`, wrong-owner
  release rejected, duplicate/upgrade rejected, 200-goroutine reader test,
  40×50 mixed stress test with live S/X-exclusivity invariant checking.
- **Metadata versioning** (Phase 3): new file starts at 1; append/overwrite
  each advance exactly once; empty append is a no-op; `LoadFile`/
  `CreateInvitation`/`AcceptInvitation` never advance it; `RevokeAccess`
  preserves it exactly; overflow returns an explicit error, never wraps;
  round-trips through the real encrypt/MAC/decrypt path.
- **Core file-content 2PL** (Phase 4): 100 concurrent appends all survive
  exactly once; a forced schedule proves a second append cannot reach its
  own Metadata read until the first releases File X; Load-vs-overwrite in
  both orderings never observes partial/mixed state; append-vs-overwrite
  settles into one legal serial ordering across 30 forced iterations;
  concurrent overwrites serialize; same-name creates serialize into one
  logical file; independent files provably never block each other (hook-
  forced); 20 concurrent readers provably batch under Shared (not
  Exclusive); a failed operation's locks are provably not leaked.
- **Sharing/revocation 2PL** (Phase 5): 15 concurrent `CreateInvitation`s
  all survive; the classic RecipientBoxes-loss race is proven closed;
  Accept-vs-Revoke is tested in both forced orderings with the correct
  outcome each way; Load-vs-Revoke and Append-vs-Revoke likewise, both
  orderings; Revoke-vs-CreateInvitation in both orderings never loses or
  resurrects a recipient; two concurrent revokes on the same file both
  survive; Accept-vs-StoreFile namespace collision resolves to one legal
  state. Every test additionally re-validates persistent authorization
  invariants (owner-epoch == structure-epoch, surviving recipients stamped
  with the current epoch, Version preserved), not just return values.
- **Phase 4.5 hardening**: namespace-identity collision fix verified
  directly; TxnID allocator proven to fail *permanently*, not just once,
  past exhaustion; storage-latch coverage audited clean; test-hook
  race-safety confirmed.
- **Phase 6**: the two benchmark-only baseline strategies were themselves
  validated before being trusted for benchmarking — `StrategyNoCC` is
  proven (deterministically, via forced scheduling, not luck) to reproduce
  the classic lost-update race; `StrategyGlobalLock` is proven to serialize
  even operations on independent files, unlike production SAFER-CC.

Every test suite was re-run repeatedly (20+ full-suite iterations at
various points, since Ginkgo doesn't support `go test -count`) with zero
flakes observed.

## 8. Race-detector evidence/status

**NOT VERIFIED WITH GO RACE DETECTOR.** This development environment
(Windows) has no C compiler (`gcc`/`clang` not found, `CGO_ENABLED=0`),
and `go test -race` requires cgo. WSL is present but only as Docker
Desktop's minimal internal VM (`PRETTY_NAME="Docker Desktop"`, no `gcc`,
no `go`) — not a general-purpose Linux development environment, so no
toolchain was provisioned into it.

To verify once a suitable environment is available (Linux, WSL with a real
distro, or Windows with mingw-w64 on `PATH`):

```bash
go test -race ./...
go test -race -count=50 ./client/lockmanager/...
go test -race -count=20 ./client/...
```

Correctness in this environment was instead demonstrated via deterministic,
hook-forced concurrency tests (proving specific lock-ordering outcomes, not
timing-dependent guesses) and many repeated full-suite runs. This is
real evidence of *logical* correctness; it is not a substitute for `-race`
verification of Go-level memory-safety, and no claim of memory-race
freedom is made here.

## 9. Benchmark evidence

Full methodology, environment, and raw tables: `benchmarks/benchmark-report.md`
and `benchmarks/benchmark-results.json` (generated by `cmd/benchmark`,
runnable via `go run ./cmd/benchmark`). Environment: Windows/amd64, Go
1.26.5, 12 logical CPUs, default `GOMAXPROCS=12`, 5 repetitions per trial
(median reported).

Three strategies were compared running the **identical** SAFER business
logic (same crypto, same datastore calls, same chunk traversal, same
Version bookkeeping) — only the lock-guard implementation backing each
operation differs (`client.go`, `newOperationGuard`):

| Workload | Finding |
|---|---|
| A: same-file append, 64 workers | NoCC lost 3,849 successful writes across repetitions (measured, not simulated). GlobalLock and SAFER-CC both lost zero. SAFER-CC ran at 10,300 ops/s (median) vs. GlobalLock's 11,475 ops/s — SAFER-CC does **not** outperform a global lock here, as expected (both fully serialize one file), and the gap is honestly attributed to per-resource lock-manager bookkeeping overhead, not reported as a win. |
| B: 90% read / 10% write, 64 workers | SAFER-CC 2,778 ops/s vs. GlobalLock 1,061 ops/s — **2.6x**. |
| C: independent files, 64 workers / 32 files | SAFER-CC 49,337 ops/s vs. GlobalLock 8,772 ops/s — **5.6x**. Both strategies lost zero writes, including the 2-workers-per-file overlap case at 64 workers, where SAFER-CC's per-`FileID` File X correctly serialized only the colliding pairs while the other files ran fully in parallel. |
| D: 64 concurrent readers, one file | SAFER-CC 8,988 ops/s vs. GlobalLock 3,332 ops/s — **2.7x**, direct evidence that Shared-mode File locks let non-conflicting readers overlap where a global mutex cannot. |
| E: authorization contention (correctness-focused) | SAFER-CC: 28 attempts, 4 expected semantic rejections (Accept-vs-Revoke races resolving as designed), 0 unexpected errors, 0 persistent authorization-invariant violations. |

## 10. Correctness metrics (definitions used)

- `lostSuccessfulWrites`: `(initial Version + successful-mutation count) −
  final Version`, whenever positive. The primary, Version-based oracle.
- Errors are never aggregated into one vague bucket: Workload E explicitly
  separates `expectedRejects` (matched against known SAFER error-message
  substrings, e.g. "stale access box", "not a direct share") from
  `unexpectedErrors` (everything else).
- `authorizationInvariantViolations`: from
  `CheckAuthorizationInvariantsForBenchmark`, which independently
  re-derives owner-epoch/structure-epoch/Version/surviving-recipient-epoch
  consistency from persisted state — not inferred from return values.

## 11. Major tradeoffs (honest, including negatives)

- **Coarse `FileID` granularity**: content-mutation locking
  (`AppendToFile`/overwrite) and authorization-mutation locking
  (`CreateInvitation`/`RevokeAccess`) share one File X per file. A
  `CreateInvitation` call currently blocks a concurrent `AppendToFile` on
  the same file even though they touch disjoint state. Splitting this into
  `ContentResource(FileID)`/`AuthorizationResource(FileID)` is a plausible
  V1.5 optimization, deliberately not attempted here.
- **No MVCC**: readers block behind writers (S/X), rather than reading a
  consistent snapshot lock-free. An explicit V1 non-goal.
- **Process-local only**: `saferLockManager` coordinates goroutines sharing
  one process's memory. It is not, and does not claim to be, a distributed
  lock service.
- **Storage latches are a second, coarser mechanism** layered under the
  transaction locks — necessary because `userlib`'s maps aren't
  goroutine-safe, not a concurrency-control design choice.
- **Fairness has a cost**: Workload A shows SAFER-CC (10,300 ops/s) very
  slightly behind a global mutex (11,475 ops/s) on pure same-file
  contention — the FIFO-fair queue/map bookkeeping in `lockmanager` is
  real, measurable overhead when there is no parallelism to win back. This
  is reported honestly rather than concealed.
- **No wait-for-graph deadlock detection**: correctness instead rests on
  the fixed Namespace-before-File ordering, verified by code review (every
  acquisition site follows it) and empirically by zero deadlocks/hangs
  across dozens of full-suite and benchmark runs involving all seven
  operations mixed together (Workload E).

## 12. Generated evaluation files

- `docs/concurrency-control.md` — full phase-by-phase design document.
- `benchmarks/benchmark-results.json` — raw machine-readable trial data.
- `benchmarks/benchmark-report.md` — human-readable methodology + tables +
  findings.
- `review.md` — this document.

## 13. Technically defensible resume bullet candidates

Based only on the measured evidence above:

- *Designed and implemented a fine-grained shared/exclusive lock manager in
  Go with strict two-phase locking, deterministic lock ordering, and
  capability revalidation under lock, eliminating a measured lost-update
  rate of up to 3,849 silently-dropped writes under concurrent same-file
  appends (100% of the loss the uncoordinated baseline exhibited).*
- *Achieved 5.6x median throughput over a global-mutex baseline on a
  64-worker/32-file independent-file workload, and 2.7x on a 64-reader
  concurrent-read workload, by replacing whole-system serialization with
  per-resource Namespace/File locking — while measuring, and honestly
  reporting, near-parity (not a win) under same-file write contention where
  no additional parallelism is structurally available.*
- *Built a 3-strategy (no-coordination / global-lock / fine-grained 2PL)
  benchmark harness in Go that exercises identical business logic across
  strategies via a shared lock-guard interface, producing reproducible,
  version-counter-verified correctness evidence alongside performance
  comparisons.*
- *Diagnosed and fixed a Go-runtime-level concurrent-map crash in an
  underlying, non-thread-safe storage layer, distinguishing and separately
  implementing storage-engine memory-safety latches from higher-level
  transaction locks.*

Do **not** use: "distributed locking" (it is not distributed), "verified
race-free with `go test -race`" (not run in this environment), or any
specific "X times faster" claim not tied to the exact workload/worker
count/median-of-N stated above.

## 14. Final correctness/race-verification checklist

| Property | Status | Evidence |
|---|---|---|
| Lock compatibility (S/S, S blocks X, X blocks all) | ✅ tested | lockmanager Tests A-D |
| Writer fairness (no starvation) | ✅ tested | `TestWriterFairness`, `TestBatchedReadersAheadOfWriter` |
| No lock upgrade / no reentrancy | ✅ tested | `TestDuplicateAndUpgradeRejected` |
| Strict 2PL (locks held through commit) | ✅ tested | Phase 4 Test 18, all Phase 5 forced-ordering tests |
| Deterministic lock ordering (Namespace before File, no exceptions) | ✅ verified | code audit (every acquisition site) + zero hangs across all stress/benchmark runs |
| Canonical namespace identity (collision-free) | ✅ tested | Phase 4.5 A1 tests |
| TxnID uniqueness / permanent exhaustion | ✅ tested | Phase 4.5 A2 test |
| `Version` semantics (init/advance/no-op/overflow/round-trip) | ✅ tested | Phase 3 Tests A-J |
| `EpochID` semantics (rotates only on revoke, `Version` preserved) | ✅ tested | Phase 5 revoke tests + `checkAuthorizationInvariants` |
| Capability revalidation under File lock | ✅ tested | Phase 5 Tests 3-8 |
| Datastore/keystore latch coverage | ✅ audited | Phase 4.5 A3, re-confirmed in Phase 6 |
| Persistent authorization invariants | ✅ tested | `CheckAuthorizationInvariantsForBenchmark`, used in every Phase 5/6 test |
| Error-path lock release | ✅ tested | Phase 4 Test 25 |
| Go race-detector verification | ❌ not verified | no C toolchain in this environment (documented above) |

Every property above has direct test or benchmark evidence except
`-race` verification, which is explicitly and honestly flagged as
outstanding rather than assumed.

## Recommendation

SAFER-CC V1 is **resume-ready as a systems/concurrency-control project**,
with the caveats stated throughout this document disclosed alongside any
claim: correctness is backed by ~55 deterministic, hook-forced concurrency
tests plus real (not simulated) benchmark evidence including one honestly
reported non-win; the one open item is Go race-detector verification,
which requires an environment this session does not have and should be
run before any claim of memory-race freedom. No further V1 feature work
(caching, MVCC, distributed locking, WAL, replication) should be added —
V1's scope is complete and evaluated.
