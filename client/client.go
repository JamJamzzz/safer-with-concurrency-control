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
//
// SAFER-CC extension note: this project has been forked into SAFER-CC, an
// independent concurrency-control extension (see docs/concurrency-control.md).
// The import restriction above was specific to the original CS161 autograder
// and no longer applies here; SAFER-CC additionally uses the Go standard
// library ("sync/atomic") and its own generic client/lockmanager package.

import (
	"encoding/json"

	userlib "github.com/cs161-staff/project2-userlib"
	"github.com/google/uuid"

	"github.com/cs161-staff/project2-starter-code/client/lockmanager"

	// hex.EncodeToString(...) is useful for converting []byte to string

	// Useful for string manipulation
	_ "strings"

	// Useful for formatting strings (e.g. `fmt.Sprintf`).
	"fmt"

	// Useful for creating new error messages to return using errors.New("...")
	"errors"

	"strconv"

	"encoding/hex"

	"sync"
	"sync/atomic"
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

// ---------------------------------------------------------------------
// Storage-layer thread safety.
//
// userlib's Datastore/Keystore are implemented as plain, unsynchronized Go
// maps (see project2-userlib's datastoreType/keystoreType). They were never
// designed for concurrent access from multiple goroutines -- and Go's
// runtime actively crashes the whole process ("fatal error: concurrent map
// read and map write") the moment two goroutines touch a map concurrently,
// independent of the race detector and independent of which logical keys
// they touch. Phase 4 is the first point in SAFER-CC where operations
// really do run concurrently against this shared storage, which surfaced
// exactly that crash during testing.
//
// This is a distinct concern from saferLockManager's strict 2PL: the
// LockManager coordinates SAFER's own logical resources (namespace
// entries, logical files) so that, e.g., two AppendToFile calls on
// DIFFERENT files never block each other. But even two operations on
// completely unrelated files still both end up calling into the very same
// underlying Go map, and that raw map access itself is not safe without
// its own, separate guard -- analogous to a real database engine's
// storage-layer buffer-pool latches being a different mechanism from its
// transaction manager's row/table locks. datastoreMu/keystoreMu below are
// that guard: a Mutex around every raw Datastore access and an RWMutex
// around Keystore accesses. DatastoreGet is NOT read-only: userlib v0.5.1
// increments its shared bandwidth counter even when reading distinct keys,
// so allowing concurrent Gets under RLock races on that counter. Hold the
// datastore latch exclusively for the single raw call (including its copy
// and accounting), not for the surrounding logical operation. These latches
// have no TxnID, no 2PL semantics, and no participation in
// saferLockManager whatsoever. Every production call site in this file
// goes through the datastoreGet/datastoreSet/datastoreDelete/
// keystoreGet/keystoreSet wrappers below instead of calling
// userlib.Datastore*/Keystore* directly.
var datastoreMu sync.Mutex
var keystoreMu sync.RWMutex

func datastoreGet(id uuid.UUID) ([]byte, bool) {
	datastoreMu.Lock()
	defer datastoreMu.Unlock()
	return userlib.DatastoreGet(id)
}

func datastoreSet(id uuid.UUID, value []byte) {
	datastoreMu.Lock()
	defer datastoreMu.Unlock()
	userlib.DatastoreSet(id, value)
}

func datastoreDelete(id uuid.UUID) {
	datastoreMu.Lock()
	defer datastoreMu.Unlock()
	userlib.DatastoreDelete(id)
}

func keystoreGet(name string) (userlib.PublicKeyType, bool) {
	keystoreMu.RLock()
	defer keystoreMu.RUnlock()
	return userlib.KeystoreGet(name)
}

func keystoreSet(name string, key userlib.PublicKeyType) error {
	keystoreMu.Lock()
	defer keystoreMu.Unlock()
	return userlib.KeystoreSet(name, key)
}

// ---------------------------------------------------------------------

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
	_, accountExists := datastoreGet(accountUUID)
	_, pkeExists := keystoreGet(getPKEKeyName(username))
	_, verifyExists := keystoreGet(getVerifyKeyName(username))

	return accountExists || pkeExists || verifyExists
}

//derive the secret keys for enc and mac
func deriveAccountKey(username string, password string, accountUUID uuid.UUID) (
	encKey []byte, macKey []byte, err error,
) {
	salt := userlib.Hash([]byte("password-salt-" + username))

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
	err = keystoreSet(
		getPKEKeyName(username),
		pkePulic,
	)

	if err != nil {
		return nil, err
	}

	//Store the key for verify the d.s.
	err = keystoreSet(
		getVerifyKeyName(username),
		verifyPublic,
	)

	if err != nil {
		return nil, err
	}

	//Store the emac account to datastore
	datastoreSet(accountUUID, encAccount)

	userdata := User{
		Username:      username,
		NamespaceRoot: namespaceRoot,
		PKEPrivate:    pkePrivate,
		SignPrivate:   signPrivate,
	}

	return &userdata, nil
}

 /** 
 Helper Method for GetUser
 **/
//MAC then decrypt the account then open the account
func openAccount(
	envelopeBytes []byte,
	accountUUID uuid.UUID,
	encKey []byte,
	macKey []byte,
) (Account, error) {
	var emptyAccount Account

	//Check the length
	if len(encKey) != symmetricKeySize {
		return emptyAccount, errors.New("invalid account encryption key")
	}

	if len(macKey) != symmetricKeySize {
        return emptyAccount, errors.New("invalid account MAC key")
    }

	var envelope AuthenticatedEnvelope
	err := json.Unmarshal(envelopeBytes, &envelope)
	//Checking whether the envelope is valid or not
	if err != nil {
        return emptyAccount, errors.New("invalid account envelope")
    }

	if len(envelope.MAC) != userlib.HashSizeBytes {
		return emptyAccount, errors.New("Invalid account MAC length")
	}

	//Get the expected mac based on the ciphertext
	expectedMAC, err := userlib.HMACEval(
		macKey,
		accountMACMessage(accountUUID, envelope.Ciphertext),
	)

	if err != nil {
		return emptyAccount, err
	}

	//If the mac failed, lowkey end of the world
	if !userlib.HMACEqual(expectedMAC, envelope.MAC) {
		return emptyAccount, errors.New("account authentication failed")
	}

	//Not for security check, but for too short c.t. that will panic the system
	if len(envelope.Ciphertext) < userlib.AESBlockSizeBytes {
		return emptyAccount, errors.New("account ciphertext is too short")
	}

	plaintext := userlib.SymDec(encKey, envelope.Ciphertext,)

	var account Account
	err = json.Unmarshal(plaintext, &account)
	if err != nil {
        return emptyAccount, errors.New("invalid account plaintext")
    }

    return account, nil
}

func GetUser(username string, password string) (userdataptr *User, err error) {
	if username == "" {
		return nil, errors.New("username cant be empty")
	}

	accountUUID, err := getUserUUID(username)
    if err != nil {
        return nil, err
    }

	//Get the envelope
	envelopeBytes, exists := datastoreGet(accountUUID)

	if !exists {
		return nil, errors.New("user account does not exits")
	}

	//Derive the account enc and mac key based on username and password and uuid
	accountEncKey, accountMACKey, err := deriveAccountKey(
		username,
		password,
		accountUUID,
	)

	if err != nil {
		return nil, err
	}

	account, err := openAccount(
		envelopeBytes,
		accountUUID,
		accountEncKey,
		accountMACKey,
	)

	if err != nil {
		return nil, err
	}

	userdata := User{
		Username: username,
		NamespaceRoot: account.NamespaceRoot,
		PKEPrivate: account.PKEPrivate,
		SignPrivate: account.SignPrivate,
	}

	return &userdata, nil
}

//Basic data structure for store file
//Local file
type NamespaceEntry struct {
	IsOwner bool
	FileID uuid.UUID

	//Point to accessbox
	AccessBoxUUID uuid.UUID
	AccessBoxEncKey []byte
	AccessBoxMACKey []byte

	//Owner's ablity to manage the accessbox structure
	AccessBoxStructureUUID uuid.UUID
	AccessBoxStructureEncKey []byte
	AccessBoxStructureMACKey []byte
}

type AccessBox struct {
	FileID uuid.UUID
	EpochID uuid.UUID
	
	MetadataUUID uuid.UUID
	MetadataEncKey []byte
	MetadataMACKey []byte

	FileRoot []byte

	AccessBoxStructureUUID uuid.UUID
	StatusUUID uuid.UUID
	OwnerVerifyKeyName string
}

type FileStatusBody struct {
	FileID uuid.UUID
	EpochID uuid.UUID
	CurrentAccessCommitment []byte //Hashed AccessBox
	OwnerVerifyKeyName string
}

type FileStatus struct {
	Body FileStatusBody
	Signature []byte
}

//Components of files
//
// EpochID and Version are intentionally independent counters:
//   - EpochID is the security/authorization generation. It changes only
//     when RevokeAccess rotates capabilities; it says nothing about
//     whether the file's logical bytes changed.
//   - Version is the logical file-content generation. It changes only
//     when the bytes a reader would see via LoadFile actually change
//     (new file, AppendToFile with non-empty content, or an overwriting
//     StoreFile). It is preserved unchanged across authorization-only
//     operations, including epoch rotation during RevokeAccess, because
//     re-encrypting the same content under a new epoch is a physical
//     migration, not a logical content mutation.
type Metadata struct {
	FileID uuid.UUID
	EpochID uuid.UUID
	Version uint64
	TailUUID uuid.UUID
	ChunkCount uint64
}

// nextMetadataVersion computes the version that should be published after a
// logical content mutation, given the version the mutation observed. It
// exists so that no call site increments Metadata.Version with a raw "++"
// (which would silently wrap around on overflow); every increment goes
// through this overflow-checked helper instead.
func nextMetadataVersion(current uint64) (uint64, error) {
	if current == ^uint64(0) {
		return 0, errors.New("file content version exhausted")
	}

	return current + 1, nil
}

// ---------------------------------------------------------------------
// Phase 4: strict two-phase-locking integration.
//
// saferLockManager is the single, process-wide, in-process lock manager
// used by every SAFER operation that participates in strict 2PL. It is NOT
// a distributed lock service: it coordinates goroutines within this one
// process that share the same in-memory userlib Datastore/Keystore. Every
// caller -- regardless of which *User it is acting on behalf of -- must
// route through this one instance. A LockManager instantiated per-User
// would let two different users' operations on the same shared FileID
// (e.g. an owner and a recipient) run completely uncoordinated with each
// other, which would defeat the purpose of file-level locking entirely.
//
// Phase 4 wires this LockManager into StoreFile, AppendToFile, and
// LoadFile only. CreateInvitation, AcceptInvitation, and RevokeAccess are
// intentionally left unintegrated until Phase 5 -- they still exhibit the
// races documented in the Phase 1 audit. See docs/concurrency-control.md
// for the full picture of what is and is not yet covered.
var saferLockManager = lockmanager.NewLockManager()

// txnIDCounter allocates the process-wide monotonic TxnIDs handed to
// saferLockManager. 0 is reserved/invalid (the zero value of
// lockmanager.TxnID), so allocateTxnID hands out 1, 2, 3, ... A wrap all
// the way back around to 0 (practically unreachable at 2^64 allocations,
// but not left to chance) is reported as an explicit error rather than
// silently recycling an ID that could collide with a still-active
// transaction.
var txnIDCounter atomic.Uint64

