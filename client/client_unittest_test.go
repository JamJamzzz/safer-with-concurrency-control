package client

///////////////////////////////////////////////////
//                                               //
// Everything in this file will NOT be graded!!! //
//                                               //
///////////////////////////////////////////////////

// In this unit tests file, you can write white-box unit tests on your implementation.
// These are different from the black-box integration tests in client_test.go,
// because in this unit tests file, you can use details specific to your implementation.

// For example, in this unit tests file, you can access struct fields and helper methods
// that you defined, but in the integration tests (client_test.go), you can only access
// the 8 functions (StoreFile, LoadFile, etc.) that are common to all implementations.

// In this unit tests file, you can write InitUser where you would write client.InitUser in the
// integration tests (client_test.go). In other words, the "client." in front is no longer needed.

import (
	userlib "github.com/cs161-staff/project2-userlib"
	"sync"
	"testing"
)

import (
	_ "encoding/hex"
	_ "errors"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	_ "strconv"
	_ "strings"
	_ "encoding/json"
)

func TestSetupAndExecution(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Client Unit Tests")
}

var _ = Describe("Client Unit Tests", func() {

	BeforeEach(func() {
		userlib.DatastoreClear()
		userlib.KeystoreClear()
	})

	// func decryptStoredAccountForTest(
    // 	username string,
    // 	password string,
	// ) (Account, AuthenticatedEnvelope) {
    // 	accountUUID, err := getUserUUID(username)
    // 	Expect(err).To(BeNil())

    // 	storedBytes, exists := userlib.DatastoreGet(accountUUID)
    // 	Expect(exists).To(BeTrue())
    // 	Expect(storedBytes).ToNot(BeEmpty())

    // 	var envelope AuthenticatedEnvelope
    // 	err = json.Unmarshal(storedBytes, &envelope)
    // 	Expect(err).To(BeNil())

    // 	Expect(envelope.Version).To(Equal(currentVersion))
    // 	Expect(len(envelope.Ciphertext)).To(
    //     	BeNumerically(">=", userlib.AESBlockSizeBytes),
    // 	)
    // 	Expect(len(envelope.MAC)).To(Equal(userlib.HashSizeBytes))

    // 	encKey, macKey, err := deriveAccountKey(
    // 	    username,
    // 	    password,
    // 	    accountUUID,
    // 	)
    // 	Expect(err).To(BeNil())

    // 	expectedMAC, err := userlib.HMACEval(
    // 	    macKey,
    // 	    accountMACMessage(accountUUID, envelope.Ciphertext),
    // 	)
    // 	Expect(err).To(BeNil())
    // 	Expect(userlib.HMACEqual(expectedMAC, envelope.MAC)).To(BeTrue())

    // 	plaintext := userlib.SymDec(encKey, envelope.Ciphertext)

    // 	var account Account
    // 	err = json.Unmarshal(plaintext, &account)
    // 	Expect(err).To(BeNil())

    // 	return account, envelope
	// }

	Describe("Unit Tests", func() {
		Specify("Basic Test: Check that the Username field is set for a new user", func() {
			userlib.DebugMsg("Initializing user Alice.")
			// Note: In the integration tests (client_test.go) this would need to
			// be client.InitUser, but here (client_unittests.go) you can write InitUser.
			alice, err := InitUser("alice", "password")
			Expect(err).To(BeNil())

			// Note: You can access the Username field of the User struct here.
			// But in the integration tests (client_test.go), you cannot access
			// struct fields because not all implementations will have a username field.
			Expect(alice.Username).To(Equal("alice"))
		})
	})

	// Describe("InitUser white-box tests", func() {
    // 	Specify("creates complete local user state", func() {
    //     	alice, err := InitUser("alice", "password")

    //     	Expect(err).To(BeNil())
    //     	Expect(alice).ToNot(BeNil())
    //     	Expect(alice.Username).To(Equal("alice"))
    //     	Expect(alice.NamespaceRoot).To(HaveLen(16))
    //     	Expect(alice.PKEPrivate.KeyType).To(Equal("PKE"))
    //     	Expect(alice.SignPrivate.KeyType).To(Equal("DS"))
    // 	})
	// })

	// Phase 3: independent logical file-content versioning. These are
	// white-box because they inspect Metadata.Version directly, which is
	// not part of SAFER's public API by design (see Phase 3 instructions,
	// item 15: Version is not exposed through StoreFile/LoadFile/etc.).
	Describe("Phase 3: File Content Versioning", func() {

		getOwnerMetadata := func(user *User, filename string) Metadata {
			_, accessBox, err := resolveFile(user, filename)
			Expect(err).To(BeNil())

			metadata, err := loadMetadata(accessBox)
			Expect(err).To(BeNil())

			return metadata
		}

		Specify("A: a newly created file starts at Version 1", func() {
			alice, err := InitUser("alice", "password")
			Expect(err).To(BeNil())

			Expect(alice.StoreFile("file1.txt", []byte("hello"))).To(BeNil())

			metadata := getOwnerMetadata(alice, "file1.txt")
			Expect(metadata.Version).To(Equal(uint64(1)))
		})

		Specify("B: each non-empty append advances Version by exactly one", func() {
			alice, err := InitUser("alice", "password")
			Expect(err).To(BeNil())

			Expect(alice.StoreFile("file1.txt", []byte("A"))).To(BeNil())
			Expect(getOwnerMetadata(alice, "file1.txt").Version).To(Equal(uint64(1)))

			Expect(alice.AppendToFile("file1.txt", []byte("B"))).To(BeNil())
			Expect(getOwnerMetadata(alice, "file1.txt").Version).To(Equal(uint64(2)))

			Expect(alice.AppendToFile("file1.txt", []byte("C"))).To(BeNil())
			metadata := getOwnerMetadata(alice, "file1.txt")
			Expect(metadata.Version).To(Equal(uint64(3)))

			content, err := alice.LoadFile("file1.txt")
			Expect(err).To(BeNil())
			Expect(content).To(Equal([]byte("ABC")))
		})

		Specify("C: an empty append is a no-op and does not advance Version", func() {
			alice, err := InitUser("alice", "password")
			Expect(err).To(BeNil())

			Expect(alice.StoreFile("file1.txt", []byte("A"))).To(BeNil())
			Expect(alice.AppendToFile("file1.txt", []byte("B"))).To(BeNil())

			before := getOwnerMetadata(alice, "file1.txt")
			Expect(before.Version).To(Equal(uint64(2)))

			Expect(alice.AppendToFile("file1.txt", []byte{})).To(BeNil())

			after := getOwnerMetadata(alice, "file1.txt")
			Expect(after.Version).To(Equal(before.Version))
		})

		Specify("D: an overwriting StoreFile advances Version by exactly one, even though ChunkCount resets", func() {
			alice, err := InitUser("alice", "password")
			Expect(err).To(BeNil())

			Expect(alice.StoreFile("file1.txt", []byte("A"))).To(BeNil())
			Expect(alice.AppendToFile("file1.txt", []byte("B"))).To(BeNil())

			before := getOwnerMetadata(alice, "file1.txt")
			Expect(before.Version).To(Equal(uint64(2)))
			Expect(before.ChunkCount).To(Equal(uint64(2)))

			Expect(alice.StoreFile("file1.txt", []byte("overwritten"))).To(BeNil())

			after := getOwnerMetadata(alice, "file1.txt")
			Expect(after.Version).To(Equal(uint64(3)))
			Expect(after.ChunkCount).To(Equal(uint64(1)))

			content, err := alice.LoadFile("file1.txt")
			Expect(err).To(BeNil())
			Expect(content).To(Equal([]byte("overwritten")))
		})

		Specify("E: LoadFile never advances Version", func() {
			alice, err := InitUser("alice", "password")
			Expect(err).To(BeNil())

			Expect(alice.StoreFile("file1.txt", []byte("hello"))).To(BeNil())
			expected := getOwnerMetadata(alice, "file1.txt").Version

			for i := 0; i < 5; i++ {
				_, err := alice.LoadFile("file1.txt")
				Expect(err).To(BeNil())
				Expect(getOwnerMetadata(alice, "file1.txt").Version).To(Equal(expected))
			}
		})

		Specify("F: CreateInvitation never advances Version", func() {
			alice, err := InitUser("alice", "password")
			Expect(err).To(BeNil())
			_, err = InitUser("bob", "password")
			Expect(err).To(BeNil())

			Expect(alice.StoreFile("file1.txt", []byte("hello"))).To(BeNil())
			before := getOwnerMetadata(alice, "file1.txt").Version

			_, err = alice.CreateInvitation("file1.txt", "bob")
			Expect(err).To(BeNil())

			Expect(getOwnerMetadata(alice, "file1.txt").Version).To(Equal(before))
		})

		Specify("G: AcceptInvitation never advances the logical file Version", func() {
			alice, err := InitUser("alice", "password")
			Expect(err).To(BeNil())
			bob, err := InitUser("bob", "password")
			Expect(err).To(BeNil())

			Expect(alice.StoreFile("file1.txt", []byte("hello"))).To(BeNil())
			before := getOwnerMetadata(alice, "file1.txt").Version

			invite, err := alice.CreateInvitation("file1.txt", "bob")
			Expect(err).To(BeNil())

			Expect(bob.AcceptInvitation("alice", invite, "shared.txt")).To(BeNil())

			Expect(getOwnerMetadata(alice, "file1.txt").Version).To(Equal(before))
			Expect(getOwnerMetadata(bob, "shared.txt").Version).To(Equal(before))
		})

		Specify("H: RevokeAccess rotates EpochID but preserves Version exactly (mandatory)", func() {
			alice, err := InitUser("alice", "password")
			Expect(err).To(BeNil())
			bob, err := InitUser("bob", "password")
			Expect(err).To(BeNil())

			Expect(alice.StoreFile("file1.txt", []byte("A"))).To(BeNil())
			Expect(alice.AppendToFile("file1.txt", []byte("B"))).To(BeNil())
			Expect(alice.AppendToFile("file1.txt", []byte("C"))).To(BeNil())

			beforeRevoke := getOwnerMetadata(alice, "file1.txt")
			Expect(beforeRevoke.Version).To(Equal(uint64(3)))

			invite, err := alice.CreateInvitation("file1.txt", "bob")
			Expect(err).To(BeNil())
			Expect(bob.AcceptInvitation("alice", invite, "shared.txt")).To(BeNil())

			Expect(alice.RevokeAccess("file1.txt", "bob")).To(BeNil())

			afterRevoke := getOwnerMetadata(alice, "file1.txt")
			Expect(afterRevoke.Version).To(Equal(beforeRevoke.Version))
			Expect(afterRevoke.EpochID).ToNot(Equal(beforeRevoke.EpochID))

			content, err := alice.LoadFile("file1.txt")
			Expect(err).To(BeNil())
			Expect(content).To(Equal([]byte("ABC")))
		})

		Specify("I: version increment reports an explicit overflow error instead of wrapping to zero", func() {
			alice, err := InitUser("alice", "password")
			Expect(err).To(BeNil())

			Expect(alice.StoreFile("file1.txt", []byte("A"))).To(BeNil())

			_, accessBox, err := resolveFile(alice, "file1.txt")
			Expect(err).To(BeNil())

			metadata, err := loadMetadata(accessBox)
			Expect(err).To(BeNil())

			// Directly force the persisted metadata to the maximum
			// version, exactly as it would look right before the one
			// remaining increment would overflow.
			metadata.Version = ^uint64(0)
			protected, err := protectDatastoreObject(
				metadataObjectType,
				accessBox.MetadataUUID,
				metadata,
				accessBox.MetadataEncKey,
				accessBox.MetadataMACKey,
			)
			Expect(err).To(BeNil())
			userlib.DatastoreSet(accessBox.MetadataUUID, protected)

			err = alice.AppendToFile("file1.txt", []byte("B"))
			Expect(err).ToNot(BeNil())

			// The stored metadata must be unchanged: no partial/wrapped
			// write should have occurred.
			reloaded := getOwnerMetadata(alice, "file1.txt")
			Expect(reloaded.Version).To(Equal(^uint64(0)))
		})

		Specify("J: Version round-trips exactly through the normal protect/load datastore path", func() {
			alice, err := InitUser("alice", "password")
			Expect(err).To(BeNil())

			Expect(alice.StoreFile("file1.txt", []byte("A"))).To(BeNil())
			Expect(alice.AppendToFile("file1.txt", []byte("B"))).To(BeNil())

			_, accessBox, err := resolveFile(alice, "file1.txt")
			Expect(err).To(BeNil())

			var reloaded Metadata
			err = loadDatastoreObject(
				metadataObjectType,
				accessBox.MetadataUUID,
				accessBox.MetadataEncKey,
				accessBox.MetadataMACKey,
				&reloaded,
			)
			Expect(err).To(BeNil())
			Expect(reloaded.Version).To(Equal(uint64(2)))
		})
	})

	Describe("Phase 3: nextMetadataVersion helper", func() {
		Specify("increments normally", func() {
			next, err := nextMetadataVersion(1)
			Expect(err).To(BeNil())
			Expect(next).To(Equal(uint64(2)))

			next, err = nextMetadataVersion(0)
			Expect(err).To(BeNil())
			Expect(next).To(Equal(uint64(1)))
		})

		Specify("rejects overflow with an explicit error rather than wrapping", func() {
			next, err := nextMetadataVersion(^uint64(0))
			Expect(err).ToNot(BeNil())
			Expect(next).To(Equal(uint64(0)))
		})
	})
})

