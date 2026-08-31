package client

///////////////////////////////////////////////////
//                                               //
// Everything in this file will NOT be graded!!! //
//                                               //
///////////////////////////////////////////////////

// Phase 5 concurrency tests: CreateInvitation / AcceptInvitation /
// RevokeAccess strict-2PL integration, and their interaction with the
// Phase-4-integrated core file operations. Like phase4_concurrency_test.go,
// these are white-box (package client) and use setConcurrencyTestHook to
// force deterministic overlapping schedules instead of time.Sleep timing.

import (
	"fmt"
	"sync"
	"time"

	userlib "github.com/cs161-staff/project2-userlib"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// checkAuthorizationInvariants inspects persistent authoritative state
// (not just a return value) after some sequence of sharing/revocation
// operations has fully settled, per Phase 5 item B17:
//   - owner AccessBox epoch == AccessBoxStructure.CurrentEpoch
//   - Metadata.EpochID matches the current epoch
//   - Metadata.Version equals expectedVersion (preserved across
//     authorization-only transitions such as revoke)
//   - FileStatus is currently valid for the current AccessBox (verified by
//     resolveFile/validateFileAccessUnderLock's own verifyFileStatus call
//     succeeding)
//   - every surviving recipient's branch AccessBox is stamped with the
//     current epoch (not a stale one)
func checkAuthorizationInvariants(owner *User, filename string, expectedVersion uint64) {
	violation := CheckAuthorizationInvariantsForBenchmark(owner, filename, expectedVersion)
	Expect(violation).To(Equal(""), fmt.Sprintf("authorization invariant violation: %s", violation))
}

var _ = Describe("Phase 5: Strict 2PL sharing/revocation concurrency", func() {

	BeforeEach(func() {
		userlib.DatastoreClear()
		userlib.KeystoreClear()
		setConcurrencyTestHook(nil)
	})

	AfterEach(func() {
		setConcurrencyTestHook(nil)
	})

	Specify("1: concurrent CreateInvitation with many distinct recipients -- all survive", func() {
		alice, err := InitUser("alice", "password")
		Expect(err).To(BeNil())
		Expect(alice.StoreFile("file1.txt", []byte("hello"))).To(BeNil())

		const n = 15
		recipients := make([]string, n)
		for i := 0; i < n; i++ {
			name := fmt.Sprintf("bob%d", i)
			recipients[i] = name
			_, err := InitUser(name, "password")
			Expect(err).To(BeNil())
		}

		var wg sync.WaitGroup
		errs := make([]error, n)
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_, errs[i] = alice.CreateInvitation("file1.txt", recipients[i])
			}(i)
		}
		wg.Wait()

		for i, err := range errs {
			Expect(err).To(BeNil(), fmt.Sprintf("CreateInvitation for %s failed", recipients[i]))
		}

		namespaceEntry, accessBox, err := resolveFile(alice, "file1.txt")
		Expect(err).To(BeNil())
		structure, err := loadOwnerAccessBoxStructure(namespaceEntry, accessBox)
		Expect(err).To(BeNil())
		Expect(structure.RecipientBoxes).To(HaveLen(n))
		for _, name := range recipients {
			_, tracked := structure.RecipientBoxes[name]
			Expect(tracked).To(BeTrue(), fmt.Sprintf("%s should be tracked in RecipientBoxes", name))
		}

		checkAuthorizationInvariants(alice, "file1.txt", 1)
	})

	Specify("2: a second CreateInvitation cannot read RecipientBoxes while the first's File X transaction is still active", func() {
		alice, err := InitUser("alice", "password")
		Expect(err).To(BeNil())
		_, err = InitUser("bob", "password")
		Expect(err).To(BeNil())
		_, err = InitUser("carol", "password")
		Expect(err).To(BeNil())
		Expect(alice.StoreFile("file1.txt", []byte("hello"))).To(BeNil())

		_, accessBox, err := resolveFile(alice, "file1.txt")
		Expect(err).To(BeNil())
		hookTag := "create-invitation:structure-loaded:" + accessBox.FileID.String()

		pausedFirst := make(chan struct{}, 2)
		proceedFirst := make(chan struct{})

		setConcurrencyTestHook(func(tag string) {
			if tag == hookTag {
				pausedFirst <- struct{}{}
				<-proceedFirst
			}
		})

		doneBob := make(chan error, 1)
		go func() {
			_, e := alice.CreateInvitation("file1.txt", "bob")
			doneBob <- e
		}()

		Eventually(pausedFirst, 2*time.Second).Should(Receive())

		doneCarol := make(chan error, 1)
		go func() {
			_, e := alice.CreateInvitation("file1.txt", "carol")
			doneCarol <- e
		}()

		// carol's CreateInvitation cannot even reach its own
		// structure-loaded hook point while bob's File X is held.
		Consistently(doneCarol, 200*time.Millisecond).ShouldNot(Receive())

		close(proceedFirst)

		var errBob, errCarol error
		Eventually(doneBob, 2*time.Second).Should(Receive(&errBob))
		Eventually(doneCarol, 2*time.Second).Should(Receive(&errCarol))
		Expect(errBob).To(BeNil())
		Expect(errCarol).To(BeNil())

		namespaceEntry, accessBox2, err := resolveFile(alice, "file1.txt")
		Expect(err).To(BeNil())
		structure, err := loadOwnerAccessBoxStructure(namespaceEntry, accessBox2)
		Expect(err).To(BeNil())
		_, bobTracked := structure.RecipientBoxes["bob"]
		_, carolTracked := structure.RecipientBoxes["carol"]
		Expect(bobTracked).To(BeTrue(), "bob must not have been lost to the old RecipientBoxes race")
		Expect(carolTracked).To(BeTrue(), "carol must not have been lost to the old RecipientBoxes race")
	})

	Specify("3: Accept vs Revoke -- Accept wins: Accept succeeds, Revoke then succeeds, and the (now revoked) recipient's later access fails", func() {
		alice, err := InitUser("alice", "password")
		Expect(err).To(BeNil())
		bob, err := InitUser("bob", "password")
		Expect(err).To(BeNil())
		Expect(alice.StoreFile("file1.txt", []byte("hello"))).To(BeNil())

		invite, err := alice.CreateInvitation("file1.txt", "bob")
		Expect(err).To(BeNil())

		_, accessBox, err := resolveFile(alice, "file1.txt")
		Expect(err).To(BeNil())
		hookTag := "accept:validated:" + accessBox.FileID.String()

		acceptPaused := make(chan struct{}, 2)
		proceedAccept := make(chan struct{})

		setConcurrencyTestHook(func(tag string) {
			if tag == hookTag {
				acceptPaused <- struct{}{}
				<-proceedAccept
			}
		})

		doneAccept := make(chan error, 1)
		go func() {
			doneAccept <- bob.AcceptInvitation("alice", invite, "shared.txt")
		}()

		Eventually(acceptPaused, 2*time.Second).Should(Receive())

		doneRevoke := make(chan error, 1)
		go func() {
			doneRevoke <- alice.RevokeAccess("file1.txt", "bob")
		}()

		// Revoke needs File X; Accept is holding File S paused mid-way,
		// so Revoke must not be able to complete yet.
		Consistently(doneRevoke, 200*time.Millisecond).ShouldNot(Receive())

		close(proceedAccept)

		var acceptErr error
		Eventually(doneAccept, 2*time.Second).Should(Receive(&acceptErr))
		Expect(acceptErr).To(BeNil(), "Accept must succeed: it validated before Revoke committed")

		var revokeErr error
		Eventually(doneRevoke, 2*time.Second).Should(Receive(&revokeErr))
		Expect(revokeErr).To(BeNil())

		// bob's installed capability is now stale (its branch AccessBox
		// was deleted by the revoke that committed after bob's install).
		_, loadErr := bob.LoadFile("shared.txt")
		Expect(loadErr).ToNot(BeNil(), "bob's subsequent access must fail: his installed capability is now revoked")

		checkAuthorizationInvariants(alice, "file1.txt", 1)
	})

	Specify("4: Accept vs Revoke -- Revoke wins: Accept fails and installs no usable namespace access", func() {
		alice, err := InitUser("alice", "password")
		Expect(err).To(BeNil())
		bob, err := InitUser("bob", "password")
		Expect(err).To(BeNil())
		Expect(alice.StoreFile("file1.txt", []byte("hello"))).To(BeNil())

		invite, err := alice.CreateInvitation("file1.txt", "bob")
		Expect(err).To(BeNil())

		_, accessBox, err := resolveFile(alice, "file1.txt")
		Expect(err).To(BeNil())
		hookTag := "revoke:content-loaded:" + accessBox.FileID.String()

		revokePaused := make(chan struct{}, 2)
		proceedRevoke := make(chan struct{})

		setConcurrencyTestHook(func(tag string) {
			if tag == hookTag {
				revokePaused <- struct{}{}
				<-proceedRevoke
			}
		})

		doneRevoke := make(chan error, 1)
		go func() {
			doneRevoke <- alice.RevokeAccess("file1.txt", "bob")
		}()

		Eventually(revokePaused, 2*time.Second).Should(Receive())

		doneAccept := make(chan error, 1)
		go func() {
			doneAccept <- bob.AcceptInvitation("alice", invite, "shared.txt")
		}()

		// Accept needs File S; Revoke is holding File X paused mid-way.
		Consistently(doneAccept, 200*time.Millisecond).ShouldNot(Receive())

		close(proceedRevoke)

		var revokeErr error
		Eventually(doneRevoke, 2*time.Second).Should(Receive(&revokeErr))
		Expect(revokeErr).To(BeNil())

		var acceptErr error
		Eventually(doneAccept, 2*time.Second).Should(Receive(&acceptErr))
		Expect(acceptErr).ToNot(BeNil(), "Accept must fail: the capability it validated against was already revoked")

		// No usable namespace access was installed for bob.
		_, loadErr := bob.LoadFile("shared.txt")
		Expect(loadErr).ToNot(BeNil())

		checkAuthorizationInvariants(alice, "file1.txt", 1)
	})

	Specify("5: Revoke vs Load -- Load wins: Load returns the complete valid old file, then Revoke completes", func() {
		alice, err := InitUser("alice", "password")
		Expect(err).To(BeNil())
		bob, err := InitUser("bob", "password")
		Expect(err).To(BeNil())
		Expect(alice.StoreFile("file1.txt", []byte("hello"))).To(BeNil())

		invite, err := alice.CreateInvitation("file1.txt", "bob")
		Expect(err).To(BeNil())
		Expect(bob.AcceptInvitation("alice", invite, "shared.txt")).To(BeNil())

		readerPaused := make(chan struct{}, 2)
		proceedReader := make(chan struct{})

		setConcurrencyTestHook(func(tag string) {
			if tag == "load:file-locked:shared.txt" {
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
			c, e := bob.LoadFile("shared.txt")
			doneRead <- readResult{c, e}
		}()

		Eventually(readerPaused, 2*time.Second).Should(Receive())

		doneRevoke := make(chan error, 1)
		go func() {
			doneRevoke <- alice.RevokeAccess("file1.txt", "bob")
		}()

		Consistently(doneRevoke, 200*time.Millisecond).ShouldNot(Receive())

		close(proceedReader)

		var rr readResult
		Eventually(doneRead, 2*time.Second).Should(Receive(&rr))
		Expect(rr.err).To(BeNil())
		Expect(string(rr.content)).To(Equal("hello"))

		var revokeErr error
		Eventually(doneRevoke, 2*time.Second).Should(Receive(&revokeErr))
		Expect(revokeErr).To(BeNil())
	})

	Specify("6: Revoke vs Load -- Revoke wins: Load after revoke fails cleanly via access revalidation", func() {
		alice, err := InitUser("alice", "password")
		Expect(err).To(BeNil())
		bob, err := InitUser("bob", "password")
		Expect(err).To(BeNil())
		Expect(alice.StoreFile("file1.txt", []byte("hello"))).To(BeNil())

		invite, err := alice.CreateInvitation("file1.txt", "bob")
		Expect(err).To(BeNil())
		Expect(bob.AcceptInvitation("alice", invite, "shared.txt")).To(BeNil())

		_, accessBox, err := resolveFile(alice, "file1.txt")
		Expect(err).To(BeNil())
		hookTag := "revoke:content-loaded:" + accessBox.FileID.String()

		revokePaused := make(chan struct{}, 2)
		proceedRevoke := make(chan struct{})

		setConcurrencyTestHook(func(tag string) {
			if tag == hookTag {
				revokePaused <- struct{}{}
				<-proceedRevoke
			}
		})

		doneRevoke := make(chan error, 1)
		go func() {
			doneRevoke <- alice.RevokeAccess("file1.txt", "bob")
		}()

		Eventually(revokePaused, 2*time.Second).Should(Receive())

		type readResult struct {
			content []byte
			err     error
		}
		doneRead := make(chan readResult, 1)
		go func() {
			c, e := bob.LoadFile("shared.txt")
			doneRead <- readResult{c, e}
		}()

		Consistently(doneRead, 200*time.Millisecond).ShouldNot(Receive())

		close(proceedRevoke)

		var revokeErr error
		Eventually(doneRevoke, 2*time.Second).Should(Receive(&revokeErr))
		Expect(revokeErr).To(BeNil())

		var rr readResult
		Eventually(doneRead, 2*time.Second).Should(Receive(&rr))
		Expect(rr.err).ToNot(BeNil(), "bob's Load must fail cleanly after revoke, via access revalidation")
	})

	Specify("7: Revoke vs Append -- Append wins: Append commits exactly once, Version+1, Revoke preserves the resulting Version", func() {
		alice, err := InitUser("alice", "password")
		Expect(err).To(BeNil())
		_, err = InitUser("bob", "password")
		Expect(err).To(BeNil())
		Expect(alice.StoreFile("file1.txt", []byte("base"))).To(BeNil())
		invite, err := alice.CreateInvitation("file1.txt", "bob")
		Expect(err).To(BeNil())

		appendPaused := make(chan struct{}, 2)
		proceedAppend := make(chan struct{})

		setConcurrencyTestHook(func(tag string) {
			if tag == "append:metadata-loaded:file1.txt" {
				appendPaused <- struct{}{}
				<-proceedAppend
			}
		})

		doneAppend := make(chan error, 1)
		go func() {
			doneAppend <- alice.AppendToFile("file1.txt", []byte("-more"))
		}()

		Eventually(appendPaused, 2*time.Second).Should(Receive())

		doneRevoke := make(chan error, 1)
		go func() {
			doneRevoke <- alice.RevokeAccess("file1.txt", "bob")
		}()

		Consistently(doneRevoke, 200*time.Millisecond).ShouldNot(Receive())

		close(proceedAppend)

		var appendErr error
		Eventually(doneAppend, 2*time.Second).Should(Receive(&appendErr))
		Expect(appendErr).To(BeNil())

		var revokeErr error
		Eventually(doneRevoke, 2*time.Second).Should(Receive(&revokeErr))
		Expect(revokeErr).To(BeNil())

		content, err := alice.LoadFile("file1.txt")
		Expect(err).To(BeNil())
		Expect(string(content)).To(Equal("base-more"))

		_ = invite
		checkAuthorizationInvariants(alice, "file1.txt", 2) // create=1, append=2
	})

	Specify("8: Revoke vs Append -- Revoke wins: the revoked append fails before any mutation, Version unchanged", func() {
		alice, err := InitUser("alice", "password")
		Expect(err).To(BeNil())
		bob, err := InitUser("bob", "password")
		Expect(err).To(BeNil())
		Expect(alice.StoreFile("file1.txt", []byte("base"))).To(BeNil())
		invite, err := alice.CreateInvitation("file1.txt", "bob")
		Expect(err).To(BeNil())
		Expect(bob.AcceptInvitation("alice", invite, "shared.txt")).To(BeNil())

		_, accessBox, err := resolveFile(alice, "file1.txt")
		Expect(err).To(BeNil())
		hookTag := "revoke:content-loaded:" + accessBox.FileID.String()

		revokePaused := make(chan struct{}, 2)
		proceedRevoke := make(chan struct{})

		setConcurrencyTestHook(func(tag string) {
			if tag == hookTag {
				revokePaused <- struct{}{}
				<-proceedRevoke
			}
		})

		doneRevoke := make(chan error, 1)
		go func() {
			doneRevoke <- alice.RevokeAccess("file1.txt", "bob")
		}()

		Eventually(revokePaused, 2*time.Second).Should(Receive())

		doneAppend := make(chan error, 1)
		go func() {
			doneAppend <- bob.AppendToFile("shared.txt", []byte("-should-not-land"))
		}()

		Consistently(doneAppend, 200*time.Millisecond).ShouldNot(Receive())

		close(proceedRevoke)

		var revokeErr error
		Eventually(doneRevoke, 2*time.Second).Should(Receive(&revokeErr))
		Expect(revokeErr).To(BeNil())

		var appendErr error
		Eventually(doneAppend, 2*time.Second).Should(Receive(&appendErr))
		Expect(appendErr).ToNot(BeNil(), "bob's append must fail: his capability was revoked before he could acquire File X")

		content, err := alice.LoadFile("file1.txt")
		Expect(err).To(BeNil())
		Expect(string(content)).To(Equal("base"), "the failed append must not have mutated content")

		checkAuthorizationInvariants(alice, "file1.txt", 1) // unchanged by both revoke and the failed append
	})

	Specify("9a: Revoke vs CreateInvitation -- Revoke first: the surviving structure never restores a revoked recipient nor loses a newly added one", func() {
		alice, err := InitUser("alice", "password")
		Expect(err).To(BeNil())
		_, err = InitUser("bob", "password")
		Expect(err).To(BeNil())
		_, err = InitUser("carol", "password")
		Expect(err).To(BeNil())
		Expect(alice.StoreFile("file1.txt", []byte("hello"))).To(BeNil())
		_, err = alice.CreateInvitation("file1.txt", "bob")
		Expect(err).To(BeNil())

		_, accessBox, err := resolveFile(alice, "file1.txt")
		Expect(err).To(BeNil())
		revokeHookTag := "revoke:content-loaded:" + accessBox.FileID.String()

		revokePaused := make(chan struct{}, 2)
		proceedRevoke := make(chan struct{})

		setConcurrencyTestHook(func(tag string) {
			if tag == revokeHookTag {
				revokePaused <- struct{}{}
				<-proceedRevoke
			}
		})

		doneRevoke := make(chan error, 1)
		go func() {
			doneRevoke <- alice.RevokeAccess("file1.txt", "bob")
		}()

		Eventually(revokePaused, 2*time.Second).Should(Receive())

		doneInvite := make(chan error, 1)
		go func() {
			_, e := alice.CreateInvitation("file1.txt", "carol")
			doneInvite <- e
		}()

		Consistently(doneInvite, 200*time.Millisecond).ShouldNot(Receive())

		close(proceedRevoke)

		var revokeErr, inviteErr error
		Eventually(doneRevoke, 2*time.Second).Should(Receive(&revokeErr))
		Eventually(doneInvite, 2*time.Second).Should(Receive(&inviteErr))
		Expect(revokeErr).To(BeNil())
		Expect(inviteErr).To(BeNil())

		namespaceEntry, accessBox2, err := resolveFile(alice, "file1.txt")
		Expect(err).To(BeNil())
		structure, err := loadOwnerAccessBoxStructure(namespaceEntry, accessBox2)
		Expect(err).To(BeNil())

		_, bobTracked := structure.RecipientBoxes["bob"]
		_, carolTracked := structure.RecipientBoxes["carol"]
		Expect(bobTracked).To(BeFalse(), "bob must remain revoked, not accidentally restored")
		Expect(carolTracked).To(BeTrue(), "carol's invitation must not be lost to the revoke's structure replacement")

		checkAuthorizationInvariants(alice, "file1.txt", 1)
	})

	Specify("9b: Revoke vs CreateInvitation -- CreateInvitation first: a newly added recipient survives a concurrent unrelated revoke", func() {
		alice, err := InitUser("alice", "password")
		Expect(err).To(BeNil())
		_, err = InitUser("bob", "password")
		Expect(err).To(BeNil())
		_, err = InitUser("carol", "password")
		Expect(err).To(BeNil())
		Expect(alice.StoreFile("file1.txt", []byte("hello"))).To(BeNil())
		_, err = alice.CreateInvitation("file1.txt", "bob")
		Expect(err).To(BeNil())

		_, accessBox, err := resolveFile(alice, "file1.txt")
		Expect(err).To(BeNil())
		inviteHookTag := "create-invitation:structure-loaded:" + accessBox.FileID.String()

		invitePaused := make(chan struct{}, 2)
		proceedInvite := make(chan struct{})

		setConcurrencyTestHook(func(tag string) {
			if tag == inviteHookTag {
				invitePaused <- struct{}{}
				<-proceedInvite
			}
		})

		doneInvite := make(chan error, 1)
		go func() {
			_, e := alice.CreateInvitation("file1.txt", "carol")
			doneInvite <- e
		}()

		Eventually(invitePaused, 2*time.Second).Should(Receive())

		doneRevoke := make(chan error, 1)
		go func() {
			doneRevoke <- alice.RevokeAccess("file1.txt", "bob")
		}()

		Consistently(doneRevoke, 200*time.Millisecond).ShouldNot(Receive())

		close(proceedInvite)

		var inviteErr, revokeErr error
		Eventually(doneInvite, 2*time.Second).Should(Receive(&inviteErr))
		Eventually(doneRevoke, 2*time.Second).Should(Receive(&revokeErr))
		Expect(inviteErr).To(BeNil())
		Expect(revokeErr).To(BeNil())

		namespaceEntry, accessBox2, err := resolveFile(alice, "file1.txt")
		Expect(err).To(BeNil())
		structure, err := loadOwnerAccessBoxStructure(namespaceEntry, accessBox2)
		Expect(err).To(BeNil())

		_, bobTracked := structure.RecipientBoxes["bob"]
		_, carolTracked := structure.RecipientBoxes["carol"]
		Expect(bobTracked).To(BeFalse())
		Expect(carolTracked).To(BeTrue(), "carol, added before the revoke committed, must survive it")

		checkAuthorizationInvariants(alice, "file1.txt", 1)
	})

	Specify("10: concurrent RevokeAccess for two distinct recipients on the same file both survive", func() {
		alice, err := InitUser("alice", "password")
		Expect(err).To(BeNil())
		_, err = InitUser("bob", "password")
		Expect(err).To(BeNil())
		_, err = InitUser("carol", "password")
		Expect(err).To(BeNil())
		Expect(alice.StoreFile("file1.txt", []byte("hello"))).To(BeNil())
		_, err = alice.CreateInvitation("file1.txt", "bob")
		Expect(err).To(BeNil())
		_, err = alice.CreateInvitation("file1.txt", "carol")
		Expect(err).To(BeNil())

		var wg sync.WaitGroup
		var revokeBobErr, revokeCarolErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			revokeBobErr = alice.RevokeAccess("file1.txt", "bob")
		}()
		go func() {
			defer wg.Done()
			revokeCarolErr = alice.RevokeAccess("file1.txt", "carol")
		}()
		wg.Wait()

		Expect(revokeBobErr).To(BeNil())
		Expect(revokeCarolErr).To(BeNil())

		namespaceEntry, accessBox, err := resolveFile(alice, "file1.txt")
		Expect(err).To(BeNil())
		structure, err := loadOwnerAccessBoxStructure(namespaceEntry, accessBox)
		Expect(err).To(BeNil())

		_, bobTracked := structure.RecipientBoxes["bob"]
		_, carolTracked := structure.RecipientBoxes["carol"]
		Expect(bobTracked).To(BeFalse(), "bob's revocation must not be lost to a last-writer-wins structure replacement")
		Expect(carolTracked).To(BeFalse(), "carol's revocation must not be lost to a last-writer-wins structure replacement")

		checkAuthorizationInvariants(alice, "file1.txt", 1)
	})

	Specify("11: AcceptInvitation vs StoreFile targeting the same (recipient, filename) serialize under Namespace X with a legal outcome", func() {
		alice, err := InitUser("alice", "password")
		Expect(err).To(BeNil())
		bob, err := InitUser("bob", "password")
		Expect(err).To(BeNil())
		Expect(alice.StoreFile("file1.txt", []byte("owner-content"))).To(BeNil())

		invite, err := alice.CreateInvitation("file1.txt", "bob")
		Expect(err).To(BeNil())

		var wg sync.WaitGroup
		var acceptErr, storeErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			acceptErr = bob.AcceptInvitation("alice", invite, "shared.txt")
		}()
		go func() {
			defer wg.Done()
			storeErr = bob.StoreFile("shared.txt", []byte("bob-own-content"))
		}()
		wg.Wait()

		// Exactly one legal final state: the namespace entry for
		// (bob, "shared.txt") must exist and be self-consistent, and the
		// outcome of each call must be explicable by one serial order.
		//
		// Order A (Accept first): Accept installs bob's alias to alice's
		// file; StoreFile then legitimately overwrites through that alias
		// (an existing, non-Phase-5 SAFER behavior -- a recipient's
		// StoreFile on an aliased name overwrites the aliased file). Both
		// calls succeed.
		//
		// Order B (StoreFile first): StoreFile creates a brand-new,
		// bob-owned file at that name; Accept's occupied-check then
		// correctly reports the name taken and fails cleanly, without
		// consuming the invitation or installing anything.
		switch {
		case acceptErr == nil && storeErr == nil:
			content, err := bob.LoadFile("shared.txt")
			Expect(err).To(BeNil())
			Expect(string(content)).To(Equal("bob-own-content"))
		case acceptErr != nil && storeErr == nil:
			content, err := bob.LoadFile("shared.txt")
			Expect(err).To(BeNil())
			Expect(string(content)).To(Equal("bob-own-content"))
		default:
			Fail(fmt.Sprintf("unexpected outcome: acceptErr=%v storeErr=%v", acceptErr, storeErr))
		}
	})
})
