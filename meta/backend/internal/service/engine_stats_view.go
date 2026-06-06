package service

import (
	"fmt"
	"time"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
	"gorm.io/gorm"
)

type engineScanStats struct {
	totalCount   map[uint]int64
	scannedCount map[uint]int64
	lastScanByID map[uint]*time.Time
}

func loadEngineScanStats(db *gorm.DB, engineIDs []uint) (*engineScanStats, error) {
	stats := &engineScanStats{
		totalCount:   make(map[uint]int64),
		scannedCount: make(map[uint]int64),
		lastScanByID: make(map[uint]*time.Time),
	}
	if len(engineIDs) == 0 {
		return stats, nil
	}

	type countRow struct {
		EngineID uint
		Count    int64
	}
	var totals []countRow
	if err := db.Table("meta.meta_node").
		Where("engine_id IN ? AND parent_node_id IN (?)", engineIDs,
			db.Table("meta.meta_node").
				Select("id").
				Where("engine_id IN ? AND parent_node_id IS NULL AND full_name = ?", engineIDs, ""),
		).
		Select("engine_id, COUNT(*) AS count").
		Group("engine_id").
		Scan(&totals).Error; err != nil {
		return nil, fmt.Errorf("failed to count meta nodes: %w", err)
	}
	for _, row := range totals {
		stats.totalCount[row.EngineID] = row.Count
	}

	var scanned []countRow
	if err := db.Table("meta.meta_node").
		Where("engine_id IN ? AND scan_status = ? AND parent_node_id IN (?)", engineIDs, "completed",
			db.Table("meta.meta_node").
				Select("id").
				Where("engine_id IN ? AND parent_node_id IS NULL AND full_name = ?", engineIDs, ""),
		).
		Select("engine_id, COUNT(*) AS count").
		Group("engine_id").
		Scan(&scanned).Error; err != nil {
		return nil, fmt.Errorf("failed to count scanned nodes: %w", err)
	}
	for _, row := range scanned {
		stats.scannedCount[row.EngineID] = row.Count
	}

	type lastScanRow struct {
		EngineID   uint
		LastScanAt *time.Time `gorm:"column:scanned_at"`
	}
	var lastScans []lastScanRow
	if err := db.Table("meta.meta_node").
		Where("engine_id IN ?", engineIDs).
		Where("scanned_at IS NOT NULL").
		Select("engine_id, MAX(scanned_at) AS scanned_at").
		Group("engine_id").
		Scan(&lastScans).Error; err != nil {
		return nil, fmt.Errorf("failed to query node last scan time: %w", err)
	}
	for _, row := range lastScans {
		stats.lastScanByID[row.EngineID] = row.LastScanAt
	}

	return stats, nil
}

func buildResourceWithStats(resource *commonModels.Engine, stats *engineScanStats) *models.ResourceWithStats {
	if resource == nil {
		return nil
	}
	totalCatalogNodes := 0
	scannedCatalogNodes := 0
	lastScanAt := ""
	if stats != nil {
		if cnt, ok := stats.totalCount[resource.ID]; ok {
			totalCatalogNodes = int(cnt)
		}
		if cnt, ok := stats.scannedCount[resource.ID]; ok {
			scannedCatalogNodes = int(cnt)
		}
		if ts, ok := stats.lastScanByID[resource.ID]; ok && ts != nil {
			lastScanAt = ts.Format("2006-01-02 15:04:05")
		}
	}

	lastCheckAt := ""
	if resource.LastCheckAt != nil {
		lastCheckAt = resource.LastCheckAt.Format("2006-01-02 15:04:05")
	}

	engineFamily, catalogRootTerm, catalogTopTerm, catalogTopI18nKey, catalogLeafTerm, catalogLeafI18nKey := catalogViewTerms(resource.EngineType)

	return &models.ResourceWithStats{
		EngineID:              resource.ID,
		ResourceName:          resource.Name,
		ResourceType:          resource.EngineType,
		EngineFamily:          engineFamily,
		CatalogRootTerm:       catalogRootTerm,
		CatalogTopTerm:        catalogTopTerm,
		CatalogTopI18nKey:     catalogTopI18nKey,
		CatalogLeafTerm:       catalogLeafTerm,
		CatalogLeafI18nKey:    catalogLeafI18nKey,
		TotalCatalogNodes:     totalCatalogNodes,
		ScannedCatalogNodes:   scannedCatalogNodes,
		UnscannedCatalogNodes: totalCatalogNodes - scannedCatalogNodes,
		ScannedAt:             lastScanAt,
		ConnectionStatus:      resource.ConnectionStatus,
		LastCheckAt:           lastCheckAt,
		CheckMessage:          resource.CheckMessage,
	}
}

func catalogViewTerms(engineType string) (engineFamily, rootTerm, topTerm, topI18nKey, leafTerm, leafI18nKey string) {
	enginePlugin, err := plugin.Get(engineType)
	if err != nil {
		return "", "", "", "", "", ""
	}

	capabilities := enginePlugin.Capabilities()
	engineFamily = capabilities.EngineFamily

	model := scanflow.CatalogModelForPlugin(enginePlugin)
	if model == nil {
		return engineFamily, "", "", "", "", ""
	}

	rootTerm = model.RootTerm
	if level, ok := plugin.CatalogFirstBusinessBranch(*model); ok {
		topTerm = level.Term
		topI18nKey = level.I18nKey
		if topI18nKey == "" {
			topI18nKey = plugin.CatalogLevelI18nKey(*model, level.Term)
		}
	}
	leafTerm = plugin.CatalogLeafTerm(*model)
	leafI18nKey = plugin.CatalogLevelI18nKey(*model, leafTerm)
	return engineFamily, rootTerm, topTerm, topI18nKey, leafTerm, leafI18nKey
}
