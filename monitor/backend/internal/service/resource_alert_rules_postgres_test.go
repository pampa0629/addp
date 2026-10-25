package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	commonmodels "github.com/addp/common/models"
	"github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/repository"
	"github.com/addp/monitor/internal/resourcequery"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type resourceRuleIdentity struct {
	processFacts []commonmodels.ObservabilityModuleIdentity
	node         string
	version      int64
	calls        int
	disabled     bool
	err          error
}

func (i *resourceRuleIdentity) GetObservabilityIdentities(context.Context) (*commonmodels.ObservabilityIdentitySnapshot, error) {
	i.calls++
	nodes := []commonmodels.ObservabilityNodeIdentity{}
	if !i.disabled {
		nodes = append(nodes, commonmodels.ObservabilityNodeIdentity{NodeID: i.node, Version: i.version})
	}
	return &commonmodels.ObservabilityIdentitySnapshot{ObservedAt: time.Now().UTC(), Nodes: nodes, ModuleInstances: append([]commonmodels.ObservabilityModuleIdentity{}, i.processFacts...)}, i.err
}

type resourceRuleBackend struct {
	resourceBackend
	value       float64
	changeAt    time.Time
	changeValue float64
	hook        func()
	fail        bool
	plans       []resourcequery.Plan
}

func (b *resourceRuleBackend) Query(_ context.Context, p resourcequery.Plan, scope resourcequery.Scope, _ resourcequery.Budget) ([]resourcequery.Series, error) {
	b.calls++
	b.scope = scope
	b.plans = append(b.plans, p)
	if b.hook != nil {
		b.hook()
	}
	if b.fail {
		return nil, errors.New("fixture transport failure")
	}
	rows := []resourcequery.Series{}
	var uptime resourcequery.Series
	for _, key := range []string{"node.memory.used_percent", "node.cpu.busy_percent"} {
		metric, witness := pressureWindow(key, b.value, p.End)
		uptime = witness
		if !b.changeAt.IsZero() {
			for j := range metric.Points {
				if !metric.Points[j].EvaluatedAt.Before(b.changeAt) {
					v := b.changeValue
					metric.Points[j].Value = &v
				}
			}
		}
		for _, descriptor := range p.Metrics {
			if descriptor.Key == key {
				rows = append(rows, metric)
			}
		}
	}
	return append(rows, uptime), nil
}

type resourceRuleFixture struct {
	*logRetryFixture
	s          *ResourceAlertRuleService
	o          *ResourceObservationService
	backend    *resourceRuleBackend
	identities *resourceRuleIdentity
	target     models.MonitoringTarget
	at         time.Time
	input      ResourceAlertRuleInput
}

