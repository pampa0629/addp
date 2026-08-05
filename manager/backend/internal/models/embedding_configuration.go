package models

import "time"

const EmbeddingVectorDimension = 2560

// EmbeddingConfiguration is Manager's platform-scoped embedding configuration.
// ID is fixed to one because the current product supports one active model.
type EmbeddingConfiguration struct {
	ID               uint      `gorm:"primaryKey;check:id = 1" json:"-"`
	Version          uint64    `gorm:"not null" json:"version"`
	BaseURL          string    `gorm:"type:text;not null" json:"base_url"`
	Model            string    `gorm:"size:255;not null" json:"model"`
	TimeoutSeconds   int       `gorm:"not null" json:"timeout_seconds"`
	MaxDistance      float64   `gorm:"not null" json:"max_distance"`
	MaxFileSizeMB    int       `gorm:"not null" json:"max_file_size_mb"`
	BatchConcurrency int       `gorm:"not null" json:"batch_concurrency"`
	UpdatedBy        uint      `gorm:"not null" json:"updated_by"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

func (EmbeddingConfiguration) TableName() string {
	return "manager.embedding_configuration"
}
