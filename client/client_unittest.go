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
})
