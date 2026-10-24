package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/addp/monitor/internal/metricsdiscovery"
	"github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/resourcequery"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const resourceRulePrincipal = "monitor:resource-alert-evaluator"

type resourceRuleSource struct {
	nodeVersion int64
	target      metricsdiscovery.NodeTarget
	address     string
}

// Run has no queue or overlapping goroutines. A missed round invalidates the
// next evidence window, rather than treating scheduler time as sampled time.
func (s *ResourceAlertRuleService) Run(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		if err := s.Evaluate(ctx, time.Now().UTC().Truncate(time.Second)); err != nil && ctx.Err() == nil {
			log.Print("Resource alert evaluation unavailable")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *ResourceAlertRuleService) Evaluate(parent context.Context, at time.Time) error {
	if s == nil || s.db == nil || at.IsZero() || at.Nanosecond() != 0 {
		return ErrLogInvalid
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	var rules []models.ResourceAlertRule
	if err := s.db.WithContext(ctx).Where("enabled=?", true).Order("node_id,metric_key").Limit(resourceAlertRuleLimit + 1).Find(&rules).Error; err != nil {
		return err
	}
	if len(rules) == 0 {
		return nil
	}
	if len(rules) > resourceAlertRuleLimit {
		return ErrLogConflict
	}
	fail := func(verdict PlatformAlertVerdict, reason string) error {
		// The bounded query may have expired. Recording unknown uses a separate,
		// bounded DB operation; cancellation of the owner still cancels this write.
		writeCtx, done := context.WithTimeout(parent, 5*time.Second)
		defer done()
		return s.resetResourceRules(writeCtx, rules, at, verdict, reason)
	}
	o := s.observations
	if o == nil {
		return fail(PlatformAlertUnknown, "unconfigured")
	}
	if !o.enabled {
		return fail(PlatformAlertPaused, "collection_disabled")
	}
	if o.targets == nil || o.policy == nil || o.backend == nil || s.identities == nil {
		return fail(PlatformAlertUnknown, "unconfigured")
	}
	release, err := o.limiter.Acquire(resourceRulePrincipal, resourcequery.DefaultBudget())
	if err != nil {
		return fail(PlatformAlertUnknown, "query_budget")
	}
	defer release()
	cfg, err := o.Policy(ctx)
	if err != nil {
		return fail(PlatformAlertUnknown, "query_policy")
	}
	if err = o.limiter.Check(resourceRulePrincipal, cfg.Budget); err != nil {
		return fail(PlatformAlertUnknown, "query_budget")
	}
	ctx, done := context.WithTimeout(ctx, time.Duration(cfg.TimeoutSeconds)*time.Second)
	defer done()
	sources, err := s.resourceRuleSources(ctx)
	if err != nil {
		return fail(PlatformAlertUnknown, "identity_unavailable")
	}
	type observation struct {
		source resourceRuleSource
		series []resourcequery.Series
		reason string
	}
	observations := map[string]observation{}
	for i := 0; i < len(rules); {
		j := i + 1
		for j < len(rules) && rules[j].NodeID == rules[i].NodeID {
			j++
		}
		node := rules[i].NodeID
		source, ok := sources[node]
		if !ok {
			observations[node] = observation{reason: "collection_paused"}
			i = j
			continue
		}
		keys := []string{"node.uptime_seconds"}
		for _, r := range rules[i:j] {
			keys = append(keys, r.MetricKey)
		}
		plan, e := resourcequery.NewPlan(keys, at.Add(-315*time.Second), at, at, true, nil, cfg.Budget)
		row := observation{source: source}
		if e != nil || plan.StepSeconds != resourcequery.MinimumStepSeconds {
			row.reason = "query_budget"
		} else {
			row.series, e = o.backend.Query(ctx, plan, resourcequery.Scope{NodeID: node, Instance: source.address}, cfg.Budget)
			if e != nil || ctx.Err() != nil {
				row.reason = "query_unavailable"
				row.series = nil
			}
		}
		observations[node] = row
		i = j
	}
	// Re-read current System identities, targets and resolved addresses after all
	// network work. No private System table or administrator token is consulted.
	current, err := s.resourceRuleSources(ctx)
	if err != nil {
		return fail(PlatformAlertUnknown, "identity_unavailable")
	}
	writeCtx, writeDone := context.WithTimeout(parent, 5*time.Second)
	defer writeDone()

	resets := map[string][]models.ResourceAlertRule{}
	for _, r := range rules {
		row := observations[r.NodeID]
		source, ok := current[r.NodeID]
		if row.reason == "" && (!ok || source != row.source) {
			row.reason = "source_changed"
		}
		if row.reason != "" {
			resets[row.reason] = append(resets[row.reason], r)
			continue
		}
		if err = s.applyResourceRule(writeCtx, r, at, &row.source, cfg, row.series, PlatformAlertUnknown, ""); err != nil {
			return err
		}
	}
	for reason, rows := range resets {
		verdict := PlatformAlertUnknown
		if reason == "collection_paused" {
			verdict = PlatformAlertPaused
		}
		if err = s.resetResourceRules(writeCtx, rows, at, verdict, reason); err != nil {
			return err
		}
	}
	return nil
}

func (s *ResourceAlertRuleService) resourceRuleSources(ctx context.Context) (map[string]resourceRuleSource, error) {
	o := s.observations
	targets, err := o.targets.Snapshot(ctx)
	if err != nil {
		return nil, err
	}

	if err := metricsdiscovery.CheckReservations(targets); err != nil {
		return nil, err
	}
	nodes, err := metricsdiscovery.CurrentNodeVersions(ctx, s.identities)
	if err != nil {
		return nil, err
	}
	result := map[string]resourceRuleSource{}
	physical := map[string]bool{}
	for _, target := range targets {
		version := nodes[target.Subject.NodeID]
		if !target.Enabled || target.MonitorKind != "host_resources" || version < 1 {
			continue
		}
		resolved, e := o.policy.Resolve(ctx, target.Source.Endpoint)
		if e != nil {
			return nil, e
		}
		if physical[resolved.Address] {
			return nil, metricsdiscovery.ErrInvalidTarget
		}
		physical[resolved.Address] = true
		result[target.Subject.NodeID] = resourceRuleSource{nodeVersion: version, target: target, address: resolved.Address}
	}
	return result, nil
}

func (s *ResourceAlertRuleService) applyResourceRule(ctx context.Context, expected models.ResourceAlertRule, at time.Time, source *resourceRuleSource, cfg ResourceQueryPolicyResponse, series []resourcequery.Series, verdict PlatformAlertVerdict, reason string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var r models.ResourceAlertRule
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=?", expected.ID).Take(&r).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if !r.Enabled || r.Version != expected.Version || (r.LastEvaluatedAt != nil && !at.After(*r.LastEvaluatedAt)) {
			return nil
		}
		updates := map[string]any{"last_evaluated_at": at, "last_verdict": string(verdict), "last_error": reason, "last_sample_at": nil, "last_value": nil, "window_signature": "", "window_since": nil}
		var evidence *models.ResourceAlertEvidence
		if reason == "" && source != nil {
			// Share the policy CAS lock, including the not-yet-created default row.
			if err = tx.Exec("SELECT pg_advisory_xact_lock(?, ?)", models.ResourceQueryPolicyLockNamespace, models.ResourceQueryPolicyLockID).Error; err != nil {
				return err
			}
			var policy models.ResourceQueryPolicy
			e := tx.First(&policy, 1).Error
			if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
				return e
			}
			var target models.MonitoringTarget
			e = tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id=?", source.target.ID).Take(&target).Error
			if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
				return e
			}
			if policy.Version != cfg.Version || errors.Is(e, gorm.ErrRecordNotFound) || !target.Enabled || target.Version != source.target.Version || target.NodeID != r.NodeID || target.Endpoint != source.target.Source.Endpoint {
				updates["last_error"] = "source_changed"
			} else {
				signature := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%d/%d/%s/%d/%s/%d/%s", r.Version, source.nodeVersion, target.ID, target.Version, source.address, cfg.Version, s.startedAt.Format(time.RFC3339Nano)))))
				since := at
				if r.WindowSignature == signature && r.WindowSince != nil && r.LastEvaluatedAt != nil && at.Sub(*r.LastEvaluatedAt) <= 16*time.Second {
					since = *r.WindowSince
				}
				if since.Before(r.UpdatedAt) {
					since = r.UpdatedAt
				}
				if since.Before(s.startedAt) {
					since = s.startedAt
				}
				policyToUse := r.ResourcePressurePolicy
				evidenceVersion := r.Version
				ruleName := r.Name
				var active models.PlatformAlertIncident
				e = tx.Where("category=? AND subject_kind=? AND subject_id=? AND signal=? AND status IN ?", models.PlatformAlertCategoryResource, models.PlatformAlertSubjectHostNode, r.NodeID, r.MetricKey, []string{"open", "acknowledged"}).Take(&active).Error
				if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
					return e
				}
				hasActive := e == nil
				qualified := true
				if hasActive {
					if active.ResourceEvidence == nil || active.ResourceEvidence.RuleID != r.ID || !active.ResourceEvidence.Policy.Valid() {
						qualified = false
					} else {
						policyToUse = active.ResourceEvidence.Policy
						evidenceVersion = active.ResourceEvidence.RuleVersion
						ruleName = active.ResourceEvidence.RuleName
					}
				}
				byKey := map[string]resourcequery.Series{}
				for _, v := range series {
					if _, exists := byKey[v.MetricKey]; exists {
						qualified = false
					}
					byKey[v.MetricKey] = v
				}
				result := resourcePressureResult{Verdict: PlatformAlertUnknown}
				if qualified {
					result = evaluateResourcePressure(byKey[r.MetricKey], byKey["node.uptime_seconds"], since, at, true, hasActive, policyToUse)
				}
				verdict = result.Verdict
				updates["last_verdict"], updates["last_error"] = string(verdict), "warming_up"
				// A short valid prefix is expected during warming. An invalid newest
				// sample breaks continuity immediately and starts a new window next round.
				newestValid := pressureNewestValid(byKey[r.MetricKey], byKey["node.uptime_seconds"], at)
				if newestValid {
					updates["window_signature"], updates["window_since"] = signature, since
				}
				if verdict == PlatformAlertNormal || verdict == PlatformAlertRisk {
					updates["last_error"], updates["last_sample_at"], updates["last_value"] = "", result.SampledTo, result.Latest
					evidence = &models.ResourceAlertEvidence{RuleName: ruleName, RuleID: r.ID, RuleVersion: evidenceVersion, Policy: policyToUse, NodeVersion: source.nodeVersion, TargetID: target.ID, TargetVersion: target.Version, QueryPolicyVersion: cfg.Version, EvaluatedAt: at, SampledFrom: result.SampledFrom, SampledTo: result.SampledTo, Samples: result.Samples, Minimum: result.Minimum, Maximum: result.Maximum, Latest: result.Latest}
				} else if !newestValid {
					updates["last_error"] = "no_data"
				}
			}
		}
		if err = reconcilePlatformAlert(tx, s.alerts, PlatformAlertEvaluation{Category: models.PlatformAlertCategoryResource, SubjectKind: models.PlatformAlertSubjectHostNode, SubjectID: r.NodeID, Signal: r.MetricKey, Severity: "warning", Verdict: verdict, ResourceEvidence: evidence}, at); err != nil {
			return err
		}
		// Evaluation state never changes the user's configuration CAS or edit time.
		return tx.Model(&r).UpdateColumns(updates).Error
	})
}

