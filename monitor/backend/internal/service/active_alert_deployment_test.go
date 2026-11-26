package service

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/addp/common/infra/bootstrap"
	"github.com/addp/monitor/internal/config"
	"github.com/addp/monitor/internal/metricsdiscovery"
	"github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/repository"
	"github.com/addp/monitor/internal/resourcequery"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestInstalledActiveAlertUniqueness(t *testing.T) {
	if os.Getenv("ADDP_TEST_MONITOR_DEPLOYMENT") != "1" {
		t.Skip("requires the exclusive deployment gate")
	}
	db, err := bootstrap.OpenPlatformRuntime(config.PlatformDatabase(), "monitor", &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Run("platform notification and search", func(t *testing.T) { checkPlatformAlertNotificationAndSearch(t, db) })
	t.Run("filesystem rule lookup uses exact mount dimensions", func(t *testing.T) {
		node := uuid.NewString()
		dimensions := resourcequery.Dimensions{"device": "/dev/sdb", "mountpoint": "/data/资料", "fstype": "ext4"}
		rule := &models.ResourceAlertRule{
			ID: uuid.NewString(), Version: 1, Name: "filesystem fixture", NodeID: node, MetricKey: models.FilesystemCapacityMetric,
			Dimensions: dimensions, MountKey: models.ResourceMountKey(dimensions),
			ResourcePressurePolicy: defaultResourcePressurePolicy(models.FilesystemCapacityMetric),
		}
		if err := db.Create(rule).Error; err != nil {
			t.Fatal(err)
		}
		svc := &ResourceAlertRuleService{db: db}
		filter := ResourceAlertRuleFilter{NodeID: node, MetricKey: models.FilesystemCapacityMetric, Dimensions: dimensions}
		page, err := svc.List(t.Context(), 1, 10, filter)
		if err != nil || page.Total != 1 || len(page.Data) != 1 || page.Data[0].ID != rule.ID {
			t.Fatalf("exact mount lookup: count=%d err=%v", page.Total, err)
		}
		filter.Dimensions = resourcequery.Dimensions{"device": "/dev/sdb", "mountpoint": "/data/other", "fstype": "ext4"}
		page, err = svc.List(t.Context(), 1, 10, filter)
		if err != nil || page.Total != 0 || len(page.Data) != 0 {
			t.Fatalf("different mount lookup: count=%d err=%v", page.Total, err)
		}
	})
	t.Run("concurrent first evaluations", func(t *testing.T) { checkConcurrentPlatformAlertEvaluations(t, db) })
	t.Run("configuration compare and swap", func(t *testing.T) {
		tx := db.Begin()
		if tx.Error != nil {
			t.Fatal(tx.Error)
		}
		t.Cleanup(func() { tx.Rollback() })
		ctx := t.Context()
		policies := repository.NewResourceQueryPolicyRepository(tx)
		current, err := policies.Get(ctx)
		if err != nil {
			t.Fatal(err)
		}
		next, err := policies.Save(ctx, current, current.Version)
		if err != nil || next.Version != current.Version+1 {
			t.Fatalf("policy version=%d err=%v", next.Version, err)
		}
		if _, err = policies.Save(ctx, current, current.Version); !errors.Is(err, resourcequery.ErrConflict) {
			t.Fatalf("stale policy: %v", err)
		}
		scope := metricsdiscovery.AutomaticScope{ID: "process:monitor:backend", Kind: "process", ModuleName: "monitor", Role: "backend"}
		scopes := repository.NewAutomaticCollectionRepository(tx)
		paused, err := scopes.Save(ctx, scope, false, 1)
		if err != nil || paused.Enabled || paused.Version != 2 {
			t.Fatalf("collection pause=%+v err=%v", paused, err)
		}
		if _, err = scopes.Save(ctx, scope, true, 1); !errors.Is(err, repository.ErrTargetConflict) {
			t.Fatalf("stale collection scope: %v", err)
		}
		for _, key := range []string{"monitor.resource-alert-rules", "monitor.http-alert-rules", "monitor.process-alert-rules", "monitor.service-availability-rules"} {
			if err := repository.LockAlertRuleSet(tx, key); err != nil {
				t.Fatalf("%s: %v", key, err)
			}
		}
	})
	key := uuid.NewString()
	now := time.Now().UTC()
	tests := []struct {
		name   string
		table  interface{}
		match  string
		record func(string, string) interface{}
	}{
		{"tenant", &models.AlertIncident{}, "fingerprint = ?", func(id, status string) interface{} {
			return &models.AlertIncident{TenantID: 1, Fingerprint: id, Status: status, Severity: "warning", OpenedAt: now, LastObservedAt: now}
		}},
		{"platform", &models.PlatformAlertIncident{}, "subject_id = ?", func(id, status string) interface{} {
			return &models.PlatformAlertIncident{Category: "resource", SubjectKind: "host_node", SubjectID: id, Signal: "test", Severity: "warning", Status: status, OpenedAt: now, LastObservedAt: now}
		}},
	}
	for _, probe := range tests {
		t.Run(probe.name, func(t *testing.T) {
			create := func(id, status string) error { return db.Create(probe.record(id, status)).Error }
			for _, status := range []string{"open", "acknowledged"} {
				if err := create(key, status); err != nil {
					t.Fatal(err)
				}
				for _, duplicate := range []string{"open", "acknowledged"} {
					if err := create(key, duplicate); !errors.Is(err, gorm.ErrDuplicatedKey) {
						t.Fatalf("%s alongside %s: expected unique violation, got %v", duplicate, status, err)
					}
				}
				changed := db.Model(probe.table).Where(probe.match, key).Where("status = ?", status).Update("status", "resolved")
				if changed.Error != nil || changed.RowsAffected != 1 {
					t.Fatalf("resolve: rows=%d err=%v", changed.RowsAffected, changed.Error)
				}
			}
			if err := create(key, "open"); err != nil {
				t.Fatal(err)
			}
			if err := create(uuid.NewString(), "open"); err != nil {
				t.Fatal(err)
			}
			for status, expected := range map[string]int64{"open": 1, "resolved": 2} {
				var count int64
				if err := db.Model(probe.table).Where(probe.match, key).Where("status = ?", status).Count(&count).Error; err != nil || count != expected {
					t.Fatalf("%s: rows=%d want=%d err=%v", status, count, expected, err)
				}
			}
		})
	}
}

func checkPlatformAlertNotificationAndSearch(t *testing.T, db *gorm.DB) {
	t.Helper()
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	t.Cleanup(func() { tx.Rollback() })
	create := func(v any) {
		t.Helper()
		if err := tx.Create(v).Error; err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	id := uuid.NewString()
	incident := models.PlatformAlertIncident{Category: models.PlatformAlertCategoryLog, SubjectKind: models.PlatformAlertSubjectLogObserver, SubjectID: id, Signal: "delivery_probe", Severity: "warning", Status: "open", OpenedAt: now, LastObservedAt: now}
	create(&incident)
	event := models.PlatformAlertEvent{ID: uuid.NewString(), IncidentID: incident.ID, Type: "opened", Severity: "warning", OccurredAt: now}
	create(&event)
	var included uint
	for _, dest := range []struct {
		category string
		enabled  bool
	}{{incident.Category, true}, {models.PlatformAlertCategoryResource, true}, {incident.Category, false}} {
		d := models.PlatformAlertDestination{Name: uuid.NewString(), Channel: "email", Recipients: models.StringList{"platform@example.invalid"}, Categories: models.StringList{dest.category}, EventTypes: models.StringList{"opened"}, Enabled: dest.enabled}
		create(&d)
		if dest.category == incident.Category && dest.enabled {
			included = d.ID
		}
	}
	svc := &PlatformAlertService{db: tx}
	if err := svc.RecordTx(tx, event, incident, now); err != nil {
		t.Fatal(err)
	}
	var deliveries []models.PlatformAlertDelivery
	if err := tx.Where("event_id=?", event.ID).Find(&deliveries).Error; err != nil || len(deliveries) != 1 || deliveries[0].DestinationID != included || deliveries[0].Status != "pending" {
		t.Fatalf("deliveries=%v error=%v", deliveries, err)
	}
	logName := platformLogAlertName(incident.Signal, "en")
	if logName == "" {
		t.Fatal("missing log signal name")
	}
	filter := PlatformAlertFilter{Page: 1, PageSize: 5, Language: "en", SubjectID: id, Search: logName}
	page, err := svc.Incidents(t.Context(), filter, "")
	if err != nil || page.Total != 1 || len(page.Data) != 1 || page.Data[0].ID != incident.ID {
		t.Fatalf("log search=%+v error=%v", page, err)
	}
	resource := models.PlatformAlertIncident{Category: models.PlatformAlertCategoryResource, SubjectKind: models.PlatformAlertSubjectHostNode, SubjectID: id, Signal: "node.cpu.busy_percent", Severity: "warning", Status: "open", OpenedAt: now, LastObservedAt: now, ResourceEvidence: &models.ResourceAlertEvidence{RuleName: "Infra_%Rule"}}
	create(&resource)
	filter.Search = "RA_%r"
	page, err = svc.Incidents(t.Context(), filter, "")
	if err != nil || page.Total != 1 || len(page.Data) != 1 || page.Data[0].ID != resource.ID {
		t.Fatalf("literal rule search=%+v error=%v", page, err)
	}
	filter.Search = "ra__r"
	page, err = svc.Incidents(t.Context(), filter, "")
	if err != nil || page.Total != 0 {
		t.Fatalf("wildcard matched a literal: %+v error=%v", page, err)
	}
}

func checkConcurrentPlatformAlertEvaluations(t *testing.T, db *gorm.DB) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	ev := PlatformAlertEvaluation{Category: models.PlatformAlertCategoryLog, SubjectKind: models.PlatformAlertSubjectLogObserver,
		SubjectID: uuid.NewString(), Signal: "delivery_probe", Severity: "warning", Verdict: PlatformAlertRisk}
	rollback := errors.New("rollback first evaluation")
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := reconcilePlatformAlert(tx, nil, ev, now); err != nil {
			return err
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	// Both transactions are open on different pool connections before either observes the identity.
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	ready := make(chan struct{}, 2)
	start := make(chan struct{})
	done := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			done <- db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				ready <- struct{}{}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-start:
				}
				return reconcilePlatformAlert(tx, nil, ev, now)
			})
		}()
	}
	for i := 0; i < 2; i++ {
		select {
		case <-ready:
		case err := <-done:
			t.Fatalf("begin: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	close(start)
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	var incidents []models.PlatformAlertIncident
	if err := db.Where("subject_id=?", ev.SubjectID).Find(&incidents).Error; err != nil || len(incidents) != 1 || incidents[0].Status != "open" || incidents[0].Version != 2 {
		t.Fatalf("incidents=%v error=%v", incidents, err)
	}
	var events []models.PlatformAlertEvent
	if err := db.Where("incident_id=?", incidents[0].ID).Find(&events).Error; err != nil || len(events) != 1 || events[0].Type != "opened" {
		t.Fatalf("events=%v error=%v", events, err)
	}
}
