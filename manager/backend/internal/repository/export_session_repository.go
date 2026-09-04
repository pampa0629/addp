package repository

import (
	"github.com/addp/common/exportartifact"
	"gorm.io/gorm"
)

type ExportSessionRepository = exportartifact.GormStore

func NewExportSessionRepository(db *gorm.DB) *ExportSessionRepository {
	return exportartifact.NewGormStore(db, "manager.export_sessions")
}
