package lockmanager

import (
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------
// Test-only, white-box helpers. These read internal bookkeeping directly
// (this file is part of package lockmanager, not lockmanager_test) so that
// tests can synchronize deterministically on "has this request actually
// reached the wait queue yet" instead of guessing with sleeps. They are
// not exported and add no production API surface.
// ---------------------------------------------------------------------

// queueLen returns how many requests are currently pending (not yet
// granted) on resource.
func (lm *LockManager) queueLen(resource ResourceID) int {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	ls, ok := lm.resources[resource]
	if !ok {
		return 0
	}
	return len(ls.queue)
}

// sharedHolderCount returns the number of transactions currently holding S
// on resource.
func (lm *LockManager) sharedHolderCount(resource ResourceID) int {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	ls, ok := lm.resources[resource]
	if !ok {
		return 0
	}
	return len(ls.sharedHolders)
}

// hasExclusiveHolder reports whether resource currently has a granted X
// holder.
func (lm *LockManager) hasExclusiveHolder(resource ResourceID) bool {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	ls, ok := lm.resources[resource]
	if !ok {
		return false
	}
	return ls.hasExclusive
}

// waitUntil polls cond (a cheap, mutex-protected read) until it becomes
// true or the deadline elapses, at which point it fails the test. This is
// used only to detect "has a background goroutine's Acquire call reached
// the point of being enqueued" -- an internal, deterministic bookkeeping
// fact -- not to guess about timing of the actual grant. A short sleep
// between polls is a mechanical necessity of polling, not the scheduling
// mechanism itself.
func waitUntil(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for condition")
		}
		time.Sleep(time.Millisecond)
	}
}

// assertAcquires runs Acquire in a goroutine and signals a channel when it
// returns, so the calling test can distinguish "granted" from "blocked"
// without a fixed sleep: it waits on the channel with a bounded timeout
// only as a failure guard.
func acquireAsync(lm *LockManager, txn TxnID, resource ResourceID, mode LockMode) <-chan error {
	done := make(chan error, 1)
	go func() {
		done <- lm.Acquire(txn, resource, mode)
	}()
	return done
}

func expectGranted(t *testing.T, done <-chan error, timeout time.Duration, msg string) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("%s: Acquire returned error: %v", msg, err)
		}
	case <-time.After(timeout):
		t.Fatalf("%s: Acquire did not grant within timeout", msg)
	}
}

func expectBlocked(t *testing.T, done <-chan error, wait time.Duration, msg string) {
	t.Helper()
	select {
	case err := <-done:
		t.Fatalf("%s: Acquire unexpectedly returned (err=%v) instead of blocking", msg, err)
	case <-time.After(wait):
		// still blocked, as expected
	}
}

const testTimeout = 2 * time.Second

// ---------------------------------------------------------------------
// Test A -- multiple shared holders coexist.
// ---------------------------------------------------------------------
func TestMultipleSharedHolders(t *testing.T) {
	lm := NewLockManager()
	r := ResourceID{Type: FileResource, Key: "A"}

	d1 := acquireAsync(lm, 1, r, SharedLock)
	d2 := acquireAsync(lm, 2, r, SharedLock)
	d3 := acquireAsync(lm, 3, r, SharedLock)

	expectGranted(t, d1, testTimeout, "T1 S(R)")
	expectGranted(t, d2, testTimeout, "T2 S(R)")
	expectGranted(t, d3, testTimeout, "T3 S(R)")

	if got := lm.sharedHolderCount(r); got != 3 {
		t.Fatalf("expected 3 shared holders, got %d", got)
	}
}

// ---------------------------------------------------------------------
// Test B -- exclusive blocks shared.
// ---------------------------------------------------------------------
func TestExclusiveBlocksShared(t *testing.T) {
	lm := NewLockManager()
	r := ResourceID{Type: FileResource, Key: "B"}

	if err := lm.Acquire(1, r, ExclusiveLock); err != nil {
		t.Fatalf("T1 X(R) failed: %v", err)
	}

	d2 := acquireAsync(lm, 2, r, SharedLock)
	waitUntil(t, testTimeout, func() bool { return lm.queueLen(r) == 1 })
	expectBlocked(t, d2, 100*time.Millisecond, "T2 S(R) while T1 holds X")

	if err := lm.Release(1, r); err != nil {
		t.Fatalf("T1 release failed: %v", err)
	}
	expectGranted(t, d2, testTimeout, "T2 S(R) after T1 releases")
}

