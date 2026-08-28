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
	if cfg.MeilisearchPublishedAssetIndex != "asset_published" {
		t.Fatalf("published Asset index = %q", cfg.MeilisearchPublishedAssetIndex)
	}
}

func TestLoadConfigUsesPublishedAssetSearchProjectionName(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
	t.Setenv("MEILISEARCH_PUBLISHED_ASSET_INDEX", "asset_published_test")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.MeilisearchPublishedAssetIndex != "asset_published_test" {
		t.Fatalf("published Asset index = %q", cfg.MeilisearchPublishedAssetIndex)
	}
}
