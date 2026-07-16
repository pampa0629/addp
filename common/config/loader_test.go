package config

import "testing"

func TestDevelopmentEncryptionKeyIsAES256Length(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "")
	key := LoadEncryptionKey()
	if len(key) != 32 {
		t.Fatalf("development encryption key length = %d, want 32", len(key))
	}
}
