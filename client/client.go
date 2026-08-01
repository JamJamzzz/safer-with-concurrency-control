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
	_ "strings"

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
	envelopeBytes, exists := userlib.DatastoreGet(accountUUID)

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
type Metadata struct {
	FileID uuid.UUID
	EpochID uuid.UUID
	TailUUID uuid.UUID
	ChunkCount uint64
}

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
func getNameSpaceEntryUUID (username string, filename string,) (uuid.UUID, error) {
	nameHash := userlib.Hash(
		[]byte(username + "-" + filename),
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
func (userdata *User) storeNewFile(
	filename string,
	content []byte,
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

	//Generate every random ids:
	fileID := uuid.New()
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

	userlib.DatastoreSet(
		baseChunkUUID,
		protectedChunk,
	)

	userlib.DatastoreSet(
		metadataUUID,
		protectedMetadata,
	)

	userlib.DatastoreSet(
		ownerAccessBoxUUID,
		protectedAccessBox,
	)

	userlib.DatastoreSet(
		statusUUID,
		fileStatusBytes,
	)

	userlib.DatastoreSet(
		structureUUID,
		protectedAccessBoxStructure,
	)

	userlib.DatastoreSet(
		nameUUID,
		protectedNamespaceEntry,
	)

	return nil
}

/** 
If the file already exists, we rewrite the file
**/

func (userdata *User) overwriteExistingFile(
	filename string,
	content []byte,
) error {
	 _, accessBox, err := resolveFile(
        userdata,
        filename,
    )
    if err != nil {
        return err
    }
	metadata, err := loadMetadata(accessBox)
    if err != nil {
        return err
    }
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
	newMetadata := Metadata{
        FileID:     accessBox.FileID,
        EpochID:    accessBox.EpochID,
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
	userlib.DatastoreSet(
        newBaseChunkUUID,
        protectedNewChunk,
    )

    userlib.DatastoreSet(
        accessBox.MetadataUUID,
        protectedNewMetadata,
    )
	for _, oldChunkUUID := range oldChunkUUIDs {
        userlib.DatastoreDelete(oldChunkUUID)
    }

    return nil

}

func (userdata *User) StoreFile(filename string, content []byte) (err error) {
    if userdata == nil {
        return errors.New("user cannot be nil")
    }

    nameUUID, err := getNameSpaceEntryUUID(
		userdata.Username,
		filename,
	)

	if err != nil {
        return err
    }

	_, exists := userlib.DatastoreGet(nameUUID)

	if exists {
		return userdata.overwriteExistingFile(
			filename,
			content,
		)
	}

	return userdata.storeNewFile(filename, content)
}

func (userdata *User) AppendToFile(filename string, content []byte) error {
	_, accessBox, err := resolveFile(
		userdata,
		filename,
	)

	if err != nil {
		return err 
	}

	metadata, err := loadMetadata(accessBox)
	if err != nil {
		return err 
	}

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

	updatedMetadata := Metadata{
        FileID:     metadata.FileID,
        EpochID:    metadata.EpochID,
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

	userlib.DatastoreSet(
        newChunkUUID,
        protectedNewChunk,
    )

    userlib.DatastoreSet(
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

    envelopeBytes, exists := userlib.DatastoreGet(objectUUID)
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

	statusBytes, exists := userlib.DatastoreGet(
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

	verifyKey, exists := userlib.KeystoreGet(
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

func resolveFile(
	userdata *User,
	filename string,
) (
	NamespaceEntry,
	AccessBox,
	error,
) {
	var emptyEntry NamespaceEntry
    var emptyAccessBox AccessBox

	if userdata == nil {
        return emptyEntry, emptyAccessBox,
            errors.New("user cannot be nil")
    }

    nameUUID, err := getNameSpaceEntryUUID(
        userdata.Username,
        filename,
    )
    if err != nil {
        return emptyEntry, emptyAccessBox, err
    }

    namespaceEncKey, namespaceMACKey, err :=
        deriveNamespaceEntryKeys(
            userdata.NamespaceRoot,
            filename,
        )
    if err != nil {
        return emptyEntry, emptyAccessBox, err
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
        return emptyEntry, emptyAccessBox, err
    }

	if namespaceEntry.FileID == uuid.Nil {
        return emptyEntry, emptyAccessBox,
            errors.New("invalid namespace FileID")
    }

    if namespaceEntry.AccessBoxUUID == uuid.Nil {
        return emptyEntry, emptyAccessBox,
            errors.New("invalid access box UUID")
    }

    if len(namespaceEntry.AccessBoxEncKey) !=
        symmetricKeySize {
        return emptyEntry, emptyAccessBox,
            errors.New("invalid access box encryption key")
    }

    if len(namespaceEntry.AccessBoxMACKey) !=
        symmetricKeySize {
        return emptyEntry, emptyAccessBox,
            errors.New("invalid access box MAC key")
    }

	var accessBox AccessBox

    err = loadDatastoreObject(
        accessBoxObjectType,
        namespaceEntry.AccessBoxUUID,
        namespaceEntry.AccessBoxEncKey,
        namespaceEntry.AccessBoxMACKey,
        &accessBox,
    )
    if err != nil {
        return emptyEntry, emptyAccessBox, err
    }

    if accessBox.FileID != namespaceEntry.FileID {
        return emptyEntry, emptyAccessBox,
            errors.New("access box FileID mismatch")
    }

    if accessBox.EpochID == uuid.Nil {
        return emptyEntry, emptyAccessBox,
            errors.New("invalid access box epoch")
    }

    if accessBox.MetadataUUID == uuid.Nil {
        return emptyEntry, emptyAccessBox,
            errors.New("invalid metadata UUID")
    }

    if len(accessBox.MetadataEncKey) != symmetricKeySize ||
        len(accessBox.MetadataMACKey) != symmetricKeySize {
        return emptyEntry, emptyAccessBox,
            errors.New("invalid metadata keys")
    }

    if len(accessBox.FileRoot) != symmetricKeySize {
        return emptyEntry, emptyAccessBox,
            errors.New("invalid file root")
    }

    if accessBox.AccessBoxStructureUUID == uuid.Nil {
        return emptyEntry, emptyAccessBox,
            errors.New("invalid access box structure UUID")
    }

    if accessBox.StatusUUID == uuid.Nil {
        return emptyEntry, emptyAccessBox,
            errors.New("invalid status UUID")
    }

    if accessBox.OwnerVerifyKeyName == "" {
        return emptyEntry, emptyAccessBox,
            errors.New("missing owner verification key name")
    }

    err = verifyFileStatus(accessBox)
    if err != nil {
        return emptyEntry, emptyAccessBox, err
    }

    return namespaceEntry, accessBox, nil
}

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

func (userdata *User) LoadFile(filename string) (content []byte, err error) {
	_, accessBox, err := resolveFile(
		userdata,
		filename,
	)

	if err != nil {
		return nil, err
	}

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

func (userdata *User) CreateInvitation(filename string, recipientUsername string) (
	invitationPtr uuid.UUID, err error) {
	namespaceEntry, accessBox, err := resolveFile(
		userdata,
		filename,
	)

	if err != nil {
        return uuid.Nil, err
    }

	recipientPKEKey, exists := userlib.KeystoreGet(
        getPKEKeyName(recipientUsername),
    )
    if !exists || recipientPKEKey.KeyType != "PKE" {
        return uuid.Nil,
            errors.New("recipient does not exist")
    }

    recipientVerifyKey, exists :=
        userlib.KeystoreGet(
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
    var branchBoxUUID uuid.UUID

	if namespaceEntry.IsOwner {
        structure, err :=
            loadOwnerAccessBoxStructure(
                namespaceEntry,
                accessBox,
            )
        if err != nil {
            return uuid.Nil, err
        }

		//Create the 
		branchBoxUUID = uuid.New()
        branchBoxEncKey := userlib.RandomBytes(
            symmetricKeySize,
        )
        branchBoxMACKey := userlib.RandomBytes(
            symmetricKeySize,
        )

		protectedBranchBox, err = protectDatastoreObject(
			accessBoxObjectType,
			branchBoxUUID,
			accessBox,
			branchBoxEncKey,
			branchBoxMACKey,
		)

		if err != nil {
            return uuid.Nil, err
        }

		structure.RecipientBoxes[recipientUsername] = BranchBoxRecord{
			BoxUUID: branchBoxUUID,
			BoxEncKey: branchBoxEncKey,
			BoxMACKey: branchBoxMACKey,
		}

		protectedUpdatedStructure, err = protectDatastoreObject(
			accessBoxStructureObjectType,
			accessBox.AccessBoxStructureUUID,
			structure,
			namespaceEntry.AccessBoxStructureEncKey,
			namespaceEntry.AccessBoxStructureMACKey,
		)

		if err != nil {
            return uuid.Nil, err
        }

		grantedBoxUUID = branchBoxUUID
		grantedBoxEncKey = branchBoxEncKey
		grantedBoxMACKey = branchBoxMACKey
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
        userlib.DatastoreSet(
            branchBoxUUID,
            protectedBranchBox,
        )

        userlib.DatastoreSet(
            namespaceEntry.AccessBoxStructureUUID,
            protectedUpdatedStructure,
        )
    }

	userlib.DatastoreSet(
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
        userlib.DatastoreGet(invitationPtr)

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
        userlib.KeystoreGet(
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

func (userdata *User) AcceptInvitation(senderUsername string, invitationPtr uuid.UUID, filename string) error {
	if userdata == nil {
        return errors.New("recipient cannot be nil")
    }

	nameUUID, err := getNameSpaceEntryUUID(
        userdata.Username,
        filename,
    )
    if err != nil {
        return err
    }

	_, occupied := userlib.DatastoreGet(nameUUID)

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

	var accessBox AccessBox

    err = loadDatastoreObject(
        accessBoxObjectType,
        payload.AccessBoxUUID,
        payload.AccessBoxEncKey,
        payload.AccessBoxMACKey,
        &accessBox,
    )
    if err != nil {
        return err
    }

    if accessBox.FileID != payload.FileID {
        return errors.New(
            "invitation and AccessBox FileID mismatch",
        )
    }

	err = verifyFileStatus(accessBox)
    if err != nil {
        return err
    }

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

	userlib.DatastoreSet(
        nameUUID,
        protectedNamespaceEntry,
    )

    userlib.DatastoreDelete(invitationPtr)

    return nil
}

func (userdata *User) RevokeAccess(filename string, recipientUsername string) error {
	//First get the current namespace entry and accessbox
	namespaceEntry, currentAccessBox, err := resolveFile(
		userdata,
		filename,
	)

	if err != nil {
        return err
    }

	if !namespaceEntry.IsOwner {
        return errors.New(
            "only the file owner can revoke access",
        )
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

	newMetadata := Metadata{
        FileID:     currentAccessBox.FileID,
        EpochID:    newEpochID,
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

	userlib.DatastoreSet(
        newBaseChunkUUID,
        protectedNewChunk,
    )

    userlib.DatastoreSet(
        newMetadataUUID,
        protectedNewMetadata,
    )

	userlib.DatastoreSet(
        namespaceEntry.AccessBoxUUID,
        protectedOwnerAccessBox,
    )

	for _, write := range survivingWrites {
        userlib.DatastoreSet(
            write.BoxUUID,
            write.Data,
        )
    }

	userlib.DatastoreSet(
        namespaceEntry.AccessBoxStructureUUID,
        protectedUpdatedStructure,
    )

    userlib.DatastoreSet(
        currentAccessBox.StatusUUID,
        newFileStatusBytes,
    )

	userlib.DatastoreDelete(
        revokedRecord.BoxUUID,
    )

    userlib.DatastoreDelete(
        currentAccessBox.MetadataUUID,
    )

    for _, oldChunkUUID := range oldChunkUUIDs {
        userlib.DatastoreDelete(oldChunkUUID)
    }

    return nil
}