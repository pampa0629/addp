package service

import (
	"os"
	"testing"
	"time"

	"github.com/addp/meta/migrations"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestAuthorizationBasisRevisionAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("META_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("requires standard Meta PostgreSQL gate")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Error(err)
		}
	})
	tx := db
	t.Cleanup(func() {
		if err := db.Exec("DROP SCHEMA IF EXISTS meta CASCADE").Error; err != nil {
			t.Error(err)
		}
		var count int64
		if err := db.Raw("SELECT count(*) FROM pg_namespace WHERE nspname='meta'").Scan(&count).Error; err != nil || count != 0 {
			t.Errorf("Meta revision fixture residual=%d err=%v", count, err)
		}
	})
	if err = tx.Exec(`DROP SCHEMA IF EXISTS meta CASCADE; CREATE SCHEMA meta;
 CREATE TABLE meta.meta_item(id bigint,tenant_id bigint,engine_id bigint,node_id bigint,item_type text,name text,full_name text,fingerprint text,attributes jsonb,deleted_at timestamptz,row_count bigint,scanned_at timestamptz);
 CREATE TABLE meta.meta_node(id bigint,tenant_id bigint,engine_id bigint,parent_node_id bigint,node_type text,name text,path text,full_name text,attributes jsonb,deleted_at timestamptz,scanned_at timestamptz);`).Error; err != nil {
		t.Fatal(err)
	}
	migration, err := migrations.FS.ReadFile("028_authorization_basis_revision.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Exec(string(migration)).Error; err != nil {
		t.Fatal(err)
	}
	s := NewMetadataQueryService(tx)
	read := func(tenant uint) string {
		t.Helper()
		r, e := s.GetAuthorizationBasisRevision(t.Context(), tenant)
		if e != nil {
			t.Fatal(e)
		}
		return r.Generation + "/" + r.Version
	}
	before := read(7)
	other := read(8)
	exec := func(q string) {
		t.Helper()
		if e := tx.Exec(q).Error; e != nil {
			t.Fatal(e)
		}
	}
	exec("INSERT INTO meta.meta_item(id,tenant_id,engine_id,name) VALUES(1,7,9,'first')")
	inserted := read(7)
	if inserted == before || read(8) != other {
		t.Fatal("insert did not advance exactly its Tenant")
	}
	exec("UPDATE meta.meta_item SET row_count=10,scanned_at=clock_timestamp() WHERE id=1")
	if read(7) != inserted {
		t.Fatal("statistics invalidated authorization basis")
	}
	rolled := db.Begin()
	if rolled.Error != nil {
		t.Fatal(rolled.Error)
	}
	if err := rolled.Exec("UPDATE meta.meta_item SET attributes='{}'::jsonb WHERE id=1").Error; err != nil {
		rolled.Rollback()
		t.Fatal(err)
	}
	own, err := NewMetadataQueryService(rolled).GetAuthorizationBasisRevision(t.Context(), 7)
	if err != nil {
		rolled.Rollback()
		t.Fatal(err)
	}
	if own.Generation+"/"+own.Version == inserted {
		rolled.Rollback()
		t.Fatal("attributes did not advance basis inside writer transaction")
	}
	if read(7) != inserted {
		rolled.Rollback()
		t.Fatal("uncommitted version leaked to another connection")
	}
	if err := rolled.Rollback().Error; err != nil {
		t.Fatal(err)
	}
	if read(7) != inserted {
		t.Fatal("rollback leaked a committed revision")
	}
	exec("UPDATE meta.meta_item SET tenant_id=8 WHERE id=1")
	if read(7) == inserted || read(8) == other {
		t.Fatal("Tenant move must advance both owners")
	}
	before = read(7)
	exec("INSERT INTO meta.meta_node(id,tenant_id,parent_node_id) VALUES(1,7,NULL)")
	if read(7) == before {
		t.Fatal("node insert did not advance basis")
	}
	before = read(7)
	exec("UPDATE meta.meta_node SET scanned_at=clock_timestamp()")
	if read(7) != before {
		t.Fatal("node scan statistics changed basis")
	}
	exec("UPDATE meta.meta_node SET parent_node_id=2")
	if read(7) == before {
		t.Fatal("parent chain change did not advance basis")
	}
	before = read(7)
	exec("TRUNCATE meta.meta_item")
	if read(7) == before {
		t.Fatal("truncate reused prior generation")
	}
	exec("INSERT INTO meta.meta_item(id,tenant_id,engine_id,name) VALUES(10,7,9,'one'),(11,7,9,'two')")
	var prior int64
	if err := db.Raw("SELECT version FROM meta.authorization_basis_revisions WHERE tenant_id=7").Scan(&prior).Error; err != nil {
		t.Fatal(err)
	}
	first, second := db.Begin(), db.Begin()
	defer func() { first.Rollback(); second.Rollback() }()
	if first.Error != nil || second.Error != nil {
		t.Fatal(first.Error, second.Error)
	}
	if err := first.Exec("UPDATE meta.meta_item SET name='one changed' WHERE id=10").Error; err != nil {
		t.Fatal(err)
	}
	var secondPID int
	if err := second.Raw("SELECT pg_backend_pid()").Scan(&secondPID).Error; err != nil {
		t.Fatal(err)
	}
	completed := make(chan error, 1)
	go func() { completed <- second.Exec("UPDATE meta.meta_item SET name='two changed' WHERE id=11").Error }()
	limit := time.Now().Add(3 * time.Second)
	blocked := false
	for time.Now().Before(limit) {
		if err := db.Raw("SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=? AND wait_event_type='Lock')", secondPID).Scan(&blocked).Error; err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !blocked {
		t.Fatal("second Tenant writer did not serialize on committed revision")
	}
	if err := first.Commit().Error; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-completed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("revision writer did not resume")
	}
	var observed int64
	if err := db.Raw("SELECT version FROM meta.authorization_basis_revisions WHERE tenant_id=7").Scan(&observed).Error; err != nil || observed != prior+1 {
		t.Fatalf("uncommitted late writer changed observed version=%d want=%d err=%v", observed, prior+1, err)
	}
	if err := second.Commit().Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Raw("SELECT version FROM meta.authorization_basis_revisions WHERE tenant_id=7").Scan(&observed).Error; err != nil || observed != prior+2 {
		t.Fatalf("late commit was lost: version=%d want=%d err=%v", observed, prior+2, err)
	}
}
