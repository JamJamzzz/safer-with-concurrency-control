package client

///////////////////////////////////////////////////
//                                               //
// Everything in this file will NOT be graded!!! //
//                                               //
///////////////////////////////////////////////////

// Phase 4 concurrency tests: StoreFile / AppendToFile / LoadFile strict-2PL
// integration. These are white-box (package client) so they can inspect
// Metadata directly (via resolveFile/loadMetadata) as a correctness oracle
// for "how many logical content mutations actually landed," and so they
// can install concurrencyTestHook (see client.go) to force deterministic
// overlapping schedules instead of relying on time.Sleep timing.

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	userlib "github.com/cs161-staff/project2-userlib"
	"github.com/google/uuid"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Phase 4: Strict 2PL core file-content concurrency", func() {

	BeforeEach(func() {
		userlib.DatastoreClear()
		userlib.KeystoreClear()
		setConcurrencyTestHook(nil)
	})

	AfterEach(func() {
		setConcurrencyTestHook(nil)
	})

	Specify("A: 100 concurrent non-empty appends all survive exactly once", func() {
		alice, err := InitUser("alice", "password")
		Expect(err).To(BeNil())
		Expect(alice.StoreFile("file1.txt", []byte("START"))).To(BeNil())

		const n = 100
		var wg sync.WaitGroup
		errs := make([]error, n)

		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				errs[i] = alice.AppendToFile("file1.txt", []byte(fmt.Sprintf("<%d>", i)))
			}(i)
		}
		wg.Wait()

		for i, appendErr := range errs {
			Expect(appendErr).To(BeNil(), fmt.Sprintf("append %d failed", i))
		}

		content, err := alice.LoadFile("file1.txt")
		Expect(err).To(BeNil())
		Expect(string(content)).To(HavePrefix("START"))

		for i := 0; i < n; i++ {
			marker := fmt.Sprintf("<%d>", i)
			Expect(strings.Count(string(content), marker)).To(Equal(1), "marker %s should appear exactly once", marker)
		}

		_, accessBox, err := resolveFile(alice, "file1.txt")
		Expect(err).To(BeNil())
		metadata, err := loadMetadata(accessBox)
		Expect(err).To(BeNil())
		Expect(metadata.ChunkCount).To(Equal(uint64(1 + n)))
		Expect(metadata.Version).To(Equal(uint64(1 + n)))
	})

	Specify("18: a second append cannot reach its own Metadata read until the first releases File X", func() {
		alice, err := InitUser("alice", "password")
		Expect(err).To(BeNil())
		Expect(alice.StoreFile("file1.txt", []byte("base"))).To(BeNil())

		reachedA := make(chan struct{}, 2)
		proceedA := make(chan struct{})

		setConcurrencyTestHook(func(tag string) {
			if tag == "append:metadata-loaded:file1.txt" {
				reachedA <- struct{}{}
				<-proceedA
			}
		})

		doneA := make(chan error, 1)
		go func() {
			doneA <- alice.AppendToFile("file1.txt", []byte("-A"))
		}()

		Eventually(reachedA, 2*time.Second).Should(Receive())

		doneB := make(chan error, 1)
		go func() {
			doneB <- alice.AppendToFile("file1.txt", []byte("-B"))
		}()

		// B must still be blocked (it cannot even reach the hook point,
		// since it cannot acquire File X while A holds it) while A is
		// deliberately paused mid-critical-section.
		Consistently(doneB, 200*time.Millisecond).ShouldNot(Receive())

		close(proceedA)

		var errA, errB error
		Eventually(doneA, 2*time.Second).Should(Receive(&errA))
		Eventually(doneB, 2*time.Second).Should(Receive(&errB))
		Expect(errA).To(BeNil())
		Expect(errB).To(BeNil())

		content, err := alice.LoadFile("file1.txt")
		Expect(err).To(BeNil())
		Expect(string(content)).To(HavePrefix("base"))
		Expect(string(content)).To(ContainSubstring("-A"))
		Expect(string(content)).To(ContainSubstring("-B"))

		_, accessBox, err := resolveFile(alice, "file1.txt")
		Expect(err).To(BeNil())
		metadata, err := loadMetadata(accessBox)
		Expect(err).To(BeNil())
		Expect(metadata.Version).To(Equal(uint64(3))) // base=1, +A=2, +B=3
		Expect(metadata.ChunkCount).To(Equal(uint64(3)))
	})

	Specify("19a: LoadFile started first completes with the full old content before a concurrent overwrite proceeds", func() {
		alice, err := InitUser("alice", "password")
		Expect(err).To(BeNil())
		Expect(alice.StoreFile("file1.txt", []byte("OLD-CONTENT"))).To(BeNil())

		readerPaused := make(chan struct{}, 2)
		proceedReader := make(chan struct{})

		setConcurrencyTestHook(func(tag string) {
			if tag == "load:file-locked:file1.txt" {
				readerPaused <- struct{}{}
				<-proceedReader
			}
		})

		type readResult struct {
			content []byte
			err     error
		}
		doneRead := make(chan readResult, 1)
		go func() {
			c, e := alice.LoadFile("file1.txt")
			doneRead <- readResult{c, e}
		}()

		Eventually(readerPaused, 2*time.Second).Should(Receive())

		doneWrite := make(chan error, 1)
		go func() {
			doneWrite <- alice.StoreFile("file1.txt", []byte("NEW-CONTENT"))
		}()

		Consistently(doneWrite, 200*time.Millisecond).ShouldNot(Receive())

		close(proceedReader)

		var rr readResult
		Eventually(doneRead, 2*time.Second).Should(Receive(&rr))
		Expect(rr.err).To(BeNil())
		Expect(string(rr.content)).To(Equal("OLD-CONTENT"))

		var writeErr error
		Eventually(doneWrite, 2*time.Second).Should(Receive(&writeErr))
		Expect(writeErr).To(BeNil())

		finalContent, err := alice.LoadFile("file1.txt")
		Expect(err).To(BeNil())
		Expect(string(finalContent)).To(Equal("NEW-CONTENT"))
	})

	Specify("19b: an overwrite started first completes before a concurrent LoadFile proceeds, and the reader then sees the full new content", func() {
		alice, err := InitUser("alice", "password")
		Expect(err).To(BeNil())
		Expect(alice.StoreFile("file1.txt", []byte("OLD-CONTENT"))).To(BeNil())

		_, accessBox, err := resolveFile(alice, "file1.txt")
		Expect(err).To(BeNil())
		hookTag := "overwrite:metadata-loaded:" + accessBox.FileID.String()

		writerPaused := make(chan struct{}, 2)
		proceedWriter := make(chan struct{})

		setConcurrencyTestHook(func(tag string) {
			if tag == hookTag {
				writerPaused <- struct{}{}
				<-proceedWriter
			}
		})

		doneWrite := make(chan error, 1)
		go func() {
			doneWrite <- alice.StoreFile("file1.txt", []byte("NEW-CONTENT"))
		}()

		Eventually(writerPaused, 2*time.Second).Should(Receive())

		type readResult struct {
			content []byte
			err     error
		}
		doneRead := make(chan readResult, 1)
		go func() {
			c, e := alice.LoadFile("file1.txt")
			doneRead <- readResult{c, e}
		}()

		Consistently(doneRead, 200*time.Millisecond).ShouldNot(Receive())

		close(proceedWriter)

		var writeErr error
		Eventually(doneWrite, 2*time.Second).Should(Receive(&writeErr))
		Expect(writeErr).To(BeNil())

		var rr readResult
		Eventually(doneRead, 2*time.Second).Should(Receive(&rr))
		Expect(rr.err).To(BeNil())
		Expect(string(rr.content)).To(Equal("NEW-CONTENT"))
	})

	Specify("20: concurrent append and overwrite always settle into one legal serial ordering, and Version advances by exactly 2", func() {
		const iterations = 30

		for iter := 0; iter < iterations; iter++ {
			userlib.DatastoreClear()
			userlib.KeystoreClear()

			alice, err := InitUser("alice", "password")
			Expect(err).To(BeNil())
			Expect(alice.StoreFile("file1.txt", []byte("base"))).To(BeNil())

			_, accessBox, err := resolveFile(alice, "file1.txt")
			Expect(err).To(BeNil())
			initialMetadata, err := loadMetadata(accessBox)
			Expect(err).To(BeNil())

			var wg sync.WaitGroup
			var appendErr, overwriteErr error
			wg.Add(2)
			go func() {
				defer wg.Done()
				appendErr = alice.AppendToFile("file1.txt", []byte("+A"))
			}()
			go func() {
				defer wg.Done()
				overwriteErr = alice.StoreFile("file1.txt", []byte("NEW"))
			}()
			wg.Wait()

			Expect(appendErr).To(BeNil(), fmt.Sprintf("iteration %d: append failed", iter))
			Expect(overwriteErr).To(BeNil(), fmt.Sprintf("iteration %d: overwrite failed", iter))

			finalContent, err := alice.LoadFile("file1.txt")
			Expect(err).To(BeNil())
			Expect(string(finalContent)).To(SatisfyAny(Equal("NEW"), Equal("NEW+A")),
				fmt.Sprintf("iteration %d: illegal final content %q", iter, finalContent))

			_, accessBox2, err := resolveFile(alice, "file1.txt")
			Expect(err).To(BeNil())
			finalMetadata, err := loadMetadata(accessBox2)
			Expect(err).To(BeNil())
			Expect(finalMetadata.Version).To(Equal(initialMetadata.Version + 2))
		}
	})

	Specify("21: concurrent overwrites of the same file serialize; Version advances once per successful overwrite", func() {
		alice, err := InitUser("alice", "password")
		Expect(err).To(BeNil())
		Expect(alice.StoreFile("file1.txt", []byte("base"))).To(BeNil())

		const n = 10
		var wg sync.WaitGroup
		errs := make([]error, n)
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				errs[i] = alice.StoreFile("file1.txt", []byte(fmt.Sprintf("payload-%d", i)))
			}(i)
		}
		wg.Wait()

		for i, overwriteErr := range errs {
			Expect(overwriteErr).To(BeNil(), fmt.Sprintf("overwrite %d failed", i))
		}

		finalContent, err := alice.LoadFile("file1.txt")
		Expect(err).To(BeNil())

		matched := false
		for i := 0; i < n; i++ {
			if string(finalContent) == fmt.Sprintf("payload-%d", i) {
				matched = true
				break
			}
		}
		Expect(matched).To(BeTrue(), fmt.Sprintf("final content %q should equal exactly one of the raced payloads", finalContent))

		_, accessBox, err := resolveFile(alice, "file1.txt")
		Expect(err).To(BeNil())
		metadata, err := loadMetadata(accessBox)
		Expect(err).To(BeNil())
		Expect(metadata.Version).To(Equal(uint64(1 + n))) // 1 (create) + n overwrites
		Expect(metadata.ChunkCount).To(Equal(uint64(1)))
	})

	Specify("22: concurrent StoreFile calls for a not-yet-existing filename serialize into one create followed by overwrites", func() {
		alice, err := InitUser("alice", "password")
		Expect(err).To(BeNil())

		const n = 10
		var wg sync.WaitGroup
		errs := make([]error, n)
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				errs[i] = alice.StoreFile("newfile.txt", []byte(fmt.Sprintf("payload-%d", i)))
			}(i)
		}
		wg.Wait()

		for i, storeErr := range errs {
			Expect(storeErr).To(BeNil(), fmt.Sprintf("StoreFile %d failed", i))
		}

		namespaceEntry, accessBox, err := resolveFile(alice, "newfile.txt")
		Expect(err).To(BeNil())
		Expect(namespaceEntry.FileID).ToNot(Equal(uuid.Nil))

		metadata, err := loadMetadata(accessBox)
		Expect(err).To(BeNil())
		Expect(metadata.Version).To(Equal(uint64(n))) // first call creates (V1), remaining n-1 overwrite
		Expect(metadata.ChunkCount).To(Equal(uint64(1)))

		finalContent, err := alice.LoadFile("newfile.txt")
		Expect(err).To(BeNil())
		matched := false
		for i := 0; i < n; i++ {
			if string(finalContent) == fmt.Sprintf("payload-%d", i) {
				matched = true
			}
		}
		Expect(matched).To(BeTrue())
	})

	Specify("23: locking one file does not block operations on a different, independent file", func() {
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
		go func() {
			doneA <- alice.AppendToFile("fileA.txt", []byte("-append"))
		}()

		Eventually(pausedA, 2*time.Second).Should(Receive())

		// fileB must be completely unaffected by fileA's paused, still-held
		// File X lock -- if the LockManager secretly serialized all files
		// through one shared resource, this call would block until
		// proceedA fires below.
		errB := alice.AppendToFile("fileB.txt", []byte("-append"))
		Expect(errB).To(BeNil())

		close(proceedA)

		var errA error
		Eventually(doneA, 2*time.Second).Should(Receive(&errA))
		Expect(errA).To(BeNil())
	})

	Specify("24: LoadFile acquires Shared (not Exclusive) on the file resource -- many concurrent readers are granted together", func() {
		alice, err := InitUser("alice", "password")
		Expect(err).To(BeNil())
		Expect(alice.StoreFile("file1.txt", []byte("hello"))).To(BeNil())

		const n = 20
		var barrier sync.WaitGroup
		barrier.Add(n)
		release := make(chan struct{})
		var enteredCount int32

		setConcurrencyTestHook(func(tag string) {
			if tag == "load:file-locked:file1.txt" {
				atomic.AddInt32(&enteredCount, 1)
				barrier.Done()
				<-release
			}
		})

		barrierDone := make(chan struct{}, 1)
		go func() {
			barrier.Wait()
			barrierDone <- struct{}{}
		}()

		var wg sync.WaitGroup
		errs := make([]error, n)
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_, errs[i] = alice.LoadFile("file1.txt")
			}(i)
		}

		// If LoadFile took an Exclusive lock, only one goroutine at a time
		// could ever be paused inside the hook, and this barrier -- which
		// requires all n to be simultaneously paused there -- would never
		// complete within the timeout.
		Eventually(barrierDone, 3*time.Second).Should(Receive())
		Expect(atomic.LoadInt32(&enteredCount)).To(Equal(int32(n)))

		close(release)
		wg.Wait()

		for i, loadErr := range errs {
			Expect(loadErr).To(BeNil(), fmt.Sprintf("reader %d failed", i))
		}
	})

	Specify("25: a failed operation releases its locks -- a later operation on the same resource does not deadlock", func() {
		alice, err := InitUser("alice", "password")
		Expect(err).To(BeNil())
		Expect(alice.StoreFile("file1.txt", []byte("hello"))).To(BeNil())

		_, accessBox, err := resolveFile(alice, "file1.txt")
		Expect(err).To(BeNil())

		// Corrupt the owner AccessBox in place so that, on the next access,
		// validateFileAccessUnderLock fails authentication -- AFTER both
		// the Namespace and File locks for that operation have already
		// been acquired by AppendToFile. This exercises the deferred
		// guard.ReleaseAll() error path through real SAFER code.
		namespaceEntry, err := loadNamespaceEntry(alice, "file1.txt")
		Expect(err).To(BeNil())
		userlib.DatastoreSet(namespaceEntry.AccessBoxUUID, []byte("corrupted-not-a-valid-envelope"))

		err = alice.AppendToFile("file1.txt", []byte("more"))
		Expect(err).ToNot(BeNil())

		// If the failed AppendToFile above had leaked its Namespace/File
		// locks, this call would block forever instead of returning
		// promptly with its own (also expected) error.
		done := make(chan error, 1)
		go func() {
			_, e := alice.LoadFile("file1.txt")
			done <- e
		}()
		var loadErr error
		Eventually(done, 2*time.Second).Should(Receive(&loadErr))
		Expect(loadErr).ToNot(BeNil())

		// Restore the AccessBox and prove the resource is fully usable
		// again, not merely "fails fast."
		fixed, err := protectDatastoreObject(
			accessBoxObjectType,
			namespaceEntry.AccessBoxUUID,
			accessBox,
			namespaceEntry.AccessBoxEncKey,
			namespaceEntry.AccessBoxMACKey,
		)
		Expect(err).To(BeNil())
		userlib.DatastoreSet(namespaceEntry.AccessBoxUUID, fixed)

		content, err := alice.LoadFile("file1.txt")
		Expect(err).To(BeNil())
		Expect(string(content)).To(Equal("hello"))
	})
})
