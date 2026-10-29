package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/addp/common/cleanup"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/events"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/logger"
	"github.com/addp/service/internal/models"
	"gorm.io/gorm"
)

type CleanupService struct {
	db      *gorm.DB
	runtime *cleanup.Runner
}

type ServiceCleanupStats struct {
	QueryServices          int      `json:"query_services"`
	GraphQueryServices     int      `json:"graph_query_services"`
	TileServices           int      `json:"tile_services"`
	TileLayers             int      `json:"tile_layers"`
	DisabledServiceRecords int      `json:"disabled_service_records,omitempty"`
	DisabledTileLayers     int      `json:"disabled_tile_layers,omitempty"`
	DeletedServiceRecords  int      `json:"deleted_service_records,omitempty"`
	DeletedTileLayers      int      `json:"deleted_tile_layers,omitempty"`
	Errors                 []string `json:"errors,omitempty"`
}

func NewCleanupService(db *gorm.DB, systemClient *commonClient.SystemServiceClient) *CleanupService {
	s := &CleanupService{db: db}
	s.runtime = &cleanup.Runner{DB: db, Owner: events.ModuleService, System: systemClient, Log: logger.With("component", "service_cleanup_service"), Handle: s.runCleanup}
	return s
}
func (s *CleanupService) Start(ctx context.Context) error { return s.runtime.Start(ctx) }
func (s *CleanupService) Stop()                           { s.runtime.Stop() }

func (s *CleanupService) runCleanup(ctx context.Context, run *cleanup.Run) error {
	return run.Effect(ctx, func(tx *gorm.DB) error {
		scoped := &CleanupService{db: tx}
		var stats *ServiceCleanupStats
		var err error
		if run.Input.Action == events.CleanupActionScan {
			stats, err = scoped.ScanReclaimCandidates(ctx, run.Input.TenantID, run.Input.Context.Map())
		} else {
			stats, err = scoped.ExecuteCleanup(ctx, run.Input.TenantID, run.Input.CleanupMode, run.Input.Context.Map())
		}
		if err != nil {
			return err
		}
		if len(stats.Errors) > 0 {
			return fmt.Errorf("service cleanup domain transaction failed")
		}
		summary := serviceScanSummary(stats)
		if run.Input.Action == events.CleanupActionExecute {
			summary = serviceExecuteSummary(stats)
		}
		impact, err := events.BuildCleanupImpactData(nil, "")
		if err != nil {
			return err
		}
		if run.Input.Action == events.CleanupActionScan && run.Input.CauseEvent == events.CleanupCauseEngineDeleting {
			candidates, e := scoped.listCandidates(ctx, run.Input.TenantID, run.Input.Context.Map())
			if e != nil {
				return e
			}
			impact, err = serviceEngineDeletionImpact(candidates)
			if err != nil {
				return err
			}
		}
		result := events.CleanupResultData{TaskID: run.Input.RequestID, TenantID: run.Input.TenantID, Module: events.ModuleService, Action: run.Input.Action, CleanupMode: run.Input.CleanupMode, Status: events.CleanupResultSuccess, Summary: summary, Impact: &impact}
		receipt, err := commonExecution.NewCleanupReceipt(run.Execution, result, "")
		if err != nil {
			return err
		}
		return cleanup.Complete(ctx, tx, run.Lease, receipt, serviceCleanupCounters(stats))
	})
}

func serviceCleanupCounters(s *ServiceCleanupStats) map[string]int64 {
	return map[string]int64{"query_services": int64(s.QueryServices), "graph_query_services": int64(s.GraphQueryServices), "tile_services": int64(s.TileServices), "tile_layers": int64(s.TileLayers), "disabled_service_records": int64(s.DisabledServiceRecords), "disabled_tile_layers": int64(s.DisabledTileLayers), "deleted_service_records": int64(s.DeletedServiceRecords), "deleted_tile_layers": int64(s.DeletedTileLayers), "error_count": int64(len(s.Errors))}
}

