package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/addp/asset/internal/models"
	"github.com/addp/common/cleanup"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/events"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/logger"
	"gorm.io/gorm"
)

type CleanupService struct {
	db      *gorm.DB
	runtime *cleanup.Runner
}

type AssetCleanupStats struct {
	TypeDefinitions       int      `json:"type_definitions"`
	TypeFieldSchemas      int      `json:"type_field_schemas"`
	Categories            int      `json:"categories"`
	Assets                int      `json:"assets"`
	AssetComponents       int      `json:"asset_components"`
	AssetExtFields        int      `json:"asset_ext_fields"`
	Applications          int      `json:"applications"`
	Authorizations        int      `json:"authorizations"`
	Ratings               int      `json:"ratings"`
	OfflineAssets         int      `json:"offline_assets,omitempty"`
	RevokedAuthorizations int      `json:"revoked_authorizations,omitempty"`
	DeletedRecords        int      `json:"deleted_records,omitempty"`
	SkippedItems          int      `json:"skipped_items,omitempty"`
	Errors                []string `json:"errors,omitempty"`
}

func NewCleanupService(db *gorm.DB, systemClient *commonClient.SystemServiceClient) *CleanupService {
	s := &CleanupService{db: db}
	s.runtime = &cleanup.Runner{DB: db, Owner: events.ModuleAsset, System: systemClient, Log: logger.With("component", "asset_cleanup_service"), Handle: s.runCleanup}
	return s
}
func (s *CleanupService) Start(ctx context.Context) error { return s.runtime.Start(ctx) }
func (s *CleanupService) Stop()                           { s.runtime.Stop() }

func (s *CleanupService) runCleanup(ctx context.Context, run *cleanup.Run) error {
	return run.Effect(ctx, func(tx *gorm.DB) error {
		scoped := &CleanupService{db: tx}
		var stats *AssetCleanupStats
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
			return fmt.Errorf("asset cleanup domain transaction failed")
		}
		summary := assetScanSummary(stats)
		if run.Input.Action == events.CleanupActionExecute {
			summary = assetExecuteSummary(stats)
		}
		impact, err := events.BuildCleanupImpactData(nil, "")
		if err != nil {
			return err
		}
		result := events.CleanupResultData{TaskID: run.Input.RequestID, TenantID: run.Input.TenantID, Module: events.ModuleAsset, Action: run.Input.Action, CleanupMode: run.Input.CleanupMode, Status: events.CleanupResultSuccess, Summary: summary, Impact: &impact}
		receipt, err := commonExecution.NewCleanupReceipt(run.Execution, result, "")
		if err != nil {
			return err
		}
		return cleanup.Complete(ctx, tx, run.Lease, receipt, assetCleanupCounters(stats))
	})
}

func assetCleanupCounters(s *AssetCleanupStats) map[string]int64 {
	return map[string]int64{
		"type_definitions": int64(s.TypeDefinitions), "type_field_schemas": int64(s.TypeFieldSchemas), "categories": int64(s.Categories), "assets": int64(s.Assets), "asset_components": int64(s.AssetComponents), "asset_ext_fields": int64(s.AssetExtFields), "applications": int64(s.Applications), "authorizations": int64(s.Authorizations), "ratings": int64(s.Ratings), "offline_assets": int64(s.OfflineAssets), "revoked_authorizations": int64(s.RevokedAuthorizations), "deleted_records": int64(s.DeletedRecords), "skipped_items": int64(s.SkippedItems), "error_count": int64(len(s.Errors)),
	}
}

func (s *CleanupService) ScanReclaimCandidates(ctx context.Context, tenantID uint, cleanupContext map[string]interface{}) (*AssetCleanupStats, error) {
	candidates, err := s.listCandidates(ctx, tenantID, cleanupContext)
	if err != nil {
		return nil, err
	}
	return candidates.stats(), nil
}

func (s *CleanupService) ExecuteCleanup(ctx context.Context, tenantID uint, cleanupMode string, cleanupContext map[string]interface{}) (*AssetCleanupStats, error) {
	if err := events.ValidateCleanupMode(cleanupMode); err != nil {
		return nil, err
	}
	candidates, err := s.listCandidates(ctx, tenantID, cleanupContext)
	if err != nil {
		return nil, err
	}

	stats := candidates.stats()
	switch cleanupMode {
	case events.CleanupModeLogical:
		s.logicalCleanup(ctx, candidates, stats)
	case events.CleanupModePhysical:
		s.physicalCleanup(ctx, candidates, stats)
	}
	return stats, nil
}

