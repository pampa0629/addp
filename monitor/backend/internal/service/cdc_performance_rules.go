package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/addp/monitor/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var cdcPerformanceCodes = []string{models.AlertRuleCDCStalled, models.AlertRuleCDCDelayed, models.AlertRuleCDCBacklog}

// advanceCDCWindow consumes distinct Owner samples, never evaluator wall time.
func advanceCDCWindow(rule models.AlertRule, row commonClient.TransferCDCRiskItem, now time.Time) models.CDCPerformanceWindow {
	w := rule.CDCWindow
	if row.State == "closed" {
		return models.CDCPerformanceWindow{State: "closed"}
	}
	reset := func(state string) models.CDCPerformanceWindow {
		w.ResetAt = &now
		w.TriggerSince = nil
		w.RecoverySince = nil
		w.SampledAt = nil
		w.ExpiresAt = nil
		w.PreviousValue = ""
		w.State = state
		return w
	}
	if row.State != "observing" {
		return reset("unknown")
	}
	if w.Generation != row.Generation {
		w = models.CDCPerformanceWindow{Generation: row.Generation, ResetAt: w.ResetAt}
	}
	if row.ConsumptionState != "running" && row.ConsumptionState != "paused" && row.ConsumptionState != "blocked" {
		return reset("unknown")
	}
	if rule.RuleType != models.AlertRuleCDCDelayed && (row.ConsumptionState == "paused" || row.ConsumptionState == "blocked") {
		return reset("paused")
	}
	if row.InitialSnapshotCompleted == nil {
		return reset("unknown")
	}
	if !*row.InitialSnapshotCompleted {
		return reset("initializing")
	}
	if rule.RuleType != models.AlertRuleCDCDelayed && w.State == "paused" {
		return reset("unknown")
	}
	b := row.BufferSample
	if rule.RuleType != models.AlertRuleCDCDelayed && (b == nil || b.SampledAt.After(now) || !b.ExpiresAt.After(now)) {
		return reset("unknown")
	}
	var at, expiry time.Time
	value := ""
	if b != nil {
		at, expiry, value = b.SampledAt, b.ExpiresAt, b.CommittedOffset
	}
	if rule.RuleType == models.AlertRuleCDCDelayed {
		d := row.DelaySample
		if d == nil || d.SampledAt.After(now) || !d.ExpiresAt.After(now) || d.DelaySeconds < 0 {
			return reset("unknown")
		}
		at, expiry, value = d.SampledAt, d.ExpiresAt, d.EventCount
	}
	if w.SampledAt != nil && at.Equal(*w.SampledAt) {
		return w
	}
	if w.ResetAt != nil && !at.After(*w.ResetAt) || w.SampledAt != nil && at.Before(*w.SampledAt) {
		return reset("unknown")
	}
	if w.SampledAt == nil || w.ExpiresAt == nil || at.After(*w.ExpiresAt) {
		w = models.CDCPerformanceWindow{Generation: row.Generation, Active: w.Active}
	}
	bad, good, immediate := false, false, false
	switch rule.RuleType {
	case models.AlertRuleCDCStalled:
		lag, _ := strconv.ParseInt(b.LagOffsets, 10, 64)
		previous, _ := strconv.ParseInt(w.PreviousValue, 10, 64)
		current, _ := strconv.ParseInt(value, 10, 64)
		if w.PreviousValue != "" && current < previous {
			w = reset("unknown")
			w.SampledAt = &at
			w.ExpiresAt = &expiry
			w.PreviousValue = value
			return w
		}
		bad = lag > 0 && (w.PreviousValue == "" || w.PreviousValue == value)
		good = w.PreviousValue != "" && current > previous
		immediate = lag == 0
	case models.AlertRuleCDCBacklog:
		lag, _ := strconv.ParseInt(b.LagOffsets, 10, 64)
		limit, _ := strconv.ParseInt(rule.BacklogThreshold, 10, 64)
		bad = lag > limit
		good = lag <= (limit-1)/2
		value = b.LagOffsets
	case models.AlertRuleCDCDelayed:
		d := row.DelaySample
		count, _ := strconv.ParseInt(value, 10, 64)
		previous, _ := strconv.ParseInt(w.PreviousValue, 10, 64)
		if w.PreviousValue == "" || count <= previous || d.LastEventAgeSeconds > d.ExpiresAt.Sub(d.SampledAt).Seconds() {
			state := "idle"
			if w.PreviousValue != "" && count < previous {
				state = "unknown"
			}
			w = reset(state)
			w.SampledAt = &at
			w.ExpiresAt = &expiry
			w.PreviousValue = value
			return w
		}
		bad = d.DelaySeconds > 60
		good = d.DelaySeconds <= 30
	}
	w.Generation = row.Generation
	w.SampledAt = &at
	w.ExpiresAt = &expiry
	w.PreviousValue = value
	if immediate {
		w.Active = false
		w.TriggerSince = nil
		w.RecoverySince = nil
		w.State = "healthy"
		return w
	}
	if bad {
		if rule.RuleType != models.AlertRuleCDCStalled {
			w.RecoverySince = nil
		}
		if w.TriggerSince == nil {
			w.TriggerSince = &at
		}
		seconds := 180
		if rule.RuleType == models.AlertRuleCDCBacklog {
			seconds = 300
		}
		if at.Sub(*w.TriggerSince) >= time.Duration(seconds)*time.Second {
			w.Active = true
			w.RecoverySince = nil
		}
	} else {
		w.TriggerSince = nil
	}
	if good {
		if w.RecoverySince == nil {
			w.RecoverySince = &at
		}
		if at.Sub(*w.RecoverySince) >= time.Minute {
			w.Active = false
		}
	} else if rule.RuleType != models.AlertRuleCDCStalled {
		w.RecoverySince = nil
	}
	w.State = "healthy"
	if w.Active {
		w.State = "active"
	} else if bad {
		w.State = "pending"
	}
	return w
}

