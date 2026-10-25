package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	commonclient "github.com/addp/common/client"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/monitor/internal/metricsdiscovery"
	"github.com/addp/monitor/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type UserModuleReader interface {
	GetModulesForUser(context.Context, string) ([]commonclient.ModuleReference, error)
}
type ServiceAvailabilityRuleInput struct {
	Version         uint64 `json:"version"`
	Name            string `json:"name" binding:"required"`
	GroupID         string `json:"group_id" binding:"required"`
	TriggerSeconds  *int   `json:"trigger_seconds" binding:"required"`
	RecoverySeconds *int   `json:"recovery_seconds" binding:"required"`
	Enabled         *bool  `json:"enabled" binding:"required"`
}
type ServiceAvailabilityGroup struct {
	ID               string `json:"id"`
	DeploymentID     string `json:"deployment_id"`
	ModuleName       string `json:"module_name"`
	Role             string `json:"role"`
	RuntimeName      string `json:"runtime_name"`
	ExpectedReplicas int    `json:"expected_replicas"`
	DesiredState     string `json:"desired_state"`
	State            string `json:"state"`
	Reason           string `json:"reason"`
	OnlineReplicas   int    `json:"online_replicas"`
}
type ServiceAvailabilityGroupPage struct {
	Data  []ServiceAvailabilityGroup `json:"data"`
	Total int                        `json:"total"`
}
type ServiceAvailabilityRulePage struct {
	Data       []models.ServiceAvailabilityRule `json:"data"`
	Total      int64                            `json:"total"`
	Page       int                              `json:"page"`
	PageSize   int                              `json:"page_size"`
	TotalPages int                              `json:"total_pages"`
}
type ServiceAvailabilityRuleService struct {
	db         *gorm.DB
	users      UserModuleReader
	identities metricsdiscovery.IdentityReader
	alerts     *PlatformAlertService
	startedAt  time.Time
	now        func() time.Time
}

