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
- **`datastoreMu`/`keystoreMu`** (storage latches): a `sync.Mutex` for
  datastore calls and `sync.RWMutex` for keystore calls, held only for one
  raw call. The datastore latch protects both the map and userlib's shared
  bandwidth counter, which even `DatastoreGet` mutates. The original
  read-latch implementation prevented map crashes but left concurrent
  Gets racing on that counter; Linux CI exposed this (section 8).
  All production access in `client.go` goes through five thin
  wrappers (`datastoreGet/Set/Delete`, `keystoreGet/Set`); audited clean
  (zero unwrapped production call sites).

## 7. Test evidence

The unmodified CI-phase baseline at `c170400` passed 43 white-box specs in
`client/*_test.go` (Ginkgo), 80 black-box specs in `client_test`, 15 standalone
`lockmanager` tests, and 4 benchmark UI tests: 142 tests/specs, zero failed,
pending, or skipped. Ginkgo's two Go test wrapper functions are not counted
again. Command: `go test -json -count=1 ./...`, Go 1.26.5, Windows/amd64.
Coverage includes:

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
  settles into one legal serial ordering across 30 concurrent iterations
  (the append/overwrite winner is scheduler-selected, not forced);
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
  state. The suite additionally re-validates persistent authorization
  invariants (owner-epoch == structure-epoch, surviving recipients stamped
  with the current epoch, Version preserved), not just return values.
- **Phase 4.5 hardening**: namespace-identity collision fix verified
  directly; TxnID allocator proven to fail *permanently*, not just once,
  past exhaustion; storage-latch coverage audited clean; test-hook
  get/set behavior exercised concurrently (the atomic accessor is not a
  claim about arbitrary callbacks installed by callers).
- **Phase 6**: the two benchmark-only baseline strategies were themselves
  validated before being trusted for benchmarking — `StrategyNoCC` is
  proven (deterministically, via forced scheduling, not luck) to reproduce
  the classic lost-update race; `StrategyGlobalLock` is proven to serialize
  even operations on independent files, unlike production SAFER-CC.

Earlier development reports recorded repeated suite runs. The CI-phase
baseline and pre-push rerun both passed, with no flakes observed in those
runs; this is not a guarantee of flake freedom. `-count=1` is supported and
disables cached results. Do not use `-count=N` for repeated Ginkgo suites;
rerun the process instead. Existing stress cases already run in `./...`,
so CI does not add a duplicate stress job.

## 8. Race-detector evidence/status