func (s *CleanupService) ScanReclaimCandidates(ctx context.Context, tenantID uint, cleanupContext map[string]interface{}) (*ServiceCleanupStats, error) {
	candidates, err := s.listCandidates(ctx, tenantID, cleanupContext)
	if err != nil {
		return nil, err
	}
	return candidates.stats(), nil
}

func (s *CleanupService) ExecuteCleanup(ctx context.Context, tenantID uint, cleanupMode string, cleanupContext map[string]interface{}) (*ServiceCleanupStats, error) {
	if err := events.ValidateCleanupMode(cleanupMode); err != nil {
		return nil, err
	}
	candidates, err := s.listCandidates(ctx, tenantID, cleanupContext)
	if err != nil {
		return nil, err
	}

	stats := candidates.stats()
	if _, hasEngineID := cleanupContextUint(cleanupContext, "engine_id"); hasEngineID {
		s.disableCandidates(ctx, candidates, stats)
		return stats, nil
	}
	switch cleanupMode {
	case events.CleanupModeLogical:
		s.disableCandidates(ctx, candidates, stats)
	case events.CleanupModePhysical:
		s.deleteCandidates(ctx, candidates, stats)
	}
	return stats, nil
}

func serviceEngineDeletionImpact(candidates serviceCleanupCandidates) (events.CleanupImpactData, error) {
	items := make([]events.CleanupImpactItem, 0, len(candidates.queryServices)+len(candidates.graphQueryServices)+len(candidates.tileServices)+len(candidates.tileLayers))
	for _, item := range candidates.queryServices {
		items = append(items, events.CleanupImpactItem{StableRef: fmt.Sprintf("query_service:%d", item.ID), Disposition: events.CleanupImpactWillDisable})
	}
	for _, item := range candidates.graphQueryServices {
		items = append(items, events.CleanupImpactItem{StableRef: fmt.Sprintf("graph_query_service:%d", item.ID), Disposition: events.CleanupImpactWillDisable})
	}
	for _, item := range candidates.tileServices {
		items = append(items, events.CleanupImpactItem{StableRef: fmt.Sprintf("tile_service:%d", item.ID), Disposition: events.CleanupImpactWillDisable})
	}
	for _, item := range candidates.tileLayers {
		items = append(items, events.CleanupImpactItem{StableRef: fmt.Sprintf("tile_service_layer:%d", item.ID), Disposition: events.CleanupImpactWillDisable})
	}
	return events.BuildCleanupImpactData(items, "/service/services")
}

type serviceCleanupCandidates struct {
	queryServices      []models.QueryService
	graphQueryServices []models.GraphQueryService
	tileServices       []models.TileService
	tileLayers         []models.TileServiceLayer
}

func (c serviceCleanupCandidates) stats() *ServiceCleanupStats {
	return &ServiceCleanupStats{
		QueryServices:      len(c.queryServices),
		GraphQueryServices: len(c.graphQueryServices),
		TileServices:       len(c.tileServices),
		TileLayers:         len(c.tileLayers),
	}
}

func (s *CleanupService) listCandidates(ctx context.Context, tenantID uint, cleanupContext map[string]interface{}) (serviceCleanupCandidates, error) {
	if tenantID == 0 {
		return serviceCleanupCandidates{}, fmt.Errorf("service resource reclaim requires tenant_id")
	}
	if s == nil || s.db == nil {
		return serviceCleanupCandidates{}, fmt.Errorf("service resource reclaim database is not configured")
	}
	engineID, hasEngineID := cleanupContextUint(cleanupContext, "engine_id")
	contextTenantID, hasContextTenantID := cleanupContextUint(cleanupContext, "tenant_id")
	if !hasEngineID {
		if hasContextTenantID && contextTenantID == tenantID {
			return s.listTenantCandidates(ctx, tenantID)
		}
		return serviceCleanupCandidates{}, nil
	}
	if engineID == 0 {
		return serviceCleanupCandidates{}, nil
	}
	return s.listEngineCandidates(ctx, tenantID, engineID)
}

