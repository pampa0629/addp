package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	commonclient "github.com/addp/common/client"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/monitor/internal/models"
	"github.com/google/uuid"
)

type availabilityOwnerFixture struct {
	at            time.Time
	deployment    string
	group         commonmodels.ServiceGroupObservation
	denied        bool
	unavailable   bool
	sourceChanged bool
	calls         int
	mu            sync.Mutex
}

func (f *availabilityOwnerFixture) GetModulesForUser(context.Context, string) ([]commonclient.ModuleReference, error) {
	if f.denied {
		return nil, errors.New("permission denied")
	}
	enabled := true
	return []commonclient.ModuleReference{{ModuleName: "meta", Version: 1, Enabled: &enabled}}, nil
}
func (f *availabilityOwnerFixture) GetObservabilityIdentities(context.Context) (*commonmodels.ObservabilityIdentitySnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.unavailable {
		return nil, errors.New("owner unavailable")
	}
	g := f.group
	if f.sourceChanged && f.calls%2 == 0 {
		g.OnlineReplicas++
	}
	return &commonmodels.ObservabilityIdentitySnapshot{ObservedAt: f.at, ServiceGroups: &commonmodels.ServiceGroupSnapshot{State: "available", DeploymentID: f.deployment, Revision: 1, ObservedAt: f.at, Groups: []commonmodels.ServiceGroupObservation{g}}}, nil
}
func TestIntegrationPostgresServiceAvailabilityLifecycleAndExplicitNotification(t *testing.T) {
	f := newLogRetryFixture(t, "email")
	ctx := context.Background()
	at := time.Now().UTC().Truncate(time.Microsecond)
	owner := &availabilityOwnerFixture{at: at, deployment: availabilityHash(uuid.NewString()), group: *availabilityTestGroup(at, 0)}
	s := NewServiceAvailabilityRuleService(f.db, owner, owner, f.n)
	s.startedAt = at.Add(-time.Minute)
	s.now = func() time.Time { return owner.at }
	groupID := availabilitySubject(owner.deployment, "meta", "worker", "scan")
	var rule models.ServiceAvailabilityRule
	var dest models.PlatformAlertDestination
	t.Cleanup(func() {
		// Own subjects only; never sweep another test's or a personal deployment's rows.
		statements := []struct {
			model any
			where string
			args  []any
		}{
			{&models.PlatformAlertDelivery{}, "event_id IN (SELECT id FROM monitor.platform_alert_events WHERE incident_id IN (SELECT id FROM monitor.platform_alert_incidents WHERE subject_id=?))", []any{groupID}},
			{&models.PlatformAlertEvent{}, "incident_id IN (SELECT id FROM monitor.platform_alert_incidents WHERE subject_id=?)", []any{groupID}},
			{&models.PlatformAlertIncident{}, "subject_id=?", []any{groupID}},
			{&models.ServiceAvailabilityRule{}, "subject_key=?", []any{groupID}},
		}
		if dest.ID != 0 {
			statements = append(statements, struct {
				model any
				where string
				args  []any
			}{&models.PlatformAlertDestination{}, "id=?", []any{dest.ID}})
		}
		for _, q := range statements {
			if err := f.db.Where(q.where, q.args...).Delete(q.model).Error; err != nil {
				t.Error(err)
			}
			var n int64
			if err := f.db.Model(q.model).Where(q.where, q.args...).Count(&n).Error; err != nil || n != 0 {
				t.Errorf("cleanup residual %d %v", n, err)
			}
		}
	})
	trigger, recovery, enabled := 60, 30, false
	input := ServiceAvailabilityRuleInput{Name: "Frozen original", GroupID: groupID, TriggerSeconds: &trigger, RecoverySeconds: &recovery, Enabled: &enabled}
	var err error
	rule, err = s.Save(ctx, "", "human", input, at)
	if err != nil || rule.Enabled {
		t.Fatal(rule, err)
	}
	if _, err = s.Save(ctx, "", "human", input, at); !errors.Is(err, ErrLogConflict) {
		t.Fatal("duplicate group accepted", err)
	}
	enabled = true
	if _, err = s.Save(ctx, "", "human", input, at); !errors.Is(err, ErrLogInvalid) {
		t.Fatal("enabled creation accepted", err)
	}
	input.Version = rule.Version
	rule, err = s.Save(ctx, rule.ID, "human", input, at)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Save(ctx, rule.ID, "human", input, at); !errors.Is(err, ErrLogConflict) {
		t.Fatal("stale CAS accepted", err)
	}
	input.Version = rule.Version
	old := input.GroupID
	input.GroupID = strings.Repeat("b", 64)
	if _, err = s.Save(ctx, rule.ID, "human", input, at); !errors.Is(err, ErrLogConflict) {
		t.Fatal("target mutation accepted", err)
	}
	input.GroupID = old
	list, err := s.List(ctx, "human", 1, 20)
	if err != nil || list.Total < 1 || len(list.Data) == 0 {
		t.Fatal("page read failed", list, err)
	}
	owner.denied = true
	if _, err = s.List(ctx, "human", 1, 20); err == nil {
		t.Fatal("current user rejection ignored")
	}
	owner.denied = false
	// The old destination remains log-only. A new explicit subscription gets both events.
	dest, err = f.n.Save(ctx, 0, PlatformAlertDestinationInput{Name: "availability-" + uuid.NewString(), Channel: "email", Recipients: []string{"group@example.test"}, Categories: []string{models.PlatformAlertCategoryAvailability}, EventTypes: []string{"opened", "resolved"}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	step := func(seconds, online int) {
		t.Helper()
		owner.at = at.Add(time.Duration(seconds) * time.Second)
		owner.group.OnlineReplicas = online
		if err := s.Evaluate(ctx, owner.at); err != nil {
			t.Fatal(err)
		}
		ruleID := rule.ID
		rule = models.ServiceAvailabilityRule{}
		if err := f.db.First(&rule, "id=?", ruleID).Error; err != nil {
			t.Fatal(err)
		}
	}
	incidentCount := func() int64 {
		t.Helper()
		var n int64
		if err := f.db.Model(&models.PlatformAlertIncident{}).Where("subject_id=?", groupID).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n
	}
	for _, sec := range []int{0, 15, 30, 45, 60} {
		step(sec, 0)
	}
	if rule.ConfirmedAt != nil || rule.LastError != "waiting_confirmation" || incidentCount() != 0 {
		t.Fatal("historical count fabricated runtime", rule)
	}
	step(75, 1)
	confirmed := *rule.ConfirmedAt
	for _, sec := range []int{90, 105, 120, 135} {
		step(sec, 0)
	}
	if incidentCount() != 0 {
		t.Fatal("early alert")
	}
	step(150, 0)
	var incident models.PlatformAlertIncident
	if err := f.db.Where("subject_id=?", groupID).Take(&incident).Error; err != nil {
		t.Fatal(err)
	}
	if incident.Status != "open" || incident.AvailabilityEvidence == nil || !incident.AvailabilityEvidence.ConfirmedAt.Equal(confirmed) {
		t.Fatal(incident)
	}
	if err := s.Delete(ctx, "human", rule.ID, rule.Version); !errors.Is(err, ErrLogConflict) {
		t.Fatal("active incident rule deleted", err)
	}
	// Parallel repeated evaluation cannot add an event or delivery.
	owner.at = at.Add(165 * time.Second)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for j := 0; j < 2; j++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- s.Evaluate(ctx, owner.at) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := f.db.First(&incident, incident.ID).Error; err != nil {
		t.Fatal(err)
	}
	incident, err = f.n.ManageIncident(ctx, incident.ID, incident.Version, "acknowledge", nil, 1, owner.at)
	if err != nil {
		t.Fatal(err)
	}
	until := owner.at.Add(time.Hour)
	incident, err = f.n.ManageIncident(ctx, incident.ID, incident.Version, "suppress", &until, 1, owner.at)
	if err != nil {
		t.Fatal(err)
	}
	// Editing current policy/name cannot rewrite the active incident's recovery contract.
	input.Version = rule.Version
	input.Name = "Changed current"
	recovery = 15
	rule, err = s.Save(ctx, rule.ID, "human", input, owner.at)
	if err != nil {
		t.Fatal(err)
	}
	owner.sourceChanged = true
	step(180, 1)
	owner.sourceChanged = false
	if rule.LastError != "source_changed" {
		t.Fatal("two-read change ignored", rule)
	}
	owner.unavailable = true
	step(195, 1)
	owner.unavailable = false
	step(210, 1)
	step(225, 1)
	if err := f.db.First(&incident, incident.ID).Error; err != nil || incident.Status != "acknowledged" {
		t.Fatal("recovered before frozen 30s", incident, err)
	}
	step(240, 1)
	if err := f.db.First(&incident, incident.ID).Error; err != nil || incident.Status != "resolved" {
		t.Fatal(incident, err)
	}
	var events []models.PlatformAlertEvent
	if err := f.db.Where("incident_id=?", incident.ID).Order("occurred_at").Find(&events).Error; err != nil || len(events) != 2 {
		t.Fatal(events, err)
	}
	for _, e := range events {
		if e.AvailabilityEvidence == nil || e.AvailabilityEvidence.RuleName != "Frozen original" || e.AvailabilityEvidence.Policy.RecoverySeconds != 30 {
			t.Fatal("historical policy rewritten", e)
		}
	}
	var deliveries []models.PlatformAlertDelivery
	if err := f.db.Where("event_id IN ?", []string{events[0].ID, events[1].ID}).Find(&deliveries).Error; err != nil || len(deliveries) != 2 {
		t.Fatal(deliveries, err)
	}
	for _, d := range deliveries {
		if d.DestinationID != dest.ID {
			t.Fatal("old subscription expanded", d)
		}
		var body map[string]any
		if json.Unmarshal([]byte(d.Payload), &body) != nil || body["schema"] != "addp.platform-service-availability-alert/v1" {
			t.Fatal("bad outbox payload")
		}
	}
	for _, d := range deliveries {
		if d.EventID == events[1].ID && d.Status != "suppressed" {
			t.Fatal("suppression failed", d)
		}
	}
	history, err := f.n.Incidents(ctx, PlatformAlertFilter{Page: 1, PageSize: 20, Category: models.PlatformAlertCategoryAvailability, Status: "resolved", SubjectID: groupID, Search: "Frozen original"}, "human")
	if err != nil || history.Total != 1 || history.Data[0].Name != "Frozen original" {
		t.Fatal("frozen history name missing", history, err)
	}
	// New evaluator retains eligibility but must collect a complete new window.
	s = NewServiceAvailabilityRuleService(f.db, owner, owner, f.n)
	s.startedAt = owner.at.Add(time.Second)
	s.now = func() time.Time { return owner.at }
	step(255, 0)
	if rule.ConfirmedAt == nil || !rule.ConfirmedAt.Equal(confirmed) || rule.WindowSamples != 1 {
		t.Fatal("restart confirmation/window", rule)
	}
	owner.group.ChangedAt = owner.at
	step(270, 0)
	if rule.ConfirmedAt != nil || rule.LastError != "waiting_confirmation" {
		t.Fatal("intent change reused eligibility", rule)
	}
	step(285, 1)
	input.Version = rule.Version
	enabled = false
	rule, err = s.Save(ctx, rule.ID, "human", input, owner.at)
	if err != nil || rule.ConfirmedAt != nil {
		t.Fatal("disabled response retained confirmation", rule, err)
	}
	input.Version = rule.Version
	enabled = true
	rule, err = s.Save(ctx, rule.ID, "human", input, owner.at)
	if err != nil || rule.ConfirmedAt != nil {
		t.Fatal("re-enable reused confirmation", rule, err)
	}
	step(300, 0)
	if rule.LastError != "waiting_confirmation" {
		t.Fatal("re-enabled group did not await online", rule)
	}
	owner.at = at.Add(305 * time.Second)
	s.now = func() time.Time { return owner.at.Add(6 * time.Second) }
	if err := s.Evaluate(ctx, s.now()); err != nil {
		t.Fatal(err)
	}
	var stale models.ServiceAvailabilityRule
	if err := f.db.First(&stale, "id=?", rule.ID).Error; err != nil || stale.LastError != "identity_unavailable" || stale.WindowSince != nil {
		t.Fatal("stale owner observation accepted", stale, err)
	}
	s.now = func() time.Time { return owner.at }
	owner.group.State = "paused"
	owner.group.Reason = "planned_stopped"
	owner.group.DesiredState = "planned_stopped"
	step(315, 0)
	if rule.LastVerdict != "paused" {
		t.Fatal("planned stop not paused", rule)
	}
	// Owner failure must never prevent stopping a rule, and re-enable clears confirmation.
	owner.unavailable = true
	input.Version = rule.Version
	enabled = false
	rule, err = s.Save(ctx, rule.ID, "human", input, owner.at)
	if err != nil || rule.ConfirmedAt != nil {
		t.Fatal(rule, err)
	}
	if err = s.Delete(ctx, "human", rule.ID, rule.Version); err != nil {
		t.Fatal(err)
	}
}

// Compile-time assertions retain the one public User read and one private projection.
var _ UserModuleReader = (*availabilityOwnerFixture)(nil)
