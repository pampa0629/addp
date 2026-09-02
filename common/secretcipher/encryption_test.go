package secretcipher

import "testing"

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	ciphertext, err := Encrypt("sensitive-value", key)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if ciphertext == "sensitive-value" {
		t.Fatal("Encrypt() returned plaintext")
	}
	plaintext, err := Decrypt(ciphertext, key)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if plaintext != "sensitive-value" {
		t.Fatalf("Decrypt() = %q, want sensitive-value", plaintext)
	}
}

func TestRejectsInvalidKeyLength(t *testing.T) {
	if _, err := Encrypt("value", []byte("short")); err == nil {
		t.Fatal("Encrypt() error = nil, want invalid key length")
	}
	if _, err := Decrypt("value", []byte("short")); err == nil {
		t.Fatal("Decrypt() error = nil, want invalid key length")
	}
}
