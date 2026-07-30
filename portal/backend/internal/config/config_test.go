package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadConfigUsesPortalServiceSecretAndHasNoLegacyInternalKey(t *testing.T) {
	projectRoot := t.TempDir()
	secret := "0123456789abcdef0123456789abcdef"
	contents := "PORTAL_SERVICE_CLIENT_SECRET=" + secret + "\nINTERNAL_API_KEY=legacy-key\n"
	if err := os.WriteFile(filepath.Join(projectRoot, ".env"), []byte(contents), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	t.Setenv("PROJECT_ROOT", projectRoot)

	cfg := LoadConfig()
	if cfg.ServiceClientSecret != secret {
		t.Fatalf("ServiceClientSecret = %q", cfg.ServiceClientSecret)
	}
	if _, exists := reflect.TypeOf(*cfg).FieldByName("InternalAPIKey"); exists {
		t.Fatal("legacy InternalAPIKey field remains in Portal config")
	}
}
