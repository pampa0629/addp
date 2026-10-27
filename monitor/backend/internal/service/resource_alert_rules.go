package service

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/addp/monitor/internal/metricsdiscovery"
	"github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/resourcequery"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const resourceAlertRuleLimit = 1000

type ResourceAlertRuleInput struct {
	Version         uint64                   `json:"version"`
	Name            string                   `json:"name" binding:"required"`
	NodeID          string                   `json:"node_id" binding:"required"`
	MetricKey       string                   `json:"metric_key" binding:"required" enums:"node.cpu.busy_percent,node.memory.used_percent,node.filesystem.used_percent"`
	Dimensions      resourcequery.Dimensions `json:"dimensions"`
	TriggerPercent  *float64                 `json:"trigger_percent" binding:"required"`
	RecoveryPercent *float64                 `json:"recovery_percent" binding:"required"`
	TriggerSeconds  *int                     `json:"trigger_seconds" binding:"required"`
	RecoverySeconds *int                     `json:"recovery_seconds" binding:"required"`
	Enabled         *bool                    `json:"enabled" binding:"required"`
}

func (i ResourceAlertRuleInput) pressurePolicy() (models.ResourcePressurePolicy, bool) {
	if i.TriggerPercent == nil || i.RecoveryPercent == nil || i.TriggerSeconds == nil || i.RecoverySeconds == nil {
		return models.ResourcePressurePolicy{}, false
	}
	p := models.ResourcePressurePolicy{TriggerPercent: *i.TriggerPercent, RecoveryPercent: *i.RecoveryPercent, TriggerSeconds: *i.TriggerSeconds, RecoverySeconds: *i.RecoverySeconds}
	return p, p.Valid()
}

type ResourceRulePreset struct {
	MetricKey string                        `json:"metric_key"`
	Policy    models.ResourcePressurePolicy `json:"policy"`
}
type ResourceAlertRulePage struct {
	Presets    []ResourceRulePreset       `json:"presets"`
	Data       []models.ResourceAlertRule `json:"data"`
	Total      int64                      `json:"total"`
	Page       int                        `json:"page"`
	PageSize   int                        `json:"page_size"`
	TotalPages int                        `json:"total_pages"`
}

type ResourceAlertRuleService struct {
	db           *gorm.DB
	observations *ResourceObservationService
	identities   metricsdiscovery.IdentityReader
	alerts       *PlatformAlertService
	startedAt    time.Time
}

func NewResourceAlertRuleService(db *gorm.DB, observations *ResourceObservationService, identities metricsdiscovery.IdentityReader, alerts *PlatformAlertService) *ResourceAlertRuleService {
	return &ResourceAlertRuleService{db: db, observations: observations, identities: identities, alerts: alerts, startedAt: time.Now().UTC()}
}

func resourceRuleUUID(raw string) bool {
	id, err := uuid.Parse(raw)
	return err == nil && id != uuid.Nil && id.String() == raw
}

func resourcePressureMetric(key string) bool {
	return key == "node.cpu.busy_percent" || key == "node.memory.used_percent" || key == models.FilesystemCapacityMetric
}

type ResourceAlertRuleFilter struct {
	NodeID     string
	MetricKey  string
	Dimensions resourcequery.Dimensions
}

func (f ResourceAlertRuleFilter) Valid() bool {
	if (f.NodeID != "" && !resourceRuleUUID(f.NodeID)) || (f.MetricKey != "" && !resourcePressureMetric(f.MetricKey)) {
		return false
	}
	return len(f.Dimensions) == 0 || (f.NodeID != "" && f.MetricKey == models.FilesystemCapacityMetric && validResourceRuleDimensions(f.MetricKey, f.Dimensions))
}