func NewServiceAvailabilityRuleService(db *gorm.DB, users UserModuleReader, identities metricsdiscovery.IdentityReader, alerts *PlatformAlertService) *ServiceAvailabilityRuleService {
	return &ServiceAvailabilityRuleService{db: db, users: users, identities: identities, alerts: alerts, startedAt: time.Now().UTC(), now: time.Now}
}
func availabilityHash(parts ...string) string {
	b, _ := json.Marshal(parts)
	return fmt.Sprintf("%x", sha256.Sum256(b))
}
func availabilitySubject(deployment, module, role, purpose string) string {
	return availabilityHash(deployment, module, role, purpose)
}
func availabilityCanonicalHash(value string) bool {
	b, err := hex.DecodeString(value)
	return err == nil && len(b) == 32 && hex.EncodeToString(b) == value
}
func (s *ServiceAvailabilityRuleService) modules(ctx context.Context, token string) (map[string]bool, error) {
	if s == nil || s.users == nil {
		return nil, ErrLogUnconfigured
	}
	rows, err := s.users.GetModulesForUser(ctx, token)
	if err != nil {
		return nil, err
	}
	allowed := map[string]bool{}
	if len(rows) > 1000 {
		return nil, metricsdiscovery.ErrIdentityUnavailable
	}
	for _, r := range rows {
		if r.ModuleName == "" || r.Enabled == nil || r.Version < 1 || allowed[r.ModuleName] {
			return nil, metricsdiscovery.ErrIdentityUnavailable
		}
		allowed[r.ModuleName] = true
	}
	return allowed, nil
}
func (s *ServiceAvailabilityRuleService) snapshot(ctx context.Context) (*commonmodels.ServiceGroupSnapshot, error) {
	if s == nil || s.identities == nil {
		return nil, metricsdiscovery.ErrUnconfigured
	}
	before := s.now().UTC()
	snapshot, err := s.identities.GetObservabilityIdentities(ctx)
	if err != nil {
		return nil, err
	}
	if snapshot == nil || snapshot.ServiceGroups == nil || snapshot.ServiceGroups.Validate(snapshot.ObservedAt) != nil || snapshot.ServiceGroups.State != "available" || snapshot.ObservedAt.Before(before.Add(-5*time.Second)) || snapshot.ObservedAt.After(s.now().UTC().Add(5*time.Second)) {
		return nil, metricsdiscovery.ErrIdentityUnavailable
	}
	return snapshot.ServiceGroups, nil
}
func (s *ServiceAvailabilityRuleService) Groups(ctx context.Context, token string) (ServiceAvailabilityGroupPage, error) {
	result := ServiceAvailabilityGroupPage{Data: []ServiceAvailabilityGroup{}}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	allowed, err := s.modules(ctx, token)
	if err != nil {
		return result, err
	}
	snapshot, err := s.snapshot(ctx)
	if err != nil {
		return result, err
	}
	for _, g := range snapshot.Groups {
		if !allowed[g.ModuleName] {
			continue
		}
		result.Data = append(result.Data, ServiceAvailabilityGroup{ID: availabilitySubject(snapshot.DeploymentID, g.ModuleName, g.Role, g.RuntimeName), DeploymentID: snapshot.DeploymentID, ModuleName: g.ModuleName, Role: g.Role, RuntimeName: g.RuntimeName, ExpectedReplicas: g.ExpectedReplicas, DesiredState: g.DesiredState, State: g.State, Reason: g.Reason, OnlineReplicas: g.OnlineReplicas})
	}
	result.Total = len(result.Data)
	return result, nil
}
func (s *ServiceAvailabilityRuleService) List(ctx context.Context, token string, page, size int) (ServiceAvailabilityRulePage, error) {
	result := ServiceAvailabilityRulePage{Data: []models.ServiceAvailabilityRule{}, Page: page, PageSize: size}
	if page < 1 || page > 10000 || size < 1 || size > 100 {
		return result, ErrLogInvalid
	}
	if s == nil || s.db == nil {
		return result, ErrLogUnconfigured
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	allowed, err := s.modules(ctx, token)
	if err != nil {
		return result, err
	}
	names := []string{}
	for name := range allowed {
		names = append(names, name)
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		q := tx.Model(&models.ServiceAvailabilityRule{}).Where("module_name IN ?", names)
		if err := q.Count(&result.Total).Error; err != nil {
			return err
		}
		return q.Order("created_at DESC,id DESC").Offset((page - 1) * size).Limit(size).Find(&result.Data).Error
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	result.TotalPages = int((result.Total + int64(size) - 1) / int64(size))
	return result, err
}
func (s *ServiceAvailabilityRuleService) Save(ctx context.Context, id, token string, input ServiceAvailabilityRuleInput, at time.Time) (models.ServiceAvailabilityRule, error) {
	var result models.ServiceAvailabilityRule
	name := strings.TrimSpace(input.Name)
	if input.TriggerSeconds == nil || input.RecoverySeconds == nil || input.Enabled == nil || at.IsZero() || name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 100 || strings.IndexFunc(name, unicode.IsControl) >= 0 || !availabilityCanonicalHash(input.GroupID) || (id == "" && (input.Version != 0 || *input.Enabled)) || (id != "" && (!resourceRuleUUID(id) || input.Version == 0)) {
		return result, ErrLogInvalid
	}
	policy := models.ServiceAvailabilityPolicy{TriggerSeconds: *input.TriggerSeconds, RecoverySeconds: *input.RecoverySeconds}
	if !policy.Valid() {
		return result, ErrLogInvalid
	}
	if s == nil || s.db == nil {
		return result, ErrLogUnconfigured
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	allowed, err := s.modules(ctx, token)
	if err != nil {
		return result, err
	}
	var group ServiceAvailabilityGroup
	if id == "" {
		groups, err := s.Groups(ctx, token)
		if err != nil {
			return result, err
		}
		for _, g := range groups.Data {
			if g.ID == input.GroupID {
				group = g
				break
			}
		}
		if group.ID == "" {
			return result, metricsdiscovery.ErrIdentityUnavailable
		}
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if id == "" {
			if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended('monitor.service-availability-rules',0))").Error; err != nil {
				return err
			}
			var count int64
			if err := tx.Model(&models.ServiceAvailabilityRule{}).Count(&count).Error; err != nil {
				return err
			}
			if count >= 1000 {
				return ErrLogConflict
			}
			result = models.ServiceAvailabilityRule{ID: uuid.NewString(), SubjectKey: group.ID, Version: 1, Name: name, DeploymentID: group.DeploymentID, ModuleName: group.ModuleName, Role: group.Role, RuntimeName: group.RuntimeName, ServiceAvailabilityPolicy: policy, Enabled: false, LastVerdict: string(PlatformAlertPaused), CreatedAt: at, UpdatedAt: at}
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
		if !allowed[result.ModuleName] {
			return metricsdiscovery.ErrIdentityUnavailable
		}
		if result.Version != input.Version || result.SubjectKey != input.GroupID {
			return ErrLogConflict
		}
		updates := map[string]any{"version": result.Version + 1, "name": name, "trigger_seconds": policy.TriggerSeconds, "recovery_seconds": policy.RecoverySeconds, "enabled": *input.Enabled, "window_signature": "", "window_since": nil, "window_samples": 0, "last_evaluated_at": nil, "last_observed_at": nil, "last_error": "", "online_replicas": nil, "last_verdict": string(PlatformAlertUnknown), "updated_at": at}
		if !*input.Enabled || !result.Enabled {
			updates["confirmed_signature"], updates["confirmed_at"] = "", nil
		}
		if !*input.Enabled {
			updates["last_verdict"] = string(PlatformAlertPaused)
		}
		if err := tx.Model(&result).Updates(updates).Error; err != nil {
			return err
		}
		result = models.ServiceAvailabilityRule{}
		return tx.Where("id=?", id).Take(&result).Error
	})
	return result, err
}
func (s *ServiceAvailabilityRuleService) Delete(ctx context.Context, token, id string, version uint64) error {
	if !resourceRuleUUID(id) || version == 0 {
		return ErrLogInvalid
	}
	if s == nil || s.db == nil {
		return ErrLogUnconfigured
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	allowed, err := s.modules(ctx, token)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var r models.ServiceAvailabilityRule
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=?", id).Take(&r).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrLogNotFound
		} else if err != nil {
			return err
		}
		if !allowed[r.ModuleName] {
			return metricsdiscovery.ErrIdentityUnavailable
		}
		if r.Version != version {
			return ErrLogConflict
		}
		var count int64
		if err := tx.Model(&models.PlatformAlertIncident{}).Where("category=? AND subject_id=? AND signal=? AND status IN ?", models.PlatformAlertCategoryAvailability, r.SubjectKey, r.ID, []string{"open", "acknowledged"}).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return ErrLogConflict
		}
		return tx.Delete(&r).Error
	})
}

