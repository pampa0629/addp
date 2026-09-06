package models

import (
	"time"

	commonModels "github.com/addp/common/models"
	"gorm.io/gorm"
)

const (
	PPTXPDFArtifactVariant = "pdf_static"
	PPTXPDFStatusBuilding  = "building"
	PPTXPDFStatusReady     = "ready"
	PPTXPDFStatusFailed    = "failed"
	PPTXPDFStatusDeleted   = "deleted"
)

// PPTXPDFTask is the durable definition for one PPTX static-PDF preview.
// The stable semantic identity is tenant + item fingerprint + artifact variant;
// SourceVersion is the mutable snapshot used to decide freshness.
type PPTXPDFTask struct {
	ID       uint `gorm:"primaryKey" json:"id"`
	TenantID uint `gorm:"not null;index:idx_pptx_pdf_tasks_tenant" json:"tenant_id"`

	Name        string `gorm:"size:255;not null" json:"name"`
	Description string `gorm:"type:text" json:"description,omitempty"`
	Enabled     bool   `gorm:"not null" json:"enabled"`

	Schedule  string     `gorm:"size:255" json:"schedule,omitempty"`
	NextRunAt *time.Time `json:"next_run_at,omitempty"`

	ItemFingerprint string `gorm:"size:64;not null" json:"item_fingerprint"`
	ArtifactVariant string `gorm:"size:32;not null" json:"artifact_variant"`
	SourceEngineID  uint   `gorm:"not null" json:"source_engine_id"`
	ItemID          uint   `gorm:"not null" json:"item_id"`
	Locator         string `gorm:"type:text;not null" json:"locator"`
	SourceVersion   string `gorm:"size:128;not null" json:"source_version"`
	SourceSizeBytes int64  `gorm:"not null;default:0" json:"source_size_bytes"`

	LastRunAt           *time.Time           `json:"last_run_at,omitempty"`
	LastExecutionID     *string              `gorm:"size:36;index:idx_pptx_pdf_tasks_last_execution" json:"last_execution_id,omitempty"`
	LastExecutionStatus *string              `gorm:"size:50" json:"last_execution_status,omitempty"`
	Config              commonModels.JSONMap `gorm:"type:jsonb;not null;default:'{}'" json:"config"`
	CreatedBy           *uint                `json:"created_by,omitempty"`

	CreatedAt time.Time      `gorm:"autoCreateTime;not null" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime;not null" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index:idx_pptx_pdf_tasks_deleted_at" json:"-"`
}

func (PPTXPDFTask) TableName() string { return "manager.pptx_pdf_tasks" }

// PPTXPDF is the Manager-owned static preview artifact. It is not a Meta item.
type PPTXPDF struct {
	ID              uint   `gorm:"primaryKey" json:"id"`
	TenantID        uint   `gorm:"not null;index:idx_pptx_pdf_tenant_fingerprint,priority:1" json:"tenant_id"`
	ItemFingerprint string `gorm:"size:64;not null;index:idx_pptx_pdf_tenant_fingerprint,priority:2" json:"item_fingerprint"`
	ArtifactVariant string `gorm:"size:32;not null" json:"artifact_variant"`
	SourceVersion   string `gorm:"size:128;not null" json:"source_version"`
	SourceEngineID  uint   `gorm:"not null" json:"source_engine_id"`
	ItemID          uint   `gorm:"not null" json:"item_id"`
	Locator         string `gorm:"type:text;not null" json:"locator"`

	TaskID          *uint                `gorm:"index:idx_pptx_pdf_task" json:"task_id,omitempty"`
	LastExecutionID *string              `gorm:"size:36;index:idx_pptx_pdf_execution" json:"last_execution_id,omitempty"`
	StorageRef      string               `gorm:"type:text;not null" json:"storage_ref"`
	FileName        string               `gorm:"size:512;not null" json:"file_name"`
	SizeBytes       int64                `gorm:"not null" json:"size_bytes"`
	PageCount       int                  `gorm:"not null" json:"page_count"`
	ContentURL      string               `gorm:"type:text" json:"content_url,omitempty"`
	Status          string               `gorm:"size:32;not null" json:"status"`
	Metadata        commonModels.JSONMap `gorm:"type:jsonb;not null;default:'{}'" json:"metadata"`
	ErrorMessage    string               `gorm:"type:text" json:"error_message,omitempty"`
	CreatedBy       *uint                `json:"created_by,omitempty"`

	CreatedAt time.Time      `gorm:"autoCreateTime;not null" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime;not null" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index:idx_pptx_pdf_deleted_at" json:"-"`
}

func (PPTXPDF) TableName() string { return "manager.pptx_pdf" }
