package service

import (
	"context"
	"testing"
	"time"

	"github.com/addp/common/embedding"
	enginePlugin "github.com/addp/common/engine/plugin"
	commonInference "github.com/addp/common/inference"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestEmbeddingStateForCurrentItemDerivesOutdatedReasons(t *testing.T) {
	now := time.Now()
	size := int64(128)
	item := commonModels.MetaItem{
		ID:            9,
		EngineID:      3,
		FullName:      "bucket/data.csv",
		DataUpdatedAt: &now,
		SizeBytes:     &size,
	}
	runtime := EffectiveEmbeddingConfiguration{Dimension: 1024}
	svc := &EmbeddingService{configurationProvider: NewEmbeddingConfigurationProvider(runtime)}
	profileID := "11111111-1111-1111-1111-111111111111"
	deploymentID := "22222222-2222-2222-2222-222222222222"

	currentSourceVersion := sourceVersionForItem(commonModels.GenerateItemFingerprint(item.EngineID, item.FullName), item)

	tests := []struct {
		name       string
		state      *models.Embedding
		wantStatus string
		wantReason string
	}{
		{
			name: "source changed",
			state: &models.Embedding{
				Status:         models.EmbeddingStatusReady,
				SourceVersion:  "old-source",
				ModelProfileID: profileID,
				ProfileVersion: 2,
				DeploymentID:   deploymentID,
				Dimension:      1024,
			},
			wantStatus: models.EmbeddingStatusOutdated,
			wantReason: models.EmbeddingReasonSourceChanged,
		},
		{
			name: "model changed",
			state: &models.Embedding{
				Status:         models.EmbeddingStatusReady,
				SourceVersion:  currentSourceVersion,
				ModelProfileID: "33333333-3333-3333-3333-333333333333",
				ProfileVersion: 2,
				DeploymentID:   deploymentID,
				Dimension:      1024,
			},
			wantStatus: models.EmbeddingStatusOutdated,
			wantReason: models.EmbeddingReasonModelChanged,
		},
		{
			name: "dimension changed",
			state: &models.Embedding{
				Status:         models.EmbeddingStatusReady,
				SourceVersion:  currentSourceVersion,
				ModelProfileID: profileID,
				ProfileVersion: 2,
				DeploymentID:   deploymentID,
				Dimension:      768,
			},
			wantStatus: models.EmbeddingStatusOutdated,
			wantReason: models.EmbeddingReasonDimensionChanged,
		},
		{
			name: "ready and current",
			state: &models.Embedding{
				Status:         models.EmbeddingStatusReady,
				SourceVersion:  currentSourceVersion,
				ModelProfileID: profileID,
				ProfileVersion: 2,
				DeploymentID:   deploymentID,
				Dimension:      1024,
			},
			wantStatus: models.EmbeddingStatusReady,
			wantReason: "",
		},
		{
			name: "failed state is returned as-is",
			state: &models.Embedding{
				Status:       models.EmbeddingStatusFailed,
				StatusReason: models.EmbeddingReasonReadFailed,
			},
			wantStatus: models.EmbeddingStatusFailed,
			wantReason: models.EmbeddingReasonReadFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalStatus := tt.state.Status
			got := svc.embeddingStateForCurrentItem(item, tt.state, profileID, 2, deploymentID)
			if got.Status != tt.wantStatus || got.StatusReason != tt.wantReason {
				t.Fatalf("state = %s/%s, want %s/%s", got.Status, got.StatusReason, tt.wantStatus, tt.wantReason)
			}
			if originalStatus == models.EmbeddingStatusReady && got.Status == models.EmbeddingStatusOutdated && tt.state.Status != originalStatus {
				t.Fatalf("original state was mutated")
			}
		})
	}
}

