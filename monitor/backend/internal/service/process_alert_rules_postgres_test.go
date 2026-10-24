package service

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/resourcequery"
	"strings"
	"testing"
	"time"
)

type processRuleBackend struct {
	value float64
	state string
	hook  func()
}

func (b *processRuleBackend) ProcessSummaries(_ context.Context, scopes []resourcequery.ProcessScope, at time.Time, _ resourcequery.Budget) (map[uint]resourcequery.Summary, error) {
	state := b.state
	if state == "" {
		state = "collecting"
	}
	return map[uint]resourcequery.Summary{scopes[0].ID: {Collection: resourcequery.Collection{State: state, SampledAt: &at}}}, nil
}
func (b *processRuleBackend) ProcessTrend(_ context.Context, p resourcequery.Plan, _ resourcequery.ProcessScope, _ resourcequery.Budget) ([]resourcequery.Series, error) {
	if b.hook != nil {
		b.hook()
	}
	return []resourcequery.Series{processThresholdSeries(p.End, b.value, "process.cpu.core_equivalents"), processThresholdSeries(p.End, b.value, "process.memory.resident_bytes")}, nil
}
func TestIntegrationPostgresProcessRulesLifecycleAndIdentity(t *testing.T) {
	for _, key := range []string{"process.cpu.core_equivalents", "process.memory.resident_bytes"} {
		t.Run(key, func(t *testing.T) {
			f := newHTTPRuleFixture(t)
			ctx := context.Background()
			b := &processRuleBackend{value: 3}
			f.s.observations.process.Backend = b
			s := NewProcessAlertRuleService(f.db, f.s.observations, f.identities, f.n)
			s.startedAt = f.at
			t.Cleanup(func() {
				for _, q := range []string{
					`DELETE FROM monitor.platform_alert_deliveries WHERE event_id IN (SELECT id FROM monitor.platform_alert_events WHERE incident_id IN (SELECT id FROM monitor.platform_alert_incidents WHERE category='process_resource' AND signal IN (SELECT id FROM monitor.process_alert_rules WHERE module_name=?)))`,
					`DELETE FROM monitor.platform_alert_events WHERE incident_id IN (SELECT id FROM monitor.platform_alert_incidents WHERE category='process_resource' AND signal IN (SELECT id FROM monitor.process_alert_rules WHERE module_name=?))`,
					`DELETE FROM monitor.platform_alert_incidents WHERE category='process_resource' AND signal IN (SELECT id FROM monitor.process_alert_rules WHERE module_name=?)`,
					`DELETE FROM monitor.process_alert_rules WHERE module_name=?`,
				} {
					if e := f.db.Exec(q, f.module).Error; e != nil {
						t.Error(e)
					}
				}
				var n int64
				if e := f.db.Model(&models.ProcessAlertRule{}).Where("module_name=?", f.module).Count(&n).Error; e != nil || n != 0 {
					t.Error("fixture residue", n, e)
				}
			})
			trigger, recovery, seconds, on, off := 2., 1., 15, true, false
			input := ProcessAlertRuleInput{Name: "process pressure", InstanceID: 1, MetricKey: key, TriggerValue: &trigger, RecoveryValue: &recovery, TriggerSeconds: &seconds, RecoverySeconds: &seconds, Enabled: &off}
			r, e := s.Save(ctx, "", "current_user", input, f.at)
			if e != nil || r.Enabled {
				t.Fatal(r, e)
			}
			if _, e = s.Save(ctx, "", "current_user", input, f.at); !errors.Is(e, ErrLogConflict) {
				t.Fatal("duplicate", e)
			}
			input.Version = r.Version
			input.Enabled = &on
			r, e = s.Save(ctx, r.ID, "current_user", input, f.at)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = s.Save(ctx, r.ID, "current_user", input, f.at); !errors.Is(e, ErrLogConflict) {
				t.Fatal("CAS", e)
			}
			destInput := PlatformAlertDestinationInput{Name: f.module + "-process", Channel: "webhook", URL: "http://127.0.0.1:18080/process", Categories: []string{"process_resource"}, EventTypes: []string{"opened", "resolved"}}
			dest, e := f.n.Save(ctx, 0, destInput)
			if e != nil {
				t.Fatal(e)
			}
			t.Cleanup(func() {
				if e := f.db.Delete(&models.PlatformAlertDestination{}, dest.ID).Error; e != nil {
					t.Error(e)
				}
			})
			dest, e = f.n.SetSecret(ctx, dest.ID, dest.Version, "process-fixture-secret")
			if e != nil {
				t.Fatal(e)
			}
			destInput.Version = dest.Version
			destInput.Enabled = true
			dest, e = f.n.Save(ctx, dest.ID, destInput)
			if e != nil {
				t.Fatal(e)
			}

			eval := func(sec int) {
				t.Helper()
				if e := s.Evaluate(ctx, f.at.Add(time.Duration(sec)*time.Second)); e != nil {
					t.Fatal(e)
				}
			}
			active := func() []models.PlatformAlertIncident {
				t.Helper()
				var rows []models.PlatformAlertIncident
				if e := f.db.Where("category=? AND signal=? AND status IN ?", "process_resource", r.ID, []string{"open", "acknowledged"}).Find(&rows).Error; e != nil {
					t.Fatal(e)
				}
				return rows
			}
			for sec := 0; sec <= 90; sec += 15 {
				eval(sec)
			}
			rows := active()
			if len(rows) != 1 || rows[0].ProcessEvidence == nil || rows[0].ProcessEvidence.RuleName != input.Name {
				t.Fatal("not opened", rows)
			}
			incident := rows[0]
			if e := s.Delete(ctx, r.ID, r.Version); !errors.Is(e, ErrLogConflict) {
				t.Fatal("active deletion", e)
			}
			// Missing/exit cannot resolve. Same identity with a changed start is a different run.
			saved := f.identities.processFacts
			f.identities.processFacts = nil
			eval(105)
			if len(active()) != 1 {
				t.Fatal("exit resolved")
			}
			f.identities.processFacts = saved
			oldStart := f.identities.processFacts[0].ProcessStartedAt
			different := oldStart.Add(time.Second)
			f.identities.processFacts[0].ProcessStartedAt = &different
			eval(120)
			if len(active()) != 1 {
				t.Fatal("new start resolved")
			}
			f.identities.processFacts[0].ProcessStartedAt = oldStart
			b.state = "failed"
			eval(135)
			b.state = ""
			if len(active()) != 1 {
				t.Fatal("no data resolved")
			}
			// Updated rules cannot rewrite the opening recovery policy.
			trigger, recovery = 9, 7
			input.Version = r.Version
			input.Name = "renamed process rule"
			r, e = s.Save(ctx, r.ID, "current_user", input, f.at.Add(150*time.Second))
			if e != nil {
				t.Fatal(e)
			}
			for sec := 150; sec <= 240; sec += 15 {
				eval(sec)
			}
			if len(active()) != 1 {
				t.Fatal("new threshold resolved old alert")
			}
			b.value = 0
			eval(255)
			eval(270)
			if len(active()) != 0 {
				t.Fatal("valid zero did not recover")
			}
			var events []models.PlatformAlertEvent
			if e = f.db.Where("incident_id=?", incident.ID).Order("occurred_at").Find(&events).Error; e != nil || len(events) != 2 || events[0].Type != "opened" || events[1].Type != "resolved" {
				t.Fatal(events, e)
			}
			for _, ev := range events {
				if ev.ProcessEvidence == nil || ev.ProcessEvidence.Policy.RecoveryValue != 1 || ev.ProcessEvidence.RuleName != "process pressure" {
					t.Fatal("opening evidence mutated", ev)
				}
			}
			var deliveries []models.PlatformAlertDelivery
			if e = f.db.Where("event_id IN ?", []string{events[0].ID, events[1].ID}).Find(&deliveries).Error; e != nil || len(deliveries) != 2 {
				t.Fatal("outbox", deliveries, e)
			}
			for _, d := range deliveries {
				var p map[string]any
				if d.DestinationID != dest.ID || json.Unmarshal([]byte(d.Payload), &p) != nil || p["schema"] != "addp.platform-process-resource-alert/v1" || strings.Contains(d.Payload, "127.0.0.1") {
					t.Fatal("unsafe payload")
				}
				if _, e := wecomPlatformAlertPayload(d.Payload, d.ID); e != nil {
					t.Fatal(e)
				}
			}
			// A disable that races with a completed upstream query discards the old evaluation.
			b.value = 10
			b.hook = func() {
				b.hook = nil
				input.Version = r.Version
				input.Enabled = &off
				var err error
				r, err = s.Save(ctx, r.ID, "current_user", input, f.at.Add(285*time.Second))
				if err != nil {
					t.Fatal(err)
				}
			}
			eval(285)
			if len(active()) != 0 {
				t.Fatal("late evaluation reopened")
			}
		})
	}
}
