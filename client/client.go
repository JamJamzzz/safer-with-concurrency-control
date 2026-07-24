package client

// CS 161 Project 2

// Only the following imports are allowed! ANY additional imports
// may break the autograder!
// - bytes
// - encoding/hex
// - encoding/json
// - errors
// - fmt
// - github.com/cs161-staff/project2-userlib
// - github.com/google/uuid
// - strconv
// - strings

import (
	"encoding/json"

	userlib "github.com/cs161-staff/project2-userlib"
	"github.com/google/uuid"

	// hex.EncodeToString(...) is useful for converting []byte to string

	// Useful for string manipulation
	"strings"

	// Useful for formatting strings (e.g. `fmt.Sprintf`).
	"fmt"

	// Useful for creating new error messages to return using errors.New("...")
	"errors"

	// Optional.
	_ "strconv"

	"encoding/hex"
)

// This serves two purposes: it shows you a few useful primitives,
// and suppresses warnings for imports not being used. It can be
// safely deleted!
func someUsefulThings() {

	// Creates a random UUID.
	randomUUID := uuid.New()

	// Prints the UUID as a string. %v prints the value in a default format.
	// See https://pkg.go.dev/fmt#hdr-Printing for all Golang format string flags.
	userlib.DebugMsg("Random UUID: %v", randomUUID.String())

	// Creates a UUID deterministically, from a sequence of bytes.
	hash := userlib.Hash([]byte("user-structs/alice"))
	deterministicUUID, err := uuid.FromBytes(hash[:16])
	if err != nil {
		// Normally, we would `return err` here. But, since this function doesn't return anything,
		// we can just panic to terminate execution. ALWAYS, ALWAYS, ALWAYS check for errors! Your
		// code should have hundreds of "if err != nil { return err }" statements by the end of this
		// project. You probably want to avoid using panic statements in your own code.
		panic(errors.New("An error occurred while generating a UUID: " + err.Error()))
	}
	userlib.DebugMsg("Deterministic UUID: %v", deterministicUUID.String())

	// Declares a Course struct type, creates an instance of it, and marshals it into JSON.
	type Course struct {
		name      string
		professor []byte
	}

	course := Course{"CS 161", []byte("Nicholas Weaver")}
	courseBytes, err := json.Marshal(course)
	if err != nil {
		panic(err)
	}

	userlib.DebugMsg("Struct: %v", course)
	userlib.DebugMsg("JSON Data: %v", courseBytes)

	// Generate a random private/public keypair.
	// The "_" indicates that we don't check for the error case here.
	var pk userlib.PKEEncKey
	var sk userlib.PKEDecKey
	pk, sk, _ = userlib.PKEKeyGen()
	userlib.DebugMsg("PKE Key Pair: (%v, %v)", pk, sk)

	// Here's an example of how to use HBKDF to generate a new key from an input key.
	// Tip: generate a new key everywhere you possibly can! It's easier to generate new keys on the fly
	// instead of trying to think about all of the ways a key reuse attack could be performed. It's also easier to
	// store one key and derive multiple keys from that one key, rather than
	originalKey := userlib.RandomBytes(16)
	derivedKey, err := userlib.HashKDF(originalKey, []byte("mac-key"))
	if err != nil {
		panic(err)
	}
	userlib.DebugMsg("Original Key: %v", originalKey)
	userlib.DebugMsg("Derived Key: %v", derivedKey)

	// A couple of tips on converting between string and []byte:
	// To convert from string to []byte, use []byte("some-string-here")
	// To convert from []byte to string for debugging, use fmt.Sprintf("hello world: %s", some_byte_arr).
	// To convert from []byte to string for use in a hashmap, use hex.EncodeToString(some_byte_arr).
	// When frequently converting between []byte and string, just marshal and unmarshal the data.
	//
	// Read more: https://go.dev/blog/strings

	// Here's an example of string interpolation!
	_ = fmt.Sprintf("%s_%d", "file", 1)
}

