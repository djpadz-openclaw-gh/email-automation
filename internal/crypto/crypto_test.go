package crypto

import (
	"testing"
)

func TestEncryptDecrypt(t *testing.T) {
	enc, err := NewEncryptor("test-master-key-for-unit-tests")
	if err != nil {
		t.Fatalf("NewEncryptor: %v", err)
	}

	tests := []struct {
		name      string
		plaintext string
		accountID int64
	}{
		{"simple password", "my-secret-password", 1},
		{"empty string", "", 1},
		{"unicode password", "pässwörd-日本語", 2},
		{"long password", "a-very-long-password-that-exceeds-the-block-size-of-aes-256-gcm-encryption", 3},
		{"special chars", "p@$$w0rd!#%^&*()", 42},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encrypted, err := enc.Encrypt(tt.plaintext, tt.accountID)
			if err != nil {
				t.Fatalf("Encrypt: %v", err)
			}

			if tt.plaintext == "" {
				if encrypted != "" {
					t.Fatalf("expected empty encrypted for empty plaintext, got %q", encrypted)
				}
				return
			}

			// Encrypted should differ from plaintext
			if encrypted == tt.plaintext {
				t.Fatal("encrypted should differ from plaintext")
			}

			// Should have version prefix
			if encrypted[:2] != "v1" {
				t.Fatalf("expected v1 prefix, got %q", encrypted[:2])
			}

			decrypted, err := enc.Decrypt(encrypted, tt.accountID)
			if err != nil {
				t.Fatalf("Decrypt: %v", err)
			}

			if decrypted != tt.plaintext {
				t.Fatalf("expected %q, got %q", tt.plaintext, decrypted)
			}
		})
	}
}

func TestVersionedFormat(t *testing.T) {
	enc, err := NewEncryptor("test-key")
	if err != nil {
		t.Fatalf("NewEncryptor: %v", err)
	}

	encrypted, err := enc.Encrypt("password123", 1)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	// Should start with "v1:"
	if len(encrypted) < 3 || encrypted[:3] != "v1:" {
		t.Fatalf("expected v1: prefix, got %q", encrypted)
	}
}

func TestDifferentAccountsDifferentCiphertext(t *testing.T) {
	enc, err := NewEncryptor("test-master-key")
	if err != nil {
		t.Fatalf("NewEncryptor: %v", err)
	}

	password := "same-password"
	enc1, _ := enc.Encrypt(password, 1)
	enc2, _ := enc.Encrypt(password, 2)

	if enc1 == enc2 {
		t.Fatal("same password encrypted for different accounts should produce different ciphertext")
	}
}

func TestWrongAccountCantDecrypt(t *testing.T) {
	enc, err := NewEncryptor("test-master-key")
	if err != nil {
		t.Fatalf("NewEncryptor: %v", err)
	}

	password := "secret"
	encrypted, _ := enc.Encrypt(password, 1)

	// Decrypting with wrong account ID should fail
	_, decErr := enc.Decrypt(encrypted, 2)
	if decErr == nil {
		t.Fatal("expected error decrypting with wrong account ID")
	}
}

func TestLegacyPlaintextPassthrough(t *testing.T) {
	enc, err := NewEncryptor("test-master-key")
	if err != nil {
		t.Fatalf("NewEncryptor: %v", err)
	}

	// A plaintext password that isn't valid base64 should pass through
	legacy := "my-plain-password"
	decrypted, err := enc.Decrypt(legacy, 1)
	if err != nil {
		t.Fatalf("Decrypt legacy: %v", err)
	}
	if decrypted != legacy {
		t.Fatalf("expected %q, got %q", legacy, decrypted)
	}
}

func TestEmptyMasterKey(t *testing.T) {
	_, err := NewEncryptor("")
	if err == nil {
		t.Fatal("expected error for empty master key")
	}
}

func TestKeyRotation(t *testing.T) {
	// Start with version 1
	enc1, err := NewVersionedEncryptor(map[int]string{
		1: "master-key-v1",
	}, 1)
	if err != nil {
		t.Fatalf("NewVersionedEncryptor v1: %v", err)
	}

	password := "my-secret"
	accountID := int64(42)

	// Encrypt with v1
	encrypted, err := enc1.Encrypt(password, accountID)
	if err != nil {
		t.Fatalf("Encrypt v1: %v", err)
	}

	if encrypted[:3] != "v1:" {
		t.Fatalf("expected v1: prefix, got %q", encrypted[:5])
	}

	// Create encryptor with both v1 and v2, current = v2
	enc2, err := NewVersionedEncryptor(map[int]string{
		1: "master-key-v1",
		2: "master-key-v2",
	}, 2)
	if err != nil {
		t.Fatalf("NewVersionedEncryptor v2: %v", err)
	}

	// Should still decrypt v1 ciphertext
	decrypted, err := enc2.Decrypt(encrypted, accountID)
	if err != nil {
		t.Fatalf("Decrypt v1 with v2 encryptor: %v", err)
	}
	if decrypted != password {
		t.Fatalf("expected %q, got %q", password, decrypted)
	}

	// New encryption should use v2
	encryptedV2, err := enc2.Encrypt(password, accountID)
	if err != nil {
		t.Fatalf("Encrypt v2: %v", err)
	}
	if encryptedV2[:3] != "v2:" {
		t.Fatalf("expected v2: prefix, got %q", encryptedV2[:5])
	}

	// v2 ciphertext should decrypt correctly
	decrypted2, err := enc2.Decrypt(encryptedV2, accountID)
	if err != nil {
		t.Fatalf("Decrypt v2: %v", err)
	}
	if decrypted2 != password {
		t.Fatalf("expected %q, got %q", password, decrypted2)
	}
}

func TestNeedsRotation(t *testing.T) {
	enc, err := NewVersionedEncryptor(map[int]string{
		1: "key-v1",
		2: "key-v2",
	}, 2)
	if err != nil {
		t.Fatalf("NewVersionedEncryptor: %v", err)
	}

	// v1 ciphertext needs rotation
	enc1, _ := NewEncryptor("key-v1")
	v1cipher, _ := enc1.Encrypt("password", 1)
	if !enc.NeedsRotation(v1cipher) {
		t.Fatal("v1 ciphertext should need rotation when current is v2")
	}

	// v2 ciphertext does not need rotation
	v2cipher, _ := enc.Encrypt("password", 1)
	if enc.NeedsRotation(v2cipher) {
		t.Fatal("v2 ciphertext should not need rotation")
	}

	// Empty string does not need rotation
	if enc.NeedsRotation("") {
		t.Fatal("empty string should not need rotation")
	}

	// Legacy plaintext needs rotation
	if !enc.NeedsRotation("plain-password") {
		t.Fatal("legacy plaintext should need rotation")
	}
}

func TestVersionedEncryptorValidation(t *testing.T) {
	// No keys
	_, err := NewVersionedEncryptor(map[int]string{}, 1)
	if err == nil {
		t.Fatal("expected error for empty keys")
	}

	// Current version not in keys
	_, err = NewVersionedEncryptor(map[int]string{1: "key"}, 2)
	if err == nil {
		t.Fatal("expected error for missing current version")
	}

	// Empty key value
	_, err = NewVersionedEncryptor(map[int]string{1: ""}, 1)
	if err == nil {
		t.Fatal("expected error for empty key value")
	}
}
