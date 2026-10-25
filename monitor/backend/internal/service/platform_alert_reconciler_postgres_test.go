package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/monitor/internal/metricsdiscovery"
	"github.com/addp/monitor/internal/models"
	"github.com/addp/monitor/internal/resourcequery"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestIntegrationPostgresPlatformAlertSourceIsolationAndAtomicity(t *testing.T) {
	t.Setenv("ADDP_OBSERVABILITY_LOGS_ENABLED", "true")
	f := newLogRetryFixture(t, "webhook")
	db, ctx := f.db, context.Background()
	subject := uuid.NewString()
	now := time.Now().UTC().Truncate(time.Microsecond)
	pipeline := NewLogPipelineService(db, subject, emptyLogRegistry{}, f.n)
	if err := pipeline.Initialize(ctx, now); err != nil {
		t.Fatal(err)
	}
	ids := db.Model(&models.PlatformAlertIncident{}).Select("id").Where("subject_id=?", subject)
	eventIDs := db.Model(&models.PlatformAlertEvent{}).Select("id").Where("incident_id IN (?)", ids)
	t.Cleanup(func() {
		for _, entry := range []struct {
			model any
			where string
			value any
		}{
			{&models.PlatformAlertDelivery{}, "event_id IN (?)", eventIDs},
			{&models.PlatformAlertEvent{}, "incident_id IN (?)", ids},
			{&models.PlatformAlertIncident{}, "subject_id=?", subject},
			{&models.LogPipelineNode{}, "node=?", subject},
		} {
			if err := db.Where(entry.where, entry.value).Delete(entry.model).Error; err != nil {
				t.Error(err)
			}
			var count int64
			if err := db.Model(entry.model).Where(entry.where, entry.value).Count(&count).Error; err != nil || count != 0 {
				t.Errorf("platform alert fixture residue=%d err=%v", count, err)
			}
		}
	})
	logEvaluation := PlatformAlertEvaluation{Category: models.PlatformAlertCategoryLog, SubjectKind: models.PlatformAlertSubjectLogObserver, SubjectID: subject, Signal: "delivery_probe", Severity: "warning", Verdict: PlatformAlertRisk}
	evaluate := func(e PlatformAlertEvaluation, n *PlatformAlertService, at time.Time) error {
		return db.Transaction(func(tx *gorm.DB) error { return reconcilePlatformAlert(tx, n, e, at) })
	}
	abort := errors.New("rollback evaluation source state")
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := reconcilePlatformAlert(tx, f.n, logEvaluation, now); err != nil {
			return err
		}
		return abort
	}); !errors.Is(err, abort) {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&models.PlatformAlertIncident{}).Where("subject_id=?", subject).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("incident escaped source transaction rollback", err)
	}
	// Eight independent transactions must produce one opened incident/event/delivery.
	var wg sync.WaitGroup
	errorsCh := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errorsCh <- evaluate(logEvaluation, f.n, now) }()
	}
	wg.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	var logIncident models.PlatformAlertIncident
	if err := db.Where("category=? AND subject_id=?", logEvaluation.Category, subject).Take(&logIncident).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.PlatformAlertEvent{}).Where("incident_id=?", logIncident.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("concurrent opened events=%d error=%v", count, err)
	}
	if err := db.Model(&models.PlatformAlertDelivery{}).Where("event_id IN (?) AND destination_id=?", eventIDs, f.target.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("concurrent opened deliveries=%d error=%v", count, err)
	}
	resource := PlatformAlertEvaluation{Category: models.PlatformAlertCategoryResource, SubjectKind: models.PlatformAlertSubjectHostNode, SubjectID: subject, Signal: logEvaluation.Signal, Severity: "warning", Verdict: PlatformAlertRisk}
	if err := evaluate(resource, nil, now); err != nil {
		t.Fatal("resource subject collided with log observer", err)
	}
	var resourceIncident models.PlatformAlertIncident
	if err := db.Where("category=? AND subject_id=?", resource.Category, subject).Take(&resourceIncident).Error; err != nil {
		t.Fatal(err)
	}
	var resourceEvent models.PlatformAlertEvent
	if err := db.Where("incident_id=?", resourceIncident.ID).Take(&resourceEvent).Error; err != nil {
		t.Fatal(err)
	}
	resourceDelivery := models.PlatformAlertDelivery{ID: uuid.NewString(), EventID: resourceEvent.ID, DestinationID: f.target.ID, Channel: "webhook", Recipients: models.StringList{}, Payload: `{"resource":true}`, Status: "dead", CreatedAt: now}
	if err := db.Create(&resourceDelivery).Error; err != nil {
		t.Fatal(err)
	}
	zero := 0
	if _, err := f.n.Retry(ctx, resourceDelivery.ID, PlatformAlertDeliveryRetryInput{ExpectedManualRetryCount: &zero, DestinationVersion: f.target.Version}, now); !errors.Is(err, ErrLogRetryUnavailable) {
		t.Fatal("unsubscribed resource retry accepted", err)
	}
	deliveries, err := f.n.Deliveries(ctx, 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	foundResource := false
	for _, delivery := range deliveries.Data {
		if delivery.ID == resourceDelivery.ID {
			foundResource = delivery.Category == models.PlatformAlertCategoryResource
		}
	}
	if !foundResource {
		t.Fatal("generic delivery list omitted original resource category")
	}
	for _, verdict := range []PlatformAlertVerdict{PlatformAlertUnknown, PlatformAlertPaused} {
		resource.Verdict = verdict
		if err := evaluate(resource, nil, now.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	var unchanged models.PlatformAlertIncident
	if err := db.First(&unchanged, resourceIncident.ID).Error; err != nil || unchanged.Status != "open" || unchanged.Version != resourceIncident.Version || !unchanged.LastObservedAt.Equal(now) {
		t.Fatal("unknown/paused rewrote active incident", err)
	}
	resource.Verdict = PlatformAlertNormal
	if err := evaluate(resource, nil, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	summary, err := pipeline.Summary(ctx, now.Add(2*time.Minute))
	if err != nil || summary.ActiveIncidents != 1 || activeLogTestIncident(t, db, subject).ID != logIncident.ID {
		t.Fatal("resource recovery leaked into log view", err)
	}
	if _, err := f.n.ManageIncident(ctx, resourceIncident.ID, 1, "acknowledge", nil, 1, now); !errors.Is(err, ErrLogConflict) {
		t.Fatal("resolved resource did not enforce generic management CAS", err)
	}
	logEvaluation.Severity = "critical"
	if err := evaluate(logEvaluation, f.n, now.Add(time.Minute)); !errors.Is(err, ErrLogConflict) {
		t.Fatal("severity overwritten within active incident", err)
	}
	logEvaluation.Severity = "warning"
	if err := evaluate(logEvaluation, f.n, now.Add(-time.Minute)); !errors.Is(err, ErrLogConflict) {
		t.Fatal("stale observation rewrote incident", err)
	}
}

func TestIntegrationPostgresPlatformAlertExplicitCategoriesAndCAS(t *testing.T) {
	f := newLogRetryFixture(t, "webhook")
	ctx := context.Background()
	for _, categories := range [][]string{nil, {}, {"resource", "resource"}, {"unknown"}} {
		_, err := f.n.Save(ctx, 0, PlatformAlertDestinationInput{Name: uuid.NewString(), Channel: "webhook", URL: f.target.URL, Categories: categories, EventTypes: []string{"opened"}})
		if !errors.Is(err, ErrLogInvalid) {
			t.Fatal("invalid/implicit categories accepted", categories, err)
		}
	}
	input := PlatformAlertDestinationInput{Name: f.target.Name, Channel: f.target.Channel, URL: f.target.URL, Categories: []string{"resource", "log_pipeline"}, EventTypes: []string{"opened"}, Version: f.target.Version}
	target, err := f.n.Save(ctx, f.target.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if len(target.Categories) != 2 || target.Categories[0] != "log_pipeline" {
		t.Fatal("categories not explicit and canonical", target)
	}
	if _, err = f.n.Save(ctx, f.target.ID, input); !errors.Is(err, ErrLogConflict) {
		t.Fatal("stale category mutation accepted", err)
	}
	rows, err := f.n.Destinations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range rows {
		if row.ID == target.ID {
			found = len(row.Categories) == 2 && row.SecretCiphertext == target.SecretCiphertext
		}
	}
	if !found {
		t.Fatal("generic management omitted subscribed target")
	}
}

func TestIntegrationPostgresPlatformAlertResourceRoutingAndManagement(t *testing.T) {
	f := newLogRetryFixture(t, "webhook")
	db, ctx := f.db, context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	target, err := f.n.Save(ctx, 0, PlatformAlertDestinationInput{Categories: []string{"resource"}, Name: "resource-" + uuid.NewString(), Channel: "webhook", URL: f.target.URL, EventTypes: []string{"opened", "resolved"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Where("destination_id=?", target.ID).Delete(&models.PlatformAlertDelivery{})
		db.Delete(&models.PlatformAlertDestination{}, target.ID)
	})
	target, err = f.n.SetSecret(ctx, target.ID, target.Version, "fixture-secret-resource-123456")
	if err != nil {
		t.Fatal(err)
	}
	target, err = f.n.Save(ctx, target.ID, PlatformAlertDestinationInput{Version: target.Version, Categories: []string{"resource"}, Name: target.Name, Channel: "webhook", URL: target.URL, EventTypes: []string{"opened", "resolved"}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	subject := uuid.NewString()
	t.Cleanup(func() {
		db.Exec(`DELETE FROM monitor.platform_alert_deliveries WHERE event_id IN (SELECT id FROM monitor.platform_alert_events WHERE incident_id IN (SELECT id FROM monitor.platform_alert_incidents WHERE subject_id=?))`, subject)
		db.Exec(`DELETE FROM monitor.platform_alert_events WHERE incident_id IN (SELECT id FROM monitor.platform_alert_incidents WHERE subject_id=?)`, subject)
		db.Where("subject_id=?", subject).Delete(&models.PlatformAlertIncident{})
	})
	e := PlatformAlertEvaluation{Category: models.PlatformAlertCategoryResource, SubjectKind: models.PlatformAlertSubjectHostNode, SubjectID: subject, Signal: "node.memory.used_percent", Severity: "warning", Verdict: PlatformAlertRisk}
	if err := db.Transaction(func(tx *gorm.DB) error { return reconcilePlatformAlert(tx, f.n, e, now) }); err != nil {
		t.Fatal(err)
	}
	list, err := f.n.Incidents(ctx, PlatformAlertFilter{Page: 1, PageSize: 20, Category: "resource", Status: "active", SubjectID: subject, Severity: ""}, "")
	if err != nil || list.Total != 1 {
		t.Fatal("resource list", list, err)
	}
	incident := list.Data[0]
	row, err := f.n.ManageIncident(ctx, incident.ID, incident.Version, "acknowledge", nil, 1, now)
	if err != nil || row.Status != "acknowledged" {
		t.Fatal("acknowledge", row, err)
	}
	if _, err := f.n.ManageIncident(ctx, incident.ID, incident.Version, "acknowledge", nil, 1, now); !errors.Is(err, ErrLogConflict) {
		t.Fatal("stale action accepted", err)
	}
	until := now.Add(time.Hour)
	row, err = f.n.ManageIncident(ctx, row.ID, row.Version, "suppress", &until, 1, now)
	if err != nil || row.SuppressedUntil == nil {
		t.Fatal("suppress", row, err)
	}
	var delivery models.PlatformAlertDelivery
	if err := db.Joins("JOIN monitor.platform_alert_events e ON e.id=platform_alert_deliveries.event_id").Where("e.incident_id=?", incident.ID).Take(&delivery).Error; err != nil {
		t.Fatal(err)
	}
	if delivery.DestinationID != target.ID {
		t.Fatal("resource sent to pre-existing log subscriber", delivery.DestinationID)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(delivery.Payload), &payload); err != nil || payload["schema"] != "addp.platform-resource-alert/v1" || payload["subject_id"] != subject || payload["node"] != nil {
		t.Fatal("wrong formal resource payload", payload, err)
	}
	if _, err := wecomPlatformAlertPayload(delivery.Payload, delivery.ID); err != nil {
		t.Fatal("resource WeCom formatter", err)
	}
	if err := f.n.send(ctx, delivery, now); err != nil {
		t.Fatal("resource webhook formatter", err)
	}
	// Recovery records one same-source event. Active suppression applies to its
	// notification without rewriting the opened event or its original payload.
	e.Verdict = PlatformAlertNormal
	if err := db.Transaction(func(tx *gorm.DB) error { return reconcilePlatformAlert(tx, f.n, e, now.Add(time.Minute)) }); err != nil {
		t.Fatal(err)
	}
	list, err = f.n.Incidents(ctx, PlatformAlertFilter{Page: 1, PageSize: 20, Category: "resource", Status: "resolved", SubjectID: subject, Severity: ""}, "")
	if err != nil || list.Total != 1 {
		t.Fatal("recovery", err)
	}
	if _, err := f.n.ManageIncident(ctx, row.ID, list.Data[0].Version, "acknowledge", nil, 1, now.Add(time.Minute)); !errors.Is(err, ErrLogConflict) {
		t.Fatal("resolved event handled", err)
	}
	var events int64
	db.Model(&models.PlatformAlertEvent{}).Where("incident_id=?", incident.ID).Count(&events)
	if events != 2 {
		t.Fatal("lifecycle duplicated", events)
	}
}

func TestIntegrationPostgresPlatformAlertSeverityBeforePagination(t *testing.T) {
	f := newLogRetryFixture(t, "webhook")
	ctx, subject := context.Background(), "severity-"+uuid.NewString()
	now := time.Now().UTC().Truncate(time.Microsecond)
	rows := []models.PlatformAlertIncident{}
	for n, severity := range []string{"warning", "critical", "warning", "critical", "warning"} {
		rows = append(rows, models.PlatformAlertIncident{Category: "resource", SubjectKind: "host_node", SubjectID: subject, Signal: fmt.Sprintf("severity-%d", n), Severity: severity, Status: "open", OpenedAt: now.Add(time.Duration(n) * time.Second), LastObservedAt: now})
	}
	rows[4].Status = "resolved"
	rows = append(rows, models.PlatformAlertIncident{Category: "process_resource", SubjectKind: "module_instance", SubjectID: subject, Signal: "process.cpu.cores", Severity: "warning", Status: "open", OpenedAt: now.Add(6 * time.Second), LastObservedAt: now})
	t.Cleanup(func() {
		if err := f.db.Where("subject_id=?", subject).Delete(&models.PlatformAlertIncident{}).Error; err != nil {
			t.Error(err)
		}
		var count int64
		if err := f.db.Model(&models.PlatformAlertIncident{}).Where("subject_id=?", subject).Count(&count).Error; err != nil || count != 0 {
			t.Errorf("severity fixture residue=%d err=%v", count, err)
		}
	})
	if err := f.db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		category, status, severity string
		total                      int64
		ids                        []uint
	}{
		{"resource", "active", "warning", 2, []uint{rows[2].ID, rows[0].ID}},
		{"resource", "active", "critical", 2, []uint{rows[3].ID, rows[1].ID}},
		{"resource", "resolved", "warning", 1, []uint{rows[4].ID}},
		{"process_resource", "active", "warning", 1, []uint{rows[5].ID}},
		{"resource", "active", "", 4, []uint{rows[3].ID, rows[2].ID, rows[1].ID, rows[0].ID}},
	} {
		for page, id := range tc.ids {
			list, err := f.n.Incidents(ctx, PlatformAlertFilter{Page: page + 1, PageSize: 1, Category: tc.category, Status: tc.status, SubjectID: subject, Severity: tc.severity}, "")
			if err != nil || list.Total != tc.total || list.TotalPages != int(tc.total) || len(list.Data) != 1 || list.Data[0].ID != id {
				t.Fatalf("filter=%+v page=%d result=%+v err=%v", tc, page+1, list, err)
			}
		}
	}
	empty, err := f.n.Incidents(ctx, PlatformAlertFilter{Page: 1, PageSize: 20, Category: "process_resource", Status: "active", SubjectID: subject, Severity: "critical"}, "")
	if err != nil || empty.Total != 0 || len(empty.Data) != 0 {
		t.Fatalf("empty intersection=%+v err=%v", empty, err)
	}
	for _, severity := range []string{"info", "Critical", "warning ", "warning' OR 1=1--"} {
		if _, err := f.n.Incidents(ctx, PlatformAlertFilter{Page: 1, PageSize: 20, Category: "", Status: "", SubjectID: subject, Severity: severity}, ""); !errors.Is(err, ErrLogInvalid) {
			t.Fatalf("unsupported severity accepted: %q err=%v", severity, err)
		}
	}
}

func TestIntegrationPostgresPlatformAlertOpeningTimeRange(t *testing.T) {
	f := newLogRetryFixture(t, "webhook")
	subject := "opening-time-" + uuid.NewString()
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(10 * time.Second)
	now := time.Now().UTC().Truncate(time.Microsecond)
	opened := []time.Time{from.Add(-time.Microsecond), from, from.Add(time.Microsecond), from.Add(5 * time.Second), to, to.Add(time.Microsecond), from.Add(3 * time.Second), from.Add(4 * time.Second)}
	rows := make([]models.PlatformAlertIncident, len(opened))
	for i, at := range opened {
		rows[i] = models.PlatformAlertIncident{Category: "resource", SubjectKind: "host_node", SubjectID: subject, Signal: fmt.Sprintf("time-%d", i), Severity: "warning", Status: "open", OpenedAt: at, LastObservedAt: now}
	}
	rows[2].Status = "acknowledged"
	rows[3].Status = "resolved"
	rows[3].ResolvedAt = &now
	rows[6].Severity = "critical"
	rows[7].Category = "process_resource"
	rows[7].SubjectKind = "module_instance"
	t.Cleanup(func() {
		if err := f.db.Where("subject_id=?", subject).Delete(&models.PlatformAlertIncident{}).Error; err != nil {
			t.Error(err)
		}
		var count int64
		if err := f.db.Model(&models.PlatformAlertIncident{}).Where("subject_id=?", subject).Count(&count).Error; err != nil || count != 0 {
			t.Errorf("time fixture residue=%d err=%v", count, err)
		}
	})
	if err := f.db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	offsetFrom := from.In(time.FixedZone("east", 8*60*60))
	cases := []struct {
		name, category, status string
		from, to               *time.Time
		indices                []int
	}{
		{"half-open boundaries", "resource", "active", &from, &to, []int{2, 1}},
		{"same instant with offset", "resource", "active", &offsetFrom, &to, []int{2, 1}},
		{"resolved by opening, not recovery", "resource", "resolved", &from, &to, []int{3}},
		{"no category", "", "active", &from, &to, []int{7, 2, 1}},
		{"lower only", "resource", "active", &from, nil, []int{5, 4, 2, 1}},
		{"upper only", "resource", "active", nil, &to, []int{2, 1, 0}},
		{"all time", "resource", "active", nil, nil, []int{5, 4, 2, 1, 0}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for page, index := range tc.indices {
				list, err := f.n.Incidents(context.Background(), PlatformAlertFilter{Page: page + 1, PageSize: 1, Category: tc.category, Status: tc.status, SubjectID: subject, Severity: "warning", TimeFrom: tc.from, TimeTo: tc.to}, "")
				if err != nil || list.Total != int64(len(tc.indices)) || list.TotalPages != len(tc.indices) || len(list.Data) != 1 || list.Data[0].ID != rows[index].ID {
					t.Fatalf("page=%d list=%+v err=%v", page+1, list, err)
				}
			}
		})
	}
	zero := time.Time{}
	empty, err := f.n.Incidents(context.Background(), PlatformAlertFilter{Page: 1, PageSize: 20, SubjectID: subject, TimeTo: &zero}, "")
	if err != nil || empty.Total != 0 || len(empty.Data) != 0 {
		t.Fatalf("explicit year-one upper bound broadened: %+v err=%v", empty, err)
	}
	for _, upper := range []time.Time{from, from.Add(-time.Nanosecond)} {
		if _, err := f.n.Incidents(context.Background(), PlatformAlertFilter{Page: 1, PageSize: 20, TimeFrom: &from, TimeTo: &upper}, ""); !errors.Is(err, ErrLogInvalid) {
			t.Fatal("invalid interval accepted", err)
		}
	}
}

func TestIntegrationPostgresPlatformAlertNameSearch(t *testing.T) {
	f := newLogRetryFixture(t, "webhook")
	subject := "name-" + uuid.NewString()
	at := time.Now().UTC().Truncate(time.Microsecond)
	rows := []models.PlatformAlertIncident{}
	for n := 0; n < 25; n++ {
		rows = append(rows, models.PlatformAlertIncident{Category: "resource", SubjectKind: "host_node", SubjectID: subject, Signal: fmt.Sprint(n), Severity: "warning", Status: "open", OpenedAt: at.Add(time.Duration(n) * time.Second), LastObservedAt: at, ResourceEvidence: &models.ResourceAlertEvidence{RuleName: `主机 Cpu 90%_\ test`}})
	}
	rows = append(rows,
		models.PlatformAlertIncident{Category: "http_quality", SubjectKind: "module", SubjectID: subject, Signal: "http", Severity: "warning", Status: "resolved", OpenedAt: at, LastObservedAt: at, HTTPEvidence: &models.HTTPAlertEvidence{RuleName: "HTTP AbC"}},
		models.PlatformAlertIncident{Category: "process_resource", SubjectKind: "module_instance", SubjectID: subject, Signal: "process", Severity: "warning", Status: "open", OpenedAt: at, LastObservedAt: at, ProcessEvidence: &models.ProcessAlertEvidence{RuleName: "RSS AbC"}},
		models.PlatformAlertIncident{Category: "resource", SubjectKind: "host_node", SubjectID: subject, Signal: "old", Severity: "warning", Status: "resolved", OpenedAt: at, LastObservedAt: at, ResourceEvidence: &models.ResourceAlertEvidence{}},
		models.PlatformAlertIncident{Category: "log_pipeline", SubjectKind: "log_observer", SubjectID: subject, Signal: "receiver_loss:runtime-1", Severity: "critical", Status: "open", OpenedAt: at, LastObservedAt: at},
	)
	t.Cleanup(func() {
		if err := f.db.Where("subject_id=?", subject).Delete(&models.PlatformAlertIncident{}).Error; err != nil {
			t.Error(err)
		}
		var n int64
		if err := f.db.Model(&models.PlatformAlertIncident{}).Where("subject_id=?", subject).Count(&n).Error; err != nil || n != 0 {
			t.Errorf("name fixture residue=%d err=%v", n, err)
		}
	})
	if err := f.db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	from, to := at.Add(time.Second), at.Add(24*time.Second)
	for _, tc := range []struct {
		name, search, lang, category, severity, status string
		from, to                                       *time.Time
		want                                           int64
	}{
		{name: "literal percent", search: "90%", want: 25},
		{name: "literal underscore and slash", search: `%_\`, want: 25},
		{name: "not a wildcard", search: "%nothing", want: 0},
		{name: "case insensitive and trimmed", search: "  cpu  ", want: 25},
		{name: "unicode", search: "主机", want: 25},
		{name: "quote is literal", search: "' OR 1=1 --", want: 0},
		{name: "HTTP snapshot", search: "abc", category: "http_quality", want: 1},
		{name: "process snapshot", search: "abc", category: "process_resource", want: 1},
		{name: "all category snapshots", search: "aBc", want: 2},
		{name: "localized log prefix", search: "接收器新增", lang: "zh-cn", want: 1},
		{name: "english log", search: "receiver dropped", lang: "en", want: 1},
		{name: "current language only", search: "接收器新增", lang: "en", want: 0},
		{name: "category intersection", search: "cpu", category: "http_quality", want: 0},
		{name: "severity intersection", search: "cpu", severity: "critical", want: 0},
		{name: "status intersection", search: "cpu", status: "resolved", want: 0},
		{name: "time intersection", search: "cpu", from: &from, to: &to, want: 23},
	} {
		t.Run(tc.name, func(t *testing.T) {
			seen := map[uint]bool{}
			for page := 1; page <= int(tc.want)/7+1; page++ {
				list, err := f.n.Incidents(context.Background(), PlatformAlertFilter{Page: page, PageSize: 7, SubjectID: subject, Search: tc.search, Language: tc.lang, Category: tc.category, Severity: tc.severity, Status: tc.status, TimeFrom: tc.from, TimeTo: tc.to}, "")
				if err != nil || list.Total != tc.want {
					t.Fatalf("total=%d want=%d err=%v", list.Total, tc.want, err)
				}
				for _, row := range list.Data {
					if row.Name == "" || seen[row.ID] {
						t.Fatal("missing/repeated result", row)
					}
					seen[row.ID] = true
				}
			}
			if int64(len(seen)) != tc.want {
				t.Fatal("pagination omitted matches", len(seen))
			}
		})
	}
	list, err := f.n.Incidents(context.Background(), PlatformAlertFilter{Page: 1, PageSize: 100, SubjectID: subject}, "")
	if err != nil || list.Total != 29 {
		t.Fatal(list, err)
	}
	for _, row := range list.Data {
		if row.Signal == "old" && row.Name != "" {
			t.Fatal("missing history guessed", row.Name)
		}
	}
}

func TestIntegrationPostgresPlatformAlertCurrentHostName(t *testing.T) {
	f := newLogRetryFixture(t, "webhook")
	first, second := uuid.NewString(), uuid.NewString()
	now := time.Now().UTC().Truncate(time.Microsecond)
	hosts := &overviewNodes{rows: []client.UserHostNode{
		{NodeID: first, DisplayName: `测试 Host 90%_\`, Enabled: false},
		{NodeID: second, DisplayName: "测试 HOST second", Enabled: true},
	}}
	f.n.WithHostNodes(hosts)
	rows := []models.PlatformAlertIncident{}
	for n := 0; n < 25; n++ {
		rows = append(rows, models.PlatformAlertIncident{Category: "resource", SubjectKind: "host_node", SubjectID: first, Signal: fmt.Sprint(n), Status: "open", Severity: "warning", OpenedAt: now.Add(time.Duration(n) * time.Second), LastObservedAt: now, ResourceEvidence: &models.ResourceAlertEvidence{RuleName: "Memory warning"}})
	}
	rows = append(rows, models.PlatformAlertIncident{Category: "resource", SubjectKind: "host_node", SubjectID: second, Signal: "second", Status: "resolved", Severity: "critical", OpenedAt: now, LastObservedAt: now, ResourceEvidence: &models.ResourceAlertEvidence{RuleName: "CPU warning"}})
	// The same text or UUID never establishes a cross-kind host relationship.
	for _, kind := range []struct{ category, subject string }{{"log_pipeline", "log_observer"}, {"process_resource", "module_instance"}, {"http_quality", "module"}, {"resource", "module"}} {
		rows = append(rows, models.PlatformAlertIncident{Category: kind.category, SubjectKind: kind.subject, SubjectID: first, Signal: "same-id-" + kind.category, Status: "open", Severity: "warning", OpenedAt: now, LastObservedAt: now})
	}
	if err := f.db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := f.db.Where("subject_id IN ?", []string{first, second}).Delete(&models.PlatformAlertIncident{}).Error; err != nil {
			t.Error(err)
		}
		var count int64
		if err := f.db.Model(&models.PlatformAlertIncident{}).Where("subject_id IN ?", []string{first, second}).Count(&count).Error; err != nil || count != 0 {
			t.Errorf("host-name fixture cleanup count=%d err=%v", count, err)
		}
	})
	query := func(filter PlatformAlertFilter) (PlatformAlertList, error) {
		return f.n.Incidents(context.Background(), filter, "addp_at_current_user")
	}
	for _, tc := range []struct {
		name, host, search, category, status, severity, subject string
		count                                                   int64
	}{
		{name: "case and Unicode across hosts", host: " 测试 hOsT ", count: 26},
		{name: "literal percent underscore slash and disabled host", host: `90%_\`, count: 25},
		{name: "no wildcard expansion", host: "%", count: 25},
		{name: "unknown name", host: "no matched host", count: 0},
		{name: "intersection alert name", host: "host", search: "Memory", count: 25},
		{name: "intersection severity", host: "host", severity: "critical", count: 1},
		{name: "intersection status", host: "host", status: "resolved", count: 1},
		{name: "intersection id", host: "host", subject: second, count: 1},
		{name: "exclude log observer", host: "host", category: "log_pipeline", count: 0},
		{name: "exclude instances", host: "host", category: "process_resource", count: 0},
		{name: "exclude module", host: "host", category: "http_quality", count: 0},
		{name: "injection text literal", host: "' OR 1=1 --", count: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			filter := PlatformAlertFilter{Page: 1, PageSize: 20, HostName: tc.host, Search: tc.search, Category: tc.category, Status: tc.status, Severity: tc.severity, SubjectID: tc.subject}
			page, err := query(filter)
			if err != nil || page.Total != tc.count || len(page.Data) != min(int(tc.count), 20) {
				t.Fatalf("page=%+v err=%v", page, err)
			}
			for _, row := range page.Data {
				if row.Category != "resource" || row.SubjectKind != "host_node" {
					t.Fatal("cross-kind host inferred", row)
				}
			}
		})
	}
	page, err := query(PlatformAlertFilter{Page: 2, PageSize: 20, HostName: "90%"})
	if err != nil || page.Total != 25 || page.TotalPages != 2 || len(page.Data) != 5 {
		t.Fatal(page, err)
	}
	upper := now.Add(2 * time.Second)
	page, err = query(PlatformAlertFilter{Page: 1, PageSize: 20, HostName: "90%", TimeFrom: &now, TimeTo: &upper})
	if err != nil || page.Total != 2 {
		t.Fatal("time intersection", page, err)
	}
	hosts.rows[0].DisplayName = "renamed host"
	page, err = query(PlatformAlertFilter{Page: 1, PageSize: 20, HostName: "90%"})
	if err != nil || page.Total != 0 {
		t.Fatal("old display name reused", page, err)
	}
	page, err = query(PlatformAlertFilter{Page: 1, PageSize: 20, HostName: "renamed"})
	if err != nil || page.Total != 25 {
		t.Fatal("current name not used", page, err)
	}
	for _, tc := range []struct {
		name      string
		err, want error
	}{
		{"owner unavailable", errors.New("unavailable"), metricsdiscovery.ErrIdentityUnavailable},
		{"incomplete bounded inventory", client.ErrHostNodeScopeBudget, resourcequery.ErrBudget},
		{"deadline", context.DeadlineExceeded, context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hosts.scopeErr = tc.err
			page, err := query(PlatformAlertFilter{Page: 1, PageSize: 20, HostName: "renamed"})
			if !errors.Is(err, tc.want) || page.Total != 0 || len(page.Data) != 0 {
				t.Fatal(page, err)
			}
		})
	}
	calls := hosts.scopeCalls
	page, err = query(PlatformAlertFilter{Page: 1, PageSize: 20, HostName: " ", SubjectID: first})
	if err != nil || page.Total != 29 || hosts.scopeCalls != calls {
		t.Fatal("empty filter gained dependency", page, err)
	}
	for _, token := range hosts.tokens {
		if token != "addp_at_current_user" {
			t.Fatal("did not forward current user")
		}
	}
	for _, bad := range []string{string([]byte{255}), "\tHost", string(make([]byte, 101))} {
		if _, err = query(PlatformAlertFilter{Page: 1, PageSize: 20, HostName: bad}); !errors.Is(err, ErrLogInvalid) {
			t.Fatal("invalid host accepted", err)
		}
	}
}
