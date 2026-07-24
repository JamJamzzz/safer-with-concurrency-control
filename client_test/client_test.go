package client_test

// You MUST NOT change these default imports.  ANY additional imports may
// break the autograder and everyone will be sad.

import (
	// Some imports use an underscore to prevent the compiler from complaining
	// about unused imports.
	_ "encoding/hex"
	_ "errors"
	_ "strconv"
	_ "strings"
	"testing"

	_ "github.com/google/uuid"

	// A "dot" import is used here so that the functions in the ginko and gomega
	// modules can be used without an identifier. For example, Describe() and
	// Expect() instead of ginko.Describe() and gomega.Expect().
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	userlib "github.com/cs161-staff/project2-userlib"

	"github.com/cs161-staff/project2-starter-code/client"
)

func TestSetupAndExecution(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Client Tests")
}

// ================================================
// Global Variables (feel free to add more!)
// ================================================
const defaultPassword = "password"
const emptyString = ""
const contentOne = "Bitcoin is Nick's favorite "
const contentTwo = "digital "
const contentThree = "cryptocurrency!"

// ================================================
// Describe(...) blocks help you organize your tests
// into functional categories. They can be nested into
// a tree-like structure.
// ================================================

var _ = Describe("Client Tests", func() {

	// A few user declarations that may be used for testing. Remember to initialize these before you
	// attempt to use them!
	var alice *client.User
	var bob *client.User
	var charles *client.User
	// var doris *client.User
	// var eve *client.User
	// var frank *client.User
	// var grace *client.User
	// var horace *client.User
	// var ira *client.User

	// These declarations may be useful for multi-session testing.
	var alicePhone *client.User
	var aliceLaptop *client.User
	var aliceDesktop *client.User

	var err error

	// A bunch of filenames that may be useful.
	aliceFile := "aliceFile.txt"
	bobFile := "bobFile.txt"
	charlesFile := "charlesFile.txt"
	// dorisFile := "dorisFile.txt"
	// eveFile := "eveFile.txt"
	// frankFile := "frankFile.txt"
	// graceFile := "graceFile.txt"
	// horaceFile := "horaceFile.txt"
	// iraFile := "iraFile.txt"

	BeforeEach(func() {
		// This runs before each test within this Describe block (including nested tests).
		// Here, we reset the state of Datastore and Keystore so that tests do not interfere with each other.
		// We also initialize
		userlib.DatastoreClear()
		userlib.KeystoreClear()
	})

	Describe("Basic Tests", func() {

		Specify("Basic Test: Testing InitUser/GetUser on a single user.", func() {
			userlib.DebugMsg("Initializing user Alice.")
			alice, err = client.InitUser("alice", defaultPassword)
			Expect(err).To(BeNil())

			userlib.DebugMsg("Getting user Alice.")
			aliceLaptop, err = client.GetUser("alice", defaultPassword)
			Expect(err).To(BeNil())
		})

		Specify("Basic Test: Testing Single User Store/Load/Append.", func() {
			userlib.DebugMsg("Initializing user Alice.")
			alice, err = client.InitUser("alice", defaultPassword)
			Expect(err).To(BeNil())

			userlib.DebugMsg("Storing file data: %s", contentOne)
			err = alice.StoreFile(aliceFile, []byte(contentOne))
			Expect(err).To(BeNil())

			userlib.DebugMsg("Appending file data: %s", contentTwo)
			err = alice.AppendToFile(aliceFile, []byte(contentTwo))
			Expect(err).To(BeNil())

			userlib.DebugMsg("Appending file data: %s", contentThree)
			err = alice.AppendToFile(aliceFile, []byte(contentThree))
			Expect(err).To(BeNil())

			userlib.DebugMsg("Loading file...")
			data, err := alice.LoadFile(aliceFile)
			Expect(err).To(BeNil())
			Expect(data).To(Equal([]byte(contentOne + contentTwo + contentThree)))
		})

		Specify("Basic Test: Testing Create/Accept Invite Functionality with multiple users and multiple instances.", func() {
			userlib.DebugMsg("Initializing users Alice (aliceDesktop) and Bob.")
			aliceDesktop, err = client.InitUser("alice", defaultPassword)
			Expect(err).To(BeNil())

			bob, err = client.InitUser("bob", defaultPassword)
			Expect(err).To(BeNil())

			userlib.DebugMsg("Getting second instance of Alice - aliceLaptop")
			aliceLaptop, err = client.GetUser("alice", defaultPassword)
			Expect(err).To(BeNil())

			userlib.DebugMsg("aliceDesktop storing file %s with content: %s", aliceFile, contentOne)
			err = aliceDesktop.StoreFile(aliceFile, []byte(contentOne))
			Expect(err).To(BeNil())

			userlib.DebugMsg("aliceLaptop creating invite for Bob.")
			invite, err := aliceLaptop.CreateInvitation(aliceFile, "bob")
			Expect(err).To(BeNil())

			userlib.DebugMsg("Bob accepting invite from Alice under filename %s.", bobFile)
			err = bob.AcceptInvitation("alice", invite, bobFile)
			Expect(err).To(BeNil())

			userlib.DebugMsg("Bob appending to file %s, content: %s", bobFile, contentTwo)
			err = bob.AppendToFile(bobFile, []byte(contentTwo))
			Expect(err).To(BeNil())

			userlib.DebugMsg("aliceDesktop appending to file %s, content: %s", aliceFile, contentThree)
			err = aliceDesktop.AppendToFile(aliceFile, []byte(contentThree))
			Expect(err).To(BeNil())

			userlib.DebugMsg("Checking that aliceDesktop sees expected file data.")
			data, err := aliceDesktop.LoadFile(aliceFile)
			Expect(err).To(BeNil())
			Expect(data).To(Equal([]byte(contentOne + contentTwo + contentThree)))

			userlib.DebugMsg("Checking that aliceLaptop sees expected file data.")
			data, err = aliceLaptop.LoadFile(aliceFile)
			Expect(err).To(BeNil())
			Expect(data).To(Equal([]byte(contentOne + contentTwo + contentThree)))

			userlib.DebugMsg("Checking that Bob sees expected file data.")
			data, err = bob.LoadFile(bobFile)
			Expect(err).To(BeNil())
			Expect(data).To(Equal([]byte(contentOne + contentTwo + contentThree)))

			userlib.DebugMsg("Getting third instance of Alice - alicePhone.")
			alicePhone, err = client.GetUser("alice", defaultPassword)
			Expect(err).To(BeNil())

			userlib.DebugMsg("Checking that alicePhone sees Alice's changes.")
			data, err = alicePhone.LoadFile(aliceFile)
			Expect(err).To(BeNil())
			Expect(data).To(Equal([]byte(contentOne + contentTwo + contentThree)))
		})

		Specify("Basic Test: Testing Revoke Functionality", func() {
			userlib.DebugMsg("Initializing users Alice, Bob, and Charlie.")
			alice, err = client.InitUser("alice", defaultPassword)
			Expect(err).To(BeNil())

			bob, err = client.InitUser("bob", defaultPassword)
			Expect(err).To(BeNil())

			charles, err = client.InitUser("charles", defaultPassword)
			Expect(err).To(BeNil())

			userlib.DebugMsg("Alice storing file %s with content: %s", aliceFile, contentOne)
			alice.StoreFile(aliceFile, []byte(contentOne))

			userlib.DebugMsg("Alice creating invite for Bob for file %s, and Bob accepting invite under name %s.", aliceFile, bobFile)

			invite, err := alice.CreateInvitation(aliceFile, "bob")
			Expect(err).To(BeNil())

			err = bob.AcceptInvitation("alice", invite, bobFile)
			Expect(err).To(BeNil())

			userlib.DebugMsg("Checking that Alice can still load the file.")
			data, err := alice.LoadFile(aliceFile)
			Expect(err).To(BeNil())
			Expect(data).To(Equal([]byte(contentOne)))

			userlib.DebugMsg("Checking that Bob can load the file.")
			data, err = bob.LoadFile(bobFile)
			Expect(err).To(BeNil())
			Expect(data).To(Equal([]byte(contentOne)))

			userlib.DebugMsg("Bob creating invite for Charles for file %s, and Charlie accepting invite under name %s.", bobFile, charlesFile)
			invite, err = bob.CreateInvitation(bobFile, "charles")
			Expect(err).To(BeNil())

			err = charles.AcceptInvitation("bob", invite, charlesFile)
			Expect(err).To(BeNil())

			userlib.DebugMsg("Checking that Bob can load the file.")
			data, err = bob.LoadFile(bobFile)
			Expect(err).To(BeNil())
			Expect(data).To(Equal([]byte(contentOne)))

			userlib.DebugMsg("Checking that Charles can load the file.")
			data, err = charles.LoadFile(charlesFile)
			Expect(err).To(BeNil())
			Expect(data).To(Equal([]byte(contentOne)))

			userlib.DebugMsg("Alice revoking Bob's access from %s.", aliceFile)
			err = alice.RevokeAccess(aliceFile, "bob")
			Expect(err).To(BeNil())

			userlib.DebugMsg("Checking that Alice can still load the file.")
			data, err = alice.LoadFile(aliceFile)
			Expect(err).To(BeNil())
			Expect(data).To(Equal([]byte(contentOne)))

			userlib.DebugMsg("Checking that Bob/Charles lost access to the file.")
			_, err = bob.LoadFile(bobFile)
			Expect(err).ToNot(BeNil())

			_, err = charles.LoadFile(charlesFile)
			Expect(err).ToNot(BeNil())

			userlib.DebugMsg("Checking that the revoked users cannot append to the file.")
			err = bob.AppendToFile(bobFile, []byte(contentTwo))
			Expect(err).ToNot(BeNil())

			err = charles.AppendToFile(charlesFile, []byte(contentTwo))
			Expect(err).ToNot(BeNil())
		})

	})

	Describe("InitUser Integration Tests", func() {
    	Specify("successfully initializes a normal user", func() {
        	alice, err := client.InitUser("alice", "password")

        	Expect(err).To(BeNil())
        	Expect(alice).ToNot(BeNil())
    	})

    	Specify("rejects an empty username", func() {
        	emptyUser, err := client.InitUser("", "password")

        	Expect(err).ToNot(BeNil())
        	Expect(emptyUser).To(BeNil())
    	})

    	Specify("accepts an empty password", func() {
        	alice, err := client.InitUser("alice", "")

        	Expect(err).To(BeNil())
        	Expect(alice).ToNot(BeNil())
    	})

    	Specify("rejects duplicate usernames with the same password", func() {
        	firstAlice, err := client.InitUser("alice", "password")
        	Expect(err).To(BeNil())
        	Expect(firstAlice).ToNot(BeNil())

        	secondAlice, err := client.InitUser("alice", "password")

        	Expect(err).ToNot(BeNil())
        	Expect(secondAlice).To(BeNil())
    	})

    	Specify("rejects duplicate usernames with different passwords", func() {
        	firstAlice, err := client.InitUser("alice", "first-password")
        	Expect(err).To(BeNil())
        	Expect(firstAlice).ToNot(BeNil())

        	secondAlice, err := client.InitUser("alice", "different-password")

        	Expect(err).ToNot(BeNil())
        	Expect(secondAlice).To(BeNil())
    	})

    	Specify("allows different users to use the same password", func() {
        	alice, err := client.InitUser("alice", "shared-password")
        	Expect(err).To(BeNil())
        	Expect(alice).ToNot(BeNil())

        	bob, err := client.InitUser("bob", "shared-password")
        	Expect(err).To(BeNil())
        	Expect(bob).ToNot(BeNil())
    	})

    	Specify("treats usernames as case-sensitive", func() {
        	lowercase, err := client.InitUser("alice", "password")
        	Expect(err).To(BeNil())
        	Expect(lowercase).ToNot(BeNil())

        	uppercase, err := client.InitUser("Alice", "password")
        	Expect(err).To(BeNil())
        	Expect(uppercase).ToNot(BeNil())
    	})

    	Specify("accepts nonempty usernames containing punctuation", func() {
        	user, err := client.InitUser(
            	"alice-test@example.com",
            	"password",
        	)

        	Expect(err).To(BeNil())
        	Expect(user).ToNot(BeNil())
    	})

    	Specify("accepts a long password", func() {
        	user, err := client.InitUser(
            	"alice",
            	"this-is-a-long-password-with-many-characters-1234567890",
        	)

        	Expect(err).To(BeNil())
        	Expect(user).ToNot(BeNil())
    	})
	})

	Describe("GetUser Integration Tests", func() {
    	Specify("logs in with the correct username and password", func() {
        	initializedUser, err := client.InitUser("alice", "password")
        	Expect(err).To(BeNil())
        	Expect(initializedUser).ToNot(BeNil())

        	loggedInUser, err := client.GetUser("alice", "password")

        	Expect(err).To(BeNil())
        	Expect(loggedInUser).ToNot(BeNil())
    	})

    	Specify("rejects a nonexistent username", func() {
        	user, err := client.GetUser("alice", "password")

        	Expect(err).ToNot(BeNil())
        	Expect(user).To(BeNil())
    	})

    	Specify("rejects an empty username", func() {
        	user, err := client.GetUser("", "password")

        	Expect(err).ToNot(BeNil())
        	Expect(user).To(BeNil())
    	})

    	Specify("rejects an incorrect password", func() {
        	_, err := client.InitUser("alice", "correct-password")
        	Expect(err).To(BeNil())

        	user, err := client.GetUser("alice", "wrong-password")

       	 Expect(err).ToNot(BeNil())
        	Expect(user).To(BeNil())
    	})

    	Specify("accepts the correct empty password", func() {
        	_, err := client.InitUser("alice", "")
        	Expect(err).To(BeNil())

        	user, err := client.GetUser("alice", "")

        	Expect(err).To(BeNil())
        	Expect(user).ToNot(BeNil())
   		})

    	Specify("rejects a nonempty password for an empty-password account", func() {
        	_, err := client.InitUser("alice", "")
        	Expect(err).To(BeNil())

        	user, err := client.GetUser("alice", "password")

        	Expect(err).ToNot(BeNil())
        	Expect(user).To(BeNil())
    	})

    	Specify("treats passwords as exact and case-sensitive", func() {
        	_, err := client.InitUser("alice", "Password")
        	Expect(err).To(BeNil())

        	lowercaseAttempt, err := client.GetUser("alice", "password")
        	Expect(err).ToNot(BeNil())
        	Expect(lowercaseAttempt).To(BeNil())

        	correctAttempt, err := client.GetUser("alice", "Password")
        	Expect(err).To(BeNil())
        	Expect(correctAttempt).ToNot(BeNil())
    	})

    	Specify("treats whitespace as part of the password", func() {
        	_, err := client.InitUser("alice", " password ")
        	Expect(err).To(BeNil())

        	noWhitespace, err := client.GetUser("alice", "password")
        	Expect(err).ToNot(BeNil())
        	Expect(noWhitespace).To(BeNil())

        	exactPassword, err := client.GetUser("alice", " password ")
        	Expect(err).To(BeNil())
        	Expect(exactPassword).ToNot(BeNil())
    	})

    	Specify("treats usernames as case-sensitive", func() {
       		_, err := client.InitUser("Alice", "password")
        	Expect(err).To(BeNil())

        	wrongCase, err := client.GetUser("alice", "password")
        	Expect(err).ToNot(BeNil())
        	Expect(wrongCase).To(BeNil())

        	correctCase, err := client.GetUser("Alice", "password")
       	 	Expect(err).To(BeNil())
        	Expect(correctCase).ToNot(BeNil())
    	})

    	Specify("supports punctuation in usernames", func() {
        	username := "alice-test@example.com"

        	_, err := client.InitUser(username, "password")
        	Expect(err).To(BeNil())

        	user, err := client.GetUser(username, "password")

        	Expect(err).To(BeNil())
        	Expect(user).ToNot(BeNil())
    	})

    	Specify("supports multiple sessions for the same account", func() {
        	firstSession, err := client.InitUser("alice", "password")
        	Expect(err).To(BeNil())
        	Expect(firstSession).ToNot(BeNil())

        	secondSession, err := client.GetUser("alice", "password")
        	Expect(err).To(BeNil())
        	Expect(secondSession).ToNot(BeNil())

        	thirdSession, err := client.GetUser("alice", "password")
        	Expect(err).To(BeNil())
        	Expect(thirdSession).ToNot(BeNil())
    	})

    	Specify("supports different users with the same password", func() {
        	_, err := client.InitUser("alice", "shared-password")
        	Expect(err).To(BeNil())

        	_, err = client.InitUser("bob", "shared-password")
        	Expect(err).To(BeNil())

        	aliceSession, err := client.GetUser("alice", "shared-password")
        	Expect(err).To(BeNil())
        	Expect(aliceSession).ToNot(BeNil())

        	bobSession, err := client.GetUser("bob", "shared-password")
        	Expect(err).To(BeNil())
        	Expect(bobSession).ToNot(BeNil())
    	})

   	 	Specify("wrong-password attempts do not damage the account", func() {
        	_, err := client.InitUser("alice", "correct-password")
        	Expect(err).To(BeNil())

        	failedSession, err := client.GetUser("alice", "wrong-password")
        	Expect(err).ToNot(BeNil())
        	Expect(failedSession).To(BeNil())

        	validSession, err := client.GetUser("alice", "correct-password")
        	Expect(err).To(BeNil())
        	Expect(validSession).ToNot(BeNil())
    	})

    	Specify("duplicate registration failure does not damage login", func() {
        	_, err := client.InitUser("alice", "correct-password")
        	Expect(err).To(BeNil())

        	duplicate, err := client.InitUser("alice", "other-password")
        	Expect(err).ToNot(BeNil())
        	Expect(duplicate).To(BeNil())

        	originalUser, err := client.GetUser("alice", "correct-password")
        	Expect(err).To(BeNil())
        	Expect(originalUser).ToNot(BeNil())
    	})

		Specify("rejects truncated persistent account data", func() {
    		_, err := client.InitUser("alice", "password")
    		Expect(err).To(BeNil())

    		truncatedSomething := false

    		for objectUUID, objectData := range userlib.DatastoreGetMap() {
        		if len(objectData) < 2 {
            		continue
        		}

        		truncatedData := append(
           			[]byte(nil),
            		objectData[:len(objectData)/2]...,
        		)

        		userlib.DatastoreSet(objectUUID, truncatedData)
        		truncatedSomething = true
    		}

    		Expect(truncatedSomething).To(BeTrue())

    		user, err := client.GetUser("alice", "password")

    		Expect(err).ToNot(BeNil())
    		Expect(user).To(BeNil())
		})

		Specify("rejects swapping encrypted Accounts between users", func() {
    		_, err := client.InitUser("alice", "alice-password")
    		Expect(err).To(BeNil())

    		_, err = client.InitUser("bob", "bob-password")
    		Expect(err).To(BeNil())

    		var objectUUIDs []userlib.UUID
    		var objectData [][]byte

    		for objectUUID, data := range userlib.DatastoreGetMap() {
        		objectUUIDs = append(objectUUIDs, objectUUID)
        		objectData = append(
            		objectData,
            		append([]byte(nil), data...),
        		)
    		}

    		//Only one permanent user account 
    		Expect(objectUUIDs).To(HaveLen(2))
    		Expect(objectData).To(HaveLen(2))

    		userlib.DatastoreSet(objectUUIDs[0], objectData[1])
    		userlib.DatastoreSet(objectUUIDs[1], objectData[0])

    		aliceSession, aliceErr := client.GetUser(
        		"alice",
        		"alice-password",
    		)
    		Expect(aliceErr).ToNot(BeNil())
    		Expect(aliceSession).To(BeNil())

    		bobSession, bobErr := client.GetUser(
        		"bob",
        		"bob-password",
    		)
    		Expect(bobErr).ToNot(BeNil())
    		Expect(bobSession).To(BeNil())
		})
	})
})
