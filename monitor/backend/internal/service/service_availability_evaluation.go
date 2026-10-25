package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	commonmodels "github.com/addp/common/models"
	"github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/resourcequery"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func availabilityIntentSignature(deployment string, g commonmodels.ServiceGroupObservation) string {
	b, _ := json.Marshal([]any{deployment, g.ServiceGroupExpectation})
	return availabilityHash(string(b))
}
func availabilityGroup(snapshot *commonmodels.ServiceGroupSnapshot, r models.ServiceAvailabilityRule) *commonmodels.ServiceGroupObservation {
	if snapshot == nil || snapshot.DeploymentID != r.DeploymentID {
		return nil
	}
	for i := range snapshot.Groups {
		g := &snapshot.Groups[i]
		if g.ModuleName == r.ModuleName && g.Role == r.Role && g.RuntimeName == r.RuntimeName {
			return g
		}
	}
	return nil
}
func resetAvailabilityWindow(r *models.ServiceAvailabilityRule) {
	r.WindowSignature = ""
	r.WindowSince = nil
	r.WindowSamples = 0
}

// advanceAvailability uses only new authoritative System observations. Confirmation
// survives evaluator restart; a complete new continuous window never does.
func advanceAvailability(r *models.ServiceAvailabilityRule, g *commonmodels.ServiceGroupObservation, observed, at, started time.Time, recoverySeconds int) PlatformAlertVerdict {
	r.LastError = ""
	r.OnlineReplicas = nil
	previousEvaluation := r.LastEvaluatedAt
	r.LastEvaluatedAt = &at
	if g == nil {
		resetAvailabilityWindow(r)
		r.LastError = "identity_unavailable"
		return PlatformAlertUnknown
	}
	signature := availabilityIntentSignature(r.DeploymentID, *g)
	if signature != r.ConfirmedSignature || r.ConfirmedAt == nil || r.ConfirmedAt.IsZero() {
		r.ConfirmedAt = nil
		r.ConfirmedSignature = ""
	}
	if g.State == "paused" {
		resetAvailabilityWindow(r)
		r.ConfirmedAt = nil
		r.ConfirmedSignature = ""
		r.LastError = g.Reason
		r.LastObservedAt = nil
		return PlatformAlertPaused
	}
	if g.State != "observed" || g.DesiredState != "running" {
		resetAvailabilityWindow(r)
		r.LastError = g.Reason
		return PlatformAlertUnknown
	}
	if observed.IsZero() || (r.LastObservedAt != nil && !observed.After(*r.LastObservedAt)) {
		resetAvailabilityWindow(r)
		r.LastError = "observation_not_advanced"
		return PlatformAlertUnknown
	}
	gap := r.LastObservedAt != nil && observed.Sub(*r.LastObservedAt) > 16*time.Second
	if gap || (previousEvaluation != nil && previousEvaluation.Before(started)) {
		resetAvailabilityWindow(r)
	}
	r.LastObservedAt = &observed
	online := g.OnlineReplicas
	r.OnlineReplicas = &online
	if r.ConfirmedSignature == "" {
		if online == 0 {
			resetAvailabilityWindow(r)
			r.LastError = "waiting_confirmation"
			return PlatformAlertUnknown
		}
		r.ConfirmedSignature = signature
		r.ConfirmedAt = &observed
		resetAvailabilityWindow(r)
	}
	verdict := PlatformAlertNormal
	seconds := recoverySeconds
	if online == 0 {
		verdict = PlatformAlertRisk
		seconds = r.TriggerSeconds
	}
	window := availabilityHash(signature, fmt.Sprint(r.Version), string(verdict))
	if window != r.WindowSignature || r.WindowSince == nil {
		r.WindowSignature = window
		r.WindowSince = &observed
		r.WindowSamples = 1
	} else {
		r.WindowSamples++
	}
	if observed.Sub(*r.WindowSince) < time.Duration(seconds)*time.Second {
		r.LastError = "warming_up"
		return PlatformAlertUnknown
	}
	return verdict
}
func (s *ServiceAvailabilityRuleService) Run(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		if err := s.Evaluate(ctx, time.Now().UTC()); err != nil && ctx.Err() == nil {
			log.Printf("Service availability evaluation failed: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (s *ServiceAvailabilityRuleService) Evaluate(parent context.Context, at time.Time) error {
	if s == nil || s.db == nil || at.IsZero() {
		return ErrLogInvalid
	}
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	var rules []models.ServiceAvailabilityRule
	if err := s.db.WithContext(ctx).Where("enabled=?", true).Order("id").Limit(1001).Find(&rules).Error; err != nil {
		return err
	}
	if len(rules) > 1000 {
		return resourcequery.ErrBudget
	}
	if len(rules) == 0 {
		return nil
	}
	before, err := s.snapshot(ctx)
	var after *commonmodels.ServiceGroupSnapshot
	if err == nil {
		after, err = s.snapshot(ctx)
	}
	reason := ""
	if err != nil {
		reason = "identity_unavailable"
	}
	// Record the evaluation after both reads, rather than backdating events to
	// the start of network I/O. Continuous windows still use System observations.
	at = s.now().UTC()
	return s.evaluateAvailabilitySnapshots(ctx, rules, before, after, reason, at)
}
func (s *ServiceAvailabilityRuleService) evaluateAvailabilitySnapshots(ctx context.Context, rules []models.ServiceAvailabilityRule, before, after *commonmodels.ServiceGroupSnapshot, reason string, at time.Time) error {
	for _, selected := range rules {
		if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var r models.ServiceAvailabilityRule
			err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("id=? AND enabled=?", selected.ID, true).Take(&r).Error
			if err == gorm.ErrRecordNotFound {
				return nil
			}
			if err != nil {
				return err
			}
			if r.Version != selected.Version || (r.LastEvaluatedAt != nil && !at.After(*r.LastEvaluatedAt)) {
				return nil
			}
			var active models.PlatformAlertIncident
			activeErr := tx.Where("category=? AND subject_kind=? AND subject_id=? AND signal=? AND status IN ?", models.PlatformAlertCategoryAvailability, models.PlatformAlertSubjectGroup, r.SubjectKey, r.ID, []string{"open", "acknowledged"}).Take(&active).Error
			if activeErr != nil && activeErr != gorm.ErrRecordNotFound {
				return activeErr
			}
			recovery := r.RecoverySeconds
			if activeErr == nil {
				if !validAvailabilityEvidence(active.AvailabilityEvidence, r.SubjectKey, r.ID) {
					return ErrLogConflict
				}
				recovery = active.AvailabilityEvidence.Policy.RecoverySeconds
			}
			currentReason := reason
			now := s.now().UTC()
			if currentReason == "" && (before == nil || after == nil || before.Validate(before.ObservedAt) != nil || after.Validate(after.ObservedAt) != nil || before.ObservedAt.Before(now.Add(-5*time.Second)) || after.ObservedAt.Before(now.Add(-5*time.Second)) || before.ObservedAt.After(now.Add(5*time.Second)) || after.ObservedAt.After(now.Add(5*time.Second))) {
				currentReason = "identity_unavailable"
			}
			g := availabilityGroup(before, r)
			next := availabilityGroup(after, r)
			if currentReason == "" && (g == nil || next == nil || availabilityIntentSignature(r.DeploymentID, *g) != availabilityIntentSignature(r.DeploymentID, *next) || g.State != next.State || g.Reason != next.Reason || g.OnlineReplicas != next.OnlineReplicas || after.ObservedAt.Before(before.ObservedAt)) {
				currentReason = "source_changed"
			}
			verdict := PlatformAlertUnknown
			if currentReason != "" {
				resetAvailabilityWindow(&r)
				r.LastError = currentReason
				r.LastEvaluatedAt = &at
				r.OnlineReplicas = nil
			} else {
				verdict = advanceAvailability(&r, g, before.ObservedAt, at, s.startedAt, recovery)
			}
			r.LastVerdict = string(verdict)
			updates := map[string]any{"confirmed_signature": r.ConfirmedSignature, "confirmed_at": r.ConfirmedAt, "window_signature": r.WindowSignature, "window_since": r.WindowSince, "window_samples": r.WindowSamples, "last_evaluated_at": r.LastEvaluatedAt, "last_observed_at": r.LastObservedAt, "last_verdict": r.LastVerdict, "last_error": r.LastError, "online_replicas": r.OnlineReplicas}
			if err := tx.Model(&r).Updates(updates).Error; err != nil {
				return err
			}
			if verdict != PlatformAlertRisk && verdict != PlatformAlertNormal {
				return nil
			}
			evidence := &models.ServiceAvailabilityEvidence{RuleID: r.ID, RuleName: r.Name, RuleVersion: r.Version, DeploymentID: r.DeploymentID, ModuleName: r.ModuleName, Role: r.Role, RuntimeName: r.RuntimeName, ExpectedReplicas: g.ExpectedReplicas, IntentChangedAt: g.ChangedAt, Policy: r.ServiceAvailabilityPolicy, ConfirmedAt: *r.ConfirmedAt, ObservedFrom: *r.WindowSince, ObservedTo: before.ObservedAt, OnlineReplicas: g.OnlineReplicas, Samples: r.WindowSamples}
			if activeErr == nil {
				evidence.RuleName = active.AvailabilityEvidence.RuleName
				evidence.RuleVersion = active.AvailabilityEvidence.RuleVersion
				evidence.Policy = active.AvailabilityEvidence.Policy
			}
			return reconcilePlatformAlert(tx, s.alerts, PlatformAlertEvaluation{Category: models.PlatformAlertCategoryAvailability, SubjectKind: models.PlatformAlertSubjectGroup, SubjectID: r.SubjectKey, Signal: r.ID, Severity: "warning", Verdict: verdict, AvailabilityEvidence: evidence}, at)
		}); err != nil {
			return err
		}
	}
	return nil
}
