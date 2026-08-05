package service

import (
	"context"
	"errors"
	"testing"

	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestEmbeddingConfigurationDefaultsAndVersionedUpdate(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:embedding-configuration?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`ATTACH DATABASE ':memory:' AS manager`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.EmbeddingConfiguration{}); err != nil {
		t.Fatal(err)
	}
	service := NewEmbeddingConfigurationService(repository.NewEmbeddingConfigurationRepository(db), "deployment-secret")
	if err := service.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}

	defaults, err := service.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if defaults.Persisted || defaults.Version != 0 || defaults.Dimension != models.EmbeddingVectorDimension || !defaults.APIKeyConfigured {
		t.Fatalf("defaults = %#v", defaults)
	}

	input := UpdateEmbeddingConfigurationInput{
		Version: 0, BaseURL: "https://embedding.example.com", Model: "qwen3-vl-embedding",
		TimeoutSeconds: 20, MaxDistance: 0.7, MaxFileSizeMB: 20, BatchConcurrency: 4,
	}
	updated, err := service.Update(context.Background(), input, 42)
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Persisted || updated.Version != 1 || service.Provider().Current().Version != 1 {
		t.Fatalf("updated = %#v, runtime = %#v", updated, service.Provider().Current())
	}
	if _, err := service.Update(context.Background(), input, 43); !errors.Is(err, repository.ErrEmbeddingConfigurationVersionConflict) {
		t.Fatalf("stale update error = %v, want version conflict", err)
	}
}