//Constant variables
const symmetricKeySize = 16

// This is the type definition for the User struct.
// A Go struct is like a Python or Java class - it can have attributes
// (e.g. like the Username attribute) and methods (e.g. like the StoreFile method below).
type User struct {
	Username      string
	NamespaceRoot []byte
	PKEPrivate    userlib.PKEDecKey //Used to decrypt the ciphertext
	SignPrivate   userlib.DSSignKey //Used for sign digital signature
	// You can add other attributes here if you want! But note that in order for attributes to
	// be included when this struct is serialized to/from JSON, they must be capitalized.
	// On the flipside, if you have an attribute that you want to be able to access from
	// this struct's methods, but you DON'T want that value to be included in the serialized value
	// of this struct that's stored in datastore, then you can use a "private" variable (e.g. one that
	// begins with a lowercase letter).
}

type Account struct {
	UsernameHash  []byte
	NamespaceRoot []byte
	PKEPrivate    userlib.PKEDecKey
	SignPrivate   userlib.DSSignKey
}

//Wrap the account and stored in an envelope
type AuthenticatedEnvelope struct {
	Ciphertext []byte
	MAC        []byte
}

//Basic Helper Methods:

/**
Helper Method for InitUser
 **/
func getUserUUID(username string) (uuid.UUID, error) {
	userHash := userlib.Hash([]byte("user-record-" + username))
	//Casting from the bytes to uuid
	return uuid.FromBytes(userHash[:16])
}

//key generation:
//Generate the public key
func publicKeyName(domain string, username string) string {
	nameHash := userlib.Hash([]byte(domain + ":" + username))
	return hex.EncodeToString(nameHash[:16])
}

//Used to enc the account
func getPKEKeyName(username string) string {
	return publicKeyName("pke-public-key", username)
}

//Used to verify the d.s.
func getVerifyKeyName(username string) string {
	return publicKeyName("signature-verify-key", username)
}

//To check whether this account is taken or not
func isUsernameTaken(username string, accountUUID uuid.UUID) bool {
	_, accountExists := userlib.DatastoreGet(accountUUID)
	_, pkeExists := userlib.KeystoreGet(getPKEKeyName(username))
	_, verifyExists := userlib.KeystoreGet(getVerifyKeyName(username))

	return accountExists || pkeExists || verifyExists
}

//derive the secret keys for enc and mac
func deriveAccountKey(username string, password string, accountUUID uuid.UUID) (
	encKey []byte, macKey []byte, err error,
) {
	salt := userlib.Hash([]byte("salt-" + username))

	passwordRoot := userlib.Argon2Key([]byte(password), salt, 16)

	encContent := []byte("account-enc" + accountUUID.String())
	enc, err := userlib.HashKDF(passwordRoot, encContent)

	if err != nil {
		return nil, nil, err
	}

	macContent := []byte("account-mac" + accountUUID.String())
	mac, err := userlib.HashKDF(passwordRoot, macContent)

	if err != nil {
		return nil, nil, err
	}

	encKey = enc[:16]
	macKey = mac[:16]

	return encKey, macKey, nil
}

//Construction of mac meesage
func accountMACMessage(accountUUID uuid.UUID, ciphertext []byte) []byte {
	prefix := []byte(
		"authenticated-account:" + accountUUID.String() + ":",
	)
	message := make([]byte, 0, len(prefix)+len(ciphertext))
	message = append(message, prefix...)
	message = append(message, ciphertext...)
	return message
}

