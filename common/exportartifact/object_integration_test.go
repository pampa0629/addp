package exportartifact

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/format"
	"github.com/addp/common/infra/objectstore"
	objectbootstrap "github.com/addp/common/infra/objectstore/bootstrap"
	"github.com/google/uuid"
	sdk "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Real PG controls and real MinIO bytes. Transfer scheduling stays stubbed here;
// cross-process worker/Gateway evidence belongs to T4.
func TestExportAreaAgainstMinIO(t *testing.T) {
	if os.Getenv("ADDP_MANAGER_OBJECT_INTEGRATION") != "1" {
		t.Skip("requires make test-manager-postgres")
	}
	ctx := t.Context()
	db, err := gorm.Open(postgres.Open(os.Getenv("MANAGER_POSTGRES_TEST_DSN")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	table := "manager.objects_" + uuid.NewString()[:8]
	if err := EnsureStore(db, table); err != nil {
		t.Fatal(err)
	}
	defer db.Migrator().DropTable(table)
	raw, err := sdk.New(os.Getenv("MANAGER_OBJECT_TEST_ENDPOINT"), &sdk.Options{Creds: credentials.NewStaticV4(os.Getenv("MANAGER_OBJECT_TEST_USER"), os.Getenv("MANAGER_OBJECT_TEST_PASSWORD"), "")})
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.MakeBucket(ctx, "manager", sdk.MakeBucketOptions{}); err != nil && sdk.ToErrorResponse(err).Code != "BucketAlreadyOwnedByYou" {
		t.Fatal(err)
	}
	area, err := objectbootstrap.OpenContentArea(ctx, objectstore.Manager)
	if err != nil {
		t.Fatal(err)
	}
	store := NewGormStore(db, table)
	current := true
	transfer := &transferStub{result: &client.TransferExecutionResponse{Status: StatusSuccess, ExportProtectionCurrent: &current}}
	for _, owner := range []string{"manager", "develop"} {
		t.Run(owner, func(t *testing.T) {
			svc := NewService(transfer, store, area, objectstore.Manager, owner, "/exports")
			session, err := svc.Create(ctx, CreateRequest{TenantID: 7, UserID: 9, SourceRef: "query_" + owner, Format: format.FormatCSV, FileName: "rows"})
			if err != nil {
				t.Fatal(err)
			}
			row, err := store.Get(ctx, session.ID, 7, 9)
			if err != nil {
				t.Fatal(err)
			}
			key, err := InfraObjectPath(row.TargetLocator, objectstore.Manager)
			if err != nil {
				t.Fatal(err)
			}
			data := []byte("name,value\n汉字,42\n")
			if err := area.Put(ctx, key, bytes.NewReader(data), int64(len(data)), "text/csv"); err != nil {
				t.Fatal(err)
			}
			for _, who := range [][2]uint{{8, 9}, {7, 10}} {
				if _, err := svc.Open(ctx, row.ID, who[0], who[1], nil); err == nil {
					t.Fatal("foreign identity opened export")
				}
			}
			file, err := svc.Open(ctx, row.ID, 7, 9, nil)
			if err != nil {
				t.Fatal(err)
			}
			got, err := io.ReadAll(file.Reader)
			_ = file.Reader.Close()
			if err != nil || !bytes.Equal(got, data) {
				t.Fatalf("bytes %q %v", got, err)
			}
			current = false
			if _, err := svc.Open(ctx, row.ID, 7, 9, nil); !errors.Is(err, ErrProtectionChanged) {
				t.Fatalf("stale protection %v", err)
			}
			current = true
			prefix, err := SessionObjectPrefix(row, objectstore.Manager)
			if err != nil {
				t.Fatal(err)
			}
			sentinel := prefix + "-outside/sentinel.csv"
			if err := area.Put(ctx, sentinel, bytes.NewReader(data), int64(len(data)), "text/csv"); err != nil {
				t.Fatal(err)
			}
			defer area.Delete(ctx, sentinel)
			if err := db.Table(table).Where("id=?", row.ID).Update("updated_at", time.Now().Add(-72*time.Hour)).Error; err != nil {
				t.Fatal(err)
			}
			result, err := CleanupExpiredOnce(ctx, store, area, objectstore.Manager, CleanupOptions{SuccessRetention: time.Hour}, time.Now())
			if err != nil || result.DeletedSessions != 1 || result.DeletedObjects != 1 {
				t.Fatalf("cleanup %+v %v", result, err)
			}
			if refs, err := area.List(ctx, prefix+"/"); err != nil || len(refs) != 0 {
				t.Fatalf("remaining %+v %v", refs, err)
			}
			if _, err := area.Stat(ctx, sentinel); err != nil {
				t.Fatalf("outside sentinel removed: %v", err)
			}
		})
	}
	t.Run("shapefile_zip", func(t *testing.T) {
		svc := NewService(transfer, store, area, objectstore.Manager, "manager", "/exports")
		response, err := svc.Create(ctx, CreateRequest{TenantID: 7, UserID: 9, SourceRef: "multi", Format: format.FormatShapefile, FileName: "roads"})
		if err != nil {
			t.Fatal(err)
		}
		session, err := store.Get(ctx, response.ID, 7, 9)
		if err != nil {
			t.Fatal(err)
		}
		prefix, err := SessionObjectPrefix(session, objectstore.Manager)
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]string{"roads.shp": "shape-bytes", "roads.dbf": "attribute-bytes", "roads.shx": "index-bytes"}
		refs := []map[string]interface{}{}
		for name, value := range want {
			key := prefix + "/" + name
			if err := area.Put(ctx, key, bytes.NewBufferString(value), int64(len(value)), "application/octet-stream"); err != nil {
				t.Fatal(err)
			}
			refs = append(refs, map[string]interface{}{"path": "manager/" + key, "entry": name, "required": true, "primary": name == "roads.shp", "role": "main"})
		}
		transfer.result.Metadata = map[string]interface{}{"target_refs": refs}
		defer func() { transfer.result.Metadata = nil }()
		file, err := svc.Open(ctx, session.ID, 7, 9, nil)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(file.Reader)
		_ = file.Reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatal(err)
		}
		if len(archive.File) != len(want) {
			t.Fatalf("entries %d", len(archive.File))
		}
		for _, entry := range archive.File {
			r, err := entry.Open()
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(r)
			_ = r.Close()
			if err != nil || string(data) != want[entry.Name] {
				t.Fatalf("entry %s %q %v", entry.Name, data, err)
			}
		}
		if err := db.Table(table).Where("id=?", session.ID).Update("updated_at", time.Now().Add(-72*time.Hour)).Error; err != nil {
			t.Fatal(err)
		}
		result, err := CleanupExpiredOnce(ctx, store, area, objectstore.Manager, CleanupOptions{SuccessRetention: time.Hour}, time.Now())
		if err != nil || result.DeletedObjects != 3 || result.DeletedSessions != 1 {
			t.Fatalf("multi cleanup %+v %v", result, err)
		}
	})
	legacy := &Session{TenantID: 7, UserID: 9, SourceRef: "historical", Format: "csv", FileName: "old.csv", TargetLocator: "addp-infra://minio/manager/private/old.csv?type=object", TransferExecutionID: uuid.NewString(), ExecutionRequestDigest: "frozen-request", Status: StatusSuccess}
	if err := store.Create(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	if err := EnsureStore(db, table); err == nil {
		t.Fatal("upgrade accepted uncleared legacy export")
	}
	stored, err := store.Get(ctx, legacy.ID, 7, 9)
	if err != nil || stored.ExecutionRequestDigest != legacy.ExecutionRequestDigest || stored.TargetLocator != legacy.TargetLocator {
		t.Fatalf("frozen session changed %+v %v", stored, err)
	}
	if err := store.Delete(ctx, legacy.ID, 7, 9); err != nil {
		t.Fatal(err)
	}
	if err := EnsureStore(db, table); err != nil {
		t.Fatal(err)
	}
}
