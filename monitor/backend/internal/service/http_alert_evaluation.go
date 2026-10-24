package service

import (
	"context"
	"errors"
	"log"
	"time"

	commonmodels "github.com/addp/common/models"
	"github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/resourcequery"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *HTTPAlertRuleService) sources(ctx context.Context, rule models.HTTPAlertRule, policyVersion uint64) (runtimeAlertSources, error) {
	return runtimeSources(ctx, s.observations, s.identities, policyVersion, func(r commonmodels.ObservabilityModuleIdentity) bool {
		return r.ModuleName == rule.ModuleName && r.Role == rule.Role
	})
}

func (s *HTTPAlertRuleService) Run(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		if err := s.Evaluate(ctx, time.Now().UTC().Truncate(time.Second)); err != nil && ctx.Err() == nil {
			log.Print("HTTP quality alert evaluation unavailable")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *HTTPAlertRuleService) Evaluate(parent context.Context, at time.Time) error {
	if s == nil || s.db == nil || at.IsZero() || at.Nanosecond() != 0 {
		return ErrLogInvalid
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	var rules []models.HTTPAlertRule
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
	var backend HTTPResourceBackend
	if o != nil {
		backend, _ = o.process.Backend.(HTTPResourceBackend)
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
			release, err = o.limiter.Acquire("monitor:http-alert-evaluator", resourcequery.DefaultBudget())
			if err != nil {
				reason = "query_budget"
			}
		}
		if reason == "" {
			var err error
			cfg, err = o.Policy(ctx)
			if err != nil {
				reason = "query_policy"
			} else if o.limiter.Check("monitor:http-alert-evaluator", cfg.Budget) != nil {
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
			p, err := resourcequery.NewHTTPPlan(at.Add(-315*time.Second), at, at, true, false, rule.Route, rule.Method, 60, cfg.Budget)
			if err != nil || p.StepSeconds != 15 || len(source.scopes)*len(p.Metrics) > cfg.MaxSeries || len(source.scopes)*len(p.Metrics)*p.Points > cfg.MaxTotalPoints {
				reason = "query_budget"
			} else {
				observations := [][]resourcequery.HTTPObservation{}
				for _, scope := range source.scopes {
					collection, err := backend.HTTPCollection(ctx, scope, at, cfg.Budget)
					if err != nil || collection.State != "collecting" {
						reason = "no_data"
						break
					}
					rows, err := backend.HTTP(ctx, p, scope, cfg.Budget)
					if err != nil || len(rows) != 1 {
						reason = "query_unavailable"
						break
					}
					observations = append(observations, rows)
				}
				if reason == "" {
					lines, err = aggregateHTTPRuleSeries(observations, rule.MetricKey)
					if err != nil {
						reason = "no_data"
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
		writeCtx, done := context.WithTimeout(parent, 5*time.Second)
		err := s.applyHTTPRule(writeCtx, rule, source, cfg, lines, at, reason)
		done()
		if err != nil {
			return err
		}
	}
	return nil
}

// Every current instance must provide a valid QPS on the same grid. Ratios and
// means use request-rate weights; valid idle instances contribute zero weight.
// Percentiles are deliberately not averaged or used as aggregate rule inputs.
func aggregateHTTPRuleSeries(rows [][]resourcequery.HTTPObservation, key string) ([]resourcequery.Series, error) {
	if len(rows) == 0 {
		return nil, resourcequery.ErrUnavailable
	}
	var result resourcequery.Series
	for source, observations := range rows {
		if len(observations) != 1 {
			return nil, resourcequery.ErrUnavailable
		}
		var value, qps *resourcequery.Series
		for i := range observations[0].Series {
			line := &observations[0].Series[i]
			if line.MetricKey == key {
				if value != nil {
					return nil, resourcequery.ErrUnavailable
				}
				value = line
			}
			if line.MetricKey == "http.requests.per_second" {
				if qps != nil {
					return nil, resourcequery.ErrUnavailable
				}
				qps = line
			}
		}
		if value == nil || qps == nil || len(value.Points) != len(qps.Points) || len(value.Points) < 1 || len(value.Points) > 22 {
			return nil, resourcequery.ErrUnavailable
		}
		if source == 0 {
			result = *value
			result.Points = make([]resourcequery.Point, len(value.Points))
			for i, p := range value.Points {
				result.Points[i] = resourcequery.Point{EvaluatedAt: p.EvaluatedAt, DataState: "valid"}
				v := 0.0
				result.Points[i].Value = &v
			}
		}
		if len(value.Points) != len(result.Points) {
			return nil, resourcequery.ErrUnavailable
		}
		for i := range value.Points {
			point, requests := value.Points[i], qps.Points[i]
			target := &result.Points[i]
			if !point.EvaluatedAt.Equal(target.EvaluatedAt) || !requests.EvaluatedAt.Equal(target.EvaluatedAt) || !validThresholdPoint(requests) {
				target.Value = nil
				target.SampledAt = nil
				target.DataState = "no_data"
				continue
			}
			if i > 0 && validThresholdPoint(qps.Points[i-1]) && !requests.SampledAt.After(*qps.Points[i-1].SampledAt) {
				target.Value = nil
				target.SampledAt = nil
				target.DataState = "no_data"
				continue
			}
			if key != "http.requests.per_second" && *requests.Value > 0 && !validThresholdPoint(point) {
				target.Value = nil
				target.SampledAt = nil
				target.DataState = "no_data"
				continue
			}
			if target.Value == nil {
				continue
			}
			v := *requests.Value
			if key != "http.requests.per_second" {
				v = 0
				if *requests.Value > 0 {
					v = *point.Value * *requests.Value
				}
			}
			*target.Value += v
			if target.SampledAt == nil || requests.SampledAt.Before(*target.SampledAt) {
				at := *requests.SampledAt
				target.SampledAt = &at
			}
		}
	}
	if key != "http.requests.per_second" {
		for i := range result.Points {
			target := &result.Points[i]
			if target.Value == nil {
				continue
			}
			weight := 0.0
			for _, observations := range rows {
				for _, line := range observations[0].Series {
					if line.MetricKey == "http.requests.per_second" && validThresholdPoint(line.Points[i]) {
						weight += *line.Points[i].Value
					}
				}
			}
			if weight == 0 {
				target.Value = nil
				target.SampledAt = nil
				target.DataState = "no_data"
			} else {
				*target.Value /= weight
			}
		}
	}
	return []resourcequery.Series{result}, nil
}

func (s *HTTPAlertRuleService) applyHTTPRule(ctx context.Context, old models.HTTPAlertRule, source runtimeAlertSources, cfg ResourceQueryPolicyResponse, series []resourcequery.Series, at time.Time, reason string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rule models.HTTPAlertRule
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
		var evidence *models.HTTPAlertEvidence
		if reason == "" && len(series) == 1 {
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
			err := tx.Where("category=? AND subject_id=? AND signal=? AND status IN ?", models.PlatformAlertCategoryHTTP, rule.ModuleName, rule.ID, []string{"open", "acknowledged"}).Take(&active).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			policy := rule.HTTPThresholdPolicy
			version := rule.Version
			ruleName := rule.Name
			hasActive := err == nil
			if hasActive {
				if active.HTTPEvidence == nil || active.HTTPEvidence.RuleID != rule.ID || !active.HTTPEvidence.Policy.Valid(rule.MetricKey) {
					updates["last_error"] = "no_data"
					return tx.Model(&rule).UpdateColumns(updates).Error
				}
				policy = active.HTTPEvidence.Policy
				version = active.HTTPEvidence.RuleVersion
				ruleName = active.HTTPEvidence.RuleName
			}
			result := evaluateHTTPThreshold(series[0], policy, since, at, hasActive)
			verdict = result.Verdict
			updates["last_verdict"] = string(verdict)
			updates["last_error"] = "warming_up"
			newest := series[0].Points[len(series[0].Points)-1]
			if validThresholdPoint(newest) {
				updates["window_signature"], updates["window_since"] = source.signature, since
			} else {
				updates["last_error"] = "no_data"
			}
			if verdict == PlatformAlertRisk || verdict == PlatformAlertNormal {
				updates["last_error"], updates["last_value"] = "", result.Latest
				evidence = &models.HTTPAlertEvidence{RuleName: ruleName, RuleID: rule.ID, RuleVersion: version, ModuleName: rule.ModuleName, Role: rule.Role, Route: rule.Route, Method: rule.Method, MetricKey: rule.MetricKey, Policy: policy, QueryPolicyVersion: cfg.Version, InstanceIDs: source.instances, EvaluatedAt: at, SampledFrom: result.SampledFrom, SampledTo: result.SampledTo, Samples: result.Samples, Minimum: result.Minimum, Maximum: result.Maximum, Latest: result.Latest}
			}
		}
		if err := reconcilePlatformAlert(tx, s.alerts, PlatformAlertEvaluation{Category: models.PlatformAlertCategoryHTTP, SubjectKind: models.PlatformAlertSubjectModule, SubjectID: rule.ModuleName, Signal: rule.ID, Severity: "warning", Verdict: verdict, HTTPEvidence: evidence}, at); err != nil {
			return err
		}
		return tx.Model(&rule).UpdateColumns(updates).Error
	})
}

func evaluateHTTPThreshold(series resourcequery.Series, policy models.HTTPThresholdPolicy, since, at time.Time, active bool) resourcePressureResult {
	result := resourcePressureResult{Verdict: PlatformAlertUnknown}
	if !policy.Valid(series.MetricKey) || series.WindowSeconds != 60 || len(series.Dimensions) != 0 || len(series.Points) < 1 || len(series.Points) > 22 {
		return result
	}
	return evaluateThresholdWindow(series, policy.TriggerValue, policy.RecoveryValue, policy.TriggerSeconds, policy.RecoverySeconds, since, at, active, 60*time.Second)
}