func (s *CleanupService) listTenantCandidates(ctx context.Context, tenantID uint) (serviceCleanupCandidates, error) {
	var candidates serviceCleanupCandidates
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.queryServices).Error; err != nil {
		return candidates, err
	}
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.graphQueryServices).Error; err != nil {
		return candidates, err
	}
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.tileServices).Error; err != nil {
		return candidates, err
	}
	if len(candidates.tileServices) > 0 {
		serviceIDs := make([]uint, 0, len(candidates.tileServices))
		for _, service := range candidates.tileServices {
			serviceIDs = append(serviceIDs, service.ID)
		}
		if err := s.db.WithContext(ctx).Where("service_id IN ?", serviceIDs).Find(&candidates.tileLayers).Error; err != nil {
			return candidates, err
		}
	}
	return candidates, nil
}

func (s *CleanupService) listEngineCandidates(ctx context.Context, tenantID uint, engineID uint) (serviceCleanupCandidates, error) {
	var candidates serviceCleanupCandidates
	if err := s.db.WithContext(ctx).
		Where("tenant_id = ? AND engine_id = ?", tenantID, engineID).
		Find(&candidates.queryServices).Error; err != nil {
		return candidates, err
	}
	if err := s.db.WithContext(ctx).
		Where("tenant_id = ? AND engine_id = ?", tenantID, engineID).
		Find(&candidates.graphQueryServices).Error; err != nil {
		return candidates, err
	}

	var services []models.TileService
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&services).Error; err != nil {
		return candidates, err
	}
	if len(services) == 0 {
		return candidates, nil
	}
	serviceByID := make(map[uint]models.TileService, len(services))
	serviceIDs := make([]uint, 0, len(services))
	for _, service := range services {
		serviceByID[service.ID] = service
		serviceIDs = append(serviceIDs, service.ID)
	}

	var layers []models.TileServiceLayer
	if err := s.db.WithContext(ctx).Where("service_id IN ?", serviceIDs).Find(&layers).Error; err != nil {
		return candidates, err
	}
	tileServiceSeen := map[uint]struct{}{}
	for _, layer := range layers {
		if layer.GetEngineID() != engineID {
			continue
		}
		candidates.tileLayers = append(candidates.tileLayers, layer)
		if _, ok := tileServiceSeen[layer.ServiceID]; !ok {
			candidates.tileServices = append(candidates.tileServices, serviceByID[layer.ServiceID])
			tileServiceSeen[layer.ServiceID] = struct{}{}
		}
	}
	return candidates, nil
}

func (s *CleanupService) disableCandidates(ctx context.Context, candidates serviceCleanupCandidates, stats *ServiceCleanupStats) {
	message := "missing_source: resource reclaim lifecycle context"
	for _, item := range candidates.queryServices {
		if err := s.db.WithContext(ctx).Model(&models.QueryService{}).Where("id = ?", item.ID).Updates(map[string]interface{}{"status": "inactive", "error_message": message}).Error; err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("disable query service %d failed: %v", item.ID, err))
			continue
		}
		stats.DisabledServiceRecords++
	}
	for _, item := range candidates.graphQueryServices {
		if err := s.db.WithContext(ctx).Model(&models.GraphQueryService{}).Where("id = ?", item.ID).Updates(map[string]interface{}{"status": "inactive", "error_message": message}).Error; err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("disable graph query service %d failed: %v", item.ID, err))
			continue
		}
		stats.DisabledServiceRecords++
	}
	for _, item := range candidates.tileServices {
		if err := s.db.WithContext(ctx).Model(&models.TileService{}).Where("id = ?", item.ID).Updates(map[string]interface{}{"status": "inactive", "error_message": message}).Error; err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("disable tile service %d failed: %v", item.ID, err))
			continue
		}
		stats.DisabledServiceRecords++
	}
	for _, item := range candidates.tileLayers {
		if err := s.db.WithContext(ctx).Model(&models.TileServiceLayer{}).Where("id = ?", item.ID).Update("enabled", false).Error; err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("disable tile layer %d failed: %v", item.ID, err))
			continue
		}
		stats.DisabledTileLayers++
	}
}

