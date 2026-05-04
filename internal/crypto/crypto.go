package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"strconv"
	"strings"

	"golang.org/x/crypto/hkdf"
)

// Encryptor handles per-account password encryption using AES-256-GCM
// with HKDF-derived keys and versioned master keys for rotation.
type Encryptor struct {
	currentVersion int
	keys           map[int][]byte // version → hashed master key
}

// NewEncryptor creates a new Encryptor with a single master key (version 1).
func NewEncryptor(masterKey string) (*Encryptor, error) {
	if masterKey == "" {
		return nil, fmt.Errorf("encryption master key is required")
	}
	hash := sha256.Sum256([]byte(masterKey))
	return &Encryptor{
		currentVersion: 1,
		keys:           map[int][]byte{1: hash[:]},
	}, nil
}

// NewVersionedEncryptor creates an Encryptor that supports multiple key versions.
// keys maps version numbers to raw master key strings.
// currentVersion is the version used for new encryptions.
func NewVersionedEncryptor(keys map[int]string, currentVersion int) (*Encryptor, error) {
	if len(keys) == 0 {
		return nil, fmt.Errorf("at least one encryption key is required")
	}
	if _, ok := keys[currentVersion]; !ok {
		return nil, fmt.Errorf("current version %d not found in provided keys", currentVersion)
	}

	hashed := make(map[int][]byte, len(keys))
	for v, k := range keys {
		if k == "" {
			return nil, fmt.Errorf("encryption key for version %d is empty", v)
		}
		h := sha256.Sum256([]byte(k))
		hashed[v] = h[:]
	}

	return &Encryptor{
		currentVersion: currentVersion,
		keys:           hashed,
	}, nil
}

// CurrentVersion returns the version used for new encryptions.
func (e *Encryptor) CurrentVersion() int {
	return e.currentVersion
}

// deriveKey uses HKDF to derive a per-account 256-bit key from a versioned master key.
func (e *Encryptor) deriveKey(version int, accountID int64) ([]byte, error) {
	masterKey, ok := e.keys[version]
	if !ok {
		return nil, fmt.Errorf("unknown key version %d", version)
	}
	info := []byte("email-automation:account:" + strconv.FormatInt(accountID, 10))
	reader := hkdf.New(sha256.New, masterKey, nil, info)
	key := make([]byte, 32)
	if _, err := io.ReadFull(reader, key); err != nil {
		return nil, fmt.Errorf("derive key for account %d v%d: %w", accountID, version, err)
	}
	return key, nil
}

// Encrypt encrypts plaintext using the current master key version.
// Returns versioned ciphertext: "v<version>:<base64(nonce+ciphertext)>"
func (e *Encryptor) Encrypt(plaintext string, accountID int64) (string, error) {
	if plaintext == "" {
		return "", nil
	}

	key, err := e.deriveKey(e.currentVersion, accountID)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	encoded := base64.StdEncoding.EncodeToString(ciphertext)
	return fmt.Sprintf("v%d:%s", e.currentVersion, encoded), nil
}

// Decrypt decrypts versioned ciphertext using the appropriate master key.
// Supports formats:
//   - "v<N>:<base64>" — versioned encrypted password
//   - plain base64 — legacy unversioned (tries current key, falls back to plaintext)
//   - anything else — legacy plaintext passthrough
func (e *Encryptor) Decrypt(ciphertext string, accountID int64) (string, error) {
	if ciphertext == "" {
		return "", nil
	}

	// Check for versioned format: v<N>:<data>
	version, encoded, hasVersion := parseVersionedCiphertext(ciphertext)
	if hasVersion {
		return e.decryptWithVersion(encoded, version, accountID)
	}

	// Legacy: try base64 decode + decrypt with current key
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		// Not valid base64 — plaintext password
		return ciphertext, nil
	}

	result, err := e.decryptRaw(data, e.currentVersion, accountID)
	if err != nil {
		// Decryption failed — likely legacy plaintext that happened to be valid base64
		return ciphertext, nil
	}
	return result, nil
}

// NeedsRotation returns true if the ciphertext was encrypted with an older key version.
func (e *Encryptor) NeedsRotation(ciphertext string) bool {
	if ciphertext == "" {
		return false
	}
	version, _, hasVersion := parseVersionedCiphertext(ciphertext)
	if !hasVersion {
		// Legacy unversioned — needs rotation
		return true
	}
	return version != e.currentVersion
}

// decryptWithVersion decrypts base64-encoded data using a specific key version.
func (e *Encryptor) decryptWithVersion(encoded string, version int, accountID int64) (string, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("decode base64: %w", err)
	}
	return e.decryptRaw(data, version, accountID)
}

// decryptRaw decrypts raw bytes using a specific key version.
func (e *Encryptor) decryptRaw(data []byte, version int, accountID int64) (string, error) {
	key, err := e.deriveKey(version, accountID)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, encrypted := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, encrypted, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}

	return string(plaintext), nil
}

// parseVersionedCiphertext extracts version and encoded data from "v<N>:<data>".
func parseVersionedCiphertext(s string) (version int, encoded string, ok bool) {
	if !strings.HasPrefix(s, "v") {
		return 0, "", false
	}
	idx := strings.IndexByte(s, ':')
	if idx < 2 { // need at least "vN:"
		return 0, "", false
	}
	v, err := strconv.Atoi(s[1:idx])
	if err != nil {
		return 0, "", false
	}
	return v, s[idx+1:], true
}