func (s *ResourceAlertRuleService) List(ctx context.Context, page, size int, filter ResourceAlertRuleFilter) (ResourceAlertRulePage, error) {
	result := ResourceAlertRulePage{Presets: []ResourceRulePreset{{MetricKey: "node.cpu.busy_percent", Policy: defaultResourcePressurePolicy("node.cpu.busy_percent")}, {MetricKey: "node.memory.used_percent", Policy: defaultResourcePressurePolicy("node.memory.used_percent")}, {MetricKey: models.FilesystemCapacityMetric, Policy: defaultResourcePressurePolicy(models.FilesystemCapacityMetric)}}, Data: []models.ResourceAlertRule{}, Page: page, PageSize: size}
	if page < 1 || page > 10000 || size < 1 || size > 100 || !filter.Valid() {
		return result, ErrLogInvalid
	}
	if s == nil || s.db == nil {
		return result, ErrLogUnconfigured
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	query := s.db.WithContext(ctx).Model(&models.ResourceAlertRule{})
	if filter.NodeID != "" {
		query = query.Where("node_id=?", filter.NodeID)
	}
	if filter.MetricKey != "" {
		query = query.Where("metric_key=?", filter.MetricKey)
	}
	if len(filter.Dimensions) != 0 {
		d := filter.Dimensions
		query = query.Where("mount_key=? AND dimensions->>'device'=? AND dimensions->>'mountpoint'=? AND dimensions->>'fstype'=?", models.ResourceMountKey(d), d["device"], d["mountpoint"], d["fstype"])
	}
	if err := query.Count(&result.Total).Error; err != nil {
		return result, err
	}
	result.TotalPages = int((result.Total + int64(size) - 1) / int64(size))
	err := query.Order("created_at DESC,id DESC").Offset((page - 1) * size).Limit(size).Find(&result.Data).Error
	return result, err
}

func (s *ResourceAlertRuleService) Save(ctx context.Context, id, token string, input ResourceAlertRuleInput, now time.Time) (models.ResourceAlertRule, error) {
	var result models.ResourceAlertRule
	if input.Dimensions == nil {
		input.Dimensions = resourcequery.Dimensions{}
	}
	if !validResourceRuleDimensions(input.MetricKey, input.Dimensions) {
		return result, ErrLogInvalid
	}
	name := strings.TrimSpace(input.Name)
	policy, validPolicy := input.pressurePolicy()
	if now.IsZero() || input.Enabled == nil || !resourceRuleUUID(input.NodeID) || !resourcePressureMetric(input.MetricKey) || !validPolicy || name == "" || utf8.RuneCountInString(name) > 100 || !utf8.ValidString(name) || strings.IndexFunc(name, unicode.IsControl) >= 0 || (id == "" && (input.Version != 0 || *input.Enabled)) || (id != "" && (!resourceRuleUUID(id) || input.Version == 0)) {
		return result, ErrLogInvalid
	}
	if s == nil || s.db == nil || s.observations == nil || s.observations.nodes == nil {
		return result, ErrLogUnconfigured
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ref, err := s.observations.nodes.GetHostNodeForUser(ctx, input.NodeID, token)
	if err != nil {
		return result, err
	}
	if ref == nil || ref.NodeID != input.NodeID || ref.Version < 1 {
		return result, metricsdiscovery.ErrIdentityUnavailable
	}
	if input.MetricKey == models.FilesystemCapacityMetric {
		verify := id == ""
		if id != "" && *input.Enabled {
			var prior models.ResourceAlertRule
			if err := s.db.WithContext(ctx).Where("id=?", id).Take(&prior).Error; errors.Is(err, gorm.ErrRecordNotFound) {
				return result, ErrLogNotFound
			} else if err != nil {
				return result, err
			}
			verify = !prior.Enabled
		}
		if verify {
			current, err := s.observations.Query(ctx, "monitor:filesystem-rule-admission", input.NodeID, token, []string{models.FilesystemCapacityMetric}, time.Time{}, time.Time{}, false, input.Dimensions)
			if err != nil {
				return result, err
			}
			if current.Collection == nil || current.Collection.State != "collecting" || current.Collection.Filesystem != "available" || len(current.Series) != 1 || current.Series[0].MetricKey != input.MetricKey || !sameResourceDimensions(current.Series[0].Dimensions, input.Dimensions) || len(current.Series[0].Points) != 1 || !validPressurePoint(current.Series[0].Points[0]) {
				return result, ErrLogConflict
			}
		}
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if id == "" {
			if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended('monitor.resource-alert-rules',0))").Error; err != nil {
				return err
			}
			var count int64
			if err := tx.Model(&models.ResourceAlertRule{}).Count(&count).Error; err != nil {
				return err
			}
			if count >= resourceAlertRuleLimit {
				return ErrLogConflict
			}
			result = models.ResourceAlertRule{ID: uuid.NewString(), Version: 1, Name: name, NodeID: input.NodeID, MetricKey: input.MetricKey, Dimensions: input.Dimensions.Copy(), MountKey: models.ResourceMountKey(input.Dimensions), ResourcePressurePolicy: policy, Enabled: false, LastVerdict: string(PlatformAlertPaused), CreatedAt: now, UpdatedAt: now}
			created := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&result)
			if created.Error != nil {
				return created.Error
			}
			if created.RowsAffected != 1 {
				return ErrLogConflict
			}
			return nil
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=?", id).Take(&result).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrLogNotFound
		} else if err != nil {
			return err
		}
		if result.Version != input.Version || result.NodeID != input.NodeID || result.MetricKey != input.MetricKey || !sameResourceDimensions(result.Dimensions, input.Dimensions) {
			return ErrLogConflict
		}
		verdict := PlatformAlertUnknown
		if !*input.Enabled {
			verdict = PlatformAlertPaused
		}
		updates := map[string]any{"version": result.Version + 1, "name": name, "trigger_percent": policy.TriggerPercent, "recovery_percent": policy.RecoveryPercent, "trigger_seconds": policy.TriggerSeconds, "recovery_seconds": policy.RecoverySeconds, "enabled": *input.Enabled, "window_signature": "", "window_since": nil, "last_evaluated_at": nil, "last_verdict": string(verdict), "last_error": "", "last_sample_at": nil, "last_value": nil, "updated_at": now}
		if err := tx.Model(&result).Updates(updates).Error; err != nil {
			return err
		}
		return tx.Where("id=?", id).Take(&result).Error
	})
	return result, err
}

func (s *ResourceAlertRuleService) Delete(ctx context.Context, id string, version uint64) error {
	if !resourceRuleUUID(id) || version == 0 {
		return ErrLogInvalid
	}
	if s == nil || s.db == nil {
		return ErrLogUnconfigured
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rule models.ResourceAlertRule
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=?", id).Take(&rule).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrLogNotFound
		} else if err != nil {
			return err
		}
		if rule.Version != version {
			return ErrLogConflict
		}
		var active int64
		if err := tx.Model(&models.PlatformAlertIncident{}).Where("category=? AND subject_kind=? AND subject_id=? AND signal=? AND status IN ?", models.PlatformAlertCategoryResource, models.PlatformAlertSubjectHostNode, rule.NodeID, models.ResourceAlertSignal(rule.MetricKey, rule.Dimensions), []string{"open", "acknowledged"}).Count(&active).Error; err != nil {
			return err
		}
		if active > 0 {
			return ErrLogConflict
		}
		return tx.Delete(&rule).Error
	})
}