// allocateTxnID hands out a fresh, process-wide-unique logical transaction
// identifier for one SAFER operation to use as its LockManager owner. It
// never uses goroutine identity (Go does not expose a stable one) -- every
// call to a Phase-4-integrated public API allocates exactly one TxnID and
// uses it for every lock that operation acquires.
//
// Phase 4.5 hardening: a plain "id := counter.Add(1); if id == 0 { error }"
// only detects the single call that happens to observe the wraparound --
// every OTHER call racing near that boundary would still receive
// Add(1)'s next values (1, 2, 3, ...) again, silently recycling IDs that
// could collide with still-meaningful ones from process start. Instead,
// this uses a compare-and-swap loop that refuses to perform the increment
// at all once the counter is already at math.MaxUint64: no call can ever
// observe a post-wraparound value, so once the ID space is exhausted,
// EVERY subsequent call fails, permanently, not just the one call that
// happened to hit the boundary first.
func allocateTxnID() (lockmanager.TxnID, error) {
	for {
		current := txnIDCounter.Load()
		if current == ^uint64(0) {
			return lockmanager.TxnID(0), errors.New("safer-cc: transaction ID space exhausted")
		}

		next := current + 1
		if txnIDCounter.CompareAndSwap(current, next) {
			return lockmanager.TxnID(next), nil
		}
		// Another goroutine updated the counter between Load and
		// CompareAndSwap; retry with the new value.
	}
}

// namespaceResourceID builds the logical lock-manager resource identity for
// the (username, filename) namespace entry. It reuses
// getNameSpaceEntryUUID, which itself derives from the unambiguous
// canonicalNamespaceIdentity encoding (see that function's doc comment for
// why plain delimiter concatenation was not safe here), so the resource
// key is exactly as collision-resistant as the datastore addressing scheme
// SAFER already relies on -- no separate, potentially divergent encoding
// is introduced here.
func namespaceResourceID(username string, filename string) (lockmanager.ResourceID, error) {
	nameUUID, err := getNameSpaceEntryUUID(username, filename)
	if err != nil {
		return lockmanager.ResourceID{}, err
	}

	return lockmanager.ResourceID{
		Type: lockmanager.NamespaceResource,
		Key:  nameUUID.String(),
	}, nil
}

// fileResourceID builds the logical lock-manager resource identity for the
// logical file identified by fileID. This is deliberately keyed by FileID
// -- not MetadataUUID, ChunkUUID, AccessBoxUUID, or filename -- because
// FileID is the one identifier every alias/every authorized user of a
// given logical file agrees on; locking anything else would let different
// users (or an owner and a recipient) operate on the same logical file
// through different, uncoordinated resource keys.
func fileResourceID(fileID uuid.UUID) lockmanager.ResourceID {
	return lockmanager.ResourceID{
		Type: lockmanager.FileResource,
		Key:  fileID.String(),
	}
}

// concurrencyTestHook is a package-private, test-only synchronization
// point. It is nil in all normal operation, in which case
// fireConcurrencyTestHook is a no-op and production behavior is completely
// unaffected. Phase 4's concurrency tests temporarily set it to force
// deterministic overlapping schedules (e.g. "pause after this operation
// has loaded Metadata under its file lock, so a test can prove a
// concurrent operation on the same file really does block on the lock
// manager, not just on timing") instead of relying on time.Sleep. It is
// called from a small, fixed set of points inside the Phase-4-integrated
// operations, each passed a tag identifying the call site and the
// resource involved.
//
// Phase 4.5 hardening: a bare "var concurrencyTestHook func(tag string)",
// set by plain assignment from a test goroutine and read by plain
// dereference from concurrently-running production goroutines, is exactly
// the shape of code the Go race detector is designed to catch -- even
// though every Phase 4/5 test happens to set it before launching any
// goroutines that read it (which does establish a happens-before edge via
// the "go" statement) and only clears it after joining all of them, that
// safety is a property of test *discipline*, not of the mechanism, and a
// future test author could easily violate it. concurrencyTestHookPtr uses
// atomic.Pointer instead, so setting/reading/clearing the hook is safe
// under -race by construction, independent of any particular test's
// goroutine-lifecycle care.
var concurrencyTestHookPtr atomic.Pointer[func(string)]

// setConcurrencyTestHook installs (or, with nil, clears) the package-private
// test hook. Test-only; never called from production code paths.
func setConcurrencyTestHook(hook func(tag string)) {
	if hook == nil {
		concurrencyTestHookPtr.Store(nil)
		return
	}

	concurrencyTestHookPtr.Store(&hook)
}

func fireConcurrencyTestHook(tag string) {
	if hookPtr := concurrencyTestHookPtr.Load(); hookPtr != nil {
		(*hookPtr)(tag)
	}
}

// ---------------------------------------------------------------------
// Phase 6: benchmark-only concurrency strategy switch.
//
// Production SAFER-CC always runs under StrategySaferCC -- the real
// saferLockManager, fine-grained S/X strict 2PL -- which is the zero
// value of ConcurrencyStrategy. No test in this repository, and no normal
// caller, ever calls SetConcurrencyStrategyForBenchmark, so every
// existing behavior/guarantee from Phases 2-5 is completely unaffected by
// this switch's mere existence.
//
// It exists solely so cmd/benchmark can run the exact same business logic
// -- identical crypto validation, identical datastore calls, identical
// chunk traversal, identical Metadata.Version bookkeeping, identical
// authorization checks -- under two baseline coordination strategies, for
// a fair correctness/performance comparison against the real thing:
//
//   - StrategyGlobalLock: one process-wide mutex serializes each public
//     operation start-to-finish. Simple, obviously correct, but
//     eliminates concurrency across independent readers and independent
//     files.
//   - StrategyNoCC: no logical coordination at all -- deliberately
//     reproduces the Phase-1-audit races, for correctness comparison
//     against a "no concurrency control" baseline.
//
// In both baseline strategies, datastoreMu/keystoreMu (the storage-engine
// latches from Phase 4) remain fully active regardless -- they protect Go
// map memory safety, not SAFER transaction semantics, and disabling them
// would just crash the process instead of demonstrating a logical race
// (see docs/concurrency-control.md, "Storage-layer thread safety").
//
// Every public operation's body is completely unchanged by this switch
// except for the single line that constructs its `guard` variable: all
// three strategies satisfy the same lockGuardLike interface, so which one
// backs `guard` is the only thing that differs.
type ConcurrencyStrategy int32

const (
	StrategySaferCC ConcurrencyStrategy = iota
	StrategyGlobalLock
	StrategyNoCC
)

var activeConcurrencyStrategy atomic.Int32

// SetConcurrencyStrategyForBenchmark switches which coordination strategy
// subsequent public SAFER operations use, for cmd/benchmark's exclusive
// use. Not part of the SAFER client API in any meaningful sense.
func SetConcurrencyStrategyForBenchmark(strategy ConcurrencyStrategy) {
	activeConcurrencyStrategy.Store(int32(strategy))
}

var globalBenchmarkMutex sync.Mutex

// lockGuardLike is satisfied by *lockmanager.LockGuard (used unchanged
// for StrategySaferCC) and by the two benchmark-only stand-ins below.
type lockGuardLike interface {
	Acquire(resource lockmanager.ResourceID, mode lockmanager.LockMode) error
	ReleaseAll()
}

// globalMutexGuard serializes an entire operation behind one process-wide
// mutex instead of saferLockManager's fine-grained resources. It ignores
// which resource/mode is requested -- under a global lock there is only
// ever one critical section, not per-resource ones -- and takes the
// mutex on the first Acquire call for this transaction only (an
// operation may call Acquire twice: once for Namespace, once for File).
type globalMutexGuard struct {
	once   sync.Once
	locked bool
}

func (g *globalMutexGuard) Acquire(_ lockmanager.ResourceID, _ lockmanager.LockMode) error {
	g.once.Do(func() {
		globalBenchmarkMutex.Lock()
		g.locked = true
	})
	return nil
}

func (g *globalMutexGuard) ReleaseAll() {
	if g.locked {
		globalBenchmarkMutex.Unlock()
	}
}

// noCCGuard performs no coordination whatsoever -- the No-CC benchmark
// baseline.
type noCCGuard struct{}

func (noCCGuard) Acquire(_ lockmanager.ResourceID, _ lockmanager.LockMode) error { return nil }
func (noCCGuard) ReleaseAll()                                                   {}

// newOperationGuard is the one call site every public operation uses to
// obtain its lock guard. Swapping strategies never touches anything else
// in an operation's body.
func newOperationGuard(txn lockmanager.TxnID) lockGuardLike {
	switch ConcurrencyStrategy(activeConcurrencyStrategy.Load()) {
	case StrategyGlobalLock:
		return &globalMutexGuard{}
	case StrategyNoCC:
		return &noCCGuard{}
	default:
		return lockmanager.NewLockGuard(saferLockManager, txn)
	}
}

// ---------------------------------------------------------------------

type Chunk struct {
	FileID uuid.UUID
	EpochID uuid.UUID
	Index uint64
	PrevUUID uuid.UUID
	Content []byte
}

//Bracnh boxes for every recipient
type BranchBoxRecord struct{
	BoxUUID uuid.UUID
	BoxEncKey []byte
	BoxMACKey []byte
}

type AccessBoxStructure struct {
	FileID uuid.UUID
	CurrentEpoch uuid.UUID
	RecipientBoxes map[string]BranchBoxRecord
}

/**
Helper functions for StoreFile
**/

// canonicalNamespaceIdentity builds an unambiguous byte encoding of the
// logical (username, filename) pair that identifies one namespace entry.
//
// Phase 4.5 hardening: the previous encoding was plain delimiter
// concatenation (username + "-" + filename), which is ambiguous --
// username="a-b", filename="c" and username="a", filename="b-c" both
// concatenate to "a-b-c" and would hash to the identical NamespaceEntry
// UUID and identical NamespaceResource lock identity, letting two
// completely different users' files (or two different filenames for the
// same user) collide. This uses netstring-style length-prefix encoding
// (<decimal length>:<bytes>, repeated per field): a decoder would read the
// decimal digits up to the ':', then consume exactly that many bytes
// verbatim regardless of their content (including any ':' or '-'
// characters inside them), then continue with the next field. Because
// each field's length is stated before its content, no byte sequence in
// one field can ever be reinterpreted as spilling into the length or
// content of another -- the mapping from (username, filename) pairs to
// encoded byte strings is injective, which delimiter concatenation alone
// is not.
func canonicalNamespaceIdentity(username string, filename string) []byte {
	encoded := strconv.Itoa(len(username)) + ":" + username +
		strconv.Itoa(len(filename)) + ":" + filename

	return []byte(encoded)
}

// getNameSpaceEntryUUID and namespaceResourceID (below) both derive from
// canonicalNamespaceIdentity, so datastore identity (which NamespaceEntry
// object a given (username, filename) resolves to) and logical lock
// identity (which saferLockManager resource protects it) can never diverge
// -- there is exactly one canonical namespace identity, used for both.
func getNameSpaceEntryUUID (username string, filename string,) (uuid.UUID, error) {
	nameHash := userlib.Hash(
		canonicalNamespaceIdentity(username, filename),
	)

	return uuid.FromBytes(nameHash[:16])
}