func TestProcessItemSkipsCurrentReadyStateBeforeReadingSource(t *testing.T) {
	db := newEmbeddingServiceTestDB(t)
	repo := repository.NewEmbeddingRepository(db)
	runtime := EffectiveEmbeddingConfiguration{Dimension: 3, MaxFileSizeMB: 10}
	svc := &EmbeddingService{
		vectorRepo:            repo,
		configurationProvider: NewEmbeddingConfigurationProvider(runtime),
	}

	updatedAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	size := int64(12)
	item := commonModels.MetaItem{
		ID:            7,
		EngineID:      3,
		ItemType:      "object",
		Name:          "current.txt",
		FullName:      "bucket/current.txt",
		DataUpdatedAt: &updatedAt,
		SizeBytes:     &size,
	}
	itemFingerprint := commonModels.GenerateItemFingerprint(item.EngineID, item.FullName)
	sourceVersion := sourceVersionForItem(itemFingerprint, item)
	executionID := "exec-current"
	now := time.Now()
	if err := db.Exec(`
		INSERT INTO manager.embeddings
			(tenant_id, item_fingerprint, item_id, engine_id, locator, source_version, embedding, model_profile_id, profile_version, deployment_id, dimension, status, status_reason, last_execution_id, vectorized_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		1,
		itemFingerprint,
		item.ID,
		item.EngineID,
		"addp://engine/3/path/bucket/current.txt?type=object&item_id=7",
		sourceVersion,
		"[0.1,0.2,0.3]",
		"11111111-1111-1111-1111-111111111111",
		2,
		"22222222-2222-2222-2222-222222222222",
		3,
		models.EmbeddingStatusReady,
		models.EmbeddingReasonReady,
		executionID,
		now,
		now,
		now,
	).Error; err != nil {
		t.Fatalf("seed embedding state: %v", err)
	}

	outcome := svc.processItem(context.Background(), 1, item, &EmbeddingExecutionContext{
		ExecutionID: "exec-repeat", TenantID: 1, StartedAt: time.Now(), Runtime: runtime,
		Binding: ResolvedInferenceScenarioBinding{ModelProfileID: "11111111-1111-1111-1111-111111111111"},
		Profile: commonInference.ResolveProfileResponse{ProfileVersion: 2, DeploymentID: "22222222-2222-2222-2222-222222222222"},
		client:  failingEmbeddingClient{t: t},
	})
	if outcome != "ready_skipped" {
		t.Fatalf("processItem outcome = %q, want ready_skipped", outcome)
	}
	state, err := repo.GetByItemFingerprint(context.Background(), 1, itemFingerprint)
	if err != nil {
		t.Fatalf("load embedding state: %v", err)
	}
	if state == nil || state.Status != models.EmbeddingStatusReady || state.LastExecutionID == nil || *state.LastExecutionID != executionID {
		t.Fatalf("state was unexpectedly changed: %#v", state)
	}
}

func TestDetectSupportedModalityRequiresSupportedObjectFormat(t *testing.T) {
	svc := &EmbeddingService{}
	tests := []struct {
		name        string
		contentType string
		objectKey   string
		wantOK      bool
		want        embedding.Modality
	}{
		{
			name:        "image extension supported even when content type is generic",
			contentType: "application/octet-stream",
			objectKey:   "bucket/photo.jpg",
			wantOK:      true,
			want:        embedding.ModalityImage,
		},
		{
			name:        "plain text sidecar is not vectorized",
			contentType: "text/plain",
			objectKey:   "bucket/srtm_40_01.tfw",
			wantOK:      false,
		},
		{
			name:        "aux xml sidecar is not vectorized",
			contentType: "text/xml",
			objectKey:   "bucket/srtm_40_01.tif.aux.xml",
			wantOK:      false,
		},
		{
			name:        "csv text is supported",
			contentType: "text/plain",
			objectKey:   "bucket/test.csv",
			wantOK:      true,
			want:        embedding.ModalityText,
		},
		{
			name:        "unknown extension is not vectorized",
			contentType: "application/octet-stream",
			objectKey:   "bucket/data.bin",
			wantOK:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := svc.detectSupportedModality(tt.contentType, tt.objectKey)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && got != tt.want {
				t.Fatalf("modality = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestCatalogModelForEmbeddingItemSupportsObjectAndFileCatalogs(t *testing.T) {
	tests := []struct {
		name     string
		itemType string
		wantRoot string
	}{
		{name: "object storage item", itemType: "object", wantRoot: enginePlugin.CatalogTermService},
		{name: "file storage item", itemType: "file", wantRoot: enginePlugin.CatalogTermRoot},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := catalogModelForEmbeddingItem(commonModels.MetaItem{ItemType: tt.itemType})
			if got.RootTerm != tt.wantRoot {
				t.Fatalf("RootTerm = %s, want %s", got.RootTerm, tt.wantRoot)
			}
		})
	}
}

func newEmbeddingServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS manager").Error; err != nil {
		t.Fatalf("attach manager schema: %v", err)
	}
	if err := db.Exec(`CREATE TABLE manager.embeddings (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
		item_fingerprint TEXT NOT NULL,
		item_id INTEGER NOT NULL,
		engine_id INTEGER NOT NULL,
		locator TEXT NOT NULL,
		source_version TEXT NOT NULL,
		embedding TEXT,
		model_profile_id TEXT NOT NULL,
		profile_version INTEGER NOT NULL,
		deployment_id TEXT NOT NULL,
		dimension INTEGER NOT NULL,
		status TEXT NOT NULL,
		status_reason TEXT,
		error_message TEXT,
		last_execution_id TEXT,
		vectorized_at DATETIME,
		created_at DATETIME,
		updated_at DATETIME,
		UNIQUE (tenant_id, item_fingerprint)
	)`).Error; err != nil {
		t.Fatalf("create embeddings table: %v", err)
	}
	return db
}

type failingEmbeddingClient struct {
	t *testing.T
}

func (c failingEmbeddingClient) ResolveProfile(context.Context, commonInference.ResolveProfileRequest) (*commonInference.ResolveProfileResponse, error) {
	c.t.Fatalf("ResolveProfile should not be called for current ready embedding")
	return nil, nil
}

func (c failingEmbeddingClient) Embed(context.Context, commonInference.EmbeddingRequest) (*commonInference.EmbeddingResponse, error) {
	c.t.Fatalf("Embed should not be called for current ready embedding")
	return nil, nil
}
