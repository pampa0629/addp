package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	commonmodels "github.com/addp/common/models"
	"github.com/addp/monitor/internal/models"
)

func availabilityTestRule(at time.Time) models.ServiceAvailabilityRule {
	return models.ServiceAvailabilityRule{ID: "10000000-0000-4000-8000-000000000001", Version: 1, Name: "Original group", DeploymentID: strings.Repeat("a", 64), ModuleName: "meta", Role: "worker", RuntimeName: "scan", ServiceAvailabilityPolicy: models.ServiceAvailabilityPolicy{TriggerSeconds: 60, RecoverySeconds: 30}, Enabled: true, UpdatedAt: at}
}
func availabilityTestGroup(at time.Time, online int) *commonmodels.ServiceGroupObservation {
	return &commonmodels.ServiceGroupObservation{ServiceGroupExpectation: commonmodels.ServiceGroupExpectation{ModuleName: "meta", Role: "worker", RuntimeName: "scan", ExpectedReplicas: 2, DesiredState: "running", ChangedAt: at.Add(-time.Hour)}, State: "observed", OnlineReplicas: online, RegisteredCount: 99}
}
func TestServiceAvailabilityRequiresCurrentConfirmationAndCompleteWindows(t *testing.T) {
	at := time.Now().UTC()
	r := availabilityTestRule(at)
	g := availabilityTestGroup(at, 0)
	start := at.Add(-time.Minute)
	step := func(seconds int, online int) PlatformAlertVerdict {
		g.OnlineReplicas = online
		now := at.Add(time.Duration(seconds) * time.Second)
		return advanceAvailability(&r, g, now, now, start, 30)
	}
	for _, s := range []int{0, 15, 30, 45, 60, 75} {
		if step(s, 0) != PlatformAlertUnknown || r.LastError != "waiting_confirmation" || r.ConfirmedAt != nil {
			t.Fatal("historical registrations fabricated confirmation", r)
		}
	}
	step(90, 1)
	if r.ConfirmedAt == nil {
		t.Fatal("valid online replica did not confirm operation")
	}
	for _, s := range []int{105, 120, 135, 150} {
		if step(s, 0) != PlatformAlertUnknown {
			t.Fatal("incomplete loss window triggered")
		}
	}
	if step(165, 0) != PlatformAlertRisk {
		t.Fatal("complete loss window not recognized", r)
	}
	if step(180, 1) != PlatformAlertUnknown || step(195, 1) != PlatformAlertUnknown || step(210, 1) != PlatformAlertNormal {
		t.Fatal("recovery did not require a full window", r)
	}
}
func TestServiceAvailabilityRestartKeepsQualificationButRebuildsWindow(t *testing.T) {
	at := time.Now().UTC()
	r := availabilityTestRule(at)
	g := availabilityTestGroup(at, 1)
	advanceAvailability(&r, g, at, at, at.Add(-time.Minute), 30)
	g.OnlineReplicas = 0
	for _, s := range []int{15, 30, 45, 60} {
		now := at.Add(time.Duration(s) * time.Second)
		advanceAvailability(&r, g, now, now, at.Add(-time.Minute), 30)
	}
	confirmed := *r.ConfirmedAt
	restarted := at.Add(61 * time.Second)
	for _, s := range []int{75, 90, 105, 120} {
		now := at.Add(time.Duration(s) * time.Second)
		if advanceAvailability(&r, g, now, now, restarted, 30) != PlatformAlertUnknown {
			t.Fatal("pre-restart window was reused")
		}
	}
	now := at.Add(135 * time.Second)
	if advanceAvailability(&r, g, now, now, restarted, 30) != PlatformAlertRisk || !r.ConfirmedAt.Equal(confirmed) {
		t.Fatal("durable confirmation lost on evaluator restart", r)
	}
}
func TestServiceAvailabilityChangedIntentPausesUnknownAndRepeatedObservations(t *testing.T) {
	at := time.Now().UTC()
	r := availabilityTestRule(at)
	g := availabilityTestGroup(at, 1)
	advanceAvailability(&r, g, at, at, at.Add(-time.Minute), 30)
	signature := r.ConfirmedSignature
	// A new document revision for another group does not appear in this signature.
	if availabilityIntentSignature(r.DeploymentID, *g) != signature {
		t.Fatal("unstable group signature")
	}
	g.State = "unknown"
	g.Reason = "runtime_name_unavailable"
	now := at.Add(15 * time.Second)
	if advanceAvailability(&r, g, now, now, at.Add(-time.Minute), 30) != PlatformAlertUnknown || r.ConfirmedSignature != signature || r.WindowSince != nil {
		t.Fatal("unknown state changed eligibility or fabricated health")
	}
	g.State = "observed"
	g.Reason = ""
	g.OnlineReplicas = 0
	g.ChangedAt = g.ChangedAt.Add(time.Microsecond)
	now = at.Add(30 * time.Second)
	if advanceAvailability(&r, g, now, now, at.Add(-time.Minute), 30) != PlatformAlertUnknown || r.LastError != "waiting_confirmation" || r.ConfirmedAt != nil {
		t.Fatal("new intent reused prior confirmation")
	}
	g.OnlineReplicas = 1
	now = at.Add(45 * time.Second)
	advanceAvailability(&r, g, now, now, at.Add(-time.Minute), 30)
	if advanceAvailability(&r, g, now, now.Add(time.Second), at.Add(-time.Minute), 30) != PlatformAlertUnknown || r.LastError != "observation_not_advanced" {
		t.Fatal("duplicate observation was counted")
	}
	g.State = "paused"
	g.Reason = "planned_stopped"
	g.DesiredState = "planned_stopped"
	now = at.Add(60 * time.Second)
	if advanceAvailability(&r, g, now, now, at.Add(-time.Minute), 30) != PlatformAlertPaused || r.ConfirmedAt != nil || r.WindowSince != nil {
		t.Fatal("planned stop did not pause and reset eligibility")
	}
}
func TestServiceAvailabilityGapAndNanosecondBoundaryDoNotCompleteWindows(t *testing.T) {
	at := time.Now().UTC()
	r := availabilityTestRule(at)
	r.TriggerSeconds = 15
	g := availabilityTestGroup(at, 1)
	start := at.Add(-time.Minute)
	advanceAvailability(&r, g, at, at, start, 30)
	g.OnlineReplicas = 0
	now := at.Add(time.Second)
	advanceAvailability(&r, g, now, now, start, 30)
	now = at.Add(16*time.Second - time.Nanosecond)
	if advanceAvailability(&r, g, now, now, start, 30) != PlatformAlertUnknown {
		t.Fatal("fractional boundary rounded up")
	}
	now = at.Add(40 * time.Second)
	if advanceAvailability(&r, g, now, now, start, 30) != PlatformAlertUnknown || r.WindowSamples != 1 {
		t.Fatal("a gap was treated as continuous")
	}
	now = at.Add(55 * time.Second)
	if advanceAvailability(&r, g, now, now, start, 30) != PlatformAlertRisk {
		t.Fatal("new complete window was not recognized")
	}
}

