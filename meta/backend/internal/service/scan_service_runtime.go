package service

import (
	commonClient "github.com/addp/common/client"
	"github.com/addp/meta/internal/config"
	"gorm.io/gorm"
)

// NewRuntimeScanService assembles the scan runtime used by both the HTTP server
// and the asynchronous worker so indexing behavior cannot diverge by process.
func NewRuntimeScanService(db *gorm.DB, engineService *EngineService, cfg *config.Config, contentIndex *commonClient.ManagerContentClient) *ScanService {
	scanService := NewScanService(db, engineService)
	scanService.SetConfig(cfg)
	scanService.SetContentIndexClient(contentIndex)
	return scanService
}