**Full Linux race-detector suite passed after the storage-counter fix.**
[Hosted run 33740706536](https://github.com/JamJamzzz/safer-with-concurrency-control/actions/runs/33740706536),
PR head `5a124d0`, passed both jobs on 2026-09-03 UTC: all 143 tests/specs,
zero failures/pending/skips, and no race reports. The full race job took
about 107 seconds including setup, so no reduced/targeted CI suite was needed.
This is PR evidence; subsequent default-branch executions are visible in
[main's CI history](https://github.com/JamJamzzz/safer-with-concurrency-control/actions/workflows/ci.yml?query=branch%3Amain).

The original Windows environment still has no C compiler
and `CGO_ENABLED=0`: a local `go test -race ./...` exits before testing with
`-race requires cgo`. CI supplies the missing toolchain instead of
pretending the local limitation has disappeared.

[CI](.github/workflows/ci.yml) runs on pull requests and pushes to `main`,
using two independent `ubuntu-latest` jobs with `contents: read` permissions.
Go comes from `go.mod` (`go 1.20`, no `toolchain` directive); the observed
runner used Go **1.20.14**, Linux/amd64, Ubuntu **24.04.4**, and GCC
**13.3.0**. The race job explicitly checks `CGO_ENABLED=1`, the compiler,
Go version, and all four packages before executing the full suite. The
normal job runs with cgo disabled, confirming cgo is not a project dependency.

Reproduce from the repository root (the second command uses a POSIX shell
and requires a supported Go race-detector platform plus a C compiler):

```bash
go test -mod=readonly -count=1 -v -timeout=10m ./...
CGO_ENABLED=1 CC=gcc go test -race -mod=readonly -count=1 -v -timeout=15m ./...
```

`-count=1` disables cached test results. No packages, existing stress cases,
or intentionally incorrect NoCC logical-control tests are excluded. No
failure suppression, retries, or performance gates are configured.

### Actual failure and fix

[First hosted run, 33740280680](https://github.com/JamJamzzz/safer-with-concurrency-control/actions/runs/33740280680)
tested PR head `b8447fd`: normal correctness passed; race detection failed
in `client`. All 43 white-box logical specs passed even in the failing race
job, but Go correctly failed the package for detected memory races. The
other three packages passed. This illustrates why both verification layers
are necessary.

The 32 race reports all share one root cause: both conflicting accesses
reach `project2-userlib@v0.5.1/userlib.go:139`, the read/modify/write
`*bandwidth += len(value)` inside `datastoreGet`. For example, two goroutines
in Phase 4's 100-append test follow
`AppendToFile -> loadNamespaceEntry -> loadDatastoreObject -> datastoreGet`,
then read/write the same counter. Other reports involve concurrent loads,
invitations, and revokes; both access stacks still end at that same line.
The shard's `sync.Map` protects lookup, not the pointed-to integer.

This is a metrics race on a production storage path, not a test-hook race
or merely a benchmark issue. `datastoreGet` formerly used `RLock`, allowing
multiple readers to modify that shared counter concurrently. The fix
(`908dc13`) uses one exclusive datastore mutex for each raw Get/Set/Delete
call. Keystore reads retain their read latch. Crypto and whole operations
are not moved inside this mutex, and namespace/file S/X strict 2PL is
unchanged; the existing independent-file and shared-reader tests remain.

The one added regression, `client/storage_concurrency_test.go`, starts 16
workers together, each reading its own key 64 times, then joins all workers
and checks exact bytes returned and bandwidth (7,168 bytes). The pre-fix
code actually reported 7,119; the fixed code passed. There are no sleep or
throughput assertions. Scheduling is not exhaustively controlled, so this
exact-value oracle complements rather than replaces race instrumentation.
The full post-fix local suite passed 44 white-box + 80 black-box specs + 15
lockmanager + 4 UI tests (143 total), zero failures/pending/skips.

### Interpretation and limits

A successful race run means no memory race was reported in that execution;
it does not prove universal race freedom, serializability, fairness, or
authorization correctness. Forced-ordering tests, lock-manager tests, and
persistent invariants check those higher-level properties for their tested
scenarios. The acquisition-site audit confirms Namespace-before-File and
deferred `ReleaseAll` for the six file/sharing operations; the tests exercise
successful and failing paths, not every possible failure injection.

CI covers one Linux/amd64 Go release series, not an OS/toolchain matrix.
Tests have bounded liveness waits and finite schedules. Locks coordinate
only participating goroutines in one process; direct external mutation of
userlib maps or mutable User fields bypasses that boundary. Test/benchmark
setup, clears, and diagnostic reads must remain outside active workers.
Account creation is not a file-level 2PL transaction. No crash rollback,
durability, or distributed coordination is established. The standalone
benchmark workload driver is not executed by `go test`; its UI tests and
the NoCC/GlobalLock strategy regressions are included.

GitHub-runner throughput/latency is never a correctness gate or resume
benchmark evidence. The checked-in benchmark tables predate the exclusive
read-latch fix; they are preserved as historical measurements, not claimed
as remeasurement of the corrected code.

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
  across the recorded file/sharing test suites and mixed-operation
  authorization benchmark (Workload E), not a formal deadlock proof.

## 12. Generated evaluation files

- `docs/concurrency-control.md` — full phase-by-phase design document.
- `benchmarks/benchmark-results.json` — raw machine-readable trial data.
- `benchmarks/benchmark-report.md` — human-readable methodology + tables +
  findings.
- `review.md` — this document.

## 13. Technically defensible resume bullet candidates

These historical candidates refer to the recorded, pre-CI benchmark
revision. Performance claims must be remeasured for the corrected datastore
latch before describing current-code performance:

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

Do **not** use: "distributed locking" (it is not distributed), "proven
race-free" (finite dynamic tests cannot prove that), or any
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
| Persistent authorization invariants | ✅ tested | Phase 5's `checkAuthorizationInvariants` and the benchmark diagnostic checker |
| Error-path lock release | ✅ tested | Phase 4 Test 25 |
| Go race-detector verification | ✅ full Linux suite passed | run `33740706536` after fixing the bandwidth race; see section 8 |

The entries distinguish code audits, logical assertions, and dynamic
memory-race checks; none is a proof across all executions.

## Verification scope

CI adds repeatable normal and instrumented execution to the existing
logical tests without adding a new concurrency model. Claims should remain
bounded by the run evidence, the tested invariants, and the limitations in
section 8. No new storage, deployment, or distributed-system feature is part
of this verification change.
