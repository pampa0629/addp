package repository

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/addp/common/runtimelog"
	"github.com/addp/system/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrLogSourceConflict = errors.New("log source observation conflict")
var ErrLogSourceCapacity = errors.New("log source catalog capacity")

func (r *ModuleRegistryRepository) SaveLogSources(ctx context.Context, report runtimelog.SourceReport, retention time.Duration, now time.Time) error {
	body, _ := json.Marshal(report)
	hash := fmt.Sprintf("%x", sha256.Sum256(body))
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		node := models.ModuleLogSourceNode{Node: report.Node}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&node).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node=?", report.Node).Take(&node).Error; err != nil {
			return err
		}
		if node.Sequence != 0 {
			if node.BootID == report.BootID && node.Sequence == report.Sequence {
				if node.PayloadHash == hash {
					return nil
				}
				return ErrLogSourceConflict
			}
			if !report.SampledAt.After(node.SampledAt) || (node.BootID == report.BootID && report.Sequence < node.Sequence) {
				return ErrLogSourceConflict
			}
		}
		if node.BootID != report.BootID {
			var count int64
			if err := tx.Model(&models.ModuleLogSourceBoot{}).Where("boot_id=?", report.BootID).Count(&count).Error; err != nil {
				return err
			}
			if count != 0 {
				return ErrLogSourceConflict
			}
			if err := tx.Create(&models.ModuleLogSourceBoot{BootID: report.BootID, ExpiresAt: now.Add(retention)}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("expires_at <= ?", now).Delete(&models.ModuleLogSource{}).Error; err != nil {
			return err
		}
		if err := tx.Where("expires_at <= ? AND boot_id <> ?", now, report.BootID).Delete(&models.ModuleLogSourceBoot{}).Error; err != nil {
			return err
		}
		ids := make([]string, 0, len(report.Sources))
		for _, source := range report.Sources {
			ids = append(ids, source.InstanceID)
		}
		known := map[string]models.ModuleLogSource{}
		if len(ids) > 0 {
			var rows []models.ModuleLogSource
			if err := tx.Where("instance_id IN ?", ids).Find(&rows).Error; err != nil {
				return err
			}
			for _, row := range rows {
				known[row.InstanceID] = row
			}
		}
		rows := make([]models.ModuleLogSource, 0, len(ids))
		added := 0
		for _, source := range report.Sources {
			expires := source.ObservedAt.Add(retention)
			if !expires.After(now) {
				continue
			}
			old, exists := known[source.InstanceID]
			if exists {
				if old.ModuleName != source.Module || old.HostNodeName != source.Node || old.Role != source.Role || !old.CaptureStartedAt.Equal(source.CaptureStartedAt) {
					return ErrLogSourceConflict
				}
				if source.ObservedAt.Before(old.ObservedAt) {
					return ErrLogSourceConflict
				}
			} else {
				added++
			}
			rows = append(rows, models.ModuleLogSource{InstanceID: source.InstanceID, ModuleName: source.Module, HostNodeName: source.Node, Role: source.Role, CaptureStartedAt: source.CaptureStartedAt, ObservedAt: source.ObservedAt, ExpiresAt: expires})
		}
		var count int64
		if err := tx.Model(&models.ModuleLogSource{}).Count(&count).Error; err != nil {
			return err
		}
		if count+int64(added) > 16384 {
			return ErrLogSourceCapacity
		}
		if len(rows) > 0 {
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "instance_id"}}, DoUpdates: clause.AssignmentColumns([]string{"observed_at", "expires_at"})}).CreateInBatches(rows, 100).Error; err != nil {
				return err
			}
		}
		node.BootID, node.Sequence, node.PayloadHash = report.BootID, report.Sequence, hash
		node.SampledAt, node.ReceivedAt, node.Complete = report.SampledAt, now, report.Complete
		node.ScanIssues = report.Issues
		return tx.Save(&node).Error
	})
}

func (r *ModuleRegistryRepository) ListLogSources(ctx context.Context, f models.ModuleLogSourceFilter, node string, now time.Time) (*models.ModuleLogSourcePage, error) {
	result := &models.ModuleLogSourcePage{Data: []models.ModuleLogSource{}, Page: f.Page, PageSize: f.PageSize, DiscoveryState: "unknown", DiscoveryIssues: []runtimelog.SourceIssue{{Code: "observer_unobserved"}}}
	// Count and rows are bounded management queries, not a promised snapshot.
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var observation models.ModuleLogSourceNode
		if err := tx.Where("node=?", node).Take(&observation).Error; err == nil {
			result.Observation = &observation
			switch {
			case now.Sub(observation.ReceivedAt) > 120*time.Second:
				result.DiscoveryIssues = []runtimelog.SourceIssue{{Code: "observer_stale"}}
			case observation.ScanIssues == nil:
				result.DiscoveryIssues = []runtimelog.SourceIssue{{Code: "diagnostics_unavailable"}}
			case observation.Complete:
				result.DiscoveryState = "observed"
				result.DiscoveryIssues = []runtimelog.SourceIssue{}
			default:
				result.DiscoveryIssues = observation.ScanIssues
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		q := tx.Model(&models.ModuleLogSource{}).Where("expires_at > ?", now).
			Where(`NOT EXISTS (SELECT 1 FROM module_runtime_instances i JOIN module_definitions d ON d.id=i.module_definition_id WHERE i.instance_id=module_log_sources.instance_id AND d.module_name=module_log_sources.module_name AND i.host_node_name=module_log_sources.host_node_name AND i.role=module_log_sources.role)`)
		if f.Module != "" {
			q = q.Where("module_name=?", f.Module)
		}
		if f.Node != "" {
			q = q.Where("host_node_name=?", f.Node)
		}
		if f.Role != "" {
			q = q.Where("role=?", f.Role)
		}
		if !f.From.IsZero() {
			q = q.Where("capture_started_at >= ? AND capture_started_at < ?", f.From, f.To)
		}
		if err := q.Count(&result.Total).Error; err != nil {
			return err
		}
		result.TotalPages = int((result.Total + int64(f.PageSize) - 1) / int64(f.PageSize))
		return q.Order("capture_started_at DESC, instance_id DESC").Limit(f.PageSize).Offset((f.Page - 1) * f.PageSize).Find(&result.Data).Error
	})
	return result, err
}

// ResolveLogIdentity accepts only a registered instance or a retained trusted source.
func (r *ModuleRegistryRepository) ResolveLogIdentity(ctx context.Context, module, id string, now time.Time) (string, error) {
	var instance models.ModuleRuntimeInstance
	err := r.db.WithContext(ctx).Table("module_runtime_instances i").Select("i.*").Joins("JOIN module_definitions d ON d.id=i.module_definition_id").Where("d.module_name=? AND i.instance_id=?", module, id).Take(&instance).Error
	if err == nil {
		return instance.HostNodeName, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", err
	}
	var source models.ModuleLogSource
	err = r.db.WithContext(ctx).Where("module_name=? AND instance_id=? AND expires_at > ?", module, id, now).Take(&source).Error
	return source.HostNodeName, err
}