// ---------------------------------------------------------------------
// Test C -- shared blocks exclusive.
// ---------------------------------------------------------------------
func TestSharedBlocksExclusive(t *testing.T) {
	lm := NewLockManager()
	r := ResourceID{Type: FileResource, Key: "C"}

	if err := lm.Acquire(1, r, SharedLock); err != nil {
		t.Fatalf("T1 S(R) failed: %v", err)
	}

	d2 := acquireAsync(lm, 2, r, ExclusiveLock)
	waitUntil(t, testTimeout, func() bool { return lm.queueLen(r) == 1 })
	expectBlocked(t, d2, 100*time.Millisecond, "T2 X(R) while T1 holds S")

	if err := lm.Release(1, r); err != nil {
		t.Fatalf("T1 release failed: %v", err)
	}
	expectGranted(t, d2, testTimeout, "T2 X(R) after T1 releases")
}

// ---------------------------------------------------------------------
// Test D -- exclusive blocks exclusive.
// ---------------------------------------------------------------------
func TestExclusiveBlocksExclusive(t *testing.T) {
	lm := NewLockManager()
	r := ResourceID{Type: FileResource, Key: "D"}

	if err := lm.Acquire(1, r, ExclusiveLock); err != nil {
		t.Fatalf("T1 X(R) failed: %v", err)
	}

	d2 := acquireAsync(lm, 2, r, ExclusiveLock)
	waitUntil(t, testTimeout, func() bool { return lm.queueLen(r) == 1 })
	expectBlocked(t, d2, 100*time.Millisecond, "T2 X(R) while T1 holds X")

	if err := lm.Release(1, r); err != nil {
		t.Fatalf("T1 release failed: %v", err)
	}
	expectGranted(t, d2, testTimeout, "T2 X(R) after T1 releases")
}

// ---------------------------------------------------------------------
// Test E -- independent resources do not conflict.
// ---------------------------------------------------------------------
func TestIndependentResourcesDoNotConflict(t *testing.T) {
	lm := NewLockManager()
	r1 := ResourceID{Type: FileResource, Key: "E1"}
	r2 := ResourceID{Type: FileResource, Key: "E2"}

	if err := lm.Acquire(1, r1, ExclusiveLock); err != nil {
		t.Fatalf("T1 X(R1) failed: %v", err)
	}

	// T2 requesting X on a totally different resource must proceed
	// immediately, proving the manager does not secretly serialize
	// unrelated resources through a single global lock.
	d2 := acquireAsync(lm, 2, r2, ExclusiveLock)
	expectGranted(t, d2, testTimeout, "T2 X(R2) independent of T1 X(R1)")

	if err := lm.Release(1, r1); err != nil {
		t.Fatalf("T1 release failed: %v", err)
	}
	if err := lm.Release(2, r2); err != nil {
		t.Fatalf("T2 release failed: %v", err)
	}
}

// ---------------------------------------------------------------------
// Test F -- writer fairness: a queued writer is not bypassed by a later
// reader.
// ---------------------------------------------------------------------
func TestWriterFairness(t *testing.T) {
	lm := NewLockManager()
	r := ResourceID{Type: FileResource, Key: "F"}

	if err := lm.Acquire(1, r, SharedLock); err != nil {
		t.Fatalf("T1 S(R) failed: %v", err)
	}

	dWriter := acquireAsync(lm, 2, r, ExclusiveLock)
	// Confirm T2's X request has actually reached the queue before T3
	// arrives, so the schedule is deterministic rather than a race.
	waitUntil(t, testTimeout, func() bool { return lm.queueLen(r) == 1 })

	dReader := acquireAsync(lm, 3, r, SharedLock)
	// T3 must not be granted while T2's X request is queued ahead of it,
	// even though T3's request (S) would otherwise be compatible with
	// T1's currently-held S.
	waitUntil(t, testTimeout, func() bool { return lm.queueLen(r) == 2 })
	expectBlocked(t, dWriter, 100*time.Millisecond, "T2 X(R) should still be waiting on T1")
	expectBlocked(t, dReader, 100*time.Millisecond, "T3 S(R) must not bypass queued T2 X(R)")

	if err := lm.Release(1, r); err != nil {
		t.Fatalf("T1 release failed: %v", err)
	}

	// T2 (writer) must be granted next, strictly before T3.
	expectGranted(t, dWriter, testTimeout, "T2 X(R) after T1 releases")
	expectBlocked(t, dReader, 100*time.Millisecond, "T3 S(R) must still wait while T2 holds X")

	if err := lm.Release(2, r); err != nil {
		t.Fatalf("T2 release failed: %v", err)
	}
	expectGranted(t, dReader, testTimeout, "T3 S(R) after T2 releases")
}

