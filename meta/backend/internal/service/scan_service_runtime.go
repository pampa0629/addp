package service

import (
	"github.com/addp/meta/internal/config"
	"github.com/addp/meta/internal/search"
	"gorm.io/gorm"
)

// NewRuntimeScanService assembles the scan runtime used by both the HTTP server
// and the asynchronous worker so indexing behavior cannot diverge by process.
func NewRuntimeScanService(db *gorm.DB, engineService *EngineService, cfg *config.Config) (*ScanService, *search.Indexer, error) {
	indexer, err := search.NewIndexer(cfg)
	if err != nil {
		return nil, nil, err
	}
	scanService := NewScanService(db, engineService)
	scanService.SetConfig(cfg)
	scanService.SetIndexer(indexer)
	return scanService, indexer, nil
}