type assetCleanupCandidates struct {
	typeDefinitions  []models.TypeDefinition
	typeFieldSchemas []models.TypeFieldSchema
	categories       []models.AssetCategory
	assets           []models.Asset
	assetComponents  []models.AssetComponent
	assetExtFields   []models.AssetExtField
	applications     []models.Application
	authorizations   []models.Authorization
	ratings          []models.Rating
}

func (c assetCleanupCandidates) stats() *AssetCleanupStats {
	return &AssetCleanupStats{
		TypeDefinitions:  len(c.typeDefinitions),
		TypeFieldSchemas: len(c.typeFieldSchemas),
		Categories:       len(c.categories),
		Assets:           len(c.assets),
		AssetComponents:  len(c.assetComponents),
		AssetExtFields:   len(c.assetExtFields),
		Applications:     len(c.applications),
		Authorizations:   len(c.authorizations),
		Ratings:          len(c.ratings),
	}
}

func (s *CleanupService) listCandidates(ctx context.Context, tenantID uint, cleanupContext map[string]interface{}) (assetCleanupCandidates, error) {
	if tenantID == 0 {
		return assetCleanupCandidates{}, fmt.Errorf("asset resource reclaim requires tenant_id")
	}
	if s == nil || s.db == nil {
		return assetCleanupCandidates{}, fmt.Errorf("asset resource reclaim database is not configured")
	}
	contextTenantID, hasContextTenantID := assetCleanupContextUint(cleanupContext, "tenant_id")
	if !hasContextTenantID || contextTenantID != tenantID {
		return assetCleanupCandidates{}, nil
	}
	return s.listTenantCandidates(ctx, int64(tenantID))
}

func (s *CleanupService) listTenantCandidates(ctx context.Context, tenantID int64) (assetCleanupCandidates, error) {
	var candidates assetCleanupCandidates
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.typeDefinitions).Error; err != nil {
		return candidates, err
	}
	typeIDs := make([]int64, 0, len(candidates.typeDefinitions))
	for _, item := range candidates.typeDefinitions {
		typeIDs = append(typeIDs, item.ID)
	}
	if len(typeIDs) > 0 {
		if err := s.db.WithContext(ctx).Where("type_id IN ?", typeIDs).Find(&candidates.typeFieldSchemas).Error; err != nil {
			return candidates, err
		}
	}
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.categories).Error; err != nil {
		return candidates, err
	}
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.assets).Error; err != nil {
		return candidates, err
	}
	assetIDs := make([]int64, 0, len(candidates.assets))
	for _, item := range candidates.assets {
		assetIDs = append(assetIDs, item.ID)
	}
	if len(assetIDs) > 0 {
		if err := s.db.WithContext(ctx).Where("tenant_id = ? AND asset_id IN ?", tenantID, assetIDs).Find(&candidates.assetComponents).Error; err != nil {
			return candidates, err
		}
		if err := s.db.WithContext(ctx).Where("asset_id IN ?", assetIDs).Find(&candidates.assetExtFields).Error; err != nil {
			return candidates, err
		}
	}
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.applications).Error; err != nil {
		return candidates, err
	}
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.authorizations).Error; err != nil {
		return candidates, err
	}
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.ratings).Error; err != nil {
		return candidates, err
	}
	return candidates, nil
}

func (s *CleanupService) logicalCleanup(ctx context.Context, candidates assetCleanupCandidates, stats *AssetCleanupStats) {
	for _, asset := range candidates.assets {
		updates := map[string]interface{}{"status": "offline"}
		if err := s.db.WithContext(ctx).Model(&models.Asset{}).Where("id = ?", asset.ID).Updates(updates).Error; err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("offline asset %d failed: %v", asset.ID, err))
			continue
		}
		stats.OfflineAssets++
	}
	now := time.Now().UTC()
	for _, auth := range candidates.authorizations {
		if auth.Status == models.AuthorizationStatusRevoked || auth.Status == models.AuthorizationStatusRevocationPending {
			stats.SkippedItems++
			continue
		}
		if err := s.db.WithContext(ctx).Model(&models.Authorization{}).Where("id = ?", auth.ID).Updates(map[string]interface{}{
			"status":          models.AuthorizationStatusRevocationPending,
			"next_attempt_at": now,
		}).Error; err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("revoke authorization %d failed: %v", auth.ID, err))
			continue
		}
		stats.RevokedAuthorizations++
	}
}

