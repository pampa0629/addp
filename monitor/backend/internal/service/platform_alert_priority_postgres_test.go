package service

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/monitor/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestIntegrationPostgresPlatformAlertPriorityBeforePagination(t *testing.T) {
	f := newLogRetryFixture(t, "webhook")
	ctx, subject := context.Background(), uuid.NewString()
	at := time.Now().UTC().Truncate(time.Microsecond)
	future := at.Add(time.Hour)
	rows := make([]models.PlatformAlertIncident, 0, 35)
	add := func(category, kind, severity, status string, seconds int) {
		rows = append(rows, models.PlatformAlertIncident{Category: category, SubjectKind: kind, SubjectID: subject, Signal: fmt.Sprintf("priority-%d", len(rows)), Severity: severity, Status: status, OpenedAt: at.Add(time.Duration(seconds) * time.Second), LastObservedAt: at.Add(100 * time.Second), ResourceEvidence: &models.ResourceAlertEvidence{RuleName: "Priority rule"}})
	}
	for n := 0; n < 25; n++ {
		add("resource", "host_node", "warning", "open", n)
	}
	add("resource", "host_node", "critical", "open", -10)                      // 25
	add("resource", "host_node", "critical", "acknowledged", -9)               // 26
	add("resource", "host_node", "critical", "open", -8)                       // 27: suppressed, still priority
	add("http_quality", "module", "critical", "open", -7)                      // 28
	add("process_resource", "module_instance", "critical", "acknowledged", -6) // 29
	add("log_pipeline", "log_observer", "critical", "open", -6)                // 30: same time, larger ID
	add("resource", "host_node", "critical", "resolved", 40)                   // 31: ordinary group
	add("resource", "host_node", "warning", "acknowledged", 30)                // 32
	add("resource", "host_node", "warning", "open", 31)                        // 33: suppressed warning
	add("resource", "host_node", "warning", "resolved", 41)                    // 34
	rows[27].SuppressedUntil, rows[33].SuppressedUntil = &future, &future
	rows[31].ResolvedAt, rows[34].ResolvedAt = &at, &at
	incidentIDs := f.db.Model(&models.PlatformAlertIncident{}).Select("id").Where("subject_id=?", subject)
	eventIDs := f.db.Model(&models.PlatformAlertEvent{}).Select("id").Where("incident_id IN (?)", incidentIDs)
	t.Cleanup(func() {
		for _, q := range []struct {
			model any
			where string
			value any
		}{
			{&models.PlatformAlertDelivery{}, "event_id IN (?)", eventIDs},
			{&models.PlatformAlertEvent{}, "incident_id IN (?)", incidentIDs},
			{&models.PlatformAlertIncident{}, "subject_id=?", subject},
		} {
			if err := f.db.Where(q.where, q.value).Delete(q.model).Error; err != nil {
				t.Error(err)
			}
			var n int64
			if err := f.db.Model(q.model).Where(q.where, q.value).Count(&n).Error; err != nil || n != 0 {
				t.Errorf("priority cleanup residue=%d err=%v", n, err)
			}
		}
	})
	if err := f.db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	f.n.WithHostNodes(&overviewNodes{rows: []client.UserHostNode{{NodeID: subject, DisplayName: "Priority host", Enabled: true}}})
	descending := func(first, last int) []int {
		var indices []int
		for i := first; i >= last; i-- {
			indices = append(indices, i)
		}
		return indices
	}
	all := append([]int{30, 29, 28, 27, 26, 25, 34, 31, 33, 32}, descending(24, 0)...)
	check := func(t *testing.T, filter PlatformAlertFilter, indices []int, size int) {
		t.Helper()
		filter.SubjectID, filter.PageSize = subject, size
		var got []uint
		for page := 1; page <= (len(indices)+size-1)/size+1; page++ {
			filter.Page = page
			list, err := f.n.Incidents(ctx, filter, "current-user")
			if err != nil || list.Total != int64(len(indices)) || list.TotalPages != (len(indices)+size-1)/size {
				t.Fatalf("page=%d result=%+v err=%v", page, list, err)
			}
			for _, row := range list.Data {
				got = append(got, row.ID)
			}
		}
		want := make([]uint, len(indices))
		for n, index := range indices {
			want[n] = rows[index].ID
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got IDs=%v want=%v", got, want)
		}
	}
	for _, size := range []int{1, 7, 20} {
		t.Run(fmt.Sprintf("global-page-size-%d", size), func(t *testing.T) { check(t, PlatformAlertFilter{}, all, size) })
	}
	for _, tc := range []struct {
		name    string
		filter  PlatformAlertFilter
		indices []int
	}{
		{"active", PlatformAlertFilter{Status: "active"}, append([]int{30, 29, 28, 27, 26, 25, 33, 32}, descending(24, 0)...)},
		{"critical", PlatformAlertFilter{Severity: "critical"}, []int{30, 29, 28, 27, 26, 25, 31}},
		{"warnings-keep-time-order", PlatformAlertFilter{Severity: "warning"}, append([]int{34, 33, 32}, descending(24, 0)...)},
		{"acknowledged", PlatformAlertFilter{Status: "acknowledged"}, []int{29, 26, 32}},
		{"resolved-keep-time-order", PlatformAlertFilter{Status: "resolved"}, []int{34, 31}},
		{"host-and-name-intersection", PlatformAlertFilter{HostName: "priority HOST", Search: "Priority", Category: "resource"}, append([]int{27, 26, 25, 34, 31, 33, 32}, descending(24, 0)...)},
		{"time-excludes-old-priority", PlatformAlertFilter{TimeFrom: &at}, append([]int{34, 31, 33, 32}, descending(24, 0)...)},
	} {
		t.Run(tc.name, func(t *testing.T) { check(t, tc.filter, tc.indices, 7) })
	}
	t.Run("acknowledgement-and-suppression-stay-priority", func(t *testing.T) {
		updated, err := f.n.ManageIncident(ctx, rows[25].ID, rows[25].Version, "acknowledge", nil, 1, at)
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.n.ManageIncident(ctx, updated.ID, updated.Version, "suppress", &future, 1, at)
		if err != nil {
			t.Fatal(err)
		}
		check(t, PlatformAlertFilter{}, all, 20)
	})
	t.Run("recovery-returns-to-opening-time-order", func(t *testing.T) {
		source := rows[30]
		if err := f.db.Transaction(func(tx *gorm.DB) error {
			return reconcilePlatformAlert(tx, f.n, PlatformAlertEvaluation{Category: source.Category, SubjectKind: source.SubjectKind, SubjectID: subject, Signal: source.Signal, Severity: source.Severity, Verdict: PlatformAlertNormal}, at.Add(200*time.Second))
		}); err != nil {
			t.Fatal(err)
		}
		indices := append([]int{29, 28, 27, 26, 25, 34, 31, 33, 32}, descending(24, 0)...)
		indices = append(indices, 30)
		check(t, PlatformAlertFilter{}, indices, 20)
		var resolved models.PlatformAlertIncident
		if err := f.db.First(&resolved, source.ID).Error; err != nil || resolved.Status != "resolved" || !resolved.OpenedAt.Equal(source.OpenedAt) {
			t.Fatalf("lifecycle changed unexpectedly: %+v err=%v", resolved, err)
		}
	})
}