//Encrypt then MAC
func emacAccount(
	account Account, accountUUID uuid.UUID, encKey []byte, macKey []byte,
) ([]byte, error) {
	if len(encKey) != 16 || len(macKey) != 16 {
		return nil, errors.New("invalid account key length")
	}

	accountBytes, err := json.Marshal(account)
	if err != nil {
		return nil, err
	}
	//initialized vector
	iv := userlib.RandomBytes(16)
	ciphertext := userlib.SymEnc(encKey, iv, accountBytes)

	macMessage := accountMACMessage(accountUUID, ciphertext)
	mac, err := userlib.HMACEval(macKey, macMessage)

	if err != nil {
		return nil, err
	}

	envelope := AuthenticatedEnvelope{
		Ciphertext: ciphertext,
		MAC:        mac,
	}

	//JSON marshal
	envelopeBytes, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}

	return envelopeBytes, nil
}

// NOTE: The following methods have toy (insecure!) implementations.

func InitUser(username string, password string) (userdataptr *User, err error) {
	if username == "" {
		return nil, errors.New("username cannot be empty")
	}

	accountUUID, err := getUserUUID(username)

	if err != nil {
		return nil, err
	}

	if isUsernameTaken(username, accountUUID) {
		return nil, errors.New("username already exits")
	}

	//For receiveing enc invites
	pkePulic, pkePrivate, err := userlib.PKEKeyGen()

	if err != nil {
		return nil, err
	}

	//Sign ds for the invites
	signPrivate, verifyPublic, err := userlib.DSKeyGen()
	if err != nil {
		return nil, err
	}

	namespaceRoot := userlib.RandomBytes(16)

	//Create the account for the user
	account := Account{
		UsernameHash:  userlib.Hash([]byte(username)),
		NamespaceRoot: namespaceRoot,
		PKEPrivate:    pkePrivate,
		SignPrivate:   signPrivate,
	}

	accountEncKey, accountMACKey, err := deriveAccountKey(
		username,
		password,
		accountUUID,
	)

	if err != nil {
		return nil, err
	}

	encAccount, err := emacAccount(
		account,
		accountUUID,
		accountEncKey,
		accountMACKey,
	)

	if err != nil {
		return nil, err
	}

	//Store the key for dec
	err = userlib.KeystoreSet(
		getPKEKeyName(username),
		pkePulic,
	)

	if err != nil {
		return nil, err
	}

	//Store the key for verify the d.s.
	err = userlib.KeystoreSet(
		getVerifyKeyName(username),
		verifyPublic,
	)

	if err != nil {
		return nil, err
	}

	//Store the emac account to datastore
	userlib.DatastoreSet(accountUUID, encAccount)

	userdata := User{
		Username:      username,
		NamespaceRoot: namespaceRoot,
		PKEPrivate:    pkePrivate,
		SignPrivate:   signPrivate,
	}

	return &userdata, nil
}

func GetUser(username string, password string) (userdataptr *User, err error) {
	var userdata User
	userdataptr = &userdata
	return userdataptr, nil
}

func (userdata *User) StoreFile(filename string, content []byte) (err error) {
	storageKey, err := uuid.FromBytes(userlib.Hash([]byte(filename + userdata.Username))[:16])
	if err != nil {
		return err
	}
	contentBytes, err := json.Marshal(content)
	if err != nil {
		return err
	}
	userlib.DatastoreSet(storageKey, contentBytes)
	return
}

func (userdata *User) AppendToFile(filename string, content []byte) error {
	return nil
}

func (userdata *User) LoadFile(filename string) (content []byte, err error) {
	storageKey, err := uuid.FromBytes(userlib.Hash([]byte(filename + userdata.Username))[:16])
	if err != nil {
		return nil, err
	}
	dataJSON, ok := userlib.DatastoreGet(storageKey)
	if !ok {
		return nil, errors.New(strings.ToTitle("file not found"))
	}
	err = json.Unmarshal(dataJSON, &content)
	return content, err
}

func (userdata *User) CreateInvitation(filename string, recipientUsername string) (
	invitationPtr uuid.UUID, err error) {
	return
}

func (userdata *User) AcceptInvitation(senderUsername string, invitationPtr uuid.UUID, filename string) error {
	return nil
}

func (userdata *User) RevokeAccess(filename string, recipientUsername string) error {
	return nil
}