func (s *AlertService) evaluateCDCPerformanceRules(ctx context.Context, rows []commonClient.TransferCDCRiskItem, complete bool, now time.Time) ([]activeAlertSignal, []string, error) {
	signals := []activeAlertSignal{}
	preserve := []string{}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rules []models.AlertRule
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("enabled=? AND rule_type IN ?", true, cdcPerformanceCodes).Order("id").Find(&rules).Error; err != nil {
			return err
		}
		for _, rule := range rules {
			row := commonClient.TransferCDCRiskItem{State: "unknown"}
			found := false
			for _, candidate := range rows {
				if candidate.TenantID == uint(rule.TenantID) && strconv.FormatUint(uint64(candidate.TaskID), 10) == rule.SourceTaskID {
					row = candidate
					found = true
					break
				}
			}
			if complete && !found {
				row.State = "closed"
			}
			w := advanceCDCWindow(rule, row, now)
			raw, err := json.Marshal(w)
			if err != nil {
				return err
			}
			if err = tx.Model(&models.AlertRule{}).Where("id=?", rule.ID).UpdateColumn("cdc_window", string(raw)).Error; err != nil {
				return err
			}
			paused := w.State == "unknown" || w.State == "paused" || w.State == "idle" || w.State == "initializing"
			if paused {
				var current []models.AlertIncident
				if err = tx.Where("alert_rule_id=? AND status IN ?", rule.ID, []string{"open", "acknowledged"}).Find(&current).Error; err != nil {
					return err
				}
				for _, incident := range current {
					preserve = append(preserve, incident.Fingerprint)
					details := incident.Details
					if details == nil {
						details = commonModels.JSONMap{}
					}
					details["evaluation_state"] = w.State
					if err = tx.Model(&models.AlertIncident{}).Where("id=?", incident.ID).UpdateColumn("details", details).Error; err != nil {
						return err
					}
				}
				continue
			}
			if !w.Active {
				continue
			}
			digest := sha256.Sum256([]byte(fmt.Sprintf("cdc-performance|%s|%d", rule.RuleID, row.Generation)))
			details := commonModels.JSONMap{"generation": strconv.FormatUint(row.Generation, 10), "sampled_at": w.SampledAt.UTC().Format(time.RFC3339Nano), "evaluation_state": w.State}
			if rule.RuleType == models.AlertRuleCDCDelayed {
				details["delay_seconds"] = row.DelaySample.DelaySeconds
			} else {
				details["lag_offsets"] = row.BufferSample.LagOffsets
			}
			if rule.RuleType == models.AlertRuleCDCBacklog {
				details["backlog_threshold"] = rule.BacklogThreshold
			}
			seconds := 180
			if rule.RuleType == models.AlertRuleCDCBacklog {
				seconds = 300
			}
			details["trigger_seconds"] = seconds
			signals = append(signals, activeAlertSignal{TenantID: rule.TenantID, Module: rule.Module, TaskType: rule.TaskType, SourceTaskID: rule.SourceTaskID, ExecutionID: row.ExecutionID, AlertRuleID: &rule.ID, RuleID: rule.RuleID, RuleName: rule.Name, RuleThreshold: rule.FailureThreshold, RuleBacklogThreshold: rule.BacklogThreshold, Code: rule.RuleType, Severity: "warning", Details: details, Fingerprint: hex.EncodeToString(digest[:])})
		}
		return nil
	})
	return signals, preserve, err
}
