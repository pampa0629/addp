package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/addp/common/client"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/repository"
	"github.com/addp/monitor/internal/resourcequery"
	"github.com/google/uuid"
)

type httpRuleUsers struct {
	processUsers
	mu sync.Mutex
}

func (u *httpRuleUsers) GetRuntimeInstancesForUser(ctx context.Context, ids []uint, token string) ([]client.RuntimeInstanceReference, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.processUsers.GetRuntimeInstancesForUser(ctx, ids, token)
}

type httpRuleBackend struct {
	processBackend
	value    float64
	state    string
	changeAt time.Time
	hook     func()
}

func (b *httpRuleBackend) HTTPCollection(_ context.Context, _ resourcequery.ProcessScope, at time.Time, _ resourcequery.Budget) (resourcequery.Collection, error) {
	state := b.state
	if state == "" {
		state = "collecting"
	}
	return resourcequery.Collection{State: state, SampledAt: &at}, nil
}
func (b *httpRuleBackend) HTTP(_ context.Context, p resourcequery.HTTPPlan, _ resourcequery.ProcessScope, _ resourcequery.Budget) ([]resourcequery.HTTPObservation, error) {
	if b.hook != nil {
		b.hook()
	}
	b.calls++
	rows := resourcequery.Empty(p.Plan, "no_data")
	for i := range rows {
		for j := range rows[i].Points {
			point := &rows[i].Points[j]
			v := b.value
			if !b.changeAt.IsZero() && !point.EvaluatedAt.Before(b.changeAt) {
				v = 5
			}
			if rows[i].MetricKey == "http.requests.per_second" {
				v = 10
			}
			sample := point.EvaluatedAt
			point.SampledAt = &sample
			point.Value = &v
			point.DataState = "valid"
		}
	}
	return []resourcequery.HTTPObservation{{Series: rows}}, nil
}

type httpRuleFixture struct {
	*logRetryFixture
	s          *HTTPAlertRuleService
	backend    *httpRuleBackend
	users      *httpRuleUsers
	identities *resourceRuleIdentity
	input      HTTPAlertRuleInput
	at         time.Time
	module     string
}

func newHTTPRuleFixture(t *testing.T) *httpRuleFixture {
	t.Helper()
	f := newLogRetryFixture(t, "webhook")
	at := time.Now().UTC().Truncate(time.Second)
	start := at.Add(-time.Hour)
	module := "http-" + uuid.NewString()
	instance := uuid.NewString()
	declared := true
	users := &httpRuleUsers{processUsers: processUsers{rows: []client.RuntimeInstanceReference{{ID: 1, ModuleName: module, InstanceID: instance, Role: "backend", Status: "up", LeaseExpiresAt: at.Add(time.Hour), ProcessStartedAt: &start, ProcessMetricsDeclared: &declared}}}}
	identities := &resourceRuleIdentity{processFacts: []commonmodels.ObservabilityModuleIdentity{{ModuleName: module, InstanceID: instance, Role: "backend", LeaseExpiresAt: at.Add(time.Hour), ProcessStartedAt: &start, ProcessMetrics: &commonmodels.ProcessMetricsDeclaration{SchemaVersion: commonmodels.ProcessMetricsSchema, Endpoint: "https://127.0.0.1:9100/metrics"}}}}
	backend := &httpRuleBackend{value: 40}
	o := NewResourceObservationService(repository.NewResourceQueryPolicyRepository(f.db), nil, nil, nil, true, resourceSourcePolicyWithNetwork(t, []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, []uint16{9100}), ProcessObservationDependencies{Users: users, Identities: identities, Backend: backend})
	s := NewHTTPAlertRuleService(f.db, o, identities, f.n)
	s.startedAt = at
	policy := models.HTTPThresholdPolicy{TriggerValue: 30, RecoveryValue: 10, TriggerSeconds: 15, RecoverySeconds: 15}
	off := false
	result := &httpRuleFixture{f, s, backend, users, identities, HTTPAlertRuleInput{Name: "HTTP fixture", InstanceID: 1, MetricKey: "http.failures.percent", TriggerValue: &policy.TriggerValue, RecoveryValue: &policy.RecoveryValue, TriggerSeconds: &policy.TriggerSeconds, RecoverySeconds: &policy.RecoverySeconds, Enabled: &off}, at, module}
	t.Cleanup(func() {
		for _, q := range []string{
			`DELETE FROM monitor.platform_alert_deliveries WHERE event_id IN (SELECT id FROM monitor.platform_alert_events WHERE incident_id IN (SELECT id FROM monitor.platform_alert_incidents WHERE category='http_quality' AND subject_id=?))`,
			`DELETE FROM monitor.platform_alert_events WHERE incident_id IN (SELECT id FROM monitor.platform_alert_incidents WHERE category='http_quality' AND subject_id=?)`,
			`DELETE FROM monitor.platform_alert_incidents WHERE category='http_quality' AND subject_id=?`,
			`DELETE FROM monitor.http_alert_rules WHERE module_name=?`,
		} {
			if e := f.db.Exec(q, module).Error; e != nil {
				t.Error(e)
			}
		}
		var count int64
		if e := f.db.Model(&models.HTTPAlertRule{}).Where("module_name=?", module).Count(&count).Error; e != nil || count != 0 {
			t.Error("HTTP fixture residue", count, e)
		}
	})
	return result
}
func (f *httpRuleFixture) create(t *testing.T) models.HTTPAlertRule {
	t.Helper()
	r, e := f.s.Save(context.Background(), "", "current_user", f.input, f.at)
	if e != nil || r.Enabled {
		t.Fatal("disabled create", e)
	}
	return r
}
func (f *httpRuleFixture) enable(t *testing.T, r models.HTTPAlertRule) models.HTTPAlertRule {
	t.Helper()
	in := f.input
	on := true
	in.Enabled = &on
	in.Version = r.Version
	out, e := f.s.Save(context.Background(), r.ID, "current_user", in, f.at)
	if e != nil {
		t.Fatal(e)
	}
	return out
}
func (f *httpRuleFixture) evaluate(t *testing.T, seconds int) {
	t.Helper()
	if e := f.s.Evaluate(context.Background(), f.at.Add(time.Duration(seconds)*time.Second)); e != nil {
		t.Fatal(e)
	}
}
func (f *httpRuleFixture) active(t *testing.T) []models.PlatformAlertIncident {
	t.Helper()
	page, e := f.n.Incidents(context.Background(), 1, 20, "http_quality", "active", f.module)
	if e != nil {
		t.Fatal(e)
	}
	return page.Data
}

