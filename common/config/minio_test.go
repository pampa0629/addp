package config

import "testing"

func TestLoadBuiltinMinIOConfigUsesCanonicalKeysOnly(t *testing.T) {
	t.Setenv("MINIO_ENDPOINT", "")
	t.Setenv("MINIO_API_PORT", "19009")
	t.Setenv("MINIO_ROOT_USER", "root-user")
	t.Setenv("MINIO_ROOT_PASSWORD", "root-password")
	t.Setenv("MINIO_ACCESS_KEY", "legacy-user")
	t.Setenv("MINIO_SECRET_KEY", "legacy-password")
	t.Setenv("MINIO_USE_SSL", "true")

	cfg := LoadBuiltinMinIOConfig()
	if cfg.Endpoint != "localhost:19009" {
		t.Fatalf("Endpoint = %q, want localhost:19009", cfg.Endpoint)
	}
	if cfg.AccessKey != "root-user" || cfg.SecretKey != "root-password" {
		t.Fatalf("credentials = %q/%q, want canonical root credentials", cfg.AccessKey, cfg.SecretKey)
	}
	if !cfg.UseSSL {
		t.Fatal("UseSSL = false, want true")
	}
}