// validAvailabilityEvidence verifies the frozen source facts before publishing
// either an incident or an outbox event. It never reconstructs historical names.
func validAvailabilityEvidence(e *models.ServiceAvailabilityEvidence, subject, signal string) bool {
	if e == nil || !e.Policy.Valid() || e.RuleID != signal || !resourceRuleUUID(signal) || e.RuleVersion < 1 || !availabilityCanonicalHash(e.DeploymentID) || availabilitySubject(e.DeploymentID, e.ModuleName, e.Role, e.RuntimeName) != subject || e.RuleName == "" || !utf8.ValidString(e.RuleName) || utf8.RuneCountInString(e.RuleName) > 100 || strings.IndexFunc(e.RuleName, unicode.IsControl) >= 0 || e.OnlineReplicas < 0 || e.OnlineReplicas > commonmodels.ObservabilityIdentityInstanceLimit || e.Samples < 2 {
		return false
	}
	intent := commonmodels.ServiceGroupExpectation{ModuleName: e.ModuleName, Role: e.Role, RuntimeName: e.RuntimeName, ExpectedReplicas: e.ExpectedReplicas, DesiredState: "running", ChangedAt: e.IntentChangedAt}
	return intent.Valid(e.ObservedTo) && !e.ConfirmedAt.IsZero() && !e.ObservedFrom.IsZero() && !e.ObservedTo.IsZero() && !e.ConfirmedAt.Before(e.IntentChangedAt) && !e.ObservedFrom.Before(e.ConfirmedAt) && !e.ObservedTo.Before(e.ObservedFrom)
}