func TestIntegrationPostgresHTTPRuleAuthorizationCRUDAndCAS(t *testing.T) {
	f := newHTTPRuleFixture(t)
	ctx := context.Background()
	f.evaluate(t, 0)
	if f.identities.calls != 0 || f.backend.calls != 0 {
		t.Fatal("dormant rules queried sources")
	}
	f.users.err = &client.SystemAPIError{StatusCode: 403}
	if _, e := f.s.Save(ctx, "", "current_user", f.input, f.at); e == nil {
		t.Fatal("owner denial bypassed")
	}
	f.users.err = nil
	r := f.create(t)
	if _, e := f.s.Save(ctx, "", "current_user", f.input, f.at); !errors.Is(e, ErrLogConflict) {
		t.Fatal("duplicate subject accepted", e)
	}
	in := f.input
	in.Version = r.Version
	name := in.Name
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, suffix := range []string{"one", "two"} {
		wg.Add(1)
		go func(suffix string) {
			defer wg.Done()
			copy := in
			copy.Name = name + suffix
			_, e := f.s.Save(ctx, r.ID, "current_user", copy, f.at.Add(time.Second))
			results <- e
		}(suffix)
	}
	wg.Wait()
	close(results)
	wins, conflicts := 0, 0
	for e := range results {
		if e == nil {
			wins++
		} else if errors.Is(e, ErrLogConflict) {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatal("CAS winners", wins, conflicts)
	}
	f.db.Where("id=?", r.ID).Take(&r)
	if e := f.s.Delete(ctx, r.ID, r.Version-1); !errors.Is(e, ErrLogConflict) {
		t.Fatal(e)
	}
	in.Version = r.Version
	in.MetricKey = "http.requests.per_second"
	if _, e := f.s.Save(ctx, r.ID, "current_user", in, f.at); !errors.Is(e, ErrLogConflict) {
		t.Fatal("subject mutation", e)
	}
	if e := f.s.Delete(ctx, r.ID, r.Version); e != nil {
		t.Fatal(e)
	}
	for _, token := range f.users.tokens {
		if token != "current_user" {
			t.Fatal("owner token substituted")
		}
	}
}

func TestIntegrationPostgresHTTPRuleFrozenRecoveryAndAtomicOutbox(t *testing.T) {
	f := newHTTPRuleFixture(t)
	ctx := context.Background()
	r := f.enable(t, f.create(t))
	d, e := f.n.Save(ctx, 0, PlatformAlertDestinationInput{Name: "http-" + uuid.NewString(), Channel: "webhook", URL: f.target.URL, Categories: []string{"http_quality"}, EventTypes: []string{"opened", "resolved"}})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		f.db.Where("destination_id=?", d.ID).Delete(&models.PlatformAlertDelivery{})
		f.db.Delete(&models.PlatformAlertDestination{}, d.ID)
	})
	d, e = f.n.SetSecret(ctx, d.ID, d.Version, "http-test-fixture-secret")
	if e != nil {
		t.Fatal(e)
	}
	d, e = f.n.Save(ctx, d.ID, PlatformAlertDestinationInput{Version: d.Version, Name: d.Name, Channel: d.Channel, URL: d.URL, Categories: []string{"http_quality"}, EventTypes: []string{"opened", "resolved"}, Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	for _, sec := range []int{0, 15, 30, 45, 60} {
		f.evaluate(t, sec)
	}
	if len(f.active(t)) != 0 {
		t.Fatal("HTTP alert opened before a full fresh window and duration")
	}
	f.evaluate(t, 75)
	active := f.active(t)
	if len(active) != 1 || active[0].HTTPEvidence == nil {
		t.Fatal("missing HTTP opening evidence", active)
	}
	incident := active[0]
	if e := f.s.Delete(ctx, r.ID, r.Version); !errors.Is(e, ErrLogConflict) {
		t.Fatal("active rule deleted", e)
	}
	f.backend.state = "failed"
	f.evaluate(t, 90)
	if len(f.active(t)) != 1 {
		t.Fatal("source failure recovered event")
	}
	f.backend.state = ""
	in := f.input
	in.Version = r.Version
	on := true
	in.Enabled = &on
	trigger, recovery := 90., 70.
	in.TriggerValue = &trigger
	in.RecoveryValue = &recovery
	r, e = f.s.Save(ctx, r.ID, "current_user", in, f.at.Add(105*time.Second))
	if e != nil {
		t.Fatal(e)
	}
	for _, sec := range []int{105, 120, 135, 150, 165, 180} {
		f.evaluate(t, sec)
	}
	if len(f.active(t)) != 1 {
		t.Fatal("mutable recovery policy resolved opening")
	}
	f.backend.changeAt = f.at.Add(195 * time.Second)
	f.evaluate(t, 195)
	f.evaluate(t, 210)
	if len(f.active(t)) != 0 {
		t.Fatal("original recovery threshold not applied")
	}
	var events []models.PlatformAlertEvent
	if e := f.db.Where("incident_id=?", incident.ID).Order("occurred_at").Find(&events).Error; e != nil || len(events) != 2 {
		t.Fatal("events", len(events), e)
	}
	var deliveries []models.PlatformAlertDelivery
	if e := f.db.Where("event_id IN ?", []string{events[0].ID, events[1].ID}).Find(&deliveries).Error; e != nil || len(deliveries) != 2 {
		t.Fatal("atomic HTTP outbox", len(deliveries), e)
	}
	for i, event := range events {
		if event.HTTPEvidence == nil || event.HTTPEvidence.Policy.RecoveryValue != 10 || event.HTTPEvidence.RuleVersion != incident.HTTPEvidence.RuleVersion {
			t.Fatal("frozen HTTP evidence", i)
		}
	}
	for _, delivery := range deliveries {
		var payload map[string]any
		if json.Unmarshal([]byte(delivery.Payload), &payload) != nil || payload["schema"] != "addp.platform-http-alert/v1" || payload["evidence"] == nil || delivery.DestinationID != d.ID || strings.Contains(delivery.Payload, "127.0.0.1") {
			t.Fatal("HTTP subscriber isolation", delivery.ID)
		}
	}
	f.backend.hook = func() {
		f.backend.hook = nil
		in.Version = r.Version
		off := false
		in.Enabled = &off
		_, e = f.s.Save(ctx, r.ID, "current_user", in, f.at.Add(225*time.Second))
		if e != nil {
			t.Fatal(e)
		}
	}
	f.evaluate(t, 225)
	var saved models.HTTPAlertRule
	f.db.Where("id=?", r.ID).Take(&saved)
	if saved.Enabled || saved.LastVerdict != "paused" {
		t.Fatal("late evaluator overwrote disable", saved.LastVerdict)
	}
}

func TestIntegrationPostgresHTTPRulePolicyCASAndExpiredCompletionLease(t *testing.T) {
	f := newHTTPRuleFixture(t)
	rule := f.enable(t, f.create(t))
	for _, seconds := range []int{0, 15, 30, 45, 60} {
		f.evaluate(t, seconds)
	}
	policies := repository.NewResourceQueryPolicyRepository(f.db)
	t.Cleanup(func() {
		if err := f.db.Where("id=?", 1).Delete(&models.ResourceQueryPolicy{}).Error; err != nil {
			t.Error(err)
		}
	})
	f.backend.hook = func() {
		f.backend.hook = nil
		budget := resourcequery.DefaultBudget()
		budget.MaxPointsPerSeries = 10
		if _, err := policies.Save(context.Background(), models.ResourceQueryPolicy{Budget: budget}, 0); err != nil {
			t.Fatal(err)
		}
	}
	f.evaluate(t, 75)
	var saved models.HTTPAlertRule
	if err := f.db.Where("id=?", rule.ID).Take(&saved).Error; err != nil || saved.LastError != "source_changed" || saved.WindowSince != nil || len(f.active(t)) != 0 {
		t.Fatal("old policy produced an alert", saved.LastError, err)
	}
	cfg, err := f.s.observations.Policy(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	source, err := f.s.sources(context.Background(), rule, cfg.Version)
	if err != nil {
		t.Fatal(err)
	}
	source.lease = time.Now().Add(-time.Second)
	if err := f.s.applyHTTPRule(context.Background(), rule, source, cfg, []resourcequery.Series{httpThresholdSeries(f.at.Add(90*time.Second), 40)}, f.at.Add(90*time.Second), ""); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Where("id=?", rule.ID).Take(&saved).Error; err != nil || saved.LastError != "source_changed" || len(f.active(t)) != 0 {
		t.Fatal("expired lease produced an alert", err)
	}
}