func deriveNamespaceEntryKeys (namespaceRoot []byte, filename string,) (encKey []byte, macKey []byte, err error,) {
	if len(namespaceRoot) != symmetricKeySize {
		return nil, nil, errors.New("Invalid namespace root")
	}

	encResult, err := userlib.HashKDF(
		namespaceRoot,
		[]byte("namespace-entry-enc:" + filename),
	)

	if err != nil {
        return nil, nil, err
    }

	macResult, err := userlib.HashKDF(
		namespaceRoot,
		[]byte("namespace-entry-mac:" + filename),
	)

	if err != nil {
        return nil, nil, err
    }

	return encResult[:16], macResult[:16], nil
}

func deriveChunkKeys (fileRoot []byte, chunkUUID uuid.UUID,) (encKey []byte, macKey []byte, err error,) {
	if len(fileRoot) != symmetricKeySize {
		return nil, nil, errors.New("invalid file root")
	}

	encResult, err := userlib.HashKDF(
		fileRoot,
		[]byte("file-chunk-enc:" + chunkUUID.String()),
	)

	if err != nil {
        return nil, nil, err
    }

	macResult, err := userlib.HashKDF(
        fileRoot,
        []byte("file-chunk-mac:"+chunkUUID.String()),
    )
    if err != nil {
        return nil, nil, err
    }

    return encResult[:16], macResult[:16], nil
}

//General message for mac generator
func datastoreObjectMACMessage (
	objectType string,
	objectUUID uuid.UUID,
	ciphertext []byte,
) []byte {
	prefix := []byte(
		objectType + ":" + objectUUID.String() + ":",
	)

	message := make(
		[]byte,
		0,
		len(prefix) + len(ciphertext),
	)

	message = append(message, prefix...)
	message = append(message, ciphertext...)

	return message
}

//General emac generator for MD chunk accessbox namespaceentry accessboxstructure
func protectDatastoreObject (
	objectType string,
	objectUUID uuid.UUID,
	object interface{},
	encKey []byte,
	macKey []byte,
) ([]byte, error) {
	if objectType == "" {
		return nil, errors.New("object type cannot be empty")
	}

	if objectUUID == uuid.Nil {
		return nil, errors.New("object UUID cannot be nil")
	}

	if len(encKey) != symmetricKeySize || len(macKey) != symmetricKeySize {
		return nil, errors.New("invalid object keys")
	}

	plaintext, err := json.Marshal(object)
	if err != nil {
        return nil, err
    }

	iv := userlib.RandomBytes(
		userlib.AESBlockSizeBytes,
	)

	//Generate the ciphertext
	ciphertext := userlib.SymEnc(
		encKey,
		iv,
		plaintext,
	)

	mac, err := userlib.HMACEval(
		macKey,
		datastoreObjectMACMessage(
			objectType,
			objectUUID,
			ciphertext,
		),
	)

	if err != nil {
        return nil, err
    }

	envelope := AuthenticatedEnvelope{
		Ciphertext: ciphertext,
		MAC: mac,
	}

	return json.Marshal(envelope)
}

const (
    namespaceEntryObjectType     = "namespace-entry"
    accessBoxObjectType          = "access-box"
    accessBoxStructureObjectType = "access-box-structure"
    metadataObjectType           = "metadata"
    chunkObjectType              = "chunk"
)

func getAccessBoxCommitment (
	accessBox AccessBox,
) ([]byte, error) {
	accessBoxBytes, err := json.Marshal(accessBox)
	if err != nil {
        return nil, err
    }

    return userlib.Hash(accessBoxBytes), nil
}

func getFileStatusSignatureMessage (
	statusUUID uuid.UUID,
	body FileStatusBody,
) ([]byte, error) {
	bodyBytes, err := json.Marshal(body)
	if err != nil {
        return nil, err
    }

    prefix := []byte (
		"file-status-signature:" +
		statusUUID.String() +
		":",
	)

	message := make(
		[]byte,
		0,
		len(prefix) + len(bodyBytes),
	)

	message = append(message, prefix...)
	message = append(message, bodyBytes...)

	return message, nil
}

func createFileStatus (
	userdata *User,
	statusUUID uuid.UUID,
	accessBox AccessBox,
) (FileStatus, error) {
	var emptyStatus FileStatus

	commitment, err := getAccessBoxCommitment(accessBox)
	if err != nil {
        return emptyStatus, err
    }

	//Create the body for file status based on the accessbox
	body := FileStatusBody{
		FileID: accessBox.FileID,
		EpochID: accessBox.EpochID,
		CurrentAccessCommitment: commitment,
		OwnerVerifyKeyName: accessBox.OwnerVerifyKeyName,
	}

	signatureMessage, err := getFileStatusSignatureMessage(
		statusUUID,
		body,
	)

	if err != nil {
        return emptyStatus, err
    }

	signature, err := userlib.DSSign(
		userdata.SignPrivate,
		signatureMessage,
	)

	if err != nil {
        return emptyStatus, err
    }

	status := FileStatus{
		Body: body,
		Signature: signature,
	}

	return status, nil
}

//If the file hasnt been create yet, store the new file
// storeNewFileLocked creates a brand-new logical file and publishes it
// under fileID. It assumes the caller (StoreFile) already holds
// X(namespace(userdata.Username, filename)) and X(file(fileID)) for the
// entire duration of this call -- it does not acquire, and must never
// acquire, any lock itself. fileID is supplied by the caller (rather than
// generated here) precisely so that the File X lock StoreFile already
// acquired is guaranteed to match the FileID this function actually
// publishes.
func (userdata *User) storeNewFileLocked(
	filename string,
	content []byte,
	fileID uuid.UUID,
) error {
	//Calculate the local namespace entry
	nameUUID, err := getNameSpaceEntryUUID(
		userdata.Username,
		filename,
	)

	if err != nil {
    	return err
	}

	namespaceEncKey, namespaceMACKey, err :=
	deriveNamespaceEntryKeys(
		userdata.NamespaceRoot,
		filename,
	)

	if err != nil {
    	return err
	}

	//Generate the remaining random ids (fileID is supplied by the caller):
	epochID := uuid.New()

	fileRoot := userlib.RandomBytes(symmetricKeySize)

	baseChunkUUID := uuid.New()

	metadataUUID := uuid.New()
	metadataEncKey := userlib.RandomBytes(symmetricKeySize)
	metadataMACKey := userlib.RandomBytes(symmetricKeySize)

	ownerAccessBoxUUID := uuid.New()
	ownerAccessBoxEncKey := userlib.RandomBytes(symmetricKeySize)
	ownerAccessBoxMACKey := userlib.RandomBytes(symmetricKeySize)

	structureUUID := uuid.New()
	structureEncKey := userlib.RandomBytes(symmetricKeySize)
	structureMACKey := userlib.RandomBytes(symmetricKeySize)

	statusUUID := uuid.New()

	//Create the base chunk
	chunk := Chunk{
		FileID: fileID,
		EpochID: epochID,
		Index: 0,
		PrevUUID: uuid.Nil,
		Content: content,
	}

	chunkEncKey, chunkMACKey, err := deriveChunkKeys(
		fileRoot,
		baseChunkUUID,
	)

	if err != nil {
		return err
	}

	protectedChunk, err := protectDatastoreObject(
		chunkObjectType,
		baseChunkUUID,
		chunk,
		chunkEncKey,
		chunkMACKey,
	)

	if err != nil {
		return err
	}

	metadata := Metadata{
		FileID: fileID,
		EpochID: epochID,
		Version: 1,
		TailUUID: baseChunkUUID,
		ChunkCount: 1,
	}

	protectedMetadata, err := protectDatastoreObject(
    	metadataObjectType,
    	metadataUUID,
    	metadata,
    	metadataEncKey,
    	metadataMACKey,
	)
	if err != nil {
    	return err
	}

	accessBox := AccessBox{
		FileID: fileID,
		EpochID: epochID,

		MetadataUUID: metadataUUID,
		MetadataEncKey: metadataEncKey,
		MetadataMACKey: metadataMACKey,

		FileRoot: fileRoot,

		AccessBoxStructureUUID: structureUUID,
		StatusUUID: statusUUID,
		OwnerVerifyKeyName: getVerifyKeyName(userdata.Username),
	}

	protectedAccessBox, err := protectDatastoreObject(
		accessBoxObjectType,
		ownerAccessBoxUUID,
		accessBox,
		ownerAccessBoxEncKey,
		ownerAccessBoxMACKey,
	)
	if err != nil {
    	return err
	}

	fileStatus, err := createFileStatus(
		userdata, //v.k.
		statusUUID, //For generating d.s. message
		accessBox, //For fileid epochid commitment 
	)
	if err != nil {
    	return err
	}

	fileStatusBytes, err := json.Marshal(fileStatus)
	if err != nil {
    	return err
	}

	structure := AccessBoxStructure {
		FileID: fileID,
		CurrentEpoch: epochID,
		RecipientBoxes: make(map[string]BranchBoxRecord),
	}

	protectedAccessBoxStructure, err := protectDatastoreObject(
		accessBoxStructureObjectType,
		structureUUID,
		structure,
		structureEncKey,
		structureMACKey,
	)

	if err != nil {
    	return err
	}

	namespaceEntry := NamespaceEntry{
		IsOwner: true,
		FileID: fileID,
		AccessBoxUUID: ownerAccessBoxUUID,
		AccessBoxEncKey: ownerAccessBoxEncKey,
		AccessBoxMACKey: ownerAccessBoxMACKey,

		AccessBoxStructureUUID: structureUUID,
		AccessBoxStructureEncKey: structureEncKey,
		AccessBoxStructureMACKey: structureMACKey,
	}

	protectedNamespaceEntry, err := protectDatastoreObject(
		namespaceEntryObjectType,
		nameUUID,
		namespaceEntry,
		namespaceEncKey,
		namespaceMACKey,
	)

	if err != nil {
    	return err
	}

	datastoreSet(
		baseChunkUUID,
		protectedChunk,
	)

	datastoreSet(
		metadataUUID,
		protectedMetadata,
	)

	datastoreSet(
		ownerAccessBoxUUID,
		protectedAccessBox,
	)

	datastoreSet(
		statusUUID,
		fileStatusBytes,
	)

	datastoreSet(
		structureUUID,
		protectedAccessBoxStructure,
	)

	datastoreSet(
		nameUUID,
		protectedNamespaceEntry,
	)

	return nil
}

/** 
If the file already exists, we rewrite the file
**/

