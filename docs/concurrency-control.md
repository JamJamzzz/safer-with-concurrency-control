# SAFER-CC V1: Concurrency Control Design

This document is written incrementally, phase by phase. This revision covers
**Phase 2** (the standalone `lockmanager` package), **Phase 3** (independent
file-content versioning in `Metadata`), **Phase 4** (strict 2PL integration
for `StoreFile`/`AppendToFile`/`LoadFile`), **Phase 4.5** (hardening fixes to
namespace identity, TxnID allocation, and test instrumentation), and
**Phase 5** (strict 2PL integration for
`CreateInvitation`/`AcceptInvitation`/`RevokeAccess`). As of Phase 5, every
public SAFER file/sharing/revocation operation participates in the common
protocol -- see "System-wide protocol summary (Phase 5)" near the end of
this document for the final operation matrix and the precise scope of that
claim.

## CI verification

The phase-by-phase sections below include historical development notes.
The current CI commands, actual hosted-run evidence, storage-counter race
analysis, and remaining limits are maintained in
[review.md, section 8](../review.md#8-race-detector-evidencestatus).
The workflow runs the complete normal and race-instrumented suites on
GitHub-hosted Linux for pull requests and pushes to `main`. The original
Windows no-cgo/C-compiler limitation is local, not a reason to omit the
Linux race job. The complete normal and race suites passed in
[run 33740706536](https://github.com/JamJamzzz/safer-with-concurrency-control/actions/runs/33740706536)
after the datastore bandwidth-counter fix, with 143 tests/specs and no skips.

Race detection concerns memory accesses in executed paths; strict-2PL and
authorization-invariant tests concern logical operation outcomes. Neither
layer proves all possible schedules correct. Runner timings are not
performance gates, and the historical benchmark files are not updated by CI.

## LockManager (`client/lockmanager`)

### Purpose and scope

`lockmanager` is a generic, in-process, fine-grained shared/exclusive lock
manager. It is intentionally decoupled from SAFER: it has no knowledge of
`User`, `NamespaceEntry`, `AccessBox`, `Metadata`, `Chunk`, invitations, or
revocation. It understands only:

- `ResourceID{Type, Key}` — an opaque, comparable identifier for a lockable
  resource (`Type` is `NamespaceResource` or `FileResource` for SAFER-CC's
  purposes, but the manager never branches on it);
- `LockMode` — `SharedLock` or `ExclusiveLock`;
- `TxnID` — a caller-supplied logical transaction/operation identifier.

This separation is deliberate: it lets the lock manager be defended (and
tested) as a reusable concurrency-control primitive, not a grab-bag of
SAFER-specific mutexes. SAFER's own operations (Phase 4+) will construct
`ResourceID` values such as `Namespace(username, filename)` and
`File(FileID)`, but that mapping lives entirely in `client.go`, not here.

### Logical lock modes and compatibility

| Held | Request S | Request X |
| ---- | --------- | --------- |
| none | grant     | grant     |
| S    | grant     | block     |
| X    | block     | block     |

Only S and X are supported in V1. Intent locks (IS/IX/SIX) are explicitly
out of scope — SAFER-CC V1 only needs two resource classes and never takes
locks on a resource's ancestor/descendant hierarchy, so intent modes would
add complexity without buying anything.

### Ownership: explicit TxnID, not goroutine identity

Every `Acquire`/`Release`/`ReleaseAll` call takes an explicit `TxnID`
supplied by the caller. Go does not expose stable goroutine identifiers by
design, and even if it did, tying lock ownership to goroutine identity would
make the lock manager fragile to any future change in how a SAFER operation
is scheduled (e.g. if part of an operation's work were ever handed to a
helper goroutine). An explicit, caller-chosen `TxnID` makes ownership a
first-class, portable concept — this is also what will let Phase 4+ map
"one call to `AppendToFile`" cleanly onto "one logical transaction."

### No lock upgrades (S → X)

`Acquire` rejects any second acquisition attempt by a transaction that
already holds *any* lock (of any mode) on that resource — this includes the
S→X upgrade case. It returns an error immediately; it never blocks waiting
for an upgrade.

This is deliberate, not a missing feature: no current or planned SAFER-CC V1
operation needs a mid-operation lock upgrade (every operation is designed,
per the Phase 1 audit, to acquire the strongest mode it will need *before*
entering its critical section — e.g. `AppendToFile` takes File **X**
up front, it never starts with File **S** and tries to escalate). Supporting
upgrades would also introduce a classic upgrade-deadlock hazard (two
transactions both holding S and both trying to upgrade to X on the same
resource deadlock against each other), which strict two-phase locking with
this kind of "acquire everything you'll need first" discipline avoids
entirely without needing wait-for-graph detection.

### No reentrancy

A transaction acquires each resource at most once. A duplicate acquisition
(same mode) is also rejected rather than silently turned into a recursive
hold — SAFER-CC's operations are each expected to compute exactly which
resources they need once, up front, so reentrancy would only mask a bug
(e.g. accidentally calling a lock-taking helper twice within one logical
operation) rather than serve a legitimate use case.

### Internal mutex vs. logical locks

These are two different things, and the distinction matters for both
correctness and for explaining the design in an interview:

- **`lm.mu`** (internal, unexported): a single `sync.Mutex` protecting the
  `LockManager`'s own bookkeeping — the resource map, holder sets, and wait
  queues. It is held only for the brief duration of a bookkeeping mutation
  (check ownership, enqueue/dequeue a request, flip a holder flag,
  broadcast). It is **never** held across a caller's actual work — a
  goroutine blocked in `Acquire` releases `lm.mu` while parked in
  `sync.Cond.Wait()`, and a goroutine holding a *granted* lock does not hold
  `lm.mu` at all between its `Acquire` and `Release` calls.
- **Logical S/X locks**: database-style transaction locks representing "txn
  T holds S/X on resource R." These may remain logically held across an
  entire SAFER operation (e.g. an entire `AppendToFile` call, including its
  datastore reads/writes in later phases) without `lm.mu` being held for
  any of that time.

Each resource's wait queue uses its own `*sync.Cond`, but every cond for
every resource is bound to the *same* `lm.mu`. This lets `Broadcast` wake
only the goroutines waiting on one specific resource (each resource's cond
is distinct), while still using a single, simple mutex for all bookkeeping
consistency — there is no risk of two different resources' bookkeeping
being updated inconsistently relative to each other, since only one
goroutine can be inside the bookkeeping critical section at a time.

### Fairness / queueing algorithm

Each resource maintains a FIFO queue of *pending* (not yet granted)
requests. A request is removed from the queue the instant it is granted.

A pending request `req` for resource `R` is grantable iff:

1. it is compatible with `R`'s currently granted holders (X requires zero
   holders of any kind; S requires no X holder), **and**
2. every other request still pending strictly ahead of `req` in the queue is
   itself Shared, and `req` is also Shared.

Consequence: compatible Shared requests can be granted together as a batch,
even if they were enqueued at different times relative to each other and
even if earlier ones haven't been individually "checked" yet — this gives
real reader concurrency (Test G / Test K verify this directly by checking
`sharedHolderCount` reaches the full batch size). But the moment an
Exclusive request is sitting in the queue, no Shared request behind it can
be granted ahead of it, no matter how many more readers arrive afterward —
this is what prevents indefinite writer starvation (Test F verifies this
directly: a writer queued behind one reader is not bypassed by a second
reader that arrives while the writer is still waiting).

Every bookkeeping mutation (a grant, which dequeues a request and adds a
holder; a release, which removes a holder) broadcasts on that resource's
cond so that all its current waiters re-check their grant predicate. Spurious
wakeups are handled correctly because `Acquire` always waits in a
`for !canGrant(req) { cond.Wait() }` loop, never a single `if`.

### Deletion of empty per-resource state

A resource's bookkeeping record (`lockState`) is removed from the manager's
map only when it has *no* granted holders **and** an *empty* wait queue.
This is a deliberately conservative rule: any goroutine currently blocked in
`cond.Wait()` for a resource has an entry in that resource's queue for the
whole time it is blocked, so "queue is empty" is sufficient proof that no
goroutine could be stranded waiting on a soon-to-be-discarded `sync.Cond`.
The tradeoff is that a resource with zero holders but a still-draining
queue keeps its (nearly empty) record alive a little longer than strictly
necessary — accepted deliberately, as documented in-line, in exchange for
never risking a stranded waiter.

### API surface

```go
func NewLockManager() *LockManager

func (lm *LockManager) Acquire(txn TxnID, resource ResourceID, mode LockMode) error
func (lm *LockManager) Release(txn TxnID, resource ResourceID) error
func (lm *LockManager) ReleaseAll(txn TxnID) error

type LockGuard struct{ ... }
func NewLockGuard(lm *LockManager, txn TxnID) *LockGuard
func (g *LockGuard) Acquire(resource ResourceID, mode LockMode) error
func (g *LockGuard) ReleaseAll() // idempotent
```

`LockGuard` exists purely to make future integration call sites error-safe
by construction: `defer guard.ReleaseAll()` immediately after constructing
the guard means every return path — success or any error — releases every
lock the operation acquired, without each call site needing to hand-write
its own multi-resource cleanup logic. It adds no locking behavior beyond
that; `LockManager` remains the sole source of truth for lock semantics.

No internal mutable state is exposed publicly. A few unexported,
package-internal accessors (`queueLen`, `sharedHolderCount`,
`hasExclusiveHolder`) exist solely inside the test file for deterministic
test synchronization (e.g. "has T2's X request actually reached the wait
queue yet, so T3's arrival is a real race and not a guess") — they are not
part of any production API.

### Known limitations (Phase 2)

- This is an **in-process** lock manager. It coordinates goroutines within
  a single Go process that share one `*LockManager` instance. It is **not**
  a distributed lock service and makes no claims about coordinating across
  separate machines or processes — that would require an entirely different
  design (leases, consensus, a network protocol) which V1 explicitly does
  not attempt.
- There is no deadlock *detection* (no wait-for-graph). Deadlock avoidance
  in SAFER-CC V1 instead relies on a deterministic acquisition order
  (Namespace before File) enforced by each API operation, established in
  Phase 1 and to be verified per-operation in Phase 4/5. The lock manager
  itself has no opinion about ordering — it is the caller's responsibility.
- No lock timeouts / cancellation are implemented; `Acquire` blocks until
  granted. This matches the strict-2PL usage model (an operation commits to
  acquiring what it needs) and keeps the primitive simple, at the cost of
  not being able to recover from a caller that never releases (which strict
  2PL with `defer`/`LockGuard` discipline in later phases is intended to
  prevent structurally, not via manager-side timeouts).
- During Phase 2, the Windows development environment lacked the C
  toolchain/cgo needed by the race detector. This historical limitation
  does not describe the current Linux CI setup; see "CI verification"
  above for current status. At that development stage,
  50 repeated full-suite runs (`go test -count=50 ./client/lockmanager/...`,
  ~90s) passed with no flakes, including the stress test that continuously
  checks the "at most one X holder, and S/X mutual exclusion" invariant
  across 40 goroutines × 50 iterations × 3 shared resources.

## File Content Versioning (Phase 3)

### EpochID vs. Version

`Metadata` now carries two independent generation counters:

| Field | Meaning | Changes when |
|---|---|---|
| `EpochID` | Security / authorization / revocation generation | `RevokeAccess` rotates capabilities |
| `Version` | Logical file-content generation | The bytes a reader gets from `LoadFile` actually change |

They are deliberately uncoupled. A revocation can happen with the file's
bytes completely unchanged (`EpochID` advances, `Version` does not); a
content mutation can happen with authorization completely unchanged
(`Version` advances, `EpochID` does not). Conflating them would make it
impossible to answer "did the content change" or "did the authorization
change" independently -- which matters for concurrency reasoning (Phase 4's
locking is scoped around content mutation, not authorization churn) and,
per the Phase 3 brief, will eventually matter for cache validation as well
(out of scope here).

```text
create file, write "A"        -> (E1, V1)   content = "A"
append "B"                    -> (E1, V2)   content = "AB"
append "C"                    -> (E1, V3)   content = "ABC"
overwrite with "XYZ"          -> (E1, V4)   content = "XYZ"   (ChunkCount resets to 1; Version does not)
RevokeAccess(some recipient)  -> (E2, V4)   content = "XYZ"   (EpochID rotates; Version is carried over unchanged)
append "!"                    -> (E2, V5)   content = "XYZ!"
```

### Which operations touch Version

| Operation | Touches Metadata | Advances Version |
|---|---|---|
| `storeNewFile` | creates it | initializes to **1** |
| `overwriteExistingFile` | replaces it | **+1** (regardless of new `ChunkCount`) |
| `AppendToFile`, non-empty content | rewrites it | **+1** |
| `AppendToFile`, empty content (`len(content)==0`) | no-op, returns early | unchanged |
| `LoadFile` | reads it | unchanged |
| `CreateInvitation` | does not touch `Metadata` at all (only `AccessBoxStructure`) | unchanged |
| `AcceptInvitation` | does not touch `Metadata` at all (only installs a `NamespaceEntry`) | unchanged |
| `RevokeAccess` | migrates it to a new `MetadataUUID`/keys under the new epoch | **unchanged** (copied from the pre-revocation value) |

The `RevokeAccess` row is the one that requires care and is called out
explicitly in the audit below: it is tempting to treat "we just constructed
a brand-new `Metadata` object at a brand-new UUID" as "this is new content,"
but it isn't -- it is the same logical bytes, re-encrypted under a new
`FileRoot`/keys and re-chunked into a single base chunk. `Version` is copied
from the `Metadata` that was loaded (and whose content was read) earlier in
`RevokeAccess`, not recomputed or incremented.

### Overflow handling

All increments go through:

```go
func nextMetadataVersion(current uint64) (uint64, error)
```

which returns an explicit error at `current == math.MaxUint64` instead of
silently wrapping to `0` (which would collide with the sentinel described
below). No call site performs `metadata.Version++` directly.

### Version == 0 handling: strict V1 (no legacy compatibility layer)

Phase 3's instructions offered two options: strict validation, or explicit
legacy-zero normalization. This repository has **no persisted, cross-run
datastore state** (`userlib`'s datastore is in-memory and every test starts
from `DatastoreClear()`/`KeystoreClear()`), and neither the black-box test
suite (`client_test`) nor the pre-existing white-box suite constructs a
`Metadata` value directly or depends on a version-less serialized fixture.
There is therefore nothing to migrate. **Strict V1 (Option A)** was chosen:
every code path that constructs `Metadata` now sets `Version >= 1`
(`storeNewFile` sets exactly `1`; `overwriteExistingFile`/`AppendToFile` use
`nextMetadataVersion`; `RevokeAccess` copies the prior value), and
`loadMetadata` rejects `Version == 0` as invalid/corrupted metadata, the
same way it already rejects `ChunkCount == 0` or a nil `TailUUID`. `0`
remains reserved as an explicit "invalid/uninitialized" sentinel value,
never a valid on-disk version for a real file.

### What Version does NOT do (yet)

Version is **not** a concurrency-control mechanism by itself in Phase 3.
`AppendToFile` and `overwriteExistingFile` still read `Metadata` (including
`Version`) with no lock held, compute `old.Version + 1`, and publish it --
exactly the same lost-update shape that already existed for `TailUUID` and
`ChunkCount` before this phase (see the Phase 1 audit). Concretely:

```text
T1: read metadata, Version = 6
T2: read metadata, Version = 6      (before T1 has published anything)
T1: publish Version = 7
T2: publish Version = 7             (T2 overwrites T1's write; T1's chunk is orphaned)
```

Both operations *believe* they advanced the version by one, and the
persisted value does go from 6 to 7 -- but one writer's actual content
mutation silently disappears, which is precisely the "lost update" failure
mode from the Phase 1 audit. Version numbers alone do not detect or prevent
this: nothing in Phase 3 compares an expected version to a currently-stored
version before writing (that would require either a lock or an
optimistic-concurrency compare-and-swap, neither of which Phase 3
implements). This is intentional and is the reason Phase 4 exists: once the
LockManager is integrated, `AppendToFile`/`overwriteExistingFile` will
re-load `Metadata` only *after* acquiring the file resource's exclusive
lock, which is what actually closes this race. Phase 3 on its own gives
SAFER a meaningful, independently-testable content-generation counter; it
does not yet give SAFER a safe way to advance it under concurrency.

## Strict Two-Phase Locking: core file-content operations (Phase 4)

Phase 4 integrates `client/lockmanager` into `StoreFile` (both its create
and overwrite paths), `AppendToFile`, and `LoadFile`. `CreateInvitation`,
`AcceptInvitation`, and `RevokeAccess` are **not** integrated yet -- they
still exhibit every race documented in the Phase 1 audit, and remain that
way until Phase 5. **SAFER-CC is not yet linearizable as a whole system.**
The claim Phase 4 supports is narrower and precise: *the three
Phase-4-integrated operations are now mutually coordinated under strict
2PL over Namespace/File resources; cross-races involving sharing or
revocation remain open until Phase 5.*

### Transaction boundary

Each call to `StoreFile`, `AppendToFile`, or `LoadFile` is exactly one
logical transaction. It allocates one `lockmanager.TxnID` (via
`allocateTxnID`, a `sync/atomic`-backed monotonic counter with 0 reserved
as invalid and an explicit error on the practically-unreachable wraparound
case -- never a silent recycle), builds one `lockmanager.LockGuard` for
that TxnID, and `defer`s `guard.ReleaseAll()` immediately, before any lock
is acquired. Every lock the operation takes goes through that one guard, so
every return path -- success or any error -- releases every lock the
operation was holding. No helper function below the public API boundary
acquires or releases any lock; `loadNamespaceEntry`,
`validateFileAccessUnderLock`, `storeNewFileLocked`,
`overwriteExistingFileLocked`, `loadMetadata`, and
`loadFileContentAndChunkUUIDs` all assume their caller already holds
whatever lock their read/write requires, and none of them touch
`saferLockManager`. This keeps "who owns transaction/lock lifetime" visible
in the code: public APIs do, helpers don't.

### Resource acquisition per operation

```text
LoadFile:            S(namespace(user, filename))  ->  S(file(FileID))
AppendToFile:         S(namespace(user, filename))  ->  X(file(FileID))
StoreFile (create):   X(namespace(user, filename))  ->  X(file(new FileID))
StoreFile (overwrite):X(namespace(user, filename))  ->  X(file(existing FileID))
```

`Namespace` is always acquired before `File`, with no exception across any
of these four paths -- the same global ordering invariant established in
the Phase 1 audit. `StoreFile` needs Namespace **X**, not S, in both its
branches, because it performs a check-then-act on namespace existence
(`datastoreGet(nameUUID)`) and that check must be atomic with respect to
every other concurrent `StoreFile` call for the same `(user, filename)` --
that is what eliminates the same-name double-create race from the Phase 1
audit (item 4) as a side effect of Phase 4, even though `CreateInvitation`/
`AcceptInvitation` were not touched. `LoadFile` and `AppendToFile` only need
Namespace **S**, because neither changes which `FileID` the namespace entry
points to.

### Lock lifetime

All locks acquired by a transaction are held for the operation's entire
duration -- through validation, all reads, and all writes -- and released
only once, together, via the deferred `guard.ReleaseAll()` at the very end
(success or error). This is strict 2PL: there is no "acquire, read, release,
compute, reacquire" pattern anywhere in the integrated operations, and no
lock is ever released and later re-acquired within the same logical
transaction.

### Why `resolveFile` had to be split

Before Phase 4, `resolveFile` did five things in one unbroken sequence:
load+authenticate the `NamespaceEntry`, then load+authenticate the
`AccessBox`, then verify `FileStatus`. That sequence cannot be locked
correctly as one unit, because the `FileID` (needed to know *which* file
lock to acquire) is only discovered partway through it (after the
`NamespaceEntry` load), but the `AccessBox`/`FileStatus` validation that
follows is exactly the state a concurrent `RevokeAccess` can invalidate --
so that validation is only meaningful if it happens *after* the file lock
is held, not before. Calling the old, monolithic `resolveFile` from inside
a locked operation would either validate too early (before the file lock,
reintroducing the TOCTOU race Phase 4 exists to close) or require
re-validating a second time under the lock anyway (redundant and easy to
forget).

Phase 4 splits this into two pieces with disjoint locking requirements:

- **`loadNamespaceEntry(userdata, filename)`** -- loads and authenticates
  only the `NamespaceEntry`. Safe under `namespace(user, filename)` S or X.
  Returns the `FileID` needed to know which file lock to acquire next.
- **`validateFileAccessUnderLock(namespaceEntry)`** -- loads the
  `AccessBox` and verifies `FileStatus` against it. Its result is
  authoritative *only* because the caller's contract is to call it after
  already holding `file(namespaceEntry.FileID)` (S or X) -- that is what
  guarantees it observes any `RevokeAccess` that already committed, and
  (once `RevokeAccess` itself takes File X, in Phase 5) that none can
  commit while this read is in flight.

`resolveFile` itself was kept, rewritten as a thin, unlocked composition of
these two pieces (`loadNamespaceEntry` then `validateFileAccessUnderLock`,
back to back, no lock acquired) -- byte-for-byte behavior compatible with
its pre-Phase-4 form. It exists solely so `CreateInvitation`,
`AcceptInvitation`, and `RevokeAccess` (unintegrated until Phase 5) keep
working exactly as before. **Phase-4-integrated operations never call
`resolveFile`** -- they call the two pieces directly with the appropriate
lock acquired in between.

### Version interaction

```text
AppendToFile A:  S(ns) -> X(file) -> load Metadata (V8)  -> publish V9  -> release
AppendToFile B:  S(ns) -> X(file) -> load Metadata (V9)  -> publish V10 -> release
```

B's `X(file)` acquisition blocks until A's `guard.ReleaseAll()` runs, so B's
`loadMetadata` call is guaranteed to observe A's published `V9` -- not the
`V8` A itself started from. This is the mechanism (not merely an
observation) that eliminates the lost-update race Phase 3 documented as its
explicit limitation: `Metadata` is loaded once per operation, strictly
after `File X` is granted, and never reused from a snapshot taken earlier.
`overwriteExistingFileLocked` follows the identical pattern. `LoadFile`
only ever takes `File S`, so it never mutates `Version`, but its `S`
acquisition still guarantees it cannot observe a half-published
append/overwrite: it cannot even begin its `AccessBox`/`Metadata`/chunk
reads while an `X` holder is mid-mutation, and no `X` request can be
granted while it holds `S`.

### Storage-layer thread safety (a necessary, distinct fix)

Integrating real concurrency exposed a second, lower-level problem that
strict 2PL over SAFER's own resources does not by itself solve:
`userlib`'s `Datastore`/`Keystore` are implemented as plain, unsynchronized
Go maps. Two goroutines operating on *different*, unrelated
`saferLockManager` resources (e.g. two different `FileID`s, correctly
running concurrently exactly as intended) still both call into the same
underlying map, and Go's runtime crashes the entire process
(`fatal error: concurrent map read and map write`) the instant that
happens -- independent of the race detector, independent of which keys are
touched. This was reproduced directly by Phase 4's own 100-concurrent-append
test before this fix.

This is a different mechanism from `saferLockManager` and intentionally
does not reuse it: storage latches (`datastoreMu`, `keystoreMu`)
guard every production `userlib.Datastore*`/`Keystore*` call, via thin
wrappers (`datastoreGet`/`datastoreSet`/`datastoreDelete`/`keystoreGet`/
`keystoreSet`) that every production call site in `client.go` now goes
through instead of calling `userlib.*` directly. It has no `TxnID`, no 2PL
semantics, and no relationship to Namespace/File resource identity -- it is
a storage-engine-level latch, analogous to a real database's buffer-pool
latching being distinct from its transaction manager's row/table locks.
Without it, Phase 4's own concurrency would crash the process before its
locking logic could even be observed to work.

CI-phase correction: the original pair of `sync.RWMutex` latches was
insufficient. `userlib.DatastoreGet` increments a shared bandwidth counter,
so even reads of unrelated keys must not overlap inside userlib. The
datastore latch is now a `sync.Mutex` held for one raw call; keystore retains
its `sync.RWMutex`. This does not serialize entire operations, crypto work,
or logical readers. Existing independent-file and shared-reader tests
continue to check those distinctions. Raw test/benchmark resets and
diagnostics must only run after workers have stopped.

### Current limitations (Phase 4, superseded below)

- `CreateInvitation`, `AcceptInvitation`, and `RevokeAccess` are **not**
  under strict 2PL yet. All Phase 1 races involving them (concurrent
  `RecipientBoxes` mutation, Accept-vs-Revoke TOCTOU, Revoke-vs-unintegrated-
  reader/writer) remain exactly as documented. Phase 5 integrates them with
  the planned lock modes `CreateInvitation: S(namespace) -> X(file)`,
  `AcceptInvitation: X(namespace) -> S(file)`, `RevokeAccess:
  S(namespace) -> X(file)`.
- No claim of system-wide linearizability is made yet -- only that the
  three Phase-4-integrated operations are mutually serializable/consistent
  with each other.
- At the Phase 4 milestone, the race detector had not run because of the
  local C-toolchain limitation. The hook-driven tests and repeated runs
  were logical correctness evidence, not memory-race verification. See
  "CI verification" above for the later hosted race results.

*(This bullet list described the state of the system as of Phase 4. It is
now superseded -- Phase 5, below, integrates `CreateInvitation`/
`AcceptInvitation`/`RevokeAccess` exactly as planned here. It is left in
place as a historical record of what was and wasn't true at that point.)*

## Phase 4.5: hardening fixes

Three correctness gaps were found and closed before starting Phase 5
integration, since building sharing/revocation locking on top of a shaky
namespace-identity or TxnID-allocation foundation would have propagated the
same bugs into three more operations.

### Namespace identity: from ambiguous concatenation to length-prefixed encoding

`getNameSpaceEntryUUID` previously derived a `NamespaceEntry`'s identity
(and, since Phase 4, `namespaceResourceID`'s lock identity, since the
latter is defined in terms of the former) from `username + "-" + filename`.
This is ambiguous: `username="a-b", filename="c"` and `username="a",
filename="b-c"` both concatenate to `"a-b-c"`, so they hash to the *same*
`NamespaceEntry` UUID and the *same* `NamespaceResource` lock key -- one
user's file could alias another user's differently-named file, and their
operations on those "different" files would incorrectly serialize against
(or worse, read/write) each other's namespace state.

The fix, `canonicalNamespaceIdentity(username, filename)`, uses
netstring-style length-prefix encoding:
`strconv.Itoa(len(username)) + ":" + username + strconv.Itoa(len(filename))
+ ":" + filename`. A decoder (conceptually; SAFER-CC never actually decodes
this, only hashes it) reads the decimal digits up to `:`, then consumes
exactly that many bytes verbatim -- regardless of what characters they
contain, including further `:` or `-` characters -- before moving to the
next field. Because each field's length is stated before its content, no
byte sequence in one field can be reinterpreted as spilling into another
field's length or content, so the mapping from `(username, filename)`
pairs to encoded byte strings is injective. Both `getNameSpaceEntryUUID`
(datastore identity) and `namespaceResourceID` (lock identity) go through
this one function, so the two can never diverge.

### TxnID allocation: from single-wrap detection to permanent exhaustion

The Phase 4 allocator was `id := counter.Add(1); if id == 0 { error }`.
This only catches the one call that happens to observe the wraparound from
`math.MaxUint64` back to `0` -- every *other* goroutine racing near that
boundary would still receive `Add(1)`'s next values (`1, 2, 3, ...`) again,
silently recycling IDs that could collide with genuinely-in-use ones from
early in the process's life. The fix replaces the unconditional `Add` with
a compare-and-swap loop that *refuses to perform the increment at all* once
the counter already equals `math.MaxUint64`:

```go
for {
    current := txnIDCounter.Load()
    if current == ^uint64(0) {
        return 0, error // exhausted
    }
    if txnIDCounter.CompareAndSwap(current, current+1) {
        return TxnID(current + 1), nil
    }
    // retry: another goroutine updated the counter first
}
```

No CAS can ever succeed past `math.MaxUint64`, so once the space is
exhausted, *every* subsequent call fails -- permanently, not just the one
call that happened to hit the boundary first.

### Storage-engine latch coverage audit

A repository-wide grep for `userlib\.(Datastore|Keystore)(Get|Set|Delete)`
across `client/*.go` confirms every production call site goes through the
five wrapper functions (`datastoreGet`, `datastoreSet`, `datastoreDelete`,
`keystoreGet`, `keystoreSet`) introduced in Phase 4 -- the only remaining
direct `userlib.Datastore*`/`Keystore*` calls in the codebase are the
wrapper bodies themselves (which must call the real functions) and a
handful of test-only, single-goroutine, pre-concurrency corruption-
injection calls in `client_unittest_test.go`/`phase4_concurrency_test.go`
(each executes sequentially from the test's main goroutine before any
concurrent goroutine is launched, so they are not racing with anything).
No production path was found bypassing the latch layer.

### Test hook race-safety

`concurrencyTestHook` was a bare `var ... func(string)`, set by plain
assignment from a test's setup code and read by plain dereference from
concurrently-running production goroutines during a test. Every existing
test happened to set it before launching any goroutine that reads it (a
`go` statement is itself a happens-before edge) and only clear it after
joining every goroutine that could read it, so there was no *actual*
concurrent read+write in practice -- but that safety was a property of test
discipline, not of the mechanism, and one careless future test could have
introduced a real data race purely in test scaffolding, which would then
falsely blame `go test -race` failures on production code. The variable is
now `concurrencyTestHookPtr atomic.Pointer[func(string)]`, with
`setConcurrencyTestHook`/`fireConcurrencyTestHook` accessors -- safe under
`-race` by construction, independent of any test's goroutine-lifecycle
care. All existing tests were updated to call `setConcurrencyTestHook(...)`
instead of assigning the (now-removed) bare variable directly.

## Phase 5: sharing and revocation join the protocol

Phase 5 integrates `CreateInvitation`, `AcceptInvitation`, and
`RevokeAccess` with `saferLockManager`, using exactly the lock matrix
planned in the Phase 4 section above. Each is a full strict-2PL
transaction: one `TxnID`, one `LockGuard`, `defer guard.ReleaseAll()`
before any lock is acquired, Namespace always before File.

### CreateInvitation: `S(namespace) -> X(file)`

```text
Acquire S(namespace(owner, filename))
loadNamespaceEntry
Acquire X(file(FileID))
validateFileAccessUnderLock            <- fresh AccessBox/FileStatus, under lock
[keystore lookups for the recipient's PKE/DS keys -- no resource dependency]
if owner: loadOwnerAccessBoxStructure, then create/update the branch
          record in RecipientBoxes, persist structure + branch AccessBox
else:     re-share the caller's own existing AccessBoxUUID/keys directly
          (unchanged pre-Phase-5 SAFER design -- see below)
build + persist the invitation
ReleaseAll
```

File **X** is required even though only the owner branch actually mutates
`AccessBoxStructure.RecipientBoxes` -- V1 does not split "content lock"
from "authorization-control lock" (see "Granularity tradeoff" below), so a
non-owner's re-share (which only reads its own `AccessBoxUUID`/keys)
conservatively takes the same X. `Metadata.Version` is never touched by
`CreateInvitation` -- it doesn't call `loadMetadata` or write `Metadata` at
all, so there is nothing to preserve or advance; the pre-existing
non-owner-reshare design (handing out the resharer's own `AccessBoxUUID`
directly, rather than minting a fresh branch) is also unchanged by Phase 5.

**The old race this closes:** two owners' `CreateInvitation` calls used to
both `loadOwnerAccessBoxStructure` from the same pre-mutation snapshot,
both add their own recipient locally, and both write the whole structure
object back -- last writer wins, silently dropping one recipient. Under
File X, the second call cannot even begin its own
`loadOwnerAccessBoxStructure` read until the first has fully committed and
released, so there is no stale snapshot to lose a recipient from. Verified
directly by Test 2 (forces this exact old schedule and confirms neither
recipient is lost) and Test 1 (15 concurrent distinct recipients, all
survive).

### AcceptInvitation: `X(namespace) -> S(file)`

```text
Acquire X(namespace(recipient, filename))
check destination-filename occupancy          <- atomic w.r.t. concurrent
                                                   AcceptInvitation/StoreFile
openInvitation                                 <- authenticate, decrypt, learn FileID
Acquire S(file(FileID))
validateInvitationAccessUnderLock              <- fresh AccessBox/FileStatus, under lock
install recipient's NamespaceEntry
delete the consumed invitation
ReleaseAll
```

Namespace needs **X**, not S, because of the check-then-act on filename
occupancy (the same reasoning as `StoreFile`'s create path) -- this closes
the second half of item B7/B11: two concurrent `AcceptInvitation` calls (or
one `AcceptInvitation` racing a `StoreFile`) for the same
`(recipient, filename)` now fully serialize, and the loser sees a clean
`"filename is already in use"` failure *without* having consumed (deleted)
its invitation -- it remains valid for a future attempt under a different
name. File only needs **S**: this operation never mutates shared file
state, it only needs a *current* read of the granted AccessBox/FileStatus.

**The TOCTOU race this closes** (the one most visible in the Phase 1
audit): the pre-Phase-5 code validated the AccessBox/FileStatus once, with
no lock at all, then installed a `NamespaceEntry` from that single
snapshot. A concurrent `RevokeAccess` could commit in the gap between
validation and install, handing the recipient a `NamespaceEntry` that
*looked* installed but pointed at already-deleted capability state. Under
File S, `RevokeAccess` (File X) cannot commit while this validation is in
flight, and if it already committed before this validation's S was
granted, `validateInvitationAccessUnderLock`'s `verifyFileStatus` call is
guaranteed to observe it and fail. See "Accept vs Revoke" below.

### RevokeAccess: `S(namespace) -> X(file)`

```text
Acquire S(namespace(owner, filename))
loadNamespaceEntry
check IsOwner                                  <- namespace-only property, no file lock needed
Acquire X(file(FileID))
validateFileAccessUnderLock                    <- fresh AccessBox/FileStatus, under lock
loadOwnerAccessBoxStructure
look up the target recipient's branch record
loadMetadata + loadFileContentAndChunkUUIDs    <- current content, under lock
rotate epoch: new FileRoot/MetadataUUID/base chunk (Version PRESERVED)
rewrite owner AccessBox + every surviving branch AccessBox under the new epoch
update AccessBoxStructure (remove the revoked recipient, bump CurrentEpoch)
publish new signed FileStatus
delete the revoked branch's AccessBox, the old MetadataUUID, and old chunks
ReleaseAll
```

File **X** is required because `RevokeAccess` is the single owner of epoch
rotation for a file: it touches every piece of that file's shared
authorization *and* content-pointer state (Metadata, owner AccessBox, every
surviving branch AccessBox, AccessBoxStructure, FileStatus) in one atomic
transition, and none of that may interleave with a concurrent
`LoadFile`/`AppendToFile`/`CreateInvitation`/another `RevokeAccess` on the
same file -- all of which also require File S or X on that same resource.
`IsOwner` is checked immediately after the namespace-only
`loadNamespaceEntry`, *before* File X is even requested: it is a property
of the caller's own `NamespaceEntry` that no other user's operation can
change, so it needs no file-lock revalidation.

### Version vs Epoch under revoke (unchanged from Phase 3, now lock-protected)

```text
Before: Epoch = E7, Version = V42
Revoke: Epoch = E8, Version = V42   (content bytes unchanged -- physical
                                      re-encryption migration, not a
                                      logical content mutation)
```

`RevokeAccess` copies `metadata.Version` into the new `Metadata` object
verbatim; it never calls `nextMetadataVersion`. What Phase 5 adds is not a
semantic change here (Phase 3 already established this rule) but a
*guarantee*: because `AppendToFile`/`overwriteExistingFile`/`RevokeAccess`
all require File X on the same resource, "the Version `RevokeAccess`
copies" and "the Version some concurrent content mutation just published"
can never be two different in-flight values racing each other -- whichever
committed first is unambiguously what the other observes.

### Linearization examples

```text
Accept vs Revoke, Accept wins:
  Accept: X(ns) -> S(file) -> validate (epoch E1, OK) -> install -> release
  Revoke:                                                            X(file) -> rotate E1->E2 -> release
  Result: Accept succeeded (validated before revoke committed).
          Recipient's NEXT operation revalidates under E2's AccessBox
          state and fails cleanly (their branch AccessBox was deleted).

Accept vs Revoke, Revoke wins:
  Revoke: S(ns) -> X(file) -> rotate E1->E2 -> release
  Accept:                                        X(ns) -> S(file) -> validate against payload's
                                                             (now-deleted) branch AccessBox -> FAILS
  Result: Accept fails outright; no NamespaceEntry is installed.

Load vs Revoke, Load wins:
  Load:   S(ns) -> S(file) -> read complete (E1) content -> release
  Revoke:                                                     X(file) -> rotate E1->E2 -> release
  Result: Load returns the complete, valid old-epoch file. Legal.

Load vs Revoke, Revoke wins:
  Revoke: S(ns) -> X(file) -> rotate E1->E2 -> release
  Load:                                          S(ns) -> S(file) -> revalidate against the
                                                            recipient's now-deleted branch -> FAILS
  Result: clean access-revalidation failure, not a missing-chunk/datastore
          error caused by racing deletes (see "Clean failure" below).

Append vs Revoke, Append wins:
  Append: S(ns) -> X(file) -> Version V10->V11 -> release
  Revoke:                                           X(file) -> rotate epoch, Version stays V11 -> release

CreateInvitation vs Revoke (either order):
  Both require File X, so they fully serialize. Whichever commits first
  determines the epoch/RecipientBoxes state the second operates on; the
  second's own loadOwnerAccessBoxStructure read is always the freshly
  committed one, never a stale pre-lock snapshot -- so a newly added
  recipient is never lost to a revoke's structure replacement (Test 9b),
  and a revoked recipient is never accidentally restored by a subsequent
  invitation (Test 9a).
```

### Clean failure, not accidental corruption

A core Phase 5 goal: when a losing, stale operation fails, it must fail
because *current authorization state rejects it* (a `FileStatus`/epoch
mismatch, or a datastore lookup on an object that `RevokeAccess`
*deliberately and only ever* deletes as part of its designed revocation
mechanism), not because it raced a delete mid-flight and got an accidental,
undefined error shape. Because every content/authorization read that
matters for a decision now happens strictly after the relevant lock is
held, and every mutation that matters happens strictly while that lock is
held, a losing operation's failure is always a deterministic consequence of
"the authorization state it validated against is gone" -- reproducible,
attributable to one specific committed `RevokeAccess`, not to scheduling
luck.

### Granularity tradeoff (intentional V1 limitation)

`AppendToFile`, `overwriteExistingFile`, `CreateInvitation`, and
`RevokeAccess` all require File X, so they fully serialize with each other
at **file granularity**, even though `CreateInvitation` (usually) and
`RevokeAccess` mutate only authorization-control state
(`AccessBoxStructure`, `AccessBox` copies, `FileStatus`) while
`AppendToFile`/overwrite mutate only content state (`Metadata`, chunks). A
finer-grained design could split these into two independent lock resources
per file -- conceptually `ContentResource(FileID)` and
`AuthorizationResource(FileID)` -- so that, e.g., an append and a
`CreateInvitation` on the same file could proceed concurrently. V1
deliberately does not do this: it is a correctness-neutral, purely
performance-oriented optimization, and splitting it out would need its own
reasoning about *which* operations need which combination of the two
resources (e.g. does `RevokeAccess`, which touches both `Metadata`'s
`EpochID`-adjacent fields and `AccessBoxStructure`, need both?) --
deliberately deferred, not attempted here.

### System-wide protocol summary (Phase 5)

As of Phase 5, every public SAFER operation participates in one common
strict-2PL protocol over the same two resource classes:

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
these seven paths. All locks are held from acquisition through the
operation's return (success or error), released together via one deferred
`guard.ReleaseAll()`. This is the full scope of the linearizability-style
claim SAFER-CC V1 supports: *conflicting operations among these seven, on
the same logical namespace entry or the same logical file, serialize
cleanly and observe only complete, lock-consistent state.*

### Current limitations (Phase 5)

- **In-process only.** `saferLockManager` is a single Go-process, in-memory
  lock table. It coordinates goroutines that share this one process's
  `saferLockManager` instance and `userlib` Datastore/Keystore. It is
  **not** a distributed lock service and makes no claim of correctness
  across independent machines or processes -- that would require an
  entirely different design (leases, consensus, a network protocol), which
  is explicitly out of scope for V1.
- **No MVCC / snapshot isolation.** Readers and writers are coordinated
  by blocking (S/X locks), not by maintaining multiple versions for
  lock-free concurrent reads. This was an explicit V1 non-goal.
  Also unimplemented, intentionally: no caching layer, no WAL, no
  replication, no wait-for-graph deadlock detection (prevented instead by
  the fixed Namespace-before-File ordering).
- **Coarse file-level serialization between content writes and ACL
  mutation** -- see "Granularity tradeoff" above. `AppendToFile`/overwrite
  and `CreateInvitation`/`RevokeAccess` on the same file cannot currently
  proceed concurrently even though (in the common case) they touch
  disjoint pieces of that file's state.
- **Race-detector scope:** Linux CI now runs the full instrumented suite;
  see "CI verification" above for observed results. The original local
  toolchain limitation is recorded historically, not used as a substitute
  for the hosted check. Passing instrumented executions do not prove
  universal memory-race freedom or logical serializability.
