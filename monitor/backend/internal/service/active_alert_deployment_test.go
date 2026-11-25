package service

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/addp/common/infra/bootstrap"
	"github.com/addp/monitor/internal/config"
	"github.com/addp/monitor/internal/models"
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