// overwriteExistingFileLocked replaces the logical content behind an
// already-resolved, already-revalidated accessBox. The caller
// (StoreFile) is responsible for acquiring X(namespace(...)) and
// X(file(accessBox.FileID)) and for revalidating accessBox under the file
// lock (via validateFileAccessUnderLock) before calling this -- this
// function performs no locking and no access-control revalidation of its
// own; it assumes both are already established and still held.
func overwriteExistingFileLocked(
	accessBox AccessBox,
	content []byte,
) error {
	metadata, err := loadMetadata(accessBox)
    if err != nil {
        return err
    }

	fireConcurrencyTestHook("overwrite:metadata-loaded:" + accessBox.FileID.String())

	_, oldChunkUUIDs, err :=
        loadFileContentAndChunkUUIDs(
            accessBox,
            metadata,
        )
    if err != nil {
        return err
    }
	oldChunkSet := make(map[uuid.UUID]bool)

    for _, oldChunkUUID := range oldChunkUUIDs {
        oldChunkSet[oldChunkUUID] = true
    }
	var newBaseChunkUUID uuid.UUID

    for {
        newBaseChunkUUID = uuid.New()

        if !oldChunkSet[newBaseChunkUUID] {
            break
        }
    }
	newChunk := Chunk{
        FileID:   accessBox.FileID,
        EpochID:  accessBox.EpochID,
        Index:    0,
        PrevUUID: uuid.Nil,
        Content:  content,
    }

    newChunkEncKey, newChunkMACKey, err :=
        deriveChunkKeys(
            accessBox.FileRoot,
            newBaseChunkUUID,
        )
    if err != nil {
        return err
    }

    protectedNewChunk, err := protectDatastoreObject(
        chunkObjectType,
        newBaseChunkUUID,
        newChunk,
        newChunkEncKey,
        newChunkMACKey,
    )
    if err != nil {
        return err
    }

	// An overwrite always replaces the logical content, so the content
	// version advances exactly once, even though ChunkCount resets to 1.
	newVersion, err := nextMetadataVersion(metadata.Version)
	if err != nil {
		return err
	}

	newMetadata := Metadata{
        FileID:     accessBox.FileID,
        EpochID:    accessBox.EpochID,
        Version:    newVersion,
        TailUUID:   newBaseChunkUUID,
        ChunkCount: 1,
    }

    protectedNewMetadata, err :=
        protectDatastoreObject(
            metadataObjectType,
            accessBox.MetadataUUID,
            newMetadata,
            accessBox.MetadataEncKey,
            accessBox.MetadataMACKey,
        )
    if err != nil {
        return err
    }

	//Set the new chunk and new metadata
	datastoreSet(
        newBaseChunkUUID,
        protectedNewChunk,
    )

    datastoreSet(
        accessBox.MetadataUUID,
        protectedNewMetadata,
    )
	for _, oldChunkUUID := range oldChunkUUIDs {
        datastoreDelete(oldChunkUUID)
    }

    return nil

}

// StoreFile is the strict-2PL transaction boundary for both file creation
// and overwrite. Lock acquisition order, per operation:
//
//	create path:    X(namespace(user, filename))  ->  X(file(new FileID))
//	overwrite path: X(namespace(user, filename))  ->  X(file(existing FileID))
//
// Namespace X is acquired first and held for the whole call (including
// across the create-vs-overwrite existence check), which is what makes
// that check-then-act decision atomic: no concurrent StoreFile for the
// same (user, filename) can observe or change namespace existence while
// this transaction is deciding. All locks are released via
// guard.ReleaseAll(), deferred immediately after the guard is created, so
// every return path (success or error) releases them.
func (userdata *User) StoreFile(filename string, content []byte) (err error) {
	txn, err := allocateTxnID()
	if err != nil {
		return err
	}

	guard := newOperationGuard(txn)
	defer guard.ReleaseAll()

	nsResource, err := namespaceResourceID(userdata.Username, filename)
	if err != nil {
		return err
	}

	if err := guard.Acquire(nsResource, lockmanager.ExclusiveLock); err != nil {
		return err
	}

	nameUUID, err := getNameSpaceEntryUUID(
		userdata.Username,
		filename,
	)

	if err != nil {
        return err
    }

	_, exists := datastoreGet(nameUUID)

	if exists {
		namespaceEntry, err := loadNamespaceEntry(userdata, filename)
		if err != nil {
			return err
		}

		fileResource := fileResourceID(namespaceEntry.FileID)
		if err := guard.Acquire(fileResource, lockmanager.ExclusiveLock); err != nil {
			return err
		}

		accessBox, err := validateFileAccessUnderLock(namespaceEntry)
		if err != nil {
			return err
		}

		return overwriteExistingFileLocked(accessBox, content)
	}

	fileID := uuid.New()

	fileResource := fileResourceID(fileID)
	if err := guard.Acquire(fileResource, lockmanager.ExclusiveLock); err != nil {
		return err
	}

	return userdata.storeNewFileLocked(filename, content, fileID)
}

// AppendToFile is the strict-2PL transaction boundary for content append.
// Lock acquisition order: S(namespace(user, filename)) -> X(file(FileID)).
// Namespace only needs S because the append does not change which FileID
// the namespace entry points to. File needs X because Metadata (TailUUID,
// ChunkCount, Version) is mutated. Critically, Metadata is loaded (via
// loadMetadata, inside validateFileAccessUnderLock's caller below) only
// after File X has been granted -- no snapshot of Metadata taken before
// that point is ever used to compute the update, which is what closes the
// lost-update race described in the Phase 1 audit and Phase 3's
// documented limitation.
func (userdata *User) AppendToFile(filename string, content []byte) error {
	txn, err := allocateTxnID()
	if err != nil {
		return err
	}

	guard := newOperationGuard(txn)
	defer guard.ReleaseAll()

	nsResource, err := namespaceResourceID(userdata.Username, filename)
	if err != nil {
		return err
	}

	if err := guard.Acquire(nsResource, lockmanager.SharedLock); err != nil {
		return err
	}

	namespaceEntry, err := loadNamespaceEntry(userdata, filename)
	if err != nil {
		return err
	}

	fileResource := fileResourceID(namespaceEntry.FileID)
	if err := guard.Acquire(fileResource, lockmanager.ExclusiveLock); err != nil {
		return err
	}

	accessBox, err := validateFileAccessUnderLock(namespaceEntry)
	if err != nil {
		return err
	}

	// Metadata is deliberately (re)loaded here, strictly after File X was
	// granted above -- never reuse a Metadata value read before this line.
	metadata, err := loadMetadata(accessBox)
	if err != nil {
		return err
	}

	fireConcurrencyTestHook("append:metadata-loaded:" + filename)

	if len(content) == 0 {
		return nil
	}

	//In case the chunk count overflow
	if metadata.ChunkCount == ^uint64(0) {
		return errors.New("chunk count overflow")
	}

	newChunkUUID := uuid.New()

	newChunk := Chunk{
		FileID: accessBox.FileID,
		EpochID: accessBox.EpochID,
		Index: metadata.ChunkCount,
		PrevUUID: metadata.TailUUID,
		Content: content,
	}

	newChunkEncKey, newChunkMACKey, err := deriveChunkKeys(
		accessBox.FileRoot,
		newChunkUUID,
	)

	if err != nil {
        return err
    }

    protectedNewChunk, err :=
        protectDatastoreObject(
            chunkObjectType,
            newChunkUUID,
            newChunk,
            newChunkEncKey,
            newChunkMACKey,
        )
    if err != nil {
        return err
    }

	// Phase 4: safe. metadata was loaded above only after File X was
	// granted, and File X is held for the rest of this function (released
	// only via the deferred guard.ReleaseAll() in AppendToFile), so no
	// other transaction can read or write this file's Metadata/chunks
	// concurrently with this increment.
	newVersion, err := nextMetadataVersion(metadata.Version)
	if err != nil {
		return err
	}

	updatedMetadata := Metadata{
        FileID:     metadata.FileID,
        EpochID:    metadata.EpochID,
        Version:    newVersion,
        TailUUID:   newChunkUUID,
        ChunkCount: metadata.ChunkCount + 1,
    }

    protectedUpdatedMetadata, err :=
        protectDatastoreObject(
            metadataObjectType,
            accessBox.MetadataUUID,
            updatedMetadata,
            accessBox.MetadataEncKey,
            accessBox.MetadataMACKey,
        )
    if err != nil {
        return err
    }

	datastoreSet(
        newChunkUUID,
        protectedNewChunk,
    )

    datastoreSet(
        accessBox.MetadataUUID,
        protectedUpdatedMetadata,
    )

    return nil
}

/**
Helper func for loadFile
**/
func loadDatastoreObject(
	objectType string,
	objectUUID uuid.UUID,
	encKey []byte,
	macKey []byte,
	destination interface{},
) error {
	if objectType == "" {
        return errors.New("object type cannot be empty")
    }

    if objectUUID == uuid.Nil {
        return errors.New("object UUID cannot be nil")
    }

    if len(encKey) != symmetricKeySize ||
        len(macKey) != symmetricKeySize {
        return errors.New("invalid object keys")
    }

    envelopeBytes, exists := datastoreGet(objectUUID)
    if !exists {
        return errors.New("required datastore object is missing")
    }

    var envelope AuthenticatedEnvelope
    err := json.Unmarshal(envelopeBytes, &envelope)
    if err != nil {
        return errors.New("invalid datastore envelope")
    }

    if len(envelope.MAC) != userlib.HashSizeBytes {
        return errors.New("invalid datastore MAC length")
    }

    expectedMAC, err := userlib.HMACEval(
        macKey,
        datastoreObjectMACMessage(
            objectType,
            objectUUID,
            envelope.Ciphertext,
        ),
    )
    if err != nil {
        return err
    }

    if !userlib.HMACEqual(expectedMAC, envelope.MAC) {
        return errors.New("datastore object authentication failed")
    }

    if len(envelope.Ciphertext) < userlib.AESBlockSizeBytes {
        return errors.New("datastore ciphertext is too short")
    }

    plaintext := userlib.SymDec(
        encKey,
        envelope.Ciphertext,
    )

    err = json.Unmarshal(plaintext, destination)
    if err != nil {
        return errors.New("invalid datastore object plaintext")
    }

    return nil
}

func verifyFileStatus(
	accessBox AccessBox,
) error {
	if accessBox.StatusUUID == uuid.Nil {
		return errors.New("invalid status uuid")
	}

	statusBytes, exists := datastoreGet(
		accessBox.StatusUUID,
	)

	if !exists {
        return errors.New("file status is missing")
    }

	//Unmarshal the statusBytes
	var status FileStatus
    err := json.Unmarshal(statusBytes, &status)
    if err != nil {
        return errors.New("invalid file status")
    }

	verifyKey, exists := keystoreGet(
		accessBox.OwnerVerifyKeyName,
	)

	if !exists {
        return errors.New("owner verification key is missing")
    }

	if verifyKey.KeyType != "DS" {
		return errors.New("Invalid owner verification key")
	}

	signatureMessage, err := getFileStatusSignatureMessage(
		accessBox.StatusUUID,
		status.Body,
	)

	if err != nil {
        return err
    }

	err = userlib.DSVerify(
		verifyKey,
		signatureMessage,
		status.Signature,
	)

	if err != nil{
		return errors.New("invalid signature")
	}

	 if status.Body.FileID != accessBox.FileID {
        return errors.New("file status FileID mismatch")
    }

    if status.Body.EpochID != accessBox.EpochID {
        return errors.New("file status epoch mismatch")
    }

	if status.Body.OwnerVerifyKeyName != accessBox.OwnerVerifyKeyName {
		return errors.New("file status owner mismatch")
	}

	expectedCommitment, err := getAccessBoxCommitment(accessBox)

	if err != nil {
        return err
    }

	if !userlib.HMACEqual(
        expectedCommitment,
        status.Body.CurrentAccessCommitment,
    ) {
        return errors.New("stale access box")
    }

    return nil
}