// ---------------------------------------------------------------------
// Test G -- batched readers ahead of a writer.
// ---------------------------------------------------------------------
func TestBatchedReadersAheadOfWriter(t *testing.T) {
	lm := NewLockManager()
	r := ResourceID{Type: FileResource, Key: "G"}

	// Blocker establishes ordering: everything queues behind it.
	if err := lm.Acquire(0, r, ExclusiveLock); err != nil {
		t.Fatalf("T0 X(R) failed: %v", err)
	}

	d1 := acquireAsync(lm, 1, r, SharedLock)
	waitUntil(t, testTimeout, func() bool { return lm.queueLen(r) == 1 })
	d2 := acquireAsync(lm, 2, r, SharedLock)
	waitUntil(t, testTimeout, func() bool { return lm.queueLen(r) == 2 })
	d3 := acquireAsync(lm, 3, r, SharedLock)
	waitUntil(t, testTimeout, func() bool { return lm.queueLen(r) == 3 })
	d4 := acquireAsync(lm, 4, r, ExclusiveLock)
	waitUntil(t, testTimeout, func() bool { return lm.queueLen(r) == 4 })

	if err := lm.Release(0, r); err != nil {
		t.Fatalf("T0 release failed: %v", err)
	}

	// All three readers must be granted together, ahead of the writer.
	expectGranted(t, d1, testTimeout, "T1 S(R) after T0 releases")
	expectGranted(t, d2, testTimeout, "T2 S(R) after T0 releases")
	expectGranted(t, d3, testTimeout, "T3 S(R) after T0 releases")
	expectBlocked(t, d4, 100*time.Millisecond, "T4 X(R) must wait for all three readers")

	if got := lm.sharedHolderCount(r); got != 3 {
		t.Fatalf("expected 3 concurrent shared holders, got %d", got)
	}

	for _, txn := range []TxnID{1, 2, 3} {
		if err := lm.Release(txn, r); err != nil {
			t.Fatalf("release for txn %d failed: %v", txn, err)
		}
	}
	expectGranted(t, d4, testTimeout, "T4 X(R) after all readers release")
}

// ---------------------------------------------------------------------
// Test H -- ReleaseAll.
// ---------------------------------------------------------------------
func TestReleaseAll(t *testing.T) {
	lm := NewLockManager()
	r1 := ResourceID{Type: NamespaceResource, Key: "H1"}
	r2 := ResourceID{Type: FileResource, Key: "H2"}
	r3 := ResourceID{Type: FileResource, Key: "H3"}

	if err := lm.Acquire(1, r1, SharedLock); err != nil {
		t.Fatalf("T1 S(R1) failed: %v", err)
	}
	if err := lm.Acquire(1, r2, ExclusiveLock); err != nil {
		t.Fatalf("T1 X(R2) failed: %v", err)
	}
	if err := lm.Acquire(1, r3, SharedLock); err != nil {
		t.Fatalf("T1 S(R3) failed: %v", err)
	}

	// Other transactions queue on each resource, waiting for T1.
	w1 := acquireAsync(lm, 2, r1, ExclusiveLock)
	w2 := acquireAsync(lm, 2, r2, SharedLock)
	w3 := acquireAsync(lm, 2, r3, ExclusiveLock)

	waitUntil(t, testTimeout, func() bool { return lm.queueLen(r1) == 1 })
	waitUntil(t, testTimeout, func() bool { return lm.queueLen(r2) == 1 })
	waitUntil(t, testTimeout, func() bool { return lm.queueLen(r3) == 1 })

	if err := lm.ReleaseAll(1); err != nil {
		t.Fatalf("ReleaseAll failed: %v", err)
	}

	expectGranted(t, w1, testTimeout, "T2 X(R1) after ReleaseAll(T1)")
	expectGranted(t, w2, testTimeout, "T2 S(R2) after ReleaseAll(T1)")
	expectGranted(t, w3, testTimeout, "T2 X(R3) after ReleaseAll(T1)")

	// T1 should hold nothing now: it must be able to re-acquire cleanly.
	if err := lm.ReleaseAll(2); err != nil {
		t.Fatalf("cleanup ReleaseAll(T2) failed: %v", err)
	}
	if err := lm.Acquire(1, r1, ExclusiveLock); err != nil {
		t.Fatalf("T1 should be able to re-acquire R1 after ReleaseAll: %v", err)
	}
}