func newResourceRuleFixture(t *testing.T) *resourceRuleFixture {
	t.Helper()
	f := newLogRetryFixture(t, "webhook")
	node := uuid.NewString()
	at := time.Now().UTC().Truncate(time.Second)
	target := models.MonitoringTarget{ID: uuid.NewString(), NodeID: node, MonitorKind: "host_resources", SourceType: "node_exporter", Endpoint: "https://127.0.0.1:9100/metrics", Enabled: true, Version: 1}
	if err := f.db.Create(&target).Error; err != nil {
		t.Fatal(err)
	}
	backend := &resourceRuleBackend{value: 90}
	identities := &resourceRuleIdentity{node: node, version: 1}
	o := NewResourceObservationService(repository.NewResourceQueryPolicyRepository(f.db), repository.NewMonitoringTargetRepository(f.db), &resourceNodes{enabled: true}, backend, true, resourceSourcePolicy(t), ProcessObservationDependencies{})
	s := NewResourceAlertRuleService(f.db, o, identities, f.n)
	s.startedAt = at
	off := false
	pressure := models.ResourcePressurePolicy{TriggerPercent: 80, RecoveryPercent: 75, TriggerSeconds: 30, RecoverySeconds: 30}
	result := &resourceRuleFixture{f, s, o, backend, identities, target, at, ResourceAlertRuleInput{Name: "Resource " + uuid.NewString(), NodeID: node, MetricKey: "node.memory.used_percent", TriggerPercent: &pressure.TriggerPercent, RecoveryPercent: &pressure.RecoveryPercent, TriggerSeconds: &pressure.TriggerSeconds, RecoverySeconds: &pressure.RecoverySeconds, Enabled: &off}}
	t.Cleanup(func() {
		for _, sql := range []string{
			`DELETE FROM monitor.platform_alert_deliveries WHERE event_id IN (SELECT id FROM monitor.platform_alert_events WHERE incident_id IN (SELECT id FROM monitor.platform_alert_incidents WHERE subject_id=?))`,
			`DELETE FROM monitor.platform_alert_events WHERE incident_id IN (SELECT id FROM monitor.platform_alert_incidents WHERE subject_id=?)`,
			`DELETE FROM monitor.platform_alert_incidents WHERE subject_id=?`,
			`DELETE FROM monitor.resource_alert_rules WHERE node_id=?`,
			`DELETE FROM monitor.monitoring_targets WHERE node_id=?`,
		} {
			if err := f.db.Exec(sql, node).Error; err != nil {
				t.Error(err)
			}
		}
	})
	return result
}
func (f *resourceRuleFixture) create(t *testing.T) models.ResourceAlertRule {
	t.Helper()
	r, e := f.s.Save(context.Background(), "", "current_user", f.input, f.at)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func (f *resourceRuleFixture) enable(t *testing.T, r models.ResourceAlertRule) models.ResourceAlertRule {
	t.Helper()
	on := true
	in := f.input
	in.Version = r.Version
	in.Enabled = &on
	result, e := f.s.Save(context.Background(), r.ID, "current_user", in, f.at)
	if e != nil {
		t.Fatal(e)
	}
	return result
}
func (f *resourceRuleFixture) evaluate(t *testing.T, seconds int) {
	t.Helper()
	if e := f.s.Evaluate(context.Background(), f.at.Add(time.Duration(seconds)*time.Second)); e != nil {
		t.Fatal(e)
	}
}
func (f *resourceRuleFixture) active(t *testing.T) []PlatformAlertView {
	t.Helper()
	r, e := f.n.Incidents(context.Background(), PlatformAlertFilter{Page: 1, PageSize: 20, Category: "resource", Status: "active", SubjectID: f.target.NodeID, Severity: ""}, "")
	if e != nil {
		t.Fatal(e)
	}
	return r.Data
}
func TestIntegrationPostgresResourceRuleCRUDAndDormantCollection(t *testing.T) {
	f := newResourceRuleFixture(t)
	ctx := context.Background()
	f.evaluate(t, 0)
	if f.backend.calls != 0 || f.identities.calls != 0 {
		t.Fatal("empty rules accessed collectors")
	}
	bad := f.input
	on := true
	bad.Enabled = &on
	if _, e := f.s.Save(ctx, "", "token", bad, f.at); !errors.Is(e, ErrLogInvalid) {
		t.Fatal("created enabled rule", e)
	}
	r := f.create(t)
	f.evaluate(t, 0)
	if f.backend.calls != 0 || f.identities.calls != 0 {
		t.Fatal("disabled rule accessed collectors")
	}
	if _, e := f.s.Save(ctx, "", "token", f.input, f.at); !errors.Is(e, ErrLogConflict) {
		t.Fatal("duplicate subject+metric", e)
	}
	r = f.enable(t, r)
	if _, e := f.s.Save(ctx, r.ID, "token", f.input, f.at); !errors.Is(e, ErrLogInvalid) {
		t.Fatal("zero CAS", e)
	}
	old := f.input
	old.Version = 1
	if _, e := f.s.Save(ctx, r.ID, "token", old, f.at); !errors.Is(e, ErrLogConflict) {
		t.Fatal("stale CAS", e)
	}
	for _, v := range []int{0, 15, 30, 45} {
		f.evaluate(t, v)
	}
	active := f.active(t)
	if len(active) != 1 || active[0].ResourceEvidence == nil || active[0].ResourceEvidence.Samples != 3 || active[0].ResourceEvidence.Minimum != 90 || active[0].ResourceEvidence.RuleVersion != r.Version || active[0].ResourceEvidence.RuleName != r.Name {
		t.Fatalf("missing opened evidence: %+v", active)
	}
	if e := f.s.Delete(ctx, r.ID, r.Version); !errors.Is(e, ErrLogConflict) {
		t.Fatal("deleted active rule", e)
	}
	var saved models.ResourceAlertRule
	if e := f.db.First(&saved, "id=?", r.ID).Error; e != nil || saved.Version != r.Version || !saved.UpdatedAt.Equal(r.UpdatedAt) {
		t.Fatal("evaluation changed CAS", saved, e)
	}
	// Same-round concurrent workers must not increment lifecycle versions twice.
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for j := 0; j < 2; j++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- f.s.applyResourceRule(ctx, r, f.at.Add(45*time.Second), nil, ResourceQueryPolicyResponse{}, nil, PlatformAlertUnknown, "late")
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	if f.active(t)[0].Version != active[0].Version {
		t.Fatal("duplicate round changed incident")
	}
	// Control-plane removal and failed queries never recover existing incidents.
	f.identities.disabled = true
	f.evaluate(t, 60)
	if len(f.active(t)) != 1 {
		t.Fatal("missing host recovered")
	}
	f.identities.disabled = false
	f.backend.fail = true
	f.evaluate(t, 75)
	if len(f.active(t)) != 1 {
		t.Fatal("failed query recovered")
	}
	f.backend.fail = false
	f.o.enabled = false
	before := f.backend.calls
	f.evaluate(t, 90)
	if f.backend.calls != before || len(f.active(t)) != 1 {
		t.Fatal("paused collection queried or recovered")
	}
	f.o.enabled = true
	// A rules-only save uses the current user's System lookup, never a background credential.
	for _, token := range f.o.nodes.(*resourceNodes).tokens {
		if token != "current_user" && token != "token" {
			t.Fatal("unexpected owner token")
		}
	}
	if len(f.backend.plans) == 0 || f.backend.plans[0].StepSeconds != 15 || f.backend.plans[0].Points != 22 || f.backend.scope.NodeID != r.NodeID {
		t.Fatal("query bypassed fixed plan or source")
	}
}
func TestIntegrationPostgresResourceRuleRecoveryPinsOpenedPolicyAndHistory(t *testing.T) {
	f := newResourceRuleFixture(t)
	ctx := context.Background()
	r := f.enable(t, f.create(t))
	// Explicit resource subscriber; the fixture's log subscriber must stay isolated.
	destination, e := f.n.Save(ctx, 0, PlatformAlertDestinationInput{Name: "rule-" + uuid.NewString(), Channel: "webhook", URL: f.targetURL(), Categories: []string{"resource"}, EventTypes: []string{"opened", "resolved"}})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		f.db.Where("destination_id=?", destination.ID).Delete(&models.PlatformAlertDelivery{})
		f.db.Delete(&models.PlatformAlertDestination{}, destination.ID)
	})
	destination, e = f.n.SetSecret(ctx, destination.ID, destination.Version, "rule-fixture-secret-123456")
	if e != nil {
		t.Fatal(e)
	}
	_, e = f.n.Save(ctx, destination.ID, PlatformAlertDestinationInput{Version: destination.Version, Name: destination.Name, Channel: "webhook", URL: destination.URL, Categories: []string{"resource"}, EventTypes: []string{"opened", "resolved"}, Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	for _, v := range []int{0, 15, 30, 45} {
		f.evaluate(t, v)
	}
	incident := f.active(t)[0]
	changed := f.input
	changed.Name = "renamed resource rule"
	on := true
	changed.Enabled = &on
	changed.Version = r.Version
	newTrigger, newRecovery := 99., 95.
	changed.TriggerPercent = &newTrigger
	changed.RecoveryPercent = &newRecovery
	r, e = f.s.Save(ctx, r.ID, "current_user", changed, f.at.Add(60*time.Second))
	if e != nil {
		t.Fatal(e)
	}
	f.backend.value = 90
	for _, v := range []int{60, 75, 90, 105} {
		f.evaluate(t, v)
	}
	if len(f.active(t)) != 1 {
		t.Fatal("edited recovery threshold resolved original incident")
	}
	f.backend.changeAt = f.at.Add(120 * time.Second)
	f.backend.changeValue = 70
	for _, v := range []int{120, 135, 150} {
		f.evaluate(t, v)
	}
	if len(f.active(t)) != 0 {
		t.Fatal("original recovery condition not applied")
	}
	var events []models.PlatformAlertEvent
	if e = f.db.Where("incident_id=?", incident.ID).Order("occurred_at").Find(&events).Error; e != nil || len(events) != 2 {
		t.Fatal("lifecycle evidence", events, e)
	}
	if events[0].ResourceEvidence.Policy.RecoveryPercent != 75 || events[1].ResourceEvidence.Policy.RecoveryPercent != 75 || events[1].ResourceEvidence.RuleVersion != incident.ResourceEvidence.RuleVersion || events[0].ResourceEvidence.RuleName != f.input.Name || events[1].ResourceEvidence.RuleName != f.input.Name {
		t.Fatal("mutable policy evidence")
	}
	var deliveries []models.PlatformAlertDelivery
	if e = f.db.Where("event_id IN ?", []string{events[0].ID, events[1].ID}).Find(&deliveries).Error; e != nil || len(deliveries) != 2 {
		t.Fatal("atomic resource outbox", len(deliveries), e)
	}
	for _, d := range deliveries {
		var payload map[string]any
		if json.Unmarshal([]byte(d.Payload), &payload) != nil || payload["schema"] != "addp.platform-resource-alert/v1" || payload["evidence"] == nil || d.DestinationID != destination.ID || strings.Contains(d.Payload, "127.0.0.1") {
			t.Fatal("resource evidence routing", d.Payload)
		}
	}
	if e = f.s.Delete(ctx, r.ID, r.Version); e != nil {
		t.Fatal(e)
	}
	var count int64
	f.db.Model(&models.PlatformAlertEvent{}).Where("incident_id=?", incident.ID).Count(&count)
	if count != 2 {
		t.Fatal("deleting rule erased history")
	}
	list, err := f.n.Incidents(ctx, PlatformAlertFilter{Page: 1, PageSize: 20, SubjectID: incident.SubjectID, Search: f.input.Name}, "")
	if err != nil || list.Total != 1 || list.Data[0].Name != f.input.Name {
		t.Fatal("deleted rule erased historical name", list, err)
	}
	list, err = f.n.Incidents(ctx, PlatformAlertFilter{Page: 1, PageSize: 20, SubjectID: incident.SubjectID, Search: changed.Name}, "")
	if err != nil || list.Total != 0 {
		t.Fatal("current rule name rewrote history", list, err)
	}

}
func (f *resourceRuleFixture) targetURL() string { return f.logRetryFixture.target.URL }

func TestIntegrationPostgresResourceRuleRejectsLateTargetAndRuleChanges(t *testing.T) {
	f := newResourceRuleFixture(t)
	r := f.enable(t, f.create(t))
	for _, v := range []int{0, 15, 30} {
		f.evaluate(t, v)
	}
	f.backend.hook = func() {
		if e := f.db.Model(&models.MonitoringTarget{}).Where("id=?", f.target.ID).Updates(map[string]any{"enabled": false, "version": 2}).Error; e != nil {
			t.Fatal(e)
		}
	}
	f.evaluate(t, 45)
	if len(f.active(t)) != 0 {
		t.Fatal("late disabled target opened event")
	}
	f.backend.hook = nil
	if e := f.db.Model(&models.MonitoringTarget{}).Where("id=?", f.target.ID).Updates(map[string]any{"enabled": true, "version": 3}).Error; e != nil {
		t.Fatal(e)
	}
	for _, v := range []int{60, 75, 90} {
		f.evaluate(t, v)
	}
	f.backend.hook = func() {
		if e := f.db.Model(&models.ResourceAlertRule{}).Where("id=?", r.ID).UpdateColumns(map[string]any{"enabled": false, "version": r.Version + 1}).Error; e != nil {
			t.Fatal(e)
		}
	}
	f.evaluate(t, 105)
	if len(f.active(t)) != 0 {
		t.Fatal("late disabled rule opened event")
	}
}

func TestIntegrationPostgresResourceRuleBudgetAndRebootBoundary(t *testing.T) {
	f := newResourceRuleFixture(t)
	r := f.enable(t, f.create(t))
	f.o.policies = &resourcePolicies{row: models.ResourceQueryPolicy{Budget: resourcequery.DefaultBudget()}}
	policies := f.o.policies.(*resourcePolicies)
	policies.row.MaxPointsPerSeries = 10
	f.evaluate(t, 0)
	if f.backend.calls != 0 {
		t.Fatal("coarser budget queried backend")
	}
	var saved models.ResourceAlertRule
	f.db.First(&saved, "id=?", r.ID)
	if saved.LastError != "query_budget" || saved.WindowSince != nil {
		t.Fatal("budget retained window", saved)
	}
	policies.row.Budget = resourcequery.DefaultBudget()
	for _, v := range []int{15, 30, 45, 60} {
		f.evaluate(t, v)
	}
	if len(f.active(t)) != 1 {
		t.Fatal("valid budget did not resume")
	}
	// Restarting the evaluator requires an entirely new witness interval.
	restarted := NewResourceAlertRuleService(f.db, f.o, f.identities, f.n)
	restarted.startedAt = f.at.Add(75 * time.Second)
	f.backend.value = 0
	if err := restarted.Evaluate(context.Background(), f.at.Add(75*time.Second)); err != nil {
		t.Fatal(err)
	}
	if len(f.active(t)) != 1 {
		t.Fatal("process restart recovered from pre-start history")
	}
}

func TestIntegrationPostgresResourceRulePolicyCASDuringQuery(t *testing.T) {
	f := newResourceRuleFixture(t)
	r := f.enable(t, f.create(t))
	for _, v := range []int{0, 15, 30} {
		f.evaluate(t, v)
	}
	repositoryPolicy := repository.NewResourceQueryPolicyRepository(f.db)
	before, err := repositoryPolicy.Get(context.Background())
	if err != nil || before.Version != 0 {
		t.Fatal("fixture policy must be absent", before.Version, err)
	}
	t.Cleanup(func() {
		if e := f.db.Where("id=?", 1).Delete(&models.ResourceQueryPolicy{}).Error; e != nil {
			t.Error(e)
		}
	})
	f.backend.hook = func() {
		budget := resourcequery.DefaultBudget()
		budget.MaxPointsPerSeries = 10
		if _, e := repositoryPolicy.Save(context.Background(), models.ResourceQueryPolicy{Budget: budget}, 0); e != nil {
			t.Fatal(e)
		}
	}
	f.evaluate(t, 45)
	if len(f.active(t)) != 0 {
		t.Fatal("query policy changed but old evidence opened an incident")
	}
	var state models.ResourceAlertRule
	if e := f.db.First(&state, "id=?", r.ID).Error; e != nil || state.LastError != "source_changed" || state.WindowSince != nil || state.Version != r.Version {
		t.Fatal("hot policy preserved old window", state, e)
	}
	f.backend.hook = nil
	calls := f.backend.calls
	f.evaluate(t, 60)
	if f.backend.calls != calls {
		t.Fatal("hot lower budget reached the backend")
	}
}

func TestIntegrationPostgresResourceRuleUnknownBatchHonorsConfigCAS(t *testing.T) {
	f := newResourceRuleFixture(t)
	r := f.enable(t, f.create(t))
	ctx := context.Background()
	off := false
	input := f.input
	input.Version = r.Version
	input.Enabled = &off
	saved, e := f.s.Save(ctx, r.ID, "current_user", input, f.at.Add(15*time.Second))
	if e != nil {
		t.Fatal(e)
	}
	if e = f.s.resetResourceRules(ctx, []models.ResourceAlertRule{r}, f.at.Add(30*time.Second), PlatformAlertUnknown, "query_unavailable"); e != nil {
		t.Fatal(e)
	}
	var after models.ResourceAlertRule
	if e = f.db.First(&after, "id=?", r.ID).Error; e != nil || after.Version != saved.Version || after.LastVerdict != "paused" || after.LastEvaluatedAt != nil {
		t.Fatal("late batch overwrote edited rule", after, e)
	}
}

func TestIntegrationPostgresResourceRulesDoNotAdmitProcessEndpoints(t *testing.T) {
	f := newResourceRuleFixture(t)
	f.enable(t, f.create(t))
	started := time.Now().UTC().Add(-time.Hour)
	f.identities.processFacts = []commonmodels.ObservabilityModuleIdentity{{ProcessStartedAt: &started, ModuleName: "unavailable-process", InstanceID: "unavailable", Role: "backend", NodeID: f.target.NodeID, LeaseExpiresAt: time.Now().Add(time.Hour), ProcessMetrics: &commonmodels.ProcessMetricsDeclaration{SchemaVersion: commonmodels.ProcessMetricsSchema, Endpoint: "https://unreachable.invalid:9999/metrics"}}}
	for _, v := range []int{0, 15, 30, 45} {
		f.evaluate(t, v)
	}
	if len(f.active(t)) != 1 || f.backend.calls != 4 {
		t.Fatal("unrelated process source blocked host alerts")
	}
}

func TestIntegrationPostgresResourceRuleMissingHistoricalNameStaysMissing(t *testing.T) {
	f := newResourceRuleFixture(t)
	r := f.enable(t, f.create(t))
	for _, sec := range []int{0, 15, 30, 45} {
		f.evaluate(t, sec)
	}
	row := f.active(t)[0]
	// This one disposable incident represents a record made before name snapshots.
	if err := f.db.Model(&models.PlatformAlertIncident{}).Where("id=?", row.ID).UpdateColumn("resource_evidence", gorm.Expr("resource_evidence - 'rule_name'")).Error; err != nil {
		t.Fatal(err)
	}
	in := f.input
	in.Name = "new current name"
	in.Version = r.Version
	on := true
	in.Enabled = &on
	var err error
	r, err = f.s.Save(context.Background(), r.ID, "current_user", in, f.at.Add(60*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	for _, sec := range []int{60, 75, 90, 105} {
		f.evaluate(t, sec)
	}
	for _, search := range []string{in.Name, f.input.Name} {
		list, e := f.n.Incidents(context.Background(), PlatformAlertFilter{Page: 1, PageSize: 20, SubjectID: row.SubjectID, Search: search}, "")
		if e != nil || list.Total != 0 {
			t.Fatal("live rule substituted for missing history", list, e)
		}
	}
	f.backend.changeAt = f.at.Add(120 * time.Second)
	f.backend.changeValue = 70
	for _, sec := range []int{120, 135, 150} {
		f.evaluate(t, sec)
	}
	var events []models.PlatformAlertEvent
	if err = f.db.Where("incident_id=? AND type=?", row.ID, "resolved").Find(&events).Error; err != nil || len(events) != 1 || events[0].ResourceEvidence.RuleName != "" {
		t.Fatal("recovery fabricated old name", events, err)
	}
	var saved models.PlatformAlertIncident
	if err = f.db.First(&saved, row.ID).Error; err != nil || platformAlertName(saved, "en") != "" {
		t.Fatal("history filled", err)
	}
}