// loadNamespaceEntry loads and authenticates userdata's NamespaceEntry for
// filename. This is purely namespace-resource work: it never touches the
// AccessBox or FileStatus, so it is safe to call while holding only the
// namespace(user, filename) lock (S or X). It does NOT itself acquire any
// lock -- per the Phase 4 architecture, public API transaction boundaries
// (StoreFile/AppendToFile/LoadFile) own lock acquisition; this helper only
// performs data work and assumes the appropriate namespace lock is already
// held by its caller.
//
// The NamespaceEntry this returns must still be revalidated by
// validateFileAccessUnderLock after the file(FileID) lock is acquired --
// it is not yet safe to trust its AccessBoxUUID/keys as "the current
// capability," because a concurrent RevokeAccess could rotate them before
// (or, without a file lock, even while) the caller acts on them.
func loadNamespaceEntry(userdata *User, filename string) (NamespaceEntry, error) {
	var emptyEntry NamespaceEntry

	if userdata == nil {
		return emptyEntry, errors.New("user cannot be nil")
	}

	nameUUID, err := getNameSpaceEntryUUID(
		userdata.Username,
		filename,
	)
	if err != nil {
		return emptyEntry, err
	}

	namespaceEncKey, namespaceMACKey, err :=
		deriveNamespaceEntryKeys(
			userdata.NamespaceRoot,
			filename,
		)
	if err != nil {
		return emptyEntry, err
	}

	var namespaceEntry NamespaceEntry

	//Get the namespaceEntry
	err = loadDatastoreObject(
		namespaceEntryObjectType,
		nameUUID,
		namespaceEncKey,
		namespaceMACKey,
		&namespaceEntry,
	)

	if err != nil {
		return emptyEntry, err
	}

	if namespaceEntry.FileID == uuid.Nil {
		return emptyEntry, errors.New("invalid namespace FileID")
	}

	if namespaceEntry.AccessBoxUUID == uuid.Nil {
		return emptyEntry, errors.New("invalid access box UUID")
	}

	if len(namespaceEntry.AccessBoxEncKey) != symmetricKeySize {
		return emptyEntry, errors.New("invalid access box encryption key")
	}

	if len(namespaceEntry.AccessBoxMACKey) != symmetricKeySize {
		return emptyEntry, errors.New("invalid access box MAC key")
	}

	return namespaceEntry, nil
}

// validateFileAccessUnderLock loads and fully authenticates the AccessBox
// referenced by namespaceEntry -- including verifying its FileStatus,
// which is the check that detects a stale/revoked AccessBox. It performs
// no locking of its own: its authoritativeness comes entirely from the
// caller's contract to invoke it only after already holding the
// file(namespaceEntry.FileID) lock (S or X). Under that contract, this
// read is guaranteed to observe any RevokeAccess that had already
// committed, and (once RevokeAccess itself is integrated in Phase 5) no
// RevokeAccess can commit while this read is in progress -- this is what
// eliminates the TOCTOU race between validation and use that the
// unlocked, single-shot resolveFile could not close on its own.
func validateFileAccessUnderLock(namespaceEntry NamespaceEntry) (AccessBox, error) {
	var emptyAccessBox AccessBox
	var accessBox AccessBox

	err := loadDatastoreObject(
		accessBoxObjectType,
		namespaceEntry.AccessBoxUUID,
		namespaceEntry.AccessBoxEncKey,
		namespaceEntry.AccessBoxMACKey,
		&accessBox,
	)
	if err != nil {
		return emptyAccessBox, err
	}

	if accessBox.FileID != namespaceEntry.FileID {
		return emptyAccessBox, errors.New("access box FileID mismatch")
	}

	if accessBox.EpochID == uuid.Nil {
		return emptyAccessBox, errors.New("invalid access box epoch")
	}

	if accessBox.MetadataUUID == uuid.Nil {
		return emptyAccessBox, errors.New("invalid metadata UUID")
	}

	if len(accessBox.MetadataEncKey) != symmetricKeySize ||
		len(accessBox.MetadataMACKey) != symmetricKeySize {
		return emptyAccessBox, errors.New("invalid metadata keys")
	}

	if len(accessBox.FileRoot) != symmetricKeySize {
		return emptyAccessBox, errors.New("invalid file root")
	}

	if accessBox.AccessBoxStructureUUID == uuid.Nil {
		return emptyAccessBox, errors.New("invalid access box structure UUID")
	}

	if accessBox.StatusUUID == uuid.Nil {
		return emptyAccessBox, errors.New("invalid status UUID")
	}

	if accessBox.OwnerVerifyKeyName == "" {
		return emptyAccessBox, errors.New("missing owner verification key name")
	}

	err = verifyFileStatus(accessBox)
	if err != nil {
		return emptyAccessBox, err
	}

	return accessBox, nil
}

// validateInvitationAccessUnderLock is validateFileAccessUnderLock's
// counterpart for AcceptInvitation: the recipient does not yet have a
// NamespaceEntry (that is precisely what this operation is about to
// install), so there is nothing to key the AccessBox lookup off except the
// invitation payload's own AccessBoxUUID/keys. It performs exactly the
// same two checks AcceptInvitation always performed (FileID agreement,
// then FileStatus verification) -- Phase 5 does not tighten this
// validation, it only relocates it. Like validateFileAccessUnderLock, this
// performs no locking of its own: it is authoritative only because the
// caller's contract is to invoke it after already holding
// file(payload.FileID) (S is sufficient -- AcceptInvitation never mutates
// shared file state). Under that contract, a RevokeAccess that already
// committed is guaranteed visible here, and (since RevokeAccess also takes
// File X) none can commit while this read is in flight.
func validateInvitationAccessUnderLock(payload InvitationPayload) (AccessBox, error) {
	var emptyAccessBox AccessBox
	var accessBox AccessBox

	err := loadDatastoreObject(
		accessBoxObjectType,
		payload.AccessBoxUUID,
		payload.AccessBoxEncKey,
		payload.AccessBoxMACKey,
		&accessBox,
	)
	if err != nil {
		return emptyAccessBox, err
	}

	if accessBox.FileID != payload.FileID {
		return emptyAccessBox, errors.New("invitation and AccessBox FileID mismatch")
	}

	err = verifyFileStatus(accessBox)
	if err != nil {
		return emptyAccessBox, err
	}

	return accessBox, nil
}

// resolveFile composes loadNamespaceEntry and validateFileAccessUnderLock
// back into the single-shot, unlocked resolution the original
// implementation performed. As of Phase 5, every public SAFER operation
// (StoreFile, AppendToFile, LoadFile, CreateInvitation, AcceptInvitation,
// RevokeAccess) is integrated with saferLockManager and none of them call
// this unlocked path any more -- it is kept only as a convenience for
// tests and any future ad hoc, non-transactional inspection of current
// state. Production operations must acquire the appropriate Namespace/File
// locks and call loadNamespaceEntry / validateFileAccessUnderLock (or
// validateInvitationAccessUnderLock) directly, with the lock acquired in
// between, exactly as StoreFile/AppendToFile/LoadFile/CreateInvitation/
// AcceptInvitation/RevokeAccess all now do.
func resolveFile(
	userdata *User,
	filename string,
) (
	NamespaceEntry,
	AccessBox,
	error,
) {
	namespaceEntry, err := loadNamespaceEntry(userdata, filename)
	if err != nil {
		return NamespaceEntry{}, AccessBox{}, err
	}

	accessBox, err := validateFileAccessUnderLock(namespaceEntry)
	if err != nil {
		return NamespaceEntry{}, AccessBox{}, err
	}

	return namespaceEntry, accessBox, nil
}

// ---------------------------------------------------------------------
// Phase 6: diagnostic, benchmark-only exports.
//
// Metadata.Version is deliberately NOT part of SAFER's public API (see
// Phase 3): it is an internal implementation detail, and StoreFile/
// LoadFile/etc. never expose it. But Phase 6's evaluation needs exactly
// this value as its strongest correctness oracle ("N successful content
// mutations should mean Version advanced by exactly N"), and
// cmd/benchmark is a separate package that cannot see unexported
// identifiers. These two functions are the minimal, explicitly-named
// exception: diagnostic surface for cmd/benchmark only, not part of the
// 8-function graded SAFER API (InitUser, GetUser, StoreFile, LoadFile,
// AppendToFile, CreateInvitation, AcceptInvitation, RevokeAccess), and not
// intended for any other caller.
// ---------------------------------------------------------------------

// DebugFileVersionForBenchmark returns the current Metadata.Version and
// ChunkCount for filename, as seen by owner. Benchmark-only; see the
// package note above.
func DebugFileVersionForBenchmark(owner *User, filename string) (version uint64, chunkCount uint64, err error) {
	_, accessBox, err := resolveFile(owner, filename)
	if err != nil {
		return 0, 0, err
	}

	metadata, err := loadMetadata(accessBox)
	if err != nil {
		return 0, 0, err
	}

	return metadata.Version, metadata.ChunkCount, nil
}

// CheckAuthorizationInvariantsForBenchmark re-validates the persistent
// Phase 5 authorization invariants (owner AccessBox epoch ==
// AccessBoxStructure.CurrentEpoch; Metadata.EpochID matches the current
// epoch; Metadata.Version equals expectedVersion; every surviving
// recipient's branch AccessBox is stamped with the current epoch) and
// returns a description of the first violation found, or "" if none.
// Benchmark-only; see the package note above.
func CheckAuthorizationInvariantsForBenchmark(owner *User, filename string, expectedVersion uint64) string {
	namespaceEntry, accessBox, err := resolveFile(owner, filename)
	if err != nil {
		return "resolveFile: " + err.Error()
	}

	if !namespaceEntry.IsOwner {
		return "caller is not the owner"
	}

	structure, err := loadOwnerAccessBoxStructure(namespaceEntry, accessBox)
	if err != nil {
		return "loadOwnerAccessBoxStructure: " + err.Error()
	}

	if structure.CurrentEpoch != accessBox.EpochID {
		return "AccessBoxStructure.CurrentEpoch does not match owner AccessBox.EpochID"
	}

	metadata, err := loadMetadata(accessBox)
	if err != nil {
		return "loadMetadata: " + err.Error()
	}

	if metadata.EpochID != accessBox.EpochID {
		return "Metadata.EpochID does not match the current epoch"
	}

	if metadata.Version != expectedVersion {
		return fmt.Sprintf("Metadata.Version = %d, expected %d", metadata.Version, expectedVersion)
	}

	for recipient, record := range structure.RecipientBoxes {
		var branchBox AccessBox
		err := loadDatastoreObject(
			accessBoxObjectType,
			record.BoxUUID,
			record.BoxEncKey,
			record.BoxMACKey,
			&branchBox,
		)
		if err != nil {
			return "recipient " + recipient + " branch AccessBox: " + err.Error()
		}
		if branchBox.EpochID != structure.CurrentEpoch {
			return "recipient " + recipient + " branch AccessBox is stamped with a stale epoch"
		}
	}

	return ""
}

