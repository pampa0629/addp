package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEnvKeepsInjectedDeploymentValue(t *testing.T) {
	projectRootMu.Lock()
	previousRoot := projectRoot
	projectRoot = ""
	projectRootMu.Unlock()
	t.Cleanup(func() {
		projectRootMu.Lock()
		projectRoot = previousRoot
		projectRootMu.Unlock()
	})

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("POSTGRES_PORT=15432\nREDIS_PORT=16379\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PROJECT_ROOT", root)
	t.Setenv("POSTGRES_PORT", "25432")
	t.Setenv("REDIS_PORT", "26379")
	LoadEnv()
	if got := os.Getenv("POSTGRES_PORT"); got != "25432" {
		t.Fatalf("POSTGRES_PORT = %q, want injected port 25432", got)
	}
	if got := os.Getenv("REDIS_PORT"); got != "26379" {
		t.Fatalf("REDIS_PORT = %q, want injected port 26379", got)
	}
}

func TestDevelopmentEncryptionKeyIsAES256Length(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "")
	key := LoadEncryptionKey()
	if len(key) != 32 {
		t.Fatalf("development encryption key length = %d, want 32", len(key))
	}
}