func (s *CleanupService) physicalCleanup(ctx context.Context, candidates assetCleanupCandidates, stats *AssetCleanupStats) {
	for _, batch := range []struct {
		model interface{}
		ids   []int64
		name  string
	}{
		{model: &models.Rating{}, ids: ratingIDs(candidates.ratings), name: "ratings"},
		{model: &models.Authorization{}, ids: authorizationIDs(candidates.authorizations), name: "authorizations"},
		{model: &models.Application{}, ids: applicationIDs(candidates.applications), name: "applications"},
		{model: &models.AssetExtField{}, ids: assetExtFieldIDs(candidates.assetExtFields), name: "asset ext fields"},
		{model: &models.AssetComponent{}, ids: assetComponentIDs(candidates.assetComponents), name: "asset components"},
		{model: &models.Asset{}, ids: assetIDs(candidates.assets), name: "assets"},
		{model: &models.AssetCategory{}, ids: categoryIDs(candidates.categories), name: "categories"},
		{model: &models.TypeFieldSchema{}, ids: typeFieldSchemaIDs(candidates.typeFieldSchemas), name: "type field schemas"},
		{model: &models.TypeDefinition{}, ids: typeDefinitionIDs(candidates.typeDefinitions), name: "type definitions"},
	} {
		if len(batch.ids) == 0 {
			continue
		}
		if err := s.db.WithContext(ctx).Unscoped().Delete(batch.model, batch.ids).Error; err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("delete %s failed: %v", batch.name, err))
			continue
		}
		stats.DeletedRecords += len(batch.ids)
	}
}

func assetComponentIDs(items []models.AssetComponent) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func assetCleanupContextUint(cleanupContext map[string]interface{}, key string) (uint, bool) {
	if cleanupContext == nil {
		return 0, false
	}
	raw, ok := cleanupContext[key]
	if !ok || raw == nil {
		return 0, false
	}
	switch value := raw.(type) {
	case uint:
		return value, value > 0
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
		return uint(parsed), err == nil && parsed > 0
	case string:
		parsed, err := strconv.ParseUint(value, 10, 32)
		return uint(parsed), err == nil && parsed > 0
	}
	return 0, false
}

func assetScanSummary(stats *AssetCleanupStats) events.CleanupResultSummary {
	if stats == nil {
		return events.CleanupResultSummary{RiskLevel: "low"}
	}
	count := assetCandidateRecordCount(stats)
	return events.CleanupResultSummary{
		ScannedItems: count,
		ErrorCount:   len(stats.Errors),
		RiskLevel:    assetRiskLevelForCount(count),
	}
}

func assetExecuteSummary(stats *AssetCleanupStats) events.CleanupResultSummary {
	if stats == nil {
		return events.CleanupResultSummary{RiskLevel: "low"}
	}
	affected := stats.OfflineAssets + stats.RevokedAuthorizations + stats.DeletedRecords
	return events.CleanupResultSummary{
		AffectedRecords:         affected,
		MarkedMissingSource:     stats.OfflineAssets,
		DisabledTaskDefinitions: stats.RevokedAuthorizations,
		SkippedItems:            stats.SkippedItems,
		ErrorCount:              len(stats.Errors),
		RiskLevel:               "low",
	}
}

func assetCandidateRecordCount(stats *AssetCleanupStats) int {
	return stats.TypeDefinitions +
		stats.TypeFieldSchemas +
		stats.Categories +
		stats.Assets +
		stats.AssetComponents +
		stats.AssetExtFields +
		stats.Applications +
		stats.Authorizations +
		stats.Ratings
}

func assetRiskLevelForCount(count int) string {
	if count > 1000 {
		return "high"
	}
	if count > 100 {
		return "medium"
	}
	return "low"
}

func assetIntPtr(value int) *int {
	return &value
}

func typeDefinitionIDs(items []models.TypeDefinition) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func typeFieldSchemaIDs(items []models.TypeFieldSchema) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func categoryIDs(items []models.AssetCategory) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func assetIDs(items []models.Asset) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func assetExtFieldIDs(items []models.AssetExtField) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func applicationIDs(items []models.Application) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func authorizationIDs(items []models.Authorization) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func ratingIDs(items []models.Rating) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}