// ---------------------------------------------------------------------

//load metadata from datastore
func loadMetadata (
	accessBox AccessBox,
) (Metadata, error) {
	var metadata Metadata

    err := loadDatastoreObject(
        metadataObjectType,
        accessBox.MetadataUUID,
        accessBox.MetadataEncKey,
        accessBox.MetadataMACKey,
        &metadata,
    )
    if err != nil {
        return Metadata{}, err
    }

    if metadata.FileID != accessBox.FileID {
        return Metadata{},
            errors.New("metadata FileID mismatch")
    }

    if metadata.EpochID != accessBox.EpochID {
        return Metadata{},
            errors.New("metadata epoch mismatch")
    }

    if metadata.ChunkCount == 0 {
        return Metadata{},
            errors.New("metadata has zero chunks")
    }

    // Every SAFER-CC V1 metadata object is created with Version >= 1
    // (storeNewFile sets 1; AppendToFile/overwrite advance it via
    // nextMetadataVersion; RevokeAccess preserves it unchanged). There are
    // no legacy pre-V1 fixtures in this repository that would deserialize
    // with Version == 0, so this is enforced strictly rather than
    // silently normalized -- Version == 0 always indicates corrupted or
    // forged metadata.
    if metadata.Version == 0 {
        return Metadata{},
            errors.New("metadata has invalid content version")
    }

    if metadata.TailUUID == uuid.Nil {
        return Metadata{},
            errors.New("metadata tail is nil")
    }

    return metadata, nil
}

func loadFileContentAndChunkUUIDs(
	accessBox AccessBox,
	metadat Metadata,
) (
	content []byte,
	chunkUUIDs []uuid.UUID,
	err error,
) {
	currentUUID := metadat.TailUUID
	seen := make(map[uuid.UUID]bool)
	reversedContents := make([][]byte, 0)

	for remaining := metadat.ChunkCount;
		remaining > 0;
		remaining-- {
			if currentUUID == uuid.Nil {
				return nil, nil, errors.New("chunk chain ended early")
			}

			if seen[currentUUID] {
				return nil, nil, errors.New("chunk chain contains a cycle")
			}

			seen[currentUUID] = true

			chunkEncKey, chunkMACKey, err := deriveChunkKeys(
				accessBox.FileRoot,
				currentUUID,
			)

			if err != nil {
            	return nil, nil, err
        	}

        	var chunk Chunk

        	err = loadDatastoreObject(
            	chunkObjectType,
            	currentUUID,
            	chunkEncKey,
            	chunkMACKey,
            	&chunk,
        	)
        	if err != nil {
            	return nil, nil, err
        	}

			expectedIndex := remaining - 1

        	if chunk.FileID != accessBox.FileID {
            	return nil, nil,
                	errors.New("chunk FileID mismatch")
        	}

        	if chunk.EpochID != accessBox.EpochID {
            	return nil, nil,
                	errors.New("chunk epoch mismatch")
        	}

        	if chunk.Index != expectedIndex {
            	return nil, nil,
                	errors.New("chunk index mismatch")
        	}

        	if expectedIndex == 0 {
            	if chunk.PrevUUID != uuid.Nil {
                	return nil, nil,
                    	errors.New("base chunk predecessor is not nil")
            	}
        	} else {
            	if chunk.PrevUUID == uuid.Nil {
                	return nil, nil,
                    	errors.New("chunk chain is truncated")
            	}
        	}

			reversedContents = append(
            	reversedContents,
            	chunk.Content,
        	)

        	chunkUUIDs = append(
            	chunkUUIDs,
            	currentUUID,
        	)

        	currentUUID = chunk.PrevUUID
		}

		if currentUUID != uuid.Nil {
        	return nil, nil,
            	errors.New("chunk chain has extra predecessor")
    	}
		content = make([]byte, 0)

    	for index := len(reversedContents) - 1;
        	index >= 0;
        	index-- {
        	content = append(
            	content,
            	reversedContents[index]...,
        	)
    	}

    return content, chunkUUIDs, nil
}

// LoadFile is the strict-2PL transaction boundary for reads. Lock
// acquisition order: S(namespace(user, filename)) -> S(file(FileID)). Both
// locks are Shared, so many concurrent LoadFile calls (and, once Phase 5
// lands, CreateInvitation calls) can proceed together against the same
// file -- readers are never serialized against each other. What Shared
// does guarantee is mutual exclusion against any Exclusive holder: a
// concurrent AppendToFile/overwrite cannot be interleaved mid-mutation
// while this read is in progress, and this read cannot start mid-mutation
// either, so the AccessBox/Metadata/chunk chain observed here is always
// one complete, self-consistent logical file state -- never a half
// re-chunked overwrite or a half-published append.
func (userdata *User) LoadFile(filename string) (content []byte, err error) {
	txn, err := allocateTxnID()
	if err != nil {
		return nil, err
	}

	guard := newOperationGuard(txn)
	defer guard.ReleaseAll()

	nsResource, err := namespaceResourceID(userdata.Username, filename)
	if err != nil {
		return nil, err
	}

	if err := guard.Acquire(nsResource, lockmanager.SharedLock); err != nil {
		return nil, err
	}

	namespaceEntry, err := loadNamespaceEntry(userdata, filename)
	if err != nil {
		return nil, err
	}

	fileResource := fileResourceID(namespaceEntry.FileID)
	if err := guard.Acquire(fileResource, lockmanager.SharedLock); err != nil {
		return nil, err
	}

	accessBox, err := validateFileAccessUnderLock(namespaceEntry)
	if err != nil {
		return nil, err
	}

	fireConcurrencyTestHook("load:file-locked:" + filename)

	metadata, err := loadMetadata(accessBox)

	if err != nil {
		return nil, err
	}

	content, _, err = loadFileContentAndChunkUUIDs(
		accessBox,
		metadata,
	)

	if err != nil {
		return nil, err
	}

	return content, nil
}

//Basic datastructure
type InvitationPayload struct {
	SenderIdentity []byte
	RecipientIdentity []byte

	FileID uuid.UUID

	AccessBoxUUID uuid.UUID
	AccessBoxEncKey []byte
	AccessBoxMACKey []byte
}

type Invitation struct {
	WrappedRoot []byte
	Ciphertext []byte //enc payload
	MAC []byte
	Signature []byte
}

type InvitationSignedFields struct {
	WrappedRoot []byte
	Ciphertext []byte
	MAC []byte
}

func getInvitationIdentity(
	username string,
) []byte {
	return userlib.Hash(
		[]byte("invitation-identity:" + username),
	)
}

func deriveInvitationKeys(
	inviteRoot []byte,
	invitationUUID uuid.UUID,
) (
	encKey []byte,
	MACKey []byte,
	err error,
) {
	if len(inviteRoot) != symmetricKeySize {
        return nil, nil,
            errors.New("invalid invitation root")
    }

    encResult, err := userlib.HashKDF(
        inviteRoot,
        []byte(
            "invitation-enc:" +
                invitationUUID.String(),
        ),
    )
    if err != nil {
        return nil, nil, err
    }

    macResult, err := userlib.HashKDF(
        inviteRoot,
        []byte(
            "invitation-mac:" +
                invitationUUID.String(),
        ),
    )
    if err != nil {
        return nil, nil, err
    }

    return encResult[:16], macResult[:16], nil
}

//Get the invitation for generate mac message
func getInvitationMACMessage(
	invitationUUID uuid.UUID,
	ciphertext []byte,
) []byte {
	prefix := []byte(
        "invitation-ciphertext:" +
            invitationUUID.String() +
            ":",
    )

    message := make(
        []byte,
        0,
        len(prefix)+len(ciphertext),
    )

    message = append(message, prefix...)
    message = append(message, ciphertext...)

    return message
}

//Generate the digital signature
func getInvitationSignatureMessage(
    invitationUUID uuid.UUID,
    fields InvitationSignedFields,
) ([]byte, error) {
    fieldsBytes, err := json.Marshal(fields)
    if err != nil {
        return nil, err
    }

    prefix := []byte(
        "invitation-signature:" +
            invitationUUID.String() +
            ":",
    )

    message := make(
        []byte,
        0,
        len(prefix)+len(fieldsBytes),
    )

    message = append(message, prefix...)
    message = append(message, fieldsBytes...)

    return message, nil
}

func buildInvitation(
    sender *User,
    recipientPublicKey userlib.PKEEncKey,
    invitationUUID uuid.UUID,
    payload InvitationPayload,
) ([]byte, error) {
    if sender == nil {
        return nil, errors.New("sender cannot be nil")
    }

    if recipientPublicKey.KeyType != "PKE" {
        return nil,
            errors.New("invalid recipient PKE key")
    }

    inviteRoot := userlib.RandomBytes(
        symmetricKeySize,
    )

    invitationEncKey, invitationMACKey, err :=
        deriveInvitationKeys(
            inviteRoot,
            invitationUUID,
        )
    if err != nil {
        return nil, err
    }

    payloadBytes, err := json.Marshal(payload)
    if err != nil {
        return nil, err
    }

    iv := userlib.RandomBytes(
        userlib.AESBlockSizeBytes,
    )

    ciphertext := userlib.SymEnc(
        invitationEncKey,
        iv,
        payloadBytes,
    )

    mac, err := userlib.HMACEval(
        invitationMACKey,
        getInvitationMACMessage(
            invitationUUID,
            ciphertext,
        ),
    )
    if err != nil {
        return nil, err
    }

    wrappedRoot, err := userlib.PKEEnc(
        recipientPublicKey,
        inviteRoot,
    )
    if err != nil {
        return nil, err
    }

    signedFields := InvitationSignedFields{
        WrappedRoot: wrappedRoot,
        Ciphertext:  ciphertext,
        MAC:         mac,
    }

    signatureMessage, err :=
        getInvitationSignatureMessage(
            invitationUUID,
            signedFields,
        )
    if err != nil {
        return nil, err
    }

    signature, err := userlib.DSSign(
        sender.SignPrivate,
        signatureMessage,
    )
    if err != nil {
        return nil, err
    }

    invitation := Invitation{
        WrappedRoot: wrappedRoot,
        Ciphertext:  ciphertext,
        MAC:         mac,
        Signature:   signature,
    }

    return json.Marshal(invitation)
}

func loadOwnerAccessBoxStructure(
    namespaceEntry NamespaceEntry,
    accessBox AccessBox,
) (AccessBoxStructure, error) {
    var structure AccessBoxStructure

    if !namespaceEntry.IsOwner {
        return structure,
            errors.New("caller is not the owner")
    }

    if namespaceEntry.AccessBoxStructureUUID ==
        uuid.Nil {
        return structure,
            errors.New("missing structure UUID")
    }

    if namespaceEntry.AccessBoxStructureUUID !=
        accessBox.AccessBoxStructureUUID {
        return structure,
            errors.New("structure UUID mismatch")
    }

    if len(namespaceEntry.AccessBoxStructureEncKey) !=
        symmetricKeySize ||
        len(namespaceEntry.AccessBoxStructureMACKey) !=
            symmetricKeySize {
        return structure,
            errors.New("invalid structure keys")
    }

    err := loadDatastoreObject(
        accessBoxStructureObjectType,
        namespaceEntry.AccessBoxStructureUUID,
        namespaceEntry.AccessBoxStructureEncKey,
        namespaceEntry.AccessBoxStructureMACKey,
        &structure,
    )
    if err != nil {
        return AccessBoxStructure{}, err
    }

    if structure.FileID != accessBox.FileID {
        return AccessBoxStructure{},
            errors.New("structure FileID mismatch")
    }

    if structure.CurrentEpoch != accessBox.EpochID {
        return AccessBoxStructure{},
            errors.New("structure epoch mismatch")
    }

    if structure.RecipientBoxes == nil {
        return AccessBoxStructure{},
            errors.New("invalid recipient map")
    }

    return structure, nil
}

