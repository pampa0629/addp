package exportartifact

import (
	"encoding/json"
	"errors"
	"fmt"
	commonAPI "github.com/addp/common/api"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/client"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestExportDeliveryRechecksProtectionBeforeStatusAndObjectRead(t *testing.T) {
	objectReads := 0
	objects := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		objectReads++
		w.Header().Set("Last-Modified", "Mon, 02 Jan 2006 15:04:05 GMT")
		w.Header().Set("Content-Length", "12")
		w.Header().Set("Content-Type", "text/csv")
		if r.Method != http.MethodHead {
			_, _ = io.WriteString(w, "phone\n*****\n")
		}
	}))
	defer objects.Close()
	clientMinIO, err := minio.New(strings.TrimPrefix(objects.URL, "http://"), &minio.Options{Creds: credentials.NewStaticV4("test", "test-secret", ""), Region: "us-east-1"})
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureStore(db, "exports"); err != nil {
		t.Fatal(err)
	}
	store := NewGormStore(db, "exports")
	valid := true
	transfer := &transferStub{result: &client.TransferExecutionResponse{Status: StatusSuccess, ExportProtectionCurrent: &valid}}
	svc := NewService(transfer, store, clientMinIO, "manager", "manager", "/exports")
	sessionCount := 0
	newSession := func() *Session {
		sessionCount++
		item := &Session{TenantID: 7, UserID: 9, SourceRef: "source", Format: "csv", FileName: "data.csv", TargetLocator: "addp-infra://minio/manager/tenant_7/export/manager/session/data.csv?type=object", Status: StatusSuccess, TransferExecutionID: fmt.Sprintf("execution-%d", sessionCount)}
		if err := store.Create(t.Context(), item); err != nil {
			t.Fatal(err)
		}
		return item
	}
	item := newSession()
	if _, err := svc.Get(t.Context(), item.ID, 7, 9, nil); err != nil {
		t.Fatal(err)
	}
	file, err := svc.Open(t.Context(), item.ID, 7, 9, nil)
	if err != nil {
		t.Fatal(err)
	}
	bytes, err := io.ReadAll(file.Reader)
	_ = file.Reader.Close()
	if err != nil || string(bytes) != "phone\n*****\n" {
		t.Fatalf("unexpected CSV: %q %v", bytes, err)
	}
	initialReads := objectReads
	for _, state := range []string{"changed", "missing", "wrong_execution"} {
		t.Run(state, func(t *testing.T) {
			current := true
			transfer.result = &client.TransferExecutionResponse{Status: StatusSuccess, ExportProtectionCurrent: &current}
			switch state {
			case "changed":
				current = false
			case "missing":
				transfer.result.ExportProtectionCurrent = nil
			case "wrong_execution":
				transfer.result.ExecutionID = "another"
			}
			if _, err := svc.Get(t.Context(), item.ID, 7, 9, nil); !errors.Is(err, ErrProtectionChanged) {
				t.Fatalf("status allowed stale protection: %v", err)
			}
			if _, err := svc.Open(t.Context(), item.ID, 7, 9, nil); !errors.Is(err, ErrProtectionChanged) {
				t.Fatalf("file allowed stale protection: %v", err)
			}
			if objectReads != initialReads {
				t.Fatal("stale file reached object storage")
			}
		})
	}
	transfer.result = &client.TransferExecutionResponse{Status: StatusSuccess, ExportProtectionCurrent: &valid}
	fresh := newSession()
	if _, err := svc.Get(t.Context(), fresh.ID, 7, 9, nil); err != nil {
		t.Fatal("fresh export rejected", err)
	}
	freshFile, err := svc.Open(t.Context(), fresh.ID, 7, 9, nil)
	if err != nil {
		t.Fatal("fresh file unavailable", err)
	}
	freshBytes, err := io.ReadAll(freshFile.Reader)
	_ = freshFile.Reader.Close()
	if err != nil || string(freshBytes) != "phone\n*****\n" {
		t.Fatal("fresh CSV changed", err)
	}
	reads := transfer.reads
	if _, err := svc.Get(t.Context(), item.ID, 7, 10, nil); !errors.Is(err, ErrSessionNotFound) {
		t.Fatal(err)
	}
	if reads != transfer.reads {
		t.Fatal("cross-user session reached Transfer")
	}
}

func TestExportProtectionChangedHasBilingualConflictAndNoStore(t *testing.T) {
	for _, lang := range []string{"zh-cn", "en"} {
		router := gin.New()
		router.GET("/file", func(c *gin.Context) {
			ctx, err := commoni18n.WithLanguage(c.Request.Context(), lang)
			if err != nil {
				t.Fatal(err)
			}
			c.Request = c.Request.WithContext(ctx)
			commonAPI.NoStore(c)
			if !WriteProtectionError(c, fmt.Errorf("wrapped: %w", ErrProtectionChanged)) {
				t.Fatal("unhandled protection conflict")
			}
		})
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest("GET", "/file", nil))
		var body map[string]string
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if recorder.Code != 409 || recorder.Header().Get("Cache-Control") != "no-store" || body["error_code"] != "export_protection_changed" {
			t.Fatal("wrong refusal contract", recorder.Code, body)
		}
		wanted := "重新导出"
		if lang == "en" {
			wanted = "Export again"
		}
		if !strings.Contains(body["error"], wanted) {
			t.Fatal("missing re-export advice", body)
		}
	}
}