func pressureNewestValid(metric, uptime resourcequery.Series, at time.Time) bool {
	if len(metric.Points) == 0 || len(uptime.Points) == 0 {
		return false
	}
	m, u := metric.Points[len(metric.Points)-1], uptime.Points[len(uptime.Points)-1]
	return validPressurePoint(m) && validPressurePoint(u) && m.EvaluatedAt.Equal(at) && u.EvaluatedAt.Equal(at) && m.SampledAt.Equal(*u.SampledAt) && !m.SampledAt.After(at) && at.Sub(*m.SampledAt) <= 16*time.Second && *m.Value >= 0 && *m.Value <= 100 && *u.Value >= 0
}

// Unknown/paused never changes incident state. One bounded CAS update avoids a
// transaction per unconnected rule starving later rules in the same round.
func (s *ResourceAlertRuleService) resetResourceRules(ctx context.Context, rules []models.ResourceAlertRule, at time.Time, verdict PlatformAlertVerdict, reason string) error {
	if len(rules) == 0 {
		return nil
	}
	if len(rules) > resourceAlertRuleLimit || (verdict != PlatformAlertUnknown && verdict != PlatformAlertPaused) {
		return ErrLogInvalid
	}
	clauses := make([]string, 0, len(rules))
	args := make([]any, 0, len(rules)*2)
	for _, r := range rules {
		clauses = append(clauses, "(id=? AND version=?)")
		args = append(args, r.ID, r.Version)
	}
	return s.db.WithContext(ctx).Model(&models.ResourceAlertRule{}).Where("enabled=?", true).Where("last_evaluated_at IS NULL OR last_evaluated_at<?", at).Where("("+strings.Join(clauses, " OR ")+")", args...).UpdateColumns(map[string]any{"last_evaluated_at": at, "last_verdict": string(verdict), "last_error": reason, "last_sample_at": nil, "last_value": nil, "window_signature": "", "window_since": nil}).Error
}