// CreateInvitation is the strict-2PL transaction boundary for sharing.
// Lock acquisition order: S(namespace(user, filename)) -> X(file(FileID)).
// File needs X, not S, because the owner branch mutates
// AccessBoxStructure.RecipientBoxes -- shared authorization-control state
// -- and a non-owner (resharing) branch is conservatively given the same
// X requirement for protocol uniformity (see docs/concurrency-control.md,
// "granularity tradeoff": V1 does not split content-mutation locking from
// authorization-mutation locking, so CreateInvitation currently serializes
// against AppendToFile/overwrite/other CreateInvitation calls on the same
// file, even when it only touches its own branch). Namespace only needs S
// because CreateInvitation never changes which FileID the caller's own
// namespace entry points to.
func (userdata *User) CreateInvitation(filename string, recipientUsername string) (
	invitationPtr uuid.UUID, err error) {
	txn, err := allocateTxnID()
	if err != nil {
		return uuid.Nil, err
	}

	guard := newOperationGuard(txn)
	defer guard.ReleaseAll()

	nsResource, err := namespaceResourceID(userdata.Username, filename)
	if err != nil {
		return uuid.Nil, err
	}

	if err := guard.Acquire(nsResource, lockmanager.SharedLock); err != nil {
		return uuid.Nil, err
	}

	namespaceEntry, err := loadNamespaceEntry(userdata, filename)
	if err != nil {
		return uuid.Nil, err
	}

	fileResource := fileResourceID(namespaceEntry.FileID)
	if err := guard.Acquire(fileResource, lockmanager.ExclusiveLock); err != nil {
		return uuid.Nil, err
	}

	// Revalidated fresh, under File X -- never reuse AccessBox/FileStatus
	// state read before this lock was granted.
	accessBox, err := validateFileAccessUnderLock(namespaceEntry)
	if err != nil {
		return uuid.Nil, err
	}

	recipientPKEKey, exists := keystoreGet(
        getPKEKeyName(recipientUsername),
    )
    if !exists || recipientPKEKey.KeyType != "PKE" {
        return uuid.Nil,
            errors.New("recipient does not exist")
    }

    recipientVerifyKey, exists :=
        keystoreGet(
            getVerifyKeyName(recipientUsername),
        )
    if !exists ||
        recipientVerifyKey.KeyType != "DS" {
        return uuid.Nil,
            errors.New("recipient does not exist")
    }

	//target exsits, creat invitation
	invitationUUID := uuid.New()

	var grantedBoxUUID uuid.UUID
    var grantedBoxEncKey []byte
    var grantedBoxMACKey []byte

	var protectedBranchBox []byte
    var protectedUpdatedStructure []byte
	var structureChanged bool

	if namespaceEntry.IsOwner {
        structure, err :=
            loadOwnerAccessBoxStructure(
                namespaceEntry,
                accessBox,
            )
        if err != nil {
            return uuid.Nil, err
        }

		fireConcurrencyTestHook("create-invitation:structure-loaded:" + accessBox.FileID.String())

		record, alreadyTracked :=
			structure.RecipientBoxes[recipientUsername]

		if !alreadyTracked {
			record = BranchBoxRecord{
				BoxUUID: uuid.New(),
				BoxEncKey: userlib.RandomBytes(
					symmetricKeySize,
				),
				BoxMACKey: userlib.RandomBytes(
					symmetricKeySize,
				),
			}

			structure.RecipientBoxes[recipientUsername] =
				record

			protectedUpdatedStructure, err =
				protectDatastoreObject(
					accessBoxStructureObjectType,
					accessBox.AccessBoxStructureUUID,
					structure,
					namespaceEntry.AccessBoxStructureEncKey,
					namespaceEntry.AccessBoxStructureMACKey,
				)

			if err != nil {
				return uuid.Nil, err
			}

			structureChanged = true
		}

		protectedBranchBox, err = protectDatastoreObject(
			accessBoxObjectType,
			record.BoxUUID,
			accessBox,
			record.BoxEncKey,
			record.BoxMACKey,
		)

		if err != nil {
            return uuid.Nil, err
        }

		grantedBoxUUID = record.BoxUUID
		grantedBoxEncKey = record.BoxEncKey
		grantedBoxMACKey = record.BoxMACKey
	} else {
		grantedBoxUUID = namespaceEntry.AccessBoxUUID
		grantedBoxEncKey = namespaceEntry.AccessBoxEncKey
        grantedBoxMACKey = namespaceEntry.AccessBoxMACKey
	}

	payload := InvitationPayload{
		SenderIdentity: getInvitationIdentity(userdata.Username),
		RecipientIdentity: getInvitationIdentity(recipientUsername),

		FileID: accessBox.FileID,

		AccessBoxUUID:   grantedBoxUUID,
        AccessBoxEncKey: grantedBoxEncKey,
        AccessBoxMACKey: grantedBoxMACKey,
	}

	invitationBytes, err := buildInvitation(
        userdata,
        recipientPKEKey,
        invitationUUID,
        payload,
    )
    if err != nil {
        return uuid.Nil, err
    }

	if namespaceEntry.IsOwner {
        datastoreSet(
			grantedBoxUUID,
			protectedBranchBox,
		)

		if structureChanged {
			datastoreSet(
				namespaceEntry.AccessBoxStructureUUID,
				protectedUpdatedStructure,
			)
		}
	}

	datastoreSet(
        invitationUUID,
        invitationBytes,
    )

	return invitationUUID, nil
}

/**
Helper func for open invitation
**/
func openInvitation(
    userdata *User,
    senderUsername string,
    invitationPtr uuid.UUID,
) (InvitationPayload, error) {
    var emptyPayload InvitationPayload

    if userdata == nil {
        return emptyPayload,
            errors.New("recipient cannot be nil")
    }

    if invitationPtr == uuid.Nil {
        return emptyPayload,
            errors.New("invitation UUID cannot be nil")
    }

    invitationBytes, exists :=
        datastoreGet(invitationPtr)

    if !exists {
        return emptyPayload,
            errors.New("invitation is missing")
    }

    var invitation Invitation

    err := json.Unmarshal(
        invitationBytes,
        &invitation,
    )
    if err != nil {
        return emptyPayload,
            errors.New("invalid invitation")
    }

    //Get the verification key
    senderVerifyKey, exists :=
        keystoreGet(
            getVerifyKeyName(senderUsername),
        )

    if !exists ||
        senderVerifyKey.KeyType != "DS" {
        return emptyPayload,
            errors.New("sender does not exist")
    }

    signedFields := InvitationSignedFields{
        WrappedRoot: invitation.WrappedRoot,
        Ciphertext:  invitation.Ciphertext,
        MAC:         invitation.MAC,
    }


	//Get the message that to be verify
    signatureMessage, err :=
        getInvitationSignatureMessage(
            invitationPtr,
            signedFields,
        )
    if err != nil {
        return emptyPayload, err
    }

    //Verify based on the signature, message, key
    err = userlib.DSVerify(
        senderVerifyKey,
        signatureMessage,
        invitation.Signature,
    )
    if err != nil {
        return emptyPayload,
            errors.New("invalid invitation signature")
    }

    //Get the invite Root after verification
    inviteRoot, err := userlib.PKEDec(
        userdata.PKEPrivate,
        invitation.WrappedRoot,
    )
    if err != nil {
        return emptyPayload,
            errors.New("cannot decrypt invitation root")
    }

    if len(inviteRoot) != symmetricKeySize {
        return emptyPayload,
            errors.New("invalid invitation root")
    }

    invitationEncKey, invitationMACKey, err :=
        deriveInvitationKeys(
            inviteRoot,
            invitationPtr,
        )
    if err != nil {
        return emptyPayload, err
    }

    if len(invitation.MAC) !=
        userlib.HashSizeBytes {
        return emptyPayload,
            errors.New("invalid invitation MAC length")
    }

    expectedMAC, err := userlib.HMACEval(
        invitationMACKey,
        getInvitationMACMessage(
            invitationPtr,
            invitation.Ciphertext,
        ),
    )
    if err != nil {
        return emptyPayload, err
    }

    if !userlib.HMACEqual(
        expectedMAC,
        invitation.MAC,
    ) {
        return emptyPayload,
            errors.New("invitation authentication failed")
    }

    if len(invitation.Ciphertext) <
        userlib.AESBlockSizeBytes {
        return emptyPayload,
            errors.New("invitation ciphertext is too short")
    }

    payloadBytes := userlib.SymDec(
        invitationEncKey,
        invitation.Ciphertext,
    )

    var payload InvitationPayload

    err = json.Unmarshal(
        payloadBytes,
        &payload,
    )
    if err != nil {
        return emptyPayload,
            errors.New("invalid invitation payload")
    }

    expectedSenderIdentity :=
        getInvitationIdentity(senderUsername)

    if len(payload.SenderIdentity) !=
        userlib.HashSizeBytes ||
        !userlib.HMACEqual(
            payload.SenderIdentity,
            expectedSenderIdentity,
        ) {
        return emptyPayload,
            errors.New("invitation sender mismatch")
    }

    expectedRecipientIdentity :=
        getInvitationIdentity(userdata.Username)

    if len(payload.RecipientIdentity) !=
        userlib.HashSizeBytes ||
        !userlib.HMACEqual(
            payload.RecipientIdentity,
            expectedRecipientIdentity,
        ) {
        return emptyPayload,
            errors.New("invitation recipient mismatch")
    }

    if payload.FileID == uuid.Nil {
        return emptyPayload,
            errors.New("invalid invitation FileID")
    }

    if payload.AccessBoxUUID == uuid.Nil {
        return emptyPayload,
            errors.New("invalid invitation AccessBox UUID")
    }

    if len(payload.AccessBoxEncKey) !=
        symmetricKeySize ||
        len(payload.AccessBoxMACKey) !=
            symmetricKeySize {
        return emptyPayload,
            errors.New("invalid invitation AccessBox keys")
    }

    return payload, nil
}