func TestServiceAvailabilityCorruptConfirmationCannotPanicOrBecomeEligible(t *testing.T) {
	at := time.Now().UTC()
	r := availabilityTestRule(at)
	g := availabilityTestGroup(at, 0)
	r.ConfirmedSignature = availabilityIntentSignature(r.DeploymentID, *g)
	if verdict := advanceAvailability(&r, g, at, at, at.Add(-time.Minute), 30); verdict != PlatformAlertUnknown || r.LastError != "waiting_confirmation" {
		t.Fatal("partial persisted qualification was trusted", r)
	}
}
func TestServiceAvailabilityNotificationChannelsRecognizeFrozenEvidence(t *testing.T) {
	at := time.Now().UTC()
	r := availabilityTestRule(at)
	g := availabilityTestGroup(at, 0)
	subject := availabilitySubject(r.DeploymentID, r.ModuleName, r.Role, r.RuntimeName)
	evidence := &models.ServiceAvailabilityEvidence{RuleName: r.Name, RuleID: r.ID, RuleVersion: 1, DeploymentID: r.DeploymentID, ModuleName: r.ModuleName, Role: r.Role, RuntimeName: r.RuntimeName, ExpectedReplicas: 2, IntentChangedAt: g.ChangedAt, Policy: r.ServiceAvailabilityPolicy, ConfirmedAt: at.Add(-2 * time.Minute), ObservedFrom: at.Add(-time.Minute), ObservedTo: at, OnlineReplicas: 0, Samples: 5}
	if !validAvailabilityEvidence(evidence, subject, r.ID) {
		t.Fatal("valid evidence rejected")
	}
	for _, kind := range []string{"opened", "resolved"} {
		payload, err := json.Marshal(map[string]any{"schema": "addp.platform-service-availability-alert/v1", "event_id": "event-one", "event_type": kind, "incident_id": 1, "subject_kind": "service_group", "subject_id": subject, "signal": r.ID, "severity": "warning", "occurred_at": at, "evidence": evidence})
		if err != nil {
			t.Fatal(err)
		}
		msg, err := wecomPlatformAlertPayload(string(payload), "delivery-one")
		if err != nil {
			t.Fatal(err)
		}
		body, _ := json.Marshal(msg)
		if !strings.Contains(string(body), r.Name) {
			t.Fatal("frozen human rule name omitted", string(body))
		}
	}
	evidence.RuleID = "wrong"
	if validAvailabilityEvidence(evidence, subject, r.ID) {
		t.Fatal("wrong target evidence accepted")
	}
}
