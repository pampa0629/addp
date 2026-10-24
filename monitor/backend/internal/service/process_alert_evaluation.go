package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	commonmodels "github.com/addp/common/models"
	"github.com/addp/monitor/internal/metricsdiscovery"
	"github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/resourcequery"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *ProcessAlertRuleService) sources(ctx context.Context, rule models.ProcessAlertRule, version uint64) (runtimeAlertSources, error) {
	source, err := runtimeSources(ctx, s.observations, s.identities, version, func(r commonmodels.ObservabilityModuleIdentity) bool {
		return r.ModuleName == rule.ModuleName && r.InstanceID == rule.RuntimeInstanceID
	})
	if err != nil {
		return source, err
	}
	if len(source.scopes) != 1 || source.scopes[0].Role != rule.Role || source.scopes[0].StartedAt.Format(time.RFC3339Nano) != rule.ProcessStartedAt {
		return runtimeAlertSources{}, metricsdiscovery.ErrIdentityUnavailable
	}
	return source, nil
}
func (s *ProcessAlertRuleService) Run(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		if err := s.Evaluate(ctx, time.Now().UTC().Truncate(time.Second)); err != nil && ctx.Err() == nil {
			log.Print("Process resource alert evaluation unavailable")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *ProcessAlertRuleService) Evaluate(parent context.Context, at time.Time) error {
	if s == nil || s.db == nil || at.IsZero() || at.Nanosecond() != 0 {
		return ErrLogInvalid
	}
	round, finish := context.WithTimeout(parent, 10*time.Second)
	defer finish()
	ctx, cancel := context.WithTimeout(round, 5*time.Second)
	defer cancel()
	var rules []models.ProcessAlertRule
	if err := s.db.WithContext(ctx).Where("enabled=?", true).Order("module_name,role,id").Limit(1001).Find(&rules).Error; err != nil {
		return err
	}
	if len(rules) > 1000 {
		return resourcequery.ErrBudget
	}
	if len(rules) == 0 {
		return nil
	}
	o := s.observations
	var backend ProcessResourceBackend
	if o != nil {
		backend = o.process.Backend
	}
	for _, rule := range rules {
		reason := ""
		var source runtimeAlertSources
		var cfg ResourceQueryPolicyResponse
		var lines []resourcequery.Series
		if o == nil || backend == nil {
			reason = "unconfigured"
		} else if !o.enabled {
			reason = "collection_disabled"
		}
		var release func()
		if reason == "" {
			var err error
			release, err = o.limiter.Acquire("monitor:process-alert-evaluator", resourcequery.DefaultBudget())
			if err != nil {
				reason = "query_budget"
			}
		}
		if reason == "" {
			var err error
			cfg, err = o.Policy(ctx)
			if err != nil {
				reason = "query_policy"
			} else if o.limiter.Check("monitor:process-alert-evaluator", cfg.Budget) != nil {
				reason = "query_budget"
			}
		}
		if reason == "" {
			var err error
			source, err = s.sources(ctx, rule, cfg.Version)
			if err != nil {
				reason = "identity_unavailable"
			}
		}
		if reason == "" {
			p, err := resourcequery.NewProcessTrendPlan(at.Add(-315*time.Second), at, at, cfg.Budget)
			if err != nil || p.StepSeconds != 15 {
				reason = "query_budget"
			} else {
				scope := source.scopes[0]
				summaries, err := backend.ProcessSummaries(ctx, []resourcequery.ProcessScope{scope}, at, cfg.Budget)
				if err != nil || summaries[scope.ID].Collection.State != "collecting" {
					reason = "no_data"
				} else {
					rows, err := backend.ProcessTrend(ctx, p, scope, cfg.Budget)
					if err != nil {
						reason = "query_unavailable"
					} else {
						for _, line := range rows {
							if line.MetricKey == rule.MetricKey {
								lines = append(lines, line)
							}
						}
						if len(lines) != 1 {
							reason = "no_data"
						}
					}
				}
			}
		}
		if reason == "" {
			current, err := s.sources(ctx, rule, cfg.Version)
			if err != nil || current.signature != source.signature || !source.lease.After(time.Now().UTC()) {
				reason = "source_changed"
			}
		}
		if release != nil {
			release()
		}
		writeCtx, done := context.WithTimeout(round, 5*time.Second)
		err := s.applyProcessRule(writeCtx, rule, source, cfg, lines, at, reason)
		done()
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *ProcessAlertRuleService) applyProcessRule(ctx context.Context, old models.ProcessAlertRule, source runtimeAlertSources, cfg ResourceQueryPolicyResponse, series []resourcequery.Series, at time.Time, reason string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rule models.ProcessAlertRule
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=?", old.ID).Take(&rule).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		} else if err != nil {
			return err
		}
		if rule.Version != old.Version || !rule.Enabled || (rule.LastEvaluatedAt != nil && !at.After(*rule.LastEvaluatedAt)) {
			return nil
		}
		verdict := PlatformAlertUnknown
		updates := map[string]any{"last_evaluated_at": at, "last_verdict": string(verdict), "last_error": reason, "last_value": nil, "window_signature": "", "window_since": nil}
		if reason == "collection_disabled" {
			updates["last_verdict"] = string(PlatformAlertPaused)
		}
		var evidence *models.ProcessAlertEvidence
		if reason == "" && len(series) == 1 && len(series[0].Points) > 0 {
			// Serialize with the existing query-policy CAS, including its absent
			// default row. A queued database write cannot publish an expired lease
			// or evidence obtained under a replaced query policy.
			if err := tx.Exec("SELECT pg_advisory_xact_lock(?, ?)", models.ResourceQueryPolicyLockNamespace, models.ResourceQueryPolicyLockID).Error; err != nil {
				return err
			}
			var currentPolicy models.ResourceQueryPolicy
			if err := tx.First(&currentPolicy, 1).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if currentPolicy.Version != cfg.Version || !source.lease.After(time.Now().UTC()) {
				updates["last_error"] = "source_changed"
				return tx.Model(&rule).UpdateColumns(updates).Error
			}
			since := at
			if rule.WindowSince != nil && rule.WindowSignature == source.signature && rule.LastEvaluatedAt != nil && at.Sub(*rule.LastEvaluatedAt) <= 16*time.Second {
				since = *rule.WindowSince
			}
			if s.startedAt.After(since) {
				since = s.startedAt
			}
			if rule.UpdatedAt.After(since) {
				since = rule.UpdatedAt
			}
			var active models.PlatformAlertIncident
			err := tx.Where("category=? AND subject_id=? AND signal=? AND status IN ?", models.PlatformAlertCategoryProcess, fmt.Sprint(rule.AnchorInstanceID), rule.ID, []string{"open", "acknowledged"}).Take(&active).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			policy := rule.ProcessThresholdPolicy
			version := rule.Version
			ruleName := rule.Name
			hasActive := err == nil
			if hasActive {
				if active.ProcessEvidence == nil || active.ProcessEvidence.RuleID != rule.ID || active.ProcessEvidence.MetricKey != rule.MetricKey || active.ProcessEvidence.RuntimeInstanceID != rule.RuntimeInstanceID || active.ProcessEvidence.ProcessStartedAt != rule.ProcessStartedAt || active.ProcessEvidence.AnchorInstanceID != rule.AnchorInstanceID || !active.ProcessEvidence.Policy.Valid(rule.MetricKey) {
					updates["last_error"] = "no_data"
					return tx.Model(&rule).UpdateColumns(updates).Error
				}
				policy = active.ProcessEvidence.Policy
				version = active.ProcessEvidence.RuleVersion
				ruleName = active.ProcessEvidence.RuleName
			}
			result := evaluateProcessThreshold(series[0], policy, since, at, hasActive)
			verdict = result.Verdict
			updates["last_verdict"] = string(verdict)
			updates["last_error"] = "warming_up"
			newest := series[0].Points[len(series[0].Points)-1]
			if validThresholdPoint(newest) && newest.EvaluatedAt.Equal(at) && at.Sub(*newest.SampledAt) <= 16*time.Second {
				updates["window_signature"], updates["window_since"] = source.signature, since
			} else {
				updates["last_error"] = "no_data"
			}
			if verdict == PlatformAlertRisk || verdict == PlatformAlertNormal {
				updates["last_error"], updates["last_value"] = "", result.Latest
				evidence = &models.ProcessAlertEvidence{RuleName: ruleName, RuleID: rule.ID, RuleVersion: version, ModuleName: rule.ModuleName, Role: rule.Role, AnchorInstanceID: rule.AnchorInstanceID, RuntimeInstanceID: rule.RuntimeInstanceID, ProcessStartedAt: rule.ProcessStartedAt, MetricKey: rule.MetricKey, Policy: policy, QueryPolicyVersion: cfg.Version, EvaluatedAt: at, SampledFrom: result.SampledFrom, SampledTo: result.SampledTo, Samples: result.Samples, Minimum: result.Minimum, Maximum: result.Maximum, Latest: result.Latest}
			}
		}
		if err := reconcilePlatformAlert(tx, s.alerts, PlatformAlertEvaluation{Category: models.PlatformAlertCategoryProcess, SubjectKind: models.PlatformAlertSubjectInstance, SubjectID: fmt.Sprint(rule.AnchorInstanceID), InstanceID: rule.RuntimeInstanceID, Signal: rule.ID, Severity: "warning", Verdict: verdict, ProcessEvidence: evidence}, at); err != nil {
			return err
		}
		return tx.Model(&rule).UpdateColumns(updates).Error
	})
}

func evaluateProcessThreshold(series resourcequery.Series, policy models.ProcessThresholdPolicy, since, at time.Time, active bool) resourcePressureResult {
	window := 0
	unit := "bytes"
	if series.MetricKey == "process.cpu.core_equivalents" {
		window = 60
		unit = "cores"
	}
	if !policy.Valid(series.MetricKey) || series.WindowSeconds != window || series.Unit != unit || len(series.Dimensions) != 0 || len(series.Points) < 1 || len(series.Points) > 22 {
		return resourcePressureResult{Verdict: PlatformAlertUnknown}
	}
	return evaluateThresholdWindow(series, policy.TriggerValue, policy.RecoveryValue, policy.TriggerSeconds, policy.RecoverySeconds, since, at, active, 16*time.Second)
}
