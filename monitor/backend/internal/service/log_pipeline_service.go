package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/logpipeline"
	"github.com/addp/monitor/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrLogInvalid = errors.New("invalid platform log request")
var ErrLogConflict = errors.New("platform log version or sequence conflict")
var ErrLogNotFound = errors.New("platform log resource not found")

type logModuleLister interface {
	ListModules(context.Context) ([]*client.ModuleInfo, error)
}
type LogPipelineService struct {
	db            *gorm.DB
	node          string
	registry      logModuleLister
	notifications *PlatformLogNotifications
}

func NewLogPipelineService(db *gorm.DB, node string, registry logModuleLister, n *PlatformLogNotifications) *LogPipelineService {
	return &LogPipelineService{db: db, node: node, registry: registry, notifications: n}
}
func (s *LogPipelineService) Initialize(ctx context.Context, now time.Time) error {
	if s.node == "" {
		return nil
	}
	if !logpipeline.Identity.MatchString(s.node) {
		return ErrLogInvalid
	}
	node := models.LogPipelineNode{Node: s.node, StartedAt: now, Signals: map[string]models.LogSignalState{}}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&node).Error
}
func (s *LogPipelineService) Policy(ctx context.Context) (models.LogPipelinePolicy, error) {
	p := defaultLogPolicy()
	err := s.db.WithContext(ctx).FirstOrCreate(&p, models.LogPipelinePolicy{ID: 1}).Error
	return p, err
}
func (s *LogPipelineService) UpdatePolicy(ctx context.Context, p models.LogPipelinePolicy) (models.LogPipelinePolicy, error) {
	if !validLogPolicy(p) {
		return p, ErrLogInvalid
	}
	p.ID = 1
	_, err := s.Policy(ctx)
	if err != nil {
		return p, err
	}
	result := s.db.WithContext(ctx).Model(&models.LogPipelinePolicy{}).Where("id=1 AND version=?", p.Version).Updates(map[string]any{"version": gorm.Expr("version+1"), "failure_samples": p.FailureSamples, "recovery_samples": p.RecoverySamples, "stale_seconds": p.StaleSeconds, "delay_ms": p.DelayMS, "capacity_percent": p.CapacityPercent, "recovery_percent": p.RecoveryPercent, "updated_at": time.Now().UTC()})
	if result.Error != nil {
		return p, result.Error
	}
	if result.RowsAffected != 1 {
		return p, ErrLogConflict
	}
	return s.Policy(ctx)
}
func (s *LogPipelineService) Ingest(ctx context.Context, obs logpipeline.Observation, now time.Time) error {
	if s.node == "" || obs.Node != s.node || obs.Validate(now) != nil {
		return ErrLogInvalid
	}
	p, err := s.Policy(ctx)
	if err != nil {
		return err
	}
	active := map[string]string{}
	registryValid := false
	if s.registry != nil {
		modules, e := s.registry.ListModules(ctx)
		registryValid = e == nil
		if e == nil {
			for _, m := range modules {
				for _, i := range m.Instances {
					if i.Status == "up" && i.LeaseExpiresAt.After(now) && i.HostNodeName == obs.Node {
						active[i.InstanceID] = m.ModuleName
					}
				}
			}
		}
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var node models.LogPipelineNode
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node=?", s.node).Take(&node).Error; err != nil {
			return err
		}
		if node.ReceivedAt != nil {
			if obs.BootID == node.Observation.BootID {
				if obs.Sequence < node.Observation.Sequence {
					return ErrLogConflict
				}
				if obs.Sequence == node.Observation.Sequence {
					a, _ := json.Marshal(obs)
					b, _ := json.Marshal(node.Observation)
					if string(a) == string(b) {
						return nil
					}
					return ErrLogConflict
				}
			} else {
				var count int64
				if err := tx.Model(&models.LogObserverBoot{}).Where("boot_id=?", obs.BootID).Count(&count).Error; err != nil {
					return err
				}
				if count != 0 {
					return ErrLogConflict
				}
			}
			if !obs.SampledAt.After(node.Observation.SampledAt) {
				return ErrLogConflict
			}
		}
		if obs.BootID != node.Observation.BootID {
			if err := tx.Create(&models.LogObserverBoot{BootID: obs.BootID, Node: s.node}).Error; err != nil {
				return err
			}
		}
		evaluateLogObservation(&node, obs, p, active, registryValid, now)
		if err := s.reconcile(tx, node, now); err != nil {
			return err
		}
		// Retire completed instance signal state only after its recovery event is persisted.
		if registryValid {
			for key, state := range node.Signals {
				instance := receiverLogSignalInstance(key)
				if instance != "" && !state.Active {
					if _, exists := active[instance]; !exists {
						delete(node.Signals, key)
					}
				}
			}
		}
		return tx.Save(&node).Error
	})
}
func (s *LogPipelineService) reconcile(tx *gorm.DB, node models.LogPipelineNode, now time.Time) error {
	keys := make([]string, 0, len(node.Signals))
	for key := range node.Signals {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, signal := range keys {
		state := node.Signals[signal]
		var incident models.PlatformLogIncident
		err := tx.Where("node=? AND signal=? AND status IN ?", node.Node, signal, []string{"open", "acknowledged"}).Take(&incident).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		event := ""
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if !state.Active {
				continue
			}
			incident = models.PlatformLogIncident{Version: 1, Node: node.Node, Signal: signal, InstanceID: state.InstanceID, Severity: state.Severity, Status: "open", OpenedAt: now, LastObservedAt: now}
			if err = tx.Create(&incident).Error; err != nil {
				return err
			}
			event = "opened"
		} else {
			incident.Version++
			incident.LastObservedAt = now
			if !state.Active {
				incident.Status = "resolved"
				incident.ResolvedAt = &now
				event = "resolved"
			}
			if err = tx.Save(&incident).Error; err != nil {
				return err
			}
		}
		if event != "" {
			e := models.PlatformLogEvent{ID: uuid.NewString(), IncidentID: incident.ID, Type: event, Severity: incident.Severity, OccurredAt: now}
			if err = tx.Create(&e).Error; err != nil {
				return err
			}
			if s.notifications != nil {
				if err = s.notifications.RecordTx(tx, e, incident, now); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
func (s *LogPipelineService) CheckStale(ctx context.Context, now time.Time) error {
	if s.node == "" {
		return nil
	}
	p, err := s.Policy(ctx)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var node models.LogPipelineNode
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node=?", s.node).Take(&node).Error; err != nil {
			return err
		}
		last := node.StartedAt
		if node.ReceivedAt != nil {
			last = *node.ReceivedAt
		}
		if now.Sub(last) <= time.Duration(p.StaleSeconds)*time.Second {
			return nil
		}
		if node.Signals == nil {
			node.Signals = map[string]models.LogSignalState{}
		}
		signal := node.Signals["observation_missing"]
		signal.Active = true
		signal.Severity = "critical"
		signal.Successes = 0
		node.Signals["observation_missing"] = signal
		for key, sig := range node.Signals {
			sig.Successes = 0
			sig.Failures = 0
			node.Signals[key] = sig
		}
		if err := s.reconcile(tx, node, now); err != nil {
			return err
		}
		return tx.Save(&node).Error
	})
}

type LogPipelineSummary struct {
	Configured    bool                         `json:"configured"`
	Health        string                       `json:"health"`
	Node          *models.LogPipelineNode      `json:"node,omitempty"`
	Incidents     []models.PlatformLogIncident `json:"incidents"`
	Notifications string                       `json:"notifications"`
}

func (s *LogPipelineService) Summary(ctx context.Context, now time.Time) (LogPipelineSummary, error) {
	result := LogPipelineSummary{Configured: s.node != "", Health: "unknown", Notifications: "unconfigured", Incidents: []models.PlatformLogIncident{}}
	if s.node == "" {
		return result, nil
	}
	p, err := s.Policy(ctx)
	if err != nil {
		return result, err
	}
	var node models.LogPipelineNode
	if err = s.db.WithContext(ctx).Where("node=?", s.node).Take(&node).Error; err != nil {
		return result, err
	}
	result.Node = &node
	if err = s.db.WithContext(ctx).Where("node=? AND status IN ?", s.node, []string{"open", "acknowledged"}).Order("opened_at DESC,id DESC").Find(&result.Incidents).Error; err != nil {
		return result, err
	}
	result.Health = logPipelineHealth(node, p, len(result.Incidents), now)
	if s.notifications != nil {
		result.Notifications, err = s.notifications.Status(ctx)
	}
	return result, err
}

func logPipelineHealth(node models.LogPipelineNode, p models.LogPipelinePolicy, incidents int, now time.Time) string {
	if node.ReceivedAt == nil || now.Sub(*node.ReceivedAt) > time.Duration(p.StaleSeconds)*time.Second {
		return "unknown"
	}
	if incidents > 0 {
		return "alert"
	}
	if node.RegistryValid && node.Observation.APIReady && node.Observation.ProbeDelivered && node.Observation.Collector.Valid && node.Observation.SourcesValid && node.Observation.HousekeepingValid {
		return "healthy"
	}
	return "unknown"
}

func (s *LogPipelineService) ManageIncident(ctx context.Context, id uint, version uint64, action string, until *time.Time, principal int64, now time.Time) (models.PlatformLogIncident, error) {
	var i models.PlatformLogIncident
	if version == 0 || principal <= 0 || (action != "acknowledge" && action != "suppress") || (action == "suppress" && (until == nil || !until.After(now) || until.After(now.Add(24*time.Hour)))) {
		return i, ErrLogInvalid
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND node=?", id, s.node).Take(&i).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrLogNotFound
		} else if err != nil {
			return err
		}
		if i.Version != version {
			return ErrLogConflict
		}
		if i.Status == "resolved" {
			return ErrLogConflict
		}
		i.Version++
		if action == "acknowledge" {
			i.Status = "acknowledged"
			i.AcknowledgedAt = &now
			i.AcknowledgedBy = &principal
		} else {
			i.SuppressedUntil = until
		}
		return tx.Save(&i).Error
	})
	return i, err
}
func (s *LogPipelineService) Run(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if err := s.CheckStale(ctx, now.UTC()); err != nil {
				fmt.Println("platform log stale evaluation failed")
			}
		}
	}
}
func logSignalName(signal string) string { return strings.SplitN(signal, ":", 2)[0] }
