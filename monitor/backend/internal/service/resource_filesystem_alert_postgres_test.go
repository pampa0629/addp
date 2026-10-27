package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/resourcequery"
	"github.com/google/uuid"
)

func newFilesystemRuleFixture(t *testing.T) *resourceRuleFixture {
	f := newResourceRuleFixture(t)
	f.input.MetricKey = models.FilesystemCapacityMetric
	f.input.Dimensions = testFilesystemMount.Copy()
	*f.input.TriggerPercent = 85
	*f.input.RecoveryPercent = 80
	f.o.now = func() time.Time { return f.at }
	stamp := f.at
	f.backend.evidence = &resourcequery.Collection{State: "collecting", Filesystem: "available", Network: "unknown", SampledAt: &stamp}
	f.backend.filesystems = map[string]*filesystemRuleSample{models.ResourceMountKey(f.input.Dimensions): {percent: 90, total: 1024 * 1024 * 1024}}
	return f
}
func TestIntegrationPostgresFilesystemRuleIsolationMissingResizeAndRetirement(t *testing.T) {
	f := newFilesystemRuleFixture(t)
	ctx := context.Background()
	var err error
	f.logRetryFixture.target, err = f.n.Save(ctx, f.logRetryFixture.target.ID, PlatformAlertDestinationInput{Version: f.logRetryFixture.target.Version, Name: f.logRetryFixture.target.Name, Channel: "webhook", URL: f.logRetryFixture.target.URL, Enabled: true, Categories: []string{"resource"}, EventTypes: []string{"opened", "resolved"}})
	if err != nil {
		t.Fatal(err)
	}
	first := f.create(t)
	if first.Enabled || first.MountKey == "" || len(first.Dimensions) != 3 {
		t.Fatal("noncanonical creation", first)
	}
	if _, err := f.s.Save(ctx, "", "current_user", f.input, f.at); !errors.Is(err, ErrLogConflict) {
		t.Fatal("duplicate mount accepted", err)
	}
	first = f.enable(t, first)
	f.input.Dimensions = resourcequery.Dimensions{"device": "/dev/data", "mountpoint": "/data", "fstype": "xfs"}
	f.backend.filesystems[models.ResourceMountKey(f.input.Dimensions)] = &filesystemRuleSample{percent: 92, total: 1024 * 1024 * 1024}
	second := f.enable(t, f.create(t))
	for _, seconds := range []int{0, 15, 30, 45} {
		f.evaluate(t, seconds)
	}
	active := f.active(t)
	if len(active) != 2 || active[0].Signal == active[1].Signal {
		t.Fatalf("mounts collapsed: %+v", active)
	}
	for _, r := range active {
		if !validFilesystemAlertEvidence(r.Signal, r.ResourceEvidence) {
			t.Fatal("missing frozen capacity", r)
		}
	}
	var deliveries []models.PlatformAlertDelivery
	if err := f.db.Where("event_id IN (SELECT e.id FROM monitor.platform_alert_events e JOIN monitor.platform_alert_incidents i ON i.id=e.incident_id WHERE i.subject_id=?)", first.NodeID).Find(&deliveries).Error; err != nil || len(deliveries) != 2 {
		t.Fatal("filesystem outbox missing", len(deliveries), err)
	}
	for _, delivery := range deliveries {
		if _, err := wecomPlatformAlertPayload(delivery.Payload, delivery.ID); err != nil {
			t.Fatal("filesystem notification unreadable", err)
		}
	}
	// Removing one mount never recovers it or touches the other mount.
	firstSample := f.backend.filesystems[first.MountKey]
	firstSample.missing = true
	f.evaluate(t, 60)
	if len(f.active(t)) != 2 {
		t.Fatal("disappearance recovered an incident")
	}
	// Same exact mount returns after resize: wait for new full recovery window.
	firstSample.missing = false
	firstSample.changedAt = f.at.Add(65 * time.Second)
	firstSample.nextPercent = 70
	firstSample.nextTotal = 2 * firstSample.total
	for _, seconds := range []int{75, 90, 105} {
		f.evaluate(t, seconds)
		if len(f.active(t)) != 2 {
			t.Fatal("incomplete resized window recovered")
		}
	}
	f.evaluate(t, 120)
	active = f.active(t)
	if len(active) != 1 || active[0].Signal != models.ResourceAlertSignal(second.MetricKey, second.Dimensions) {
		t.Fatal("wrong mount recovered", active)
	}
	// A retired mount keeps its open incident and stopped rule, even without a source.
	f.input.Version = second.Version
	off := false
	f.input.Enabled = &off
	f.backend.filesystems[second.MountKey].missing = true
	stopped, err := f.s.Save(ctx, second.ID, "current_user", f.input, f.at.Add(121*time.Second))
	if err != nil || stopped.Enabled {
		t.Fatal("missing mount could not be stopped", err)
	}
	f.evaluate(t, 135)
	if len(f.active(t)) != 1 {
		t.Fatal("retirement fabricated recovery")
	}
	if err = f.s.Delete(ctx, stopped.ID, stopped.Version); !errors.Is(err, ErrLogConflict) {
		t.Fatal("retired active rule was deleted", err)
	}
	on := true
	f.input.Version = stopped.Version
	f.input.Enabled = &on
	if _, err = f.s.Save(ctx, stopped.ID, "current_user", f.input, f.at.Add(136*time.Second)); !errors.Is(err, ErrLogConflict) {
		t.Fatal("missing mount reenabled", err)
	}
	f.input.Dimensions = testFilesystemMount.Copy()
	if _, err = f.s.Save(ctx, stopped.ID, "current_user", f.input, f.at.Add(136*time.Second)); !errors.Is(err, ErrLogConflict) {
		t.Fatal("mount identity edited", err)
	}
}
func TestIntegrationPostgresFilesystemRuleStoreDropsOldUniqueIndex(t *testing.T) {
	f := newFilesystemRuleFixture(t)
	// Represent an existing scalar rule and the old two-column uniqueness.
	f.input.MetricKey = "node.memory.used_percent"
	f.input.Dimensions = resourcequery.Dimensions{}
	original := f.create(t)
	if err := f.db.Exec("CREATE UNIQUE INDEX uq_resource_rule_subject ON monitor.resource_alert_rules(node_id,metric_key)").Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := f.db.Exec("DROP INDEX IF EXISTS monitor.uq_resource_rule_subject").Error; err != nil {
			t.Error(err)
		}
	})
	// The sole store path must preserve that rule and remove the old constraint.
	if err := EnsureMonitorStore(f.db); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := f.db.Raw("SELECT count(*) FROM pg_indexes WHERE schemaname='monitor' AND indexname='uq_resource_rule_subject'").Scan(&count).Error; err != nil || count != 0 {
		t.Fatal("old unique path retained", count, err)
	}
	var preserved models.ResourceAlertRule
	if err := f.db.First(&preserved, "id=?", original.ID).Error; err != nil || preserved.Version != original.Version || preserved.ResourcePressurePolicy != original.ResourcePressurePolicy || len(preserved.Dimensions) != 0 || preserved.MountKey != "" {
		t.Fatal("existing scalar rule changed", preserved, err)
	}
	f.input.MetricKey = models.FilesystemCapacityMetric
	f.input.Dimensions = testFilesystemMount.Copy()
	f.create(t)
	f.input.Dimensions = resourcequery.Dimensions{"device": "/dev/other", "mountpoint": "/data", "fstype": "ext4"}
	f.backend.filesystems[models.ResourceMountKey(f.input.Dimensions)] = &filesystemRuleSample{percent: 90, total: 1024}
	f.create(t)
}

