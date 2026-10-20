package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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

type HTTPAlertRuleInput struct {
	Version         uint64   `json:"version"`
	Name            string   `json:"name" binding:"required"`
	InstanceID      uint     `json:"instance_id" binding:"required"`
	Route           *string  `json:"route"`
	Method          string   `json:"method"`
	MetricKey       string   `json:"metric_key" binding:"required" enums:"http.requests.per_second,http.failures.percent,http.duration.mean_seconds"`
	TriggerValue    *float64 `json:"trigger_value" binding:"required"`
	RecoveryValue   *float64 `json:"recovery_value" binding:"required"`
	TriggerSeconds  *int     `json:"trigger_seconds" binding:"required"`
	RecoverySeconds *int     `json:"recovery_seconds" binding:"required"`
	Enabled         *bool    `json:"enabled" binding:"required"`
}

func (i HTTPAlertRuleInput) policy() (models.HTTPThresholdPolicy, bool) {
	if i.TriggerValue == nil || i.RecoveryValue == nil || i.TriggerSeconds == nil || i.RecoverySeconds == nil {
		return models.HTTPThresholdPolicy{}, false
	}
	p := models.HTTPThresholdPolicy{TriggerValue: *i.TriggerValue, RecoveryValue: *i.RecoveryValue, TriggerSeconds: *i.TriggerSeconds, RecoverySeconds: *i.RecoverySeconds}
	return p, p.Valid(i.MetricKey)
}

type HTTPAlertRulePage struct {
	Data       []models.HTTPAlertRule `json:"data"`
	Total      int64                  `json:"total"`
	Page       int                    `json:"page"`
	PageSize   int                    `json:"page_size"`
	TotalPages int                    `json:"total_pages"`
}
type HTTPAlertRuleService struct {
	db           *gorm.DB
	observations *ResourceObservationService
	identities   metricsdiscovery.IdentityReader
	alerts       *PlatformAlertService
	startedAt    time.Time
}

func NewHTTPAlertRuleService(db *gorm.DB, observations *ResourceObservationService, identities metricsdiscovery.IdentityReader, alerts *PlatformAlertService) *HTTPAlertRuleService {
	return &HTTPAlertRuleService{db: db, observations: observations, identities: identities, alerts: alerts, startedAt: time.Now().UTC()}
}
func (s *HTTPAlertRuleService) List(ctx context.Context, page, size int) (HTTPAlertRulePage, error) {
	result := HTTPAlertRulePage{Data: []models.HTTPAlertRule{}, Page: page, PageSize: size}
	if page < 1 || page > 10000 || size < 1 || size > 100 {
		return result, ErrLogInvalid
	}
	if s == nil || s.db == nil {
		return result, ErrLogUnconfigured
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.HTTPAlertRule{}).Count(&result.Total).Error; err != nil {
			return err
		}
		return tx.Order("created_at DESC,id DESC").Offset((page - 1) * size).Limit(size).Find(&result.Data).Error
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	result.TotalPages = int((result.Total + int64(size) - 1) / int64(size))
	return result, err
}
func (s *HTTPAlertRuleService) Save(ctx context.Context, id, token string, input HTTPAlertRuleInput, now time.Time) (models.HTTPAlertRule, error) {
	var result models.HTTPAlertRule
	policy, valid := input.policy()
	name := strings.TrimSpace(input.Name)
	if !valid || input.InstanceID == 0 || input.Enabled == nil || now.IsZero() || name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 100 || strings.IndexFunc(name, unicode.IsControl) >= 0 || (id == "" && (input.Version != 0 || *input.Enabled)) || (id != "" && (!resourceRuleUUID(id) || input.Version == 0)) {
		return result, ErrLogInvalid
	}
	at := now.UTC().Truncate(time.Second)
	if _, err := resourcequery.NewHTTPPlan(at, at, at, false, false, input.Route, input.Method, 60, resourcequery.DefaultBudget()); err != nil {
		return result, ErrLogInvalid
	}
	if s == nil || s.db == nil || s.observations == nil || s.observations.process.Users == nil {
		return result, ErrLogUnconfigured
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	refs, err := s.observations.process.Users.GetRuntimeInstancesForUser(ctx, []uint{input.InstanceID}, token)
	if err != nil {
		return result, err
	}
	if len(refs) != 1 || refs[0].ID != input.InstanceID || (refs[0].Role != "backend" && refs[0].Role != "ingress") || refs[0].ModuleName == "" {
		return result, metricsdiscovery.ErrIdentityUnavailable
	}
	ref := refs[0]
	encoded, _ := json.Marshal([]any{ref.ModuleName, ref.Role, input.Route, input.Method, input.MetricKey})
	subjectKey := fmt.Sprintf("%x", sha256.Sum256(encoded))
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if id == "" {
			if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended('monitor.http-alert-rules',0))").Error; err != nil {
				return err
			}
			var count int64
			if err := tx.Model(&models.HTTPAlertRule{}).Count(&count).Error; err != nil {
				return err
			}
			if count >= 1000 {
				return ErrLogConflict
			}
			result = models.HTTPAlertRule{ID: uuid.NewString(), SubjectKey: subjectKey, AnchorInstanceID: input.InstanceID, Version: 1, Name: name, ModuleName: ref.ModuleName, Role: ref.Role, Route: input.Route, Method: input.Method, MetricKey: input.MetricKey, HTTPThresholdPolicy: policy, Enabled: false, LastVerdict: string(PlatformAlertPaused), CreatedAt: now, UpdatedAt: now}
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
		if result.Version != input.Version || result.SubjectKey != subjectKey {
			return ErrLogConflict
		}
		verdict := PlatformAlertUnknown
		if !*input.Enabled {
			verdict = PlatformAlertPaused
		}
		if err := tx.Model(&result).Updates(map[string]any{"version": result.Version + 1, "name": name, "anchor_instance_id": input.InstanceID, "trigger_value": policy.TriggerValue, "recovery_value": policy.RecoveryValue, "trigger_seconds": policy.TriggerSeconds, "recovery_seconds": policy.RecoverySeconds, "enabled": *input.Enabled, "window_signature": "", "window_since": nil, "last_evaluated_at": nil, "last_verdict": string(verdict), "last_error": "", "last_value": nil, "updated_at": now}).Error; err != nil {
			return err
		}
		return tx.Where("id=?", id).Take(&result).Error
	})
	return result, err
}
func (s *HTTPAlertRuleService) Delete(ctx context.Context, id string, version uint64) error {
	if !resourceRuleUUID(id) || version == 0 {
		return ErrLogInvalid
	}
	if s == nil || s.db == nil {
		return ErrLogUnconfigured
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rule models.HTTPAlertRule
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=?", id).Take(&rule).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrLogNotFound
		} else if err != nil {
			return err
		}
		if rule.Version != version {
			return ErrLogConflict
		}
		var active int64
		if err := tx.Model(&models.PlatformAlertIncident{}).Where("category=? AND subject_id=? AND signal=? AND status IN ?", models.PlatformAlertCategoryHTTP, rule.ModuleName, rule.ID, []string{"open", "acknowledged"}).Count(&active).Error; err != nil {
			return err
		}
		if active > 0 {
			return ErrLogConflict
		}
		return tx.Delete(&rule).Error
	})
}