var _ = Describe("Phase 4.5: hardening", func() {

	BeforeEach(func() {
		userlib.DatastoreClear()
		userlib.KeystoreClear()
	})

	Specify("A1: ambiguous-looking (username, filename) pairs never collide in namespace identity", func() {
		// "a-b" + "c" and "a" + "b-c" both concatenate (with a "-"
		// delimiter) to "a-b-c" -- the exact collision the old encoding
		// was vulnerable to.
		Expect(canonicalNamespaceIdentity("a-b", "c")).
			ToNot(Equal(canonicalNamespaceIdentity("a", "b-c")))

		nameUUID1, err := getNameSpaceEntryUUID("a-b", "c")
		Expect(err).To(BeNil())
		nameUUID2, err := getNameSpaceEntryUUID("a", "b-c")
		Expect(err).To(BeNil())
		Expect(nameUUID1).ToNot(Equal(nameUUID2))

		resource1, err := namespaceResourceID("a-b", "c")
		Expect(err).To(BeNil())
		resource2, err := namespaceResourceID("a", "b-c")
		Expect(err).To(BeNil())
		Expect(resource1).ToNot(Equal(resource2))

		// getNameSpaceEntryUUID (datastore identity) and
		// namespaceResourceID (lock identity) must never diverge for the
		// same (username, filename) pair.
		selfResource, err := namespaceResourceID("alice", "file1.txt")
		Expect(err).To(BeNil())
		selfUUID, err := getNameSpaceEntryUUID("alice", "file1.txt")
		Expect(err).To(BeNil())
		Expect(selfResource.Key).To(Equal(selfUUID.String()))
	})

	Specify("A1: this encoding fix does not break ordinary end-to-end StoreFile/LoadFile behavior", func() {
		alice, err := InitUser("alice", "password")
		Expect(err).To(BeNil())
		Expect(alice.StoreFile("a-b", []byte("for-c"))).To(BeNil())
		Expect(alice.StoreFile("b-c", []byte("for-a-prefix"))).To(BeNil())

		content1, err := alice.LoadFile("a-b")
		Expect(err).To(BeNil())
		Expect(string(content1)).To(Equal("for-c"))

		content2, err := alice.LoadFile("b-c")
		Expect(err).To(BeNil())
		Expect(string(content2)).To(Equal("for-a-prefix"))
	})

	Specify("A2: TxnID allocation is permanently exhausted, never recycled, once the counter reaches MaxUint64", func() {
		saved := txnIDCounter.Load()
		defer txnIDCounter.Store(saved)

		txnIDCounter.Store(^uint64(0) - 1)

		id1, err := allocateTxnID()
		Expect(err).To(BeNil())
		Expect(uint64(id1)).To(Equal(^uint64(0)))

		// Every subsequent call must fail -- permanently, not just the one
		// call that happened to observe the boundary. If allocateTxnID
		// merely detected a single wraparound (via a bare Add(1)), later
		// calls here would silently recycle small IDs (1, 2, 3, ...)
		// again.
		for i := 0; i < 5; i++ {
			id, err := allocateTxnID()
			Expect(err).ToNot(BeNil())
			Expect(uint64(id)).To(Equal(uint64(0)))
		}
	})

	Specify("A3: no production code path calls userlib.Datastore*/Keystore* directly -- StoreFile/LoadFile still work purely through the latch wrappers", func() {
		// This is an executable proxy for the audit: if any production
		// call site had been missed and were somehow broken by routing
		// through the wrong path, ordinary end-to-end usage would fail.
		// The audit itself (grep across client/*.go for direct
		// userlib.Datastore*/Keystore* calls outside the five wrapper
		// bodies) is reported in the Phase 4.5 completion notes.
		alice, err := InitUser("alice", "password")
		Expect(err).To(BeNil())
		Expect(alice.StoreFile("file1.txt", []byte("hello"))).To(BeNil())
		content, err := alice.LoadFile("file1.txt")
		Expect(err).To(BeNil())
		Expect(string(content)).To(Equal("hello"))
	})

	Specify("A5: concurrencyTestHook get/set is race-safe under concurrent readers while being set", func() {
		// Not a proof of absence of races (that's go test -race's job),
		// but confirms the atomic.Pointer-based accessor behaves
		// correctly under concurrent Store/Load rather than panicking or
		// deadlocking.
		var wg sync.WaitGroup
		stop := make(chan struct{})

		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					fireConcurrencyTestHook("probe")
				}
			}
		}()

		for i := 0; i < 100; i++ {
			setConcurrencyTestHook(func(tag string) {})
		}
		setConcurrencyTestHook(nil)

		close(stop)
		wg.Wait()
	})
})
