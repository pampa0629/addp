package scanruntime

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	mysqlplugin "github.com/addp/common/engine/plugins/mysql"
	"github.com/addp/common/infra/database"
	platformmysql "github.com/addp/common/infra/database/mysql"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/common/schema"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/repository"
	driver "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestMySQLScanCatalog(t *testing.T) {
	if os.Getenv("ADDP_TEST_META_MYSQL_CATALOG") != "1" {
		t.Skip("requires the disposable platform-meta-scan gate")
	}
	t.Setenv("TZ", "Asia/Shanghai")
	port := os.Getenv("ADDP_TEST_MYSQL_PORT")
	open := func(user, password string) *gorm.DB {
		t.Helper()
		dial, err := platformmysql.Dialector(database.Config{DBName: "meta", Host: "127.0.0.1", Port: port, User: user, Password: password, ConnectTimeout: 5 * time.Second})
		if err != nil {
			t.Fatal(err)
		}
		db, err := gorm.Open(dial, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		if err != nil {
			t.Fatal(err)
		}
		pool, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { pool.Close() })
		return db
	}
	db := open(os.Getenv("ADDP_MYSQL_META_WORKER_USER"), os.Getenv("ADDP_MYSQL_META_WORKER_PASSWORD"))
	if err := schema.RequireMySQL(db, "meta_scan_catalog", 1); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewScanRepository(db)
	portNumber, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	resource := &commonmodels.Engine{ID: 99, Name: "catalog sample", EngineType: "mysql", ConnectionInfo: commonmodels.ConnectionInfo{
		"host": "127.0.0.1", "port": portNumber, "database": "scan_source", "user": "scan_reader", "password": os.Getenv("ADDP_TEST_META_SOURCE_PASSWORD"),
	}}
	runtime := NewDatabaseRuntime(db, slog.New(slog.NewTextHandler(io.Discard, nil)), repo, nil)
	// The same semantic path cannot be active under two hierarchy keys.
	// Soft deletion releases that key, as in the PostgreSQL partial index.
	parent, err := repo.UpsertNode(7, 99, nil, "bucket", "path-test", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	insertPath := "INSERT INTO meta_node (tenant_id,engine_id,parent_node_id,node_type,name,depth,full_name) VALUES (7,99,?,'prefix',?,2,?)"
	longPath := strings.Repeat("目录/", 360)
	if err := db.Exec(insertPath, parent.ID, "old", longPath).Error; err != nil {
		t.Fatal(err)
	}
	var duplicate *driver.MySQLError
	if err := db.Exec(insertPath, parent.ID, "new", longPath).Error; !errors.As(err, &duplicate) || duplicate.Number != 1062 {
		t.Fatalf("active path uniqueness: %v", err)
	}
	if err := db.Model(&models.MetaNode{}).Where("tenant_id = ? AND engine_id = ? AND full_name = ?", 7, 99, longPath).Update("deleted_at", time.Now()).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(insertPath, parent.ID, "new", longPath).Error; err != nil {
		t.Fatal(err)
	}
	var firstID uint
	for range 2 {
		namespaces, tables, fields, err := runtime.ScanNamespace(t.Context(), &mysqlplugin.MySQLPlugin{}, resource, 7, 99, "scan_source", "deep", true)
		if err != nil || namespaces != 1 || tables != 1 || fields != 2 {
			t.Fatalf("scan returned %d namespaces, %d tables, %d fields: %v", namespaces, tables, fields, err)
		}
		item, found, err := repo.FindItemByFullName(7, 99, "scan_source.orders")
		if err != nil || !found {
			t.Fatalf("read scanned table: found=%v error=%v", found, err)
		}
		if item.ScannedDepth != "deep" || item.Fingerprint != commonmodels.GenerateItemFingerprint(99, "scan_source.orders") || item.ID == 0 || (firstID != 0 && item.ID != firstID) {
			t.Fatal("scanned table identity or depth changed")
		}
		firstID = item.ID
		node, err := repo.GetNodeByIDForTenant(7, item.NodeID)
		if err != nil || node.ScanStatus != "completed" || node.ScannedAt == nil {
			t.Fatalf("namespace not completed: %v", err)
		}
		if _, err := repo.GetNodeByIDForTenant(8, item.NodeID); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("other tenant namespace: %v", err)
		}
		if _, found, err := repo.FindItemByFullName(8, 99, item.FullName); err != nil || found {
			t.Fatalf("other tenant table: found=%v error=%v", found, err)
		}
	}
	for _, query := range []string{
		"INSERT INTO meta_node (tenant_id,engine_id,node_type,name,depth) VALUES (8,99,'bucket','invalid',1)",
		"INSERT INTO meta_item (tenant_id,engine_id,node_id,item_type,name,fingerprint) VALUES (8,99,1,'object','invalid','invalid')",
	} {
		// MySQL obscures the parent identity when the writer cannot read the referenced table.
		var dbErr *driver.MySQLError
		if err := db.Exec(query).Error; !errors.As(err, &dbErr) || dbErr.Number != 1216 {
			t.Fatalf("mismatched tenant/engine: %v", err)
		}
	}
	createObject := func(tenant, engine uint) (*models.MetaNode, *models.MetaItem) {
		t.Helper()
		bucket, err := repo.UpsertNode(tenant, engine, nil, "bucket", "files", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		attrs := models.JSONMap{"storage": map[string]any{"bucket": "files", "name": "sample.txt"}}
		item, err := repo.UpsertItem(tenant, engine, bucket, "object", "sample.txt", "files/sample.txt", attrs, nil, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		return bucket, item
	}
	bucket, object := createObject(7, 99)
	otherBucket, sentinel := createObject(8, 100)
	// A matching global fingerprint must never let another tenant move the live item.
	if _, err := repo.UpsertItem(8, 99, otherBucket, "object", object.Name, object.FullName, object.Attributes, nil, nil, nil); err == nil {
		t.Fatal("cross-tenant overwrite accepted")
	}
	live, found, err := repo.FindItemByFullName(7, 99, object.FullName)
	if err != nil || !found || live.NodeID != bucket.ID {
		t.Fatal("original object moved")
	}
	if err := repo.SoftDeleteMissingObjectItems(t.Context(), bucket, nil); err != nil {
		t.Fatal(err)
	}
	var deletion models.ObjectIndexDeletion
	if err := db.Take(&deletion, "fingerprint = ?", object.Fingerprint).Error; err != nil {
		t.Fatal(err)
	}
	deleted, found, err := repo.FindItemByFingerprintUnscoped(object.Fingerprint)
	if err != nil || !found || !deleted.DeletedAt.Valid || deletion.TenantID != 7 || deletion.EngineID != 99 || deletion.Confirmed || deletion.Restored || deletion.OperationID == "" || deletion.OperationID != deleted.IndexDeleteOperationID {
		t.Fatal("missing object and pending index deletion do not match")
	}
	if _, found, err := repo.FindItemByFullName(8, 100, sentinel.FullName); err != nil || !found {
		t.Fatalf("other tenant object: found=%v error=%v", found, err)
	}
	var count int64
	if err := db.Model(&models.ObjectIndexDeletion{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("unexpected deletion count %d: %v", count, err)
	}
	t.Logf("tenant=7 engine=99 table=scan_source.orders item_id=%d rescanned; tenant=8 isolated; one exact pending index deletion", firstID)
}
