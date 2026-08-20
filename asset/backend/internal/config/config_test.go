package config

import "testing"

func TestLoadConfigLoadsSharedDeploymentFields(t *testing.T) {
	// ENCRYPTION_KEY 契约是 32 字节原始密钥的 Base64 编码。
	t.Setenv("ENCRYPTION_KEY", "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.DBHost == "" || cfg.DBPort == "" || cfg.DBUser == "" || cfg.DBName == "" {
		t.Fatalf("database config was not loaded: %#v", cfg.BaseConfig)
	}
}
