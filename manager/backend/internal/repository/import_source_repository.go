package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/addp/common/client"
	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
)

// migrateImportSources prepares the Manager upload facts with native storage types.
func migrateImportSources(db *gorm.DB) error {
	switch db.Dialector.Name() {
	case "postgres":
		return db.AutoMigrate(&models.ImportSource{})
	case "mysql":
		return db.Exec(`CREATE TABLE IF NOT EXISTS manager.import_sources (
			upload_uuid CHAR(36) CHARACTER SET ascii COLLATE ascii_general_ci NOT NULL PRIMARY KEY,
			tenant_id BIGINT UNSIGNED NOT NULL CHECK (tenant_id > 0),
			uploader_id BIGINT NOT NULL CHECK (uploader_id > 0),
			source_locator TEXT NOT NULL CHECK (source_locator <> ''),
			transfer_task_id BIGINT UNSIGNED UNIQUE CHECK (transfer_task_id IS NULL OR transfer_task_id > 0),
			files JSON,
			created_at DATETIME(6) NOT NULL,
			INDEX idx_import_sources_tenant_id (tenant_id),
			CHECK (CHAR_LENGTH(upload_uuid) = 36 AND IS_UUID(upload_uuid))
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin`).Error
	default:
		return fmt.Errorf("unsupported Manager import source database %q", db.Dialector.Name())
	}
}

type ImportSourceRepository struct{ db *gorm.DB }

func NewImportSourceRepository(db *gorm.DB) *ImportSourceRepository {
	return &ImportSourceRepository{db: db}
}
func (r *ImportSourceRepository) Create(ctx context.Context, source *models.ImportSource) error {
	return r.db.WithContext(ctx).Create(source).Error
}
func (r *ImportSourceRepository) BindTask(ctx context.Context, uploadUUID string, tenantID uint, taskID uint) error {
	if taskID == 0 {
		return errors.New("invalid import task")
	}
	result := r.db.WithContext(ctx).Model(&models.ImportSource{}).Where("upload_uuid = ? AND tenant_id = ? AND transfer_task_id IS NULL", uploadUUID, tenantID).Update("transfer_task_id", taskID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
func (r *ImportSourceRepository) Verify(ctx context.Context, tenantID uint, request client.ManagerImportSourceRequest) (*client.ManagerImportSource, error) {
	var source models.ImportSource
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND uploader_id = ? AND transfer_task_id = ? AND source_locator = ?", tenantID, request.PrincipalID, request.TaskID, request.SourceLocator).First(&source).Error
	if err != nil {
		return nil, err
	}
	return &client.ManagerImportSource{UploadID: source.UploadUUID, Files: source.Files, TenantID: source.TenantID, UploaderID: source.UploaderID, TaskID: *source.TransferTaskID, SourceLocator: source.SourceLocator}, nil
}