// ---------------------------------------------------------------------
// Test I -- wrong-owner release is rejected and does not disturb the
// actual holder.
// ---------------------------------------------------------------------
func TestWrongOwnerRelease(t *testing.T) {
	lm := NewLockManager()
	r := ResourceID{Type: FileResource, Key: "I"}

	if err := lm.Acquire(1, r, ExclusiveLock); err != nil {
		t.Fatalf("T1 X(R) failed: %v", err)
	}

	if err := lm.Release(2, r); err == nil {
		t.Fatalf("expected error releasing a lock T2 does not own")
	}

	// T1's lock must still be intact: a competing X request must still
	// block.
	d3 := acquireAsync(lm, 3, r, ExclusiveLock)
	expectBlocked(t, d3, 100*time.Millisecond, "T3 X(R) should still block: T1's lock must be untouched")

	if err := lm.Release(1, r); err != nil {
		t.Fatalf("T1 release failed: %v", err)
	}
	expectGranted(t, d3, testTimeout, "T3 X(R) after the real owner releases")
}

// ---------------------------------------------------------------------
// Test J -- duplicate acquisition / unsupported upgrade rejected clearly,
// without blocking.
// ---------------------------------------------------------------------
func TestDuplicateAndUpgradeRejected(t *testing.T) {
	lm := NewLockManager()
	r := ResourceID{Type: FileResource, Key: "J"}

	if err := lm.Acquire(1, r, SharedLock); err != nil {
		t.Fatalf("T1 S(R) failed: %v", err)
	}

	// Same txn, same mode again: must return an error immediately, not
	// block (it already holds it -- reentrancy is not supported).
	done := acquireAsync(lm, 1, r, SharedLock)
	select {
	case err := <-done:
		if err == nil {
			t.Fatalf("expected error on duplicate acquisition, got nil")
		}
	case <-time.After(testTimeout):
		t.Fatalf("duplicate acquisition blocked instead of returning an error")
	}

	// Same txn, attempting to upgrade S -> X: must also return an error
	// immediately rather than deadlocking.
	done2 := acquireAsync(lm, 1, r, ExclusiveLock)
	select {
	case err := <-done2:
		if err == nil {
			t.Fatalf("expected error on unsupported S->X upgrade, got nil")
		}
	case <-time.After(testTimeout):
		t.Fatalf("upgrade attempt blocked instead of returning an error")
	}

	if err := lm.Release(1, r); err != nil {
		t.Fatalf("T1 release failed: %v", err)
	}
}

// ---------------------------------------------------------------------
// Test K -- many concurrent readers.
// ---------------------------------------------------------------------
func TestManyReaders(t *testing.T) {
	lm := NewLockManager()
	r := ResourceID{Type: FileResource, Key: "K"}

	const n = 200
	var wg sync.WaitGroup
	errs := make([]error, n)

	release := make(chan struct{})

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = lm.Acquire(TxnID(i+1), r, SharedLock)
			<-release
			if err := lm.Release(TxnID(i+1), r); err != nil {
				errs[i] = err
			}
		}(i)
	}

	waitUntil(t, testTimeout, func() bool { return lm.sharedHolderCount(r) == n })
	close(release)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("reader %d failed: %v", i, err)
		}
	}
}

