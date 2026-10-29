package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/cleanup"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/events"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/exportartifact"
	"github.com/addp/common/logger"
	"github.com/addp/common/resourcetree"
	"github.com/addp/develop/backend/internal/models"
	"github.com/minio/minio-go/v7"
	"gorm.io/gorm"
)

type CleanupService struct {
	db            *gorm.DB
	runtime       *cleanup.Runner
	exportStore   exportartifact.Store
	minioClient   *minio.Client
	minioBucket   string
	exportCleanup exportartifact.CleanupOptions
	log           *slog.Logger
	stopCh        chan struct{}
}

func (s *CleanupService) SetExportArtifacts(store exportartifact.Store, minioClient *minio.Client, bucket string, opts exportartifact.CleanupOptions) {
	if s == nil {
		return
	}
	s.exportStore = store
	s.minioClient = minioClient
	s.minioBucket = strings.Trim(bucket, "/")
	s.exportCleanup = exportartifact.NormalizeCleanupOptions(opts)
}

type DevelopCleanupStats struct {
	DevTasks      int      `json:"dev_tasks"`
	ArchivedTasks int      `json:"archived_tasks,omitempty"`
	DeletedTasks  int      `json:"deleted_tasks,omitempty"`
	SkippedTasks  int      `json:"skipped_tasks,omitempty"`
	Errors        []string `json:"errors,omitempty"`
}

func NewCleanupService(db *gorm.DB, systemClient *commonClient.SystemServiceClient) *CleanupService {
	s := &CleanupService{db: db, log: logger.With("component", "develop_cleanup_service"), stopCh: make(chan struct{})}
	s.runtime = &cleanup.Runner{DB: db, Owner: events.ModuleDevelop, System: systemClient, Log: s.log, Handle: s.runCleanup}
	return s
}
func (s *CleanupService) Start(ctx context.Context) error {
	if err := s.runtime.Start(ctx); err != nil {
		return err
	}
	if s.exportStore != nil && s.minioClient != nil && s.minioBucket != "" {
		go s.runExportSessionCleanup(ctx)
	}
	return nil
}
func (s *CleanupService) Stop() {
	if s == nil {
		return
	}
	s.runtime.Stop()
	select {
	case <-s.stopCh:
	default:
		close(s.stopCh)
	}
}

func (s *CleanupService) runExportSessionCleanup(ctx context.Context) {
	s.cleanupExportSessionsOnce(ctx)
	ticker := time.NewTicker(s.exportCleanup.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.cleanupExportSessionsOnce(ctx)
		}
	}
}

func (s *CleanupService) cleanupExportSessionsOnce(ctx context.Context) {
	result, err := exportartifact.CleanupExpiredOnce(ctx, s.exportStore, s.minioClient, s.minioBucket, s.exportCleanup, time.Now())
	if err != nil {
		s.log.Warn("清理导出暂存失败", "error", err)
		return
	}
	if result.MarkedExpired > 0 || result.DeletedSessions > 0 {
		s.log.Info("已清理导出暂存", "marked_expired", result.MarkedExpired, "sessions", result.DeletedSessions, "objects", result.DeletedObjects)
	}
}

func (s *CleanupService) runCleanup(ctx context.Context, run *cleanup.Run) error {
	return run.Effect(ctx, func(tx *gorm.DB) error {
		scoped := &CleanupService{db: tx}
		var stats *DevelopCleanupStats
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
			return fmt.Errorf("develop cleanup domain transaction failed")
		}
		summary := developScanSummary(stats)
		if run.Input.Action == events.CleanupActionExecute {
			summary = developExecuteSummary(stats)
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
			impact, err = developEngineDeletionImpact(candidates)
			if err != nil {
				return err
			}
		}
		result := events.CleanupResultData{TaskID: run.Input.RequestID, TenantID: run.Input.TenantID, Module: events.ModuleDevelop, Action: run.Input.Action, CleanupMode: run.Input.CleanupMode, Status: events.CleanupResultSuccess, Summary: summary, Impact: &impact}
		receipt, err := commonExecution.NewCleanupReceipt(run.Execution, result, "")
		if err != nil {
			return err
		}
		return cleanup.Complete(ctx, tx, run.Lease, receipt, developCleanupCounters(stats))
	})
}

