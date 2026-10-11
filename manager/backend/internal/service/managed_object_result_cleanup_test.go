package service

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/addp/manager/internal/cog"
	"github.com/addp/manager/internal/repository"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type managedObjectResultDeleter interface {
	DeleteResult(context.Context, uint, uint) error
}

func TestManagedObjectResultCleanup(t *testing.T) {
	cases := []struct {
		table, object string
		service       func(*gorm.DB, *MinIOManagedObjectCleaner) managedObjectResultDeleter
	}{
		{"point_cloud_copc", "cloud.copc.laz", func(db *gorm.DB, cleaner *MinIOManagedObjectCleaner) managedObjectResultDeleter {
			svc := NewPointCloudCOPCTaskService(repository.NewPointCloudCOPCRepository(db))
			if cleaner != nil {
				svc.SetCleaner(cleaner)
			}
			return svc
		}},
		{"model_3d_glb", "model.glb", func(db *gorm.DB, cleaner *MinIOManagedObjectCleaner) managedObjectResultDeleter {
			svc := NewModel3DGLBTaskService(repository.NewModel3DGLBRepository(db))
			if cleaner != nil {
				svc.SetCleaner(cleaner)
			}
			return svc
		}},
		{"gaussian_splat_ksplat", "scene.ksplat", func(db *gorm.DB, cleaner *MinIOManagedObjectCleaner) managedObjectResultDeleter {
			svc := NewGaussianSplatKSplatTaskService(repository.NewGaussianSplatKSplatRepository(db))
			if cleaner != nil {
				svc.SetCleaner(cleaner)
			}
			return svc
		}},
	}
	for _, tc := range cases {
		t.Run(tc.table, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			sqlDB, err := db.DB()
			if err != nil {
				t.Fatal(err)
			}
			sqlDB.SetMaxOpenConns(1)
			t.Cleanup(func() { _ = sqlDB.Close() })
			if err := db.Exec("ATTACH DATABASE ':memory:' AS manager").Error; err != nil {
				t.Fatal(err)
			}
			table := "manager." + tc.table
			if err := db.Exec("CREATE TABLE " + table + ` (
				id INTEGER PRIMARY KEY, tenant_id INTEGER, task_id INTEGER, storage_ref TEXT,
				status TEXT, error_message TEXT, metadata TEXT, last_execution_id TEXT, updated_at DATETIME, deleted_at DATETIME)`).Error; err != nil {
				t.Fatal(err)
			}
			object := "/manager/tenant_7/preview/" + tc.object
			objects := map[string]string{
				object: "managed bytes", object + ".other-task": "other task",
				"/manager/tenant_8/preview/" + tc.object: "other tenant", "/business/source": "source bytes",
			}
			var objectMu sync.Mutex
			deleteDenied, deleteCalls := true, 0
			objectValue := func(key string) string {
				objectMu.Lock()
				defer objectMu.Unlock()
				return objects[key]
			}
			callCount := func() int {
				objectMu.Lock()
				defer objectMu.Unlock()
				return deleteCalls
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodDelete {
					t.Errorf("unexpected object request: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				objectMu.Lock()
				defer objectMu.Unlock()
				deleteCalls++
				if deleteDenied {
					w.Header().Set("Content-Type", "application/xml")
					w.WriteHeader(http.StatusForbidden)
					fmt.Fprint(w, `<Error><Code>AccessDenied</Code><Message>fixture deletion denied</Message></Error>`)
					return
				}
				delete(objects, r.URL.Path)
				w.WriteHeader(http.StatusNoContent)
			}))
			t.Cleanup(server.Close)
			client, err := minio.New(strings.TrimPrefix(server.URL, "http://"), &minio.Options{
				Creds: credentials.NewStaticV4("test", "test-secret", ""), Region: "us-east-1",
			})
			if err != nil {
				t.Fatal(err)
			}
			ref := cog.ObjectStorageRef("manager", "tenant_7/preview/"+tc.object)
			for _, row := range []struct {
				id, tenant, task uint
				ref, status      string
			}{
				{1, 7, 101, ref, "ready"},
				{2, 7, 102, cog.ObjectStorageRef("manager", "tenant_7/preview/"+tc.object+".other-task"), "ready"},
				{3, 8, 101, cog.ObjectStorageRef("manager", "tenant_8/preview/"+tc.object), "ready"},
				{4, 7, 103, "", "failed"},
				{5, 7, 104, "{invalid", "ready"},
			} {
				if err := db.Exec("INSERT INTO "+table+` (id, tenant_id, task_id, storage_ref, status, error_message, metadata, last_execution_id)
					VALUES (?, ?, ?, ?, ?, 'original detail', '{"kept":true}', 'execution-history')`, row.id, row.tenant, row.task, row.ref, row.status).Error; err != nil {
					t.Fatal(err)
				}
			}
			type resultState struct {
				Status, StorageRef, ErrorMessage, Metadata, LastExecutionID string
				TenantID, TaskID                                            uint
				UpdatedAt, DeletedAt                                        sql.NullTime
			}
			state := func(id uint) resultState {
				t.Helper()
				var row resultState
				if err := db.Table(table).Where("id = ?", id).Scan(&row).Error; err != nil {
					t.Fatal(err)
				}
				return row
			}
			before := state(1)
			otherTaskBefore, otherTenantBefore := state(2), state(3)
			ctx := context.Background()
			for _, unavailable := range []struct {
				name    string
				cleaner *MinIOManagedObjectCleaner
			}{
				{"missing cleaner", nil}, {"missing store", NewMinIOManagedObjectCleaner(nil, "manager")},
			} {
				t.Run(unavailable.name, func(t *testing.T) {
					if err := tc.service(db, unavailable.cleaner).DeleteResult(ctx, 1, 7); err == nil || !strings.Contains(err.Error(), "configured") {
						t.Errorf("unavailable cleanup must fail diagnostically, got %v", err)
					}
					if got := state(1); got != before {
						t.Errorf("failed cleanup changed result: %#v", got)
					}
					if callCount() != 0 || objectValue(object) != "managed bytes" {
						t.Error("unavailable cleanup touched object store")
					}
				})
				if t.Failed() {
					return
				}
			}
			svc := tc.service(db, NewMinIOManagedObjectCleaner(client, "manager"))
			invalidBefore := state(5)
			if err := svc.DeleteResult(ctx, 5, 7); err == nil || !strings.Contains(err.Error(), "storage_ref") || state(5) != invalidBefore {
				t.Fatalf("invalid storage reference must preserve the result: %v", err)
			}
			for _, id := range []uint{3, 101} {
				if err := svc.DeleteResult(ctx, id, 7); err == nil {
					t.Fatal("foreign tenant or task ID accepted as this result")
				}
			}
			if callCount() != 0 {
				t.Fatal("foreign tenant or task ID reached object deletion")
			}
			if err := svc.DeleteResult(ctx, 1, 7); err == nil || !strings.Contains(err.Error(), tc.object) || !strings.Contains(err.Error(), "fixture deletion denied") {
				t.Fatalf("missing object deletion diagnostic: %v", err)
			}
			if state(1) != before || objectValue(object) != "managed bytes" {
				t.Fatal("denied deletion changed result or object")
			}
			objectMu.Lock()
			deleteDenied = false
			objectMu.Unlock()
			if err := svc.DeleteResult(ctx, 1, 7); err != nil {
				t.Fatal(err)
			}
			if got := state(1); got.Status != "deleted" || !got.DeletedAt.Valid {
				t.Fatalf("retry did not delete result: %#v", got)
			}
			objectMu.Lock()
			_, exists := objects[object]
			if exists || deleteCalls != 2 || len(objects) != 3 {
				t.Errorf("retry did not delete only its object: calls=%d objects=%v", deleteCalls, objects)
			}
			objectMu.Unlock()
			if state(2) != otherTaskBefore || state(3) != otherTenantBefore ||
				objectValue(object+".other-task") != "other task" ||
				objectValue("/manager/tenant_8/preview/"+tc.object) != "other tenant" ||
				objectValue("/business/source") != "source bytes" {
				t.Fatal("cleanup changed another result or source")
			}
			if err := svc.DeleteResult(ctx, 1, 7); err == nil || callCount() != 2 {
				t.Fatal("deleted result was reused for another object deletion")
			}
			if err := tc.service(db, nil).DeleteResult(ctx, 4, 7); err != nil || !state(4).DeletedAt.Valid || callCount() != 2 {
				t.Fatalf("result without an object needs no cleaner: %v", err)
			}
		})
	}
}