// ---------------------------------------------------------------------
// Test L -- mixed stress test with invariant checking.
// ---------------------------------------------------------------------
func TestStressMixedRequests(t *testing.T) {
	lm := NewLockManager()
	resources := []ResourceID{
		{Type: FileResource, Key: "stress-1"},
		{Type: FileResource, Key: "stress-2"},
		{Type: NamespaceResource, Key: "stress-3"},
	}

	const goroutines = 40
	const itersPerGoroutine = 50

	// invariant tracking: per resource, count of active X holders and S
	// holders, checked atomically around every grant/release.
	type counters struct {
		exclusive int32
		shared    int32
	}
	live := make(map[ResourceID]*counters)
	var liveMu sync.Mutex
	for _, r := range resources {
		live[r] = &counters{}
	}

	var violations int32
	var wg sync.WaitGroup

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(int64(g) * 7919))
			for i := 0; i < itersPerGoroutine; i++ {
				txn := TxnID(g*itersPerGoroutine + i + 1)
				r := resources[rng.Intn(len(resources))]
				mode := SharedLock
				if rng.Intn(3) == 0 {
					mode = ExclusiveLock
				}

				if err := lm.Acquire(txn, r, mode); err != nil {
					atomic.AddInt32(&violations, 1)
					continue
				}

				liveMu.Lock()
				c := live[r]
				if mode == ExclusiveLock {
					c.exclusive++
				} else {
					c.shared++
				}
				bad := c.exclusive > 1 || (c.exclusive == 1 && c.shared > 0)
				liveMu.Unlock()
				if bad {
					atomic.AddInt32(&violations, 1)
				}

				// Hold briefly to increase overlap probability.
				time.Sleep(time.Duration(rng.Intn(200)) * time.Microsecond)

				liveMu.Lock()
				if mode == ExclusiveLock {
					c.exclusive--
				} else {
					c.shared--
				}
				liveMu.Unlock()

				if err := lm.Release(txn, r); err != nil {
					atomic.AddInt32(&violations, 1)
				}
			}
		}(g)
	}

	wg.Wait()

	if violations != 0 {
		t.Fatalf("detected %d invariant violations (overlapping X, or S with X, or acquire/release error)", violations)
	}

	// No leaked locks: every txn used should hold nothing now.
	for g := 0; g < goroutines; g++ {
		for i := 0; i < itersPerGoroutine; i++ {
			txn := TxnID(g*itersPerGoroutine + i + 1)
			if err := lm.ReleaseAll(txn); err != nil {
				t.Fatalf("txn %d unexpectedly still held a lock after its own release: %v", txn, err)
			}
		}
	}
}

// ---------------------------------------------------------------------
// Sanity: invalid mode is rejected.
// ---------------------------------------------------------------------
func TestInvalidModeRejected(t *testing.T) {
	lm := NewLockManager()
	r := ResourceID{Type: FileResource, Key: "invalid"}
	if err := lm.Acquire(1, r, LockMode(99)); err == nil {
		t.Fatalf("expected error for invalid lock mode")
	}
}

// ---------------------------------------------------------------------
// Sanity: releasing a resource nobody holds errors cleanly.
// ---------------------------------------------------------------------
func TestReleaseWithoutAcquireErrors(t *testing.T) {
	lm := NewLockManager()
	r := ResourceID{Type: FileResource, Key: "never-acquired"}
	if err := lm.Release(1, r); err == nil {
		t.Fatalf("expected error releasing a never-acquired resource")
	}
}

// ---------------------------------------------------------------------
// LockGuard: ReleaseAll is idempotent and actually releases.
// ---------------------------------------------------------------------
func TestLockGuardReleaseAllIsIdempotent(t *testing.T) {
	lm := NewLockManager()
	r := ResourceID{Type: FileResource, Key: "guard"}

	guard := NewLockGuard(lm, 1)
	if err := guard.Acquire(r, ExclusiveLock); err != nil {
		t.Fatalf("guard Acquire failed: %v", err)
	}

	guard.ReleaseAll()
	guard.ReleaseAll() // must not panic or error on the second call

	if err := lm.Acquire(2, r, ExclusiveLock); err != nil {
		t.Fatalf("resource should be free after guard.ReleaseAll(): %v", err)
	}
}
