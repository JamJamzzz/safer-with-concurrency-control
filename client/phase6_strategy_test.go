package client

///////////////////////////////////////////////////
//                                               //
// Everything in this file will NOT be graded!!! //
//                                               //
///////////////////////////////////////////////////

// Phase 6: validates that the two benchmark-only baseline strategies
// (StrategyNoCC, StrategyGlobalLock) actually behave the way
// cmd/benchmark's evaluation depends on, before trusting any benchmark
// numbers built on top of them. Also confirms the switch has zero effect
// on production behavior when left at its zero value.

import (
	"strings"
	"time"

	userlib "github.com/cs161-staff/project2-userlib"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Phase 6: benchmark strategy switch validation", func() {

	BeforeEach(func() {
		userlib.DatastoreClear()
		userlib.KeystoreClear()
		setConcurrencyTestHook(nil)
		SetConcurrencyStrategyForBenchmark(StrategySaferCC)
	})

	AfterEach(func() {
		setConcurrencyTestHook(nil)
		SetConcurrencyStrategyForBenchmark(StrategySaferCC)
	})

	Specify("StrategyNoCC deterministically reproduces the classic append lost-update race", func() {
		SetConcurrencyStrategyForBenchmark(StrategyNoCC)

		alice, err := InitUser("alice", "password")
		Expect(err).To(BeNil())
		Expect(alice.StoreFile("file1.txt", []byte("base"))).To(BeNil())

		reachedA := make(chan struct{}, 1)
		reachedB := make(chan struct{}, 1)
		proceed := make(chan struct{})
		var seen int

		setConcurrencyTestHook(func(tag string) {
			if tag != "append:metadata-loaded:file1.txt" {
				return
			}
			seen++
			if seen == 1 {
				reachedA <- struct{}{}
			} else {
				reachedB <- struct{}{}
			}
			<-proceed
		})

		doneA := make(chan error, 1)
		doneB := make(chan error, 1)
		go func() { doneA <- alice.AppendToFile("file1.txt", []byte("-A")) }()
		Eventually(reachedA, 2*time.Second).Should(Receive())
		go func() { doneB <- alice.AppendToFile("file1.txt", []byte("-B")) }()
		Eventually(reachedB, 2*time.Second).Should(Receive())

		// Under NoCC there is no File X lock, so B was able to reach its
		// own Metadata read while A was still paused holding a stale
		// read -- the exact precondition for the lost-update race.
		close(proceed)

		var errA, errB error
		Eventually(doneA, 2*time.Second).Should(Receive(&errA))
		Eventually(doneB, 2*time.Second).Should(Receive(&errB))
		Expect(errA).To(BeNil())
		Expect(errB).To(BeNil())

		setConcurrencyTestHook(nil)

		version, chunkCount, err := DebugFileVersionForBenchmark(alice, "file1.txt")
		Expect(err).To(BeNil())

		// Two "successful" appends ran, but both computed Version=2 from
		// the same stale Version=1 read -- final Version is stuck at 2,
		// not 3. This is the deterministic signature of the lost update.
		Expect(version).To(Equal(uint64(2)), "expected the classic lost-update race to leave Version at 2, not 3")
		Expect(chunkCount).To(Equal(uint64(2)))

		content, err := alice.LoadFile("file1.txt")
		Expect(err).To(BeNil())
		hasA := strings.Contains(string(content), "-A")
		hasB := strings.Contains(string(content), "-B")
		Expect(hasA != hasB).To(BeTrue(), "exactly one of the two successful appends must have been silently lost")
	})

	Specify("StrategyGlobalLock serializes operations on independent files, unlike production SAFER-CC", func() {
		SetConcurrencyStrategyForBenchmark(StrategyGlobalLock)

		alice, err := InitUser("alice", "password")
		Expect(err).To(BeNil())
		Expect(alice.StoreFile("fileA.txt", []byte("A"))).To(BeNil())
		Expect(alice.StoreFile("fileB.txt", []byte("B"))).To(BeNil())

		pausedA := make(chan struct{}, 2)
		proceedA := make(chan struct{})

		setConcurrencyTestHook(func(tag string) {
			if tag == "append:metadata-loaded:fileA.txt" {
				pausedA <- struct{}{}
				<-proceedA
			}
		})

		doneA := make(chan error, 1)
		go func() { doneA <- alice.AppendToFile("fileA.txt", []byte("-x")) }()
		Eventually(pausedA, 2*time.Second).Should(Receive())

		doneB := make(chan error, 1)
		go func() { doneB <- alice.AppendToFile("fileB.txt", []byte("-y")) }()

		// Under one process-wide mutex, fileB's operation cannot even
		// start while fileA's is mid-flight -- contrast with production
		// SAFER-CC (Phase 4 Test 23), where independent files never
		// block each other.
		Consistently(doneB, 200*time.Millisecond).ShouldNot(Receive())

		close(proceedA)

		var errA, errB error
		Eventually(doneA, 2*time.Second).Should(Receive(&errA))
		Eventually(doneB, 2*time.Second).Should(Receive(&errB))
		Expect(errA).To(BeNil())
		Expect(errB).To(BeNil())
	})

	Specify("the strategy switch defaults to StrategySaferCC and production tests never touch it", func() {
		Expect(ConcurrencyStrategy(activeConcurrencyStrategy.Load())).To(Equal(StrategySaferCC))
	})
})
