package service

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/monitor/internal/models"
	"github.com/google/uuid"
)

func TestIntegrationPostgresPlatformAlertInPlaceMigration(t *testing.T) {
	f := newLogRetryFixture(t, "webhook")
	db := f.db
	schema := "alert_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := db.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal(err)
	}
	names := []string{"incidents", "events", "destinations", "deliveries"}
	q := func(name string) string { return schema + "." + name }
	t.Cleanup(func() {
		for _, name := range names {
			for _, prefix := range []string{"platform_log_", "platform_alert_"} {
				if err := db.Exec("DROP TABLE IF EXISTS " + q(prefix+name)).Error; err != nil {
					t.Error(err)
				}
			}
		}
		// The deliberately conflicting sequence is not owned by a table. Remove
		// it even if the rollback assertion fails before normal-path cleanup.
		for _, name := range []string{"incidents", "destinations"} {
			for _, prefix := range []string{"platform_log_", "platform_alert_"} {
				if err := db.Exec("DROP SEQUENCE IF EXISTS " + q(prefix+name+"_id_seq")).Error; err != nil {
					t.Error(err)
				}
			}
		}
		if err := db.Exec("DROP SCHEMA " + schema + " RESTRICT").Error; err != nil {
			t.Error(err)
		}
		var remaining int64
		if err := db.Raw("SELECT count(*) FROM pg_namespace WHERE nspname=?", schema).Scan(&remaining).Error; err != nil || remaining != 0 {
			t.Errorf("migration fixture residue=%d error=%v", remaining, err)
		}
	})
	ids := []any{f.incident.ID, f.event.ID, f.target.ID, f.delivery.ID}
	for i, name := range names {
		old := q("platform_log_" + name)
		if err := db.Exec("CREATE TABLE " + old + " (LIKE monitor.platform_alert_" + name + " INCLUDING ALL)").Error; err != nil {
			t.Fatal(err)
		}
		if name == "incidents" {
			if err := db.Exec("ALTER TABLE " + old + " DROP COLUMN category, DROP COLUMN subject_kind").Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Exec("ALTER TABLE " + old + " RENAME COLUMN subject_id TO node").Error; err != nil {
				t.Fatal(err)
			}
		}
		if name == "destinations" {
			if err := db.Exec("ALTER TABLE " + old + " DROP COLUMN categories").Error; err != nil {
				t.Fatal(err)
			}
		}
		var columns []string
		if err := db.Raw("SELECT column_name FROM information_schema.columns WHERE table_schema=? AND table_name=? ORDER BY ordinal_position", schema, "platform_log_"+name).Scan(&columns).Error; err != nil {
			t.Fatal(err)
		}
		source := append([]string(nil), columns...)
		for j := range source {
			if source[j] == "node" {
				source[j] = "subject_id"
			}
		}
		if err := db.Exec("INSERT INTO "+old+" ("+strings.Join(columns, ",")+") SELECT "+strings.Join(source, ",")+" FROM monitor.platform_alert_"+name+" WHERE id=?", ids[i]).Error; err != nil {
			t.Fatal(err)
		}
		if name == "incidents" || name == "destinations" {
			sequence := q("platform_log_" + name + "_id_seq")
			for _, sql := range []string{
				"CREATE SEQUENCE " + sequence + " START 100000",
				"ALTER TABLE " + old + " ALTER COLUMN id SET DEFAULT nextval('" + sequence + "')",
				"ALTER SEQUENCE " + sequence + " OWNED BY " + old + ".id",
			} {
				if err := db.Exec(sql).Error; err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	// An existing destination object must make the transition fail atomically.
	if err := db.Exec("CREATE TABLE " + q("platform_alert_events") + " (id text)").Error; err != nil {
		t.Fatal(err)
	}
	if err := migratePlatformAlertStore(db, schema); err == nil {
		t.Fatal("ambiguous source and destination sets accepted")
	}
	if err := db.Exec("DROP TABLE " + q("platform_alert_events")).Error; err != nil {
		t.Fatal(err)
	}
	// Fail after renames/column changes have begun, proving transactional rollback.
	if err := db.Exec("CREATE SEQUENCE " + q("platform_alert_incidents_id_seq")).Error; err != nil {
		t.Fatal(err)
	}
	if err := migratePlatformAlertStore(db, schema); err == nil {
		t.Fatal("sequence name conflict accepted")
	}
	var originalNode string
	if err := db.Table(q("platform_log_incidents")).Select("node").Where("id=?", f.incident.ID).Scan(&originalNode).Error; err != nil || originalNode != f.incident.SubjectID {
		t.Fatalf("failed migration changed source: node=%q err=%v", originalNode, err)
	}
	if err := db.Exec("DROP SEQUENCE " + q("platform_alert_incidents_id_seq")).Error; err != nil {
		t.Fatal(err)
	}
	for run := 0; run < 2; run++ {
		if err := migratePlatformAlertStore(db, schema); err != nil {
			t.Fatal(err)
		}
	}
	var original, incident models.PlatformAlertIncident
	if err := db.First(&original, f.incident.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table(q("platform_alert_incidents")).First(&incident, f.incident.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(incident, original) {
		t.Fatal("incident identity, timestamps or source changed")
	}
	var target models.PlatformAlertDestination
	if err := db.Table(q("platform_alert_destinations")).First(&target, f.target.ID).Error; err != nil {
		t.Fatal(err)
	}
	if target.SecretCiphertext != f.target.SecretCiphertext || target.Version != f.target.Version || !reflect.DeepEqual(target.Categories, models.StringList{models.PlatformAlertCategoryLog}) {
		t.Fatal("target credential/version changed or resource subscription added")
	}
	var before, after models.PlatformAlertDelivery
	if err := db.First(&before, "id=?", f.delivery.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table(q("platform_alert_deliveries")).First(&after, "id=?", f.delivery.ID).Error; err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("frozen delivery payload, identity, state or retry history changed", err)
	}
	for _, name := range names {
		var oldExists bool
		if err := db.Raw("SELECT to_regclass(?) IS NOT NULL", q("platform_log_"+name)).Scan(&oldExists).Error; err != nil || oldExists {
			t.Fatal("old runtime table remains", name, err)
		}
	}
	for _, name := range []string{"incidents", "destinations"} {
		var next int64
		if err := db.Raw(fmt.Sprintf("SELECT nextval('%s')", q("platform_alert_"+name+"_id_seq"))).Scan(&next).Error; err != nil || next != 100000 {
			t.Fatalf("sequence state changed: next=%d error=%v", next, err)
		}
	}
}