func developCleanupCounters(s *DevelopCleanupStats) map[string]int64 {
	return map[string]int64{"dev_tasks": int64(s.DevTasks), "archived_tasks": int64(s.ArchivedTasks), "deleted_tasks": int64(s.DeletedTasks), "skipped_tasks": int64(s.SkippedTasks), "error_count": int64(len(s.Errors))}
}

func (s *CleanupService) ScanReclaimCandidates(ctx context.Context, tenantID uint, cleanupContext map[string]interface{}) (*DevelopCleanupStats, error) {
	candidates, err := s.listCandidates(ctx, tenantID, cleanupContext)
	if err != nil {
		return nil, err
	}
	return candidates.stats(), nil
}

func (s *CleanupService) ExecuteCleanup(ctx context.Context, tenantID uint, cleanupMode string, cleanupContext map[string]interface{}) (*DevelopCleanupStats, error) {
	if err := events.ValidateCleanupMode(cleanupMode); err != nil {
		return nil, err
	}
	candidates, err := s.listCandidates(ctx, tenantID, cleanupContext)
	if err != nil {
		return nil, err
	}

	stats := candidates.stats()
	if _, hasEngineID := developCleanupContextUint(cleanupContext, "engine_id"); hasEngineID {
		for _, task := range candidates.devTasks {
			switch strings.ToLower(strings.TrimSpace(task.LastExecutionStatus)) {
			case commonExecution.ExecutionStatusPending, commonExecution.ExecutionStatusRunning:
				stats.Errors = append(stats.Errors, fmt.Sprintf("develop task %d is running", task.ID))
			}
		}
		return stats, nil
	}
	switch cleanupMode {
	case events.CleanupModeLogical:
		s.archiveTasks(ctx, candidates, stats)
	case events.CleanupModePhysical:
		s.deleteTasks(ctx, candidates, stats)
	}
	return stats, nil
}

type developCleanupCandidates struct {
	devTasks []models.DevTask
}

func (c developCleanupCandidates) stats() *DevelopCleanupStats {
	return &DevelopCleanupStats{DevTasks: len(c.devTasks)}
}

func (s *CleanupService) listCandidates(ctx context.Context, tenantID uint, cleanupContext map[string]interface{}) (developCleanupCandidates, error) {
	if tenantID == 0 {
		return developCleanupCandidates{}, fmt.Errorf("develop resource reclaim requires tenant_id")
	}
	if s == nil || s.db == nil {
		return developCleanupCandidates{}, fmt.Errorf("develop resource reclaim database is not configured")
	}
	contextTenantID, hasContextTenantID := developCleanupContextUint(cleanupContext, "tenant_id")
	engineID, hasEngineID := developCleanupContextUint(cleanupContext, "engine_id")
	if hasEngineID {
		return s.listEngineCandidates(ctx, tenantID, engineID)
	}
	if hasContextTenantID && contextTenantID == tenantID {
		return s.listTenantCandidates(ctx, tenantID)
	}
	return developCleanupCandidates{}, nil
}

func (s *CleanupService) listEngineCandidates(ctx context.Context, tenantID, engineID uint) (developCleanupCandidates, error) {
	var tasks []models.DevTask
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&tasks).Error; err != nil {
		return developCleanupCandidates{}, err
	}
	candidates := developCleanupCandidates{devTasks: make([]models.DevTask, 0, len(tasks))}
	for _, task := range tasks {
		if devTaskReferencesEngine(task, engineID) {
			candidates.devTasks = append(candidates.devTasks, task)
		}
	}
	return candidates, nil
}

func devTaskReferencesEngine(task models.DevTask, engineID uint) bool {
	return structuredValueReferencesEngine(task.ExecutionConfig, engineID) || structuredValueReferencesEngine(task.Content, engineID)
}

func structuredValueReferencesEngine(value interface{}, engineID uint) bool {
	switch typed := value.(type) {
	case models.DevTaskContent:
		return structuredValueReferencesEngine(map[string]interface{}(typed), engineID)
	case map[string]interface{}:
		for key, child := range typed {
			if (key == "engine_id" || strings.HasSuffix(key, "_engine_id")) && cleanupEngineIDValue(child) == engineID {
				return true
			}
			if structuredValueReferencesEngine(child, engineID) {
				return true
			}
		}
	case []interface{}:
		for _, child := range typed {
			if structuredValueReferencesEngine(child, engineID) {
				return true
			}
		}
	case string:
		locator, err := resourcetree.ParseURI(strings.TrimSpace(typed))
		return err == nil && locator.EngineID == engineID
	}
	return false
}

