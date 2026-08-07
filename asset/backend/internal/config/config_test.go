package config

import "testing"

func TestLoadConfigLoadsSharedDeploymentFields(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.DBHost == "" || cfg.DBPort == "" || cfg.DBUser == "" || cfg.DBName == "" {
		t.Fatalf("database config was not loaded: %#v", cfg.BaseConfig)
	}
}