func (s *CleanupService) deleteCandidates(ctx context.Context, candidates serviceCleanupCandidates, stats *ServiceCleanupStats) {
	for _, item := range candidates.tileLayers {
		if err := s.db.WithContext(ctx).Unscoped().Delete(&models.TileServiceLayer{}, item.ID).Error; err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("delete tile layer %d failed: %v", item.ID, err))
			continue
		}
		stats.DeletedTileLayers++
	}
	for _, item := range candidates.queryServices {
		if err := s.db.WithContext(ctx).Unscoped().Delete(&models.QueryService{}, item.ID).Error; err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("delete query service %d failed: %v", item.ID, err))
			continue
		}
		stats.DeletedServiceRecords++
	}
	for _, item := range candidates.graphQueryServices {
		if err := s.db.WithContext(ctx).Unscoped().Delete(&models.GraphQueryService{}, item.ID).Error; err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("delete graph query service %d failed: %v", item.ID, err))
			continue
		}
		stats.DeletedServiceRecords++
	}
	for _, item := range candidates.tileServices {
		if err := s.db.WithContext(ctx).Unscoped().Delete(&models.TileService{}, item.ID).Error; err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("delete tile service %d failed: %v", item.ID, err))
			continue
		}
		stats.DeletedServiceRecords++
	}
}

func cleanupContextUint(cleanupContext map[string]interface{}, key string) (uint, bool) {
	if cleanupContext == nil {
		return 0, false
	}
	raw, ok := cleanupContext[key]
	if !ok || raw == nil {
		return 0, false
	}
	switch value := raw.(type) {
	case uint:
		return value, true
	case int:
		if value > 0 {
			return uint(value), true
		}
	case int64:
		if value > 0 {
			return uint(value), true
		}
	case float64:
		if value > 0 {
			return uint(value), true
		}
	case json.Number:
		parsed, err := strconv.ParseUint(string(value), 10, 32)
		return uint(parsed), err == nil
	case string:
		parsed, err := strconv.ParseUint(value, 10, 32)
		return uint(parsed), err == nil
	}
	return 0, false
}

func serviceScanSummary(stats *ServiceCleanupStats) events.CleanupResultSummary {
	if stats == nil {
		return events.CleanupResultSummary{RiskLevel: "low"}
	}
	scanned := stats.QueryServices + stats.GraphQueryServices + stats.TileServices + stats.TileLayers
	return events.CleanupResultSummary{
		ScannedItems: scanned,
		ErrorCount:   len(stats.Errors),
		RiskLevel:    serviceRiskLevelForCount(scanned),
	}
}

func serviceExecuteSummary(stats *ServiceCleanupStats) events.CleanupResultSummary {
	if stats == nil {
		return events.CleanupResultSummary{RiskLevel: "low"}
	}
	affected := stats.DisabledServiceRecords + stats.DisabledTileLayers + stats.DeletedServiceRecords + stats.DeletedTileLayers
	return events.CleanupResultSummary{
		AffectedRecords: affected,
		MarkedOutdated:  stats.DisabledServiceRecords + stats.DisabledTileLayers,
		ErrorCount:      len(stats.Errors),
		RiskLevel:       "low",
	}
}

func serviceRiskLevelForCount(count int) string {
	if count > 1000 {
		return "high"
	}
	if count > 100 {
		return "medium"
	}
	return "low"
}

func serviceIntPtr(value int) *int {
	return &value
}