func cleanupEngineIDValue(value interface{}) uint {
	switch typed := value.(type) {
	case uint:
		return typed
	case int:
		if typed > 0 {
			return uint(typed)
		}
	case int64:
		if typed > 0 {
			return uint(typed)
		}
	case float64:
		if typed > 0 {
			return uint(typed)
		}
	case json.Number:
		parsed, _ := strconv.ParseUint(string(typed), 10, 32)
		return uint(parsed)
	case string:
		parsed, _ := strconv.ParseUint(strings.TrimSpace(typed), 10, 32)
		return uint(parsed)
	}
	return 0
}

func developEngineDeletionImpact(candidates developCleanupCandidates) (events.CleanupImpactData, error) {
	items := make([]events.CleanupImpactItem, 0, len(candidates.devTasks)*2)
	for _, task := range candidates.devTasks {
		stableRef := fmt.Sprintf("dev_task:%d", task.ID)
		items = append(items, events.CleanupImpactItem{StableRef: stableRef, Disposition: events.CleanupImpactRebindable})
		switch strings.ToLower(strings.TrimSpace(task.LastExecutionStatus)) {
		case commonExecution.ExecutionStatusPending, commonExecution.ExecutionStatusRunning:
			items = append(items, events.CleanupImpactItem{StableRef: stableRef, Disposition: events.CleanupImpactRunning})
		}
	}
	return events.BuildCleanupImpactData(items, "/develop/tasks")
}

func (s *CleanupService) listTenantCandidates(ctx context.Context, tenantID uint) (developCleanupCandidates, error) {
	var candidates developCleanupCandidates
	if err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&candidates.devTasks).Error; err != nil {
		return candidates, err
	}
	return candidates, nil
}

func (s *CleanupService) archiveTasks(ctx context.Context, candidates developCleanupCandidates, stats *DevelopCleanupStats) {
	for _, item := range candidates.devTasks {
		updates := map[string]interface{}{
			"status": "archived",
		}
		if err := s.db.WithContext(ctx).Model(&models.DevTask{}).Where("id = ?", item.ID).Updates(updates).Error; err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("archive dev task %d failed: %v", item.ID, err))
			continue
		}
		stats.ArchivedTasks++
	}
}

func (s *CleanupService) deleteTasks(ctx context.Context, candidates developCleanupCandidates, stats *DevelopCleanupStats) {
	for _, item := range candidates.devTasks {
		if err := s.db.WithContext(ctx).Unscoped().Delete(&models.DevTask{}, item.ID).Error; err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("delete dev task %d failed: %v", item.ID, err))
			continue
		}
		stats.DeletedTasks++
	}
}

func developCleanupContextUint(cleanupContext map[string]interface{}, key string) (uint, bool) {
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

func developScanSummary(stats *DevelopCleanupStats) events.CleanupResultSummary {
	if stats == nil {
		return events.CleanupResultSummary{RiskLevel: "low"}
	}
	return events.CleanupResultSummary{
		ScannedItems: stats.DevTasks,
		ErrorCount:   len(stats.Errors),
		RiskLevel:    developRiskLevelForCount(stats.DevTasks),
	}
}

func developExecuteSummary(stats *DevelopCleanupStats) events.CleanupResultSummary {
	if stats == nil {
		return events.CleanupResultSummary{RiskLevel: "low"}
	}
	affected := stats.ArchivedTasks + stats.DeletedTasks
	return events.CleanupResultSummary{
		AffectedRecords:         affected,
		DisabledTaskDefinitions: stats.ArchivedTasks,
		SkippedItems:            stats.SkippedTasks,
		ErrorCount:              len(stats.Errors),
		RiskLevel:               "low",
	}
}

func developRiskLevelForCount(count int) string {
	if count > 1000 {
		return "high"
	}
	if count > 100 {
		return "medium"
	}
	return "low"
}

func developIntPtr(value int) *int {
	return &value
}
