package models

import "time"

const EmbeddingVectorDimension = 2560

// EmbeddingConfiguration is Manager's platform-scoped embedding configuration.
// ID is fixed to one because the current product supports one active model.
type EmbeddingConfiguration struct {
	ID               uint      `gorm:"primaryKey;check:id = 1" json:"-"`
	Version          uint64    `gorm:"not null" json:"version"`
	MaxDistance      float64   `gorm:"not null" json:"max_distance"`
	MaxFileSizeMB    int       `gorm:"not null" json:"max_file_size_mb"`
	BatchConcurrency int       `gorm:"not null" json:"batch_concurrency"`
	UpdatedBy        uint      `gorm:"not null" json:"updated_by"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

const InferenceScenarioSemanticSearchEmbedding = "semantic_search_embedding"

type InferenceScenarioBinding struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	ScenarioCode   string    `gorm:"size:80;not null" json:"scenario_code"`
	ScopeType      string    `gorm:"size:16;not null" json:"scope_type"`
	TenantID       *uint     `gorm:"index" json:"tenant_id,omitempty"`
	ModelProfileID string    `gorm:"type:uuid;not null" json:"model_profile_id"`
	Version        uint64    `gorm:"not null" json:"version"`
	UpdatedBy      uint      `gorm:"not null" json:"updated_by"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (InferenceScenarioBinding) TableName() string {
	return "manager.inference_scenario_bindings"
}

func (EmbeddingConfiguration) TableName() string {
	return "manager.embedding_configuration"
}