// AcceptInvitation is the strict-2PL transaction boundary for invitation
// acceptance. Lock acquisition order: X(namespace(recipient, filename)) ->
// S(file(FileID)). Namespace needs X (not S) because this operation
// performs a check-then-act on destination-filename occupancy, exactly
// like StoreFile's create path -- that check must be atomic with respect
// to every other concurrent writer of (recipient, filename), including
// another AcceptInvitation and StoreFile itself. File only needs S: this
// operation never mutates shared file state, it only needs an
// authoritative, current read of the granted AccessBox/FileStatus to
// decide whether the capability it is about to install is still valid.
//
// This is the operation where the Phase 1 audit's TOCTOU race lived most
// visibly: the old code validated the AccessBox/FileStatus once, with no
// lock at all, and then installed a NamespaceEntry based on that single
// snapshot -- a concurrent RevokeAccess could commit in between validation
// and install, handing the recipient a NamespaceEntry that *looked*
// installed but pointed at already-revoked capability state. Under File S,
// that window is closed: RevokeAccess needs File X, so it cannot commit
// while this validation is in flight, and if it already committed before
// this validation's File S was granted, that validation (via
// validateInvitationAccessUnderLock's call to verifyFileStatus) is
// guaranteed to observe it and fail.
func (userdata *User) AcceptInvitation(senderUsername string, invitationPtr uuid.UUID, filename string) error {
	if userdata == nil {
        return errors.New("recipient cannot be nil")
    }

	txn, err := allocateTxnID()
	if err != nil {
		return err
	}

	guard := newOperationGuard(txn)
	defer guard.ReleaseAll()

	nsResource, err := namespaceResourceID(userdata.Username, filename)
	if err != nil {
		return err
	}

	if err := guard.Acquire(nsResource, lockmanager.ExclusiveLock); err != nil {
		return err
	}

	nameUUID, err := getNameSpaceEntryUUID(
        userdata.Username,
        filename,
    )
    if err != nil {
        return err
    }

	// Occupancy check performed while Namespace X is held: no concurrent
	// AcceptInvitation or StoreFile for this same (recipient, filename)
	// can install/observe a different namespace state in between this
	// check and this operation's own eventual install below.
	_, occupied := datastoreGet(nameUUID)

    if occupied {
        return errors.New("filename is already in use")
    }

	namespaceEncKey, namespaceMACKey, err :=
        deriveNamespaceEntryKeys(
            userdata.NamespaceRoot,
            filename,
        )
    if err != nil {
        return err
    }

	payload, err := openInvitation(
        userdata,
        senderUsername,
        invitationPtr,
    )
    if err != nil {
        return err
    }

	fileResource := fileResourceID(payload.FileID)
	if err := guard.Acquire(fileResource, lockmanager.SharedLock); err != nil {
		return err
	}

	// Revalidated fresh, under File S -- this is the authoritative check
	// that a concurrent RevokeAccess cannot race past (see doc comment
	// above).
	_, err = validateInvitationAccessUnderLock(payload)
	if err != nil {
		return err
	}

	fireConcurrencyTestHook("accept:validated:" + payload.FileID.String())

	sharedEntry := NamespaceEntry{
        IsOwner: false,
        FileID:  payload.FileID,

        AccessBoxUUID:   payload.AccessBoxUUID,
        AccessBoxEncKey: payload.AccessBoxEncKey,
        AccessBoxMACKey: payload.AccessBoxMACKey,

        // Shared user doesnt have owner-only structure credentials
        AccessBoxStructureUUID:   uuid.Nil,
        AccessBoxStructureEncKey: nil,
        AccessBoxStructureMACKey: nil,
    }

	protectedNamespaceEntry, err :=
        protectDatastoreObject(
            namespaceEntryObjectType,
            nameUUID,
            sharedEntry,
            namespaceEncKey,
            namespaceMACKey,
        )
    if err != nil {
        return err
    }

	datastoreSet(
        nameUUID,
        protectedNamespaceEntry,
    )

    datastoreDelete(invitationPtr)

    return nil
}

// RevokeAccess is the strict-2PL transaction boundary for revocation.
// Lock acquisition order: S(namespace(owner, filename)) -> X(file(FileID)).
// File needs X because this operation is the single owner of epoch
// rotation for a file: it replaces Metadata (new MetadataUUID, chunk,
// content re-encrypted under a new FileRoot), rewrites the owner AccessBox
// and every surviving branch AccessBox, rewrites AccessBoxStructure, and
// publishes a new signed FileStatus -- none of that may interleave with a
// concurrent LoadFile/AppendToFile/CreateInvitation/another RevokeAccess
// on the same file, all of which also require File S or X. IsOwner is
// checked immediately after the namespace-only loadNamespaceEntry, before
// File X is even requested, because ownership is a property of the
// caller's own NamespaceEntry that no other user's operation can change --
// it needs no file-lock revalidation.
func (userdata *User) RevokeAccess(filename string, recipientUsername string) error {
	txn, err := allocateTxnID()
	if err != nil {
		return err
	}

	guard := newOperationGuard(txn)
	defer guard.ReleaseAll()

	nsResource, err := namespaceResourceID(userdata.Username, filename)
	if err != nil {
		return err
	}

	if err := guard.Acquire(nsResource, lockmanager.SharedLock); err != nil {
		return err
	}

	namespaceEntry, err := loadNamespaceEntry(userdata, filename)
	if err != nil {
		return err
	}

	if !namespaceEntry.IsOwner {
        return errors.New(
            "only the file owner can revoke access",
        )
    }

	fileResource := fileResourceID(namespaceEntry.FileID)
	if err := guard.Acquire(fileResource, lockmanager.ExclusiveLock); err != nil {
		return err
	}

	// Revalidated fresh, under File X -- never reuse AccessBox/FileStatus
	// state read before this lock was granted.
	currentAccessBox, err := validateFileAccessUnderLock(namespaceEntry)
	if err != nil {
		return err
	}

	//load the structure
	structure, err :=
        loadOwnerAccessBoxStructure(
            namespaceEntry,
            currentAccessBox,
        )
    if err != nil {
        return err
    }

	revokedRecord, exists :=
        structure.RecipientBoxes[recipientUsername]

    if !exists {
        return errors.New(
            "recipient is not a direct share",
        )
    }

	//load the metadata
	metadata, err := loadMetadata(currentAccessBox)
    if err != nil {
        return err
    }

	fileContent, oldChunkUUIDs, err :=
        loadFileContentAndChunkUUIDs(
            currentAccessBox,
            metadata,
        )
    if err != nil {
        return err
    }

	fireConcurrencyTestHook("revoke:content-loaded:" + currentAccessBox.FileID.String())

	//All clear, time to update the new access box for users
	newEpochID := uuid.New()
    newFileRoot := userlib.RandomBytes(
        symmetricKeySize,
    )

	newMetadataUUID := uuid.New()
    newMetadataEncKey := userlib.RandomBytes(
        symmetricKeySize,
    )
    newMetadataMACKey := userlib.RandomBytes(
        symmetricKeySize,
    )

	newBaseChunkUUID := uuid.New()

    newChunk := Chunk{
        FileID:   currentAccessBox.FileID,
        EpochID:  newEpochID,
        Index:    0,
        PrevUUID: uuid.Nil,
        Content:  fileContent,
    }

	newChunkEncKey, newChunkMACKey, err :=
        deriveChunkKeys(
            newFileRoot,
            newBaseChunkUUID,
        )
    if err != nil {
        return err
    }

	protectedNewChunk, err :=
        protectDatastoreObject(
            chunkObjectType,
            newBaseChunkUUID,
            newChunk,
            newChunkEncKey,
            newChunkMACKey,
        )
    if err != nil {
        return err
    }

	// Epoch rotation re-encrypts the same logical content under a new
	// epoch/FileRoot/keys -- it is a physical migration of authorization
	// state, not a logical content mutation, so Version is carried over
	// unchanged rather than advanced (only EpochID changes).
	newMetadata := Metadata{
        FileID:     currentAccessBox.FileID,
        EpochID:    newEpochID,
        Version:    metadata.Version,
        TailUUID:   newBaseChunkUUID,
        ChunkCount: 1,
    }

	protectedNewMetadata, err :=
        protectDatastoreObject(
            metadataObjectType,
            newMetadataUUID,
            newMetadata,
            newMetadataEncKey,
            newMetadataMACKey,
        )
    if err != nil {
        return err
    }

	newAccessBox := AccessBox{
        FileID:  currentAccessBox.FileID,
        EpochID: newEpochID,

        MetadataUUID:   newMetadataUUID,
        MetadataEncKey: newMetadataEncKey,
        MetadataMACKey: newMetadataMACKey,

        FileRoot: newFileRoot,

        //These remain stable
        AccessBoxStructureUUID: currentAccessBox.AccessBoxStructureUUID,
        StatusUUID: currentAccessBox.StatusUUID,
		OwnerVerifyKeyName: currentAccessBox.OwnerVerifyKeyName,
    }

	protectedOwnerAccessBox, err :=
        protectDatastoreObject(
            accessBoxObjectType,
            namespaceEntry.AccessBoxUUID,
            newAccessBox,
            namespaceEntry.AccessBoxEncKey,
            namespaceEntry.AccessBoxMACKey,
        )
    if err != nil {
        return err
    }

	type preparedBoxWrite struct {
        BoxUUID uuid.UUID
        Data    []byte
    }

	survivingWrites := make(
        []preparedBoxWrite,
        0,
    )

	for directRecipient, record := range structure.RecipientBoxes {
		if directRecipient == recipientUsername {
            // Do not give the revoked branch the new capability.
            continue
        }

		protectedBranchBox, err :=
            protectDatastoreObject(
                accessBoxObjectType,
                record.BoxUUID,
                newAccessBox,
                record.BoxEncKey,
                record.BoxMACKey,
            )
		
		if err != nil {
            return err
        }
		
		survivingWrites = append(
            survivingWrites,
            preparedBoxWrite{
                BoxUUID: record.BoxUUID,
                Data:    protectedBranchBox,
            },
        )
	}

	delete(
        structure.RecipientBoxes,
        recipientUsername,
    )

	structure.CurrentEpoch = newEpochID

	protectedUpdatedStructure, err :=
        protectDatastoreObject(
            accessBoxStructureObjectType,
            namespaceEntry.AccessBoxStructureUUID,
            structure,
            namespaceEntry.AccessBoxStructureEncKey,
            namespaceEntry.AccessBoxStructureMACKey,
        )
    if err != nil {
        return err
    }

	newFileStatus, err := createFileStatus(
        userdata,
        currentAccessBox.StatusUUID,
        newAccessBox,
    )
    if err != nil {
        return err
    }

	//Marshal it
	newFileStatusBytes, err :=
        json.Marshal(newFileStatus)
    if err != nil {
        return err
    }

	datastoreSet(
        newBaseChunkUUID,
        protectedNewChunk,
    )

    datastoreSet(
        newMetadataUUID,
        protectedNewMetadata,
    )

	datastoreSet(
        namespaceEntry.AccessBoxUUID,
        protectedOwnerAccessBox,
    )

	for _, write := range survivingWrites {
        datastoreSet(
            write.BoxUUID,
            write.Data,
        )
    }

	datastoreSet(
        namespaceEntry.AccessBoxStructureUUID,
        protectedUpdatedStructure,
    )

    datastoreSet(
        currentAccessBox.StatusUUID,
        newFileStatusBytes,
    )

	datastoreDelete(
        revokedRecord.BoxUUID,
    )

    datastoreDelete(
        currentAccessBox.MetadataUUID,
    )

    for _, oldChunkUUID := range oldChunkUUIDs {
        datastoreDelete(oldChunkUUID)
    }

    return nil
}