func TestIntegrationPostgresFilesystemRuleExactFilterBeforePagination(t *testing.T) {
	f := newFilesystemRuleFixture(t)
	wanted := f.create(t)
	rows := []models.ResourceAlertRule{}
	for i := 0; i < 25; i++ {
		r := wanted
		r.ID = uuid.NewString()
		r.Dimensions = wanted.Dimensions.Copy()
		r.Dimensions["device"] = "/dev/other" + r.ID
		r.MountKey = models.ResourceMountKey(r.Dimensions)
		r.CreatedAt = f.at.Add(time.Duration(i+1) * time.Second)
		rows = append(rows, r)
	}
	otherType := wanted
	otherType.ID = uuid.NewString()
	otherType.Dimensions = wanted.Dimensions.Copy()
	otherType.Dimensions["fstype"] = "xfs"
	otherType.MountKey = models.ResourceMountKey(otherType.Dimensions)
	rows = append(rows, otherType)
	if e := f.db.Create(&rows).Error; e != nil {
		t.Fatal(e)
	}
	filter := ResourceAlertRuleFilter{NodeID: wanted.NodeID, MetricKey: models.FilesystemCapacityMetric, Dimensions: wanted.Dimensions.Copy()}
	page, e := f.s.List(context.Background(), 1, 20, filter)
	if e != nil || page.Total != 1 || page.TotalPages != 1 || len(page.Data) != 1 || page.Data[0].ID != wanted.ID || page.Data[0].Enabled {
		t.Fatalf("exact dormant mount: total=%d len=%d err=%v", page.Total, len(page.Data), e)
	}
	page, e = f.s.List(context.Background(), 2, 20, filter)
	if e != nil || page.Total != 1 || len(page.Data) != 0 {
		t.Fatalf("page outside exact scope: %+v %v", page, e)
	}
	filter.Dimensions["mountpoint"] = "/%"
	page, e = f.s.List(context.Background(), 1, 20, filter)
	if e != nil || page.Total != 0 || len(page.Data) != 0 {
		t.Fatalf("missing literal mount widened: %+v %v", page, e)
	}
	filter = ResourceAlertRuleFilter{NodeID: wanted.NodeID}
	page, e = f.s.List(context.Background(), 1, 20, filter)
	if e != nil || page.Total != 27 || len(page.Data) != 20 {
		t.Fatalf("host pagination: total=%d len=%d err=%v", page.Total, len(page.Data), e)
	}
	for _, bad := range []ResourceAlertRuleFilter{{MetricKey: "arbitrary"}, {MetricKey: models.FilesystemCapacityMetric, Dimensions: wanted.Dimensions}, {NodeID: wanted.NodeID, MetricKey: models.FilesystemCapacityMetric, Dimensions: resourcequery.Dimensions{"device": "x"}}} {
		if _, e = f.s.List(context.Background(), 1, 20, bad); !errors.Is(e, ErrLogInvalid) {
			t.Fatalf("invalid filter: %v", e)
		}
	}
}
