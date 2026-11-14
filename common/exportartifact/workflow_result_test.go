package exportartifact

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/dataprotection"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestWorkflowExportStagesOnlyDigestBoundCompleteTableAndCleansFailedDispatch(t *testing.T) {
	for _, scenario := range []string{"success", "dispatch_failed", "expired", "different_columns"} {
		t.Run(scenario, func(t *testing.T) {
			var uploaded []byte
			puts, deletes := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					w.Header().Set("Content-Type", "application/xml")
					io.WriteString(w, `<LocationConstraint>us-east-1</LocationConstraint>`)
					return
				}
				if r.Method == "PUT" {
					puts++
					uploaded, _ = io.ReadAll(r.Body)
					w.Header().Set("ETag", `"a"`)
				}
				if r.Method == "DELETE" {
					deletes++
					w.WriteHeader(204)
				}
			}))
			defer server.Close()
			m, err := minio.New(strings.TrimPrefix(server.URL, "http://"), &minio.Options{Creds: credentials.NewStaticV4("test", "test-secret", ""), Region: "us-east-1"})
			if err != nil {
				t.Fatal(err)
			}
			db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			if err := EnsureStore(db, "sessions"); err != nil {
				t.Fatal(err)
			}
			transfer := &transferStub{}
			if scenario == "dispatch_failed" {
				transfer.err = io.ErrUnexpectedEOF
			}
			service := NewService(transfer, NewGormStore(db, "sessions"), m, "manager", "develop", "/exports")
			table := &plugin.WorkflowTableResult{SchemaVersion: "addp.workflow.table-result/v1", Columns: []string{"surface"}, Rows: []map[string]interface{}{{"surface": 42.375}}, RowCount: 1}
			data, _ := client.WorkflowResultExportRows(table)
			binding := &client.WorkflowResultExportSource{ExecutionID: uuid.NewString(), Attempt: 1, ExpiresAt: time.Now().Add(time.Minute), Columns: table.Columns, RowCount: 1, SHA256: client.WorkflowResultRowsDigest(data), Targets: []dataprotection.ResourceReference{{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: "source"}}}
			if scenario == "expired" {
				binding.ExpiresAt = time.Now().Add(-time.Second)
			}
			if scenario == "different_columns" {
				binding.Columns = []string{"other"}
			}
			result, err := service.Create(context.Background(), CreateRequest{TenantID: 7, UserID: 9, SourceRef: binding.ExecutionID, Format: format.FormatCSV, FileName: "areas", WorkflowTable: table, ExecutionConfig: client.TransferExecutionConfig{WorkflowResult: binding}})
			if scenario == "success" {
				if err != nil || result == nil || puts != 1 || deletes != 0 {
					t.Fatalf("result=%v err=%v put=%d delete=%d", result, err, puts, deletes)
				}
				// Signed MinIO streaming may frame payload bytes, so assert exact JSON content occurs.
				if !strings.Contains(string(uploaded), string(data)) {
					t.Fatal("staged input lost complete rows")
				}
				req := transfer.request
				if req.Config.WorkflowResult != binding || !strings.Contains(req.Config.Source.Locator, "/workflow-result.json?type=object") {
					t.Fatal("lost immutable binding")
				}
			} else if err == nil {
				t.Fatal("accepted invalid export")
			}
			if scenario == "dispatch_failed" && (puts != 1 || deletes != 1) {
				t.Fatal("failed dispatch retained private input", puts, deletes)
			}
			if (scenario == "expired" || scenario == "different_columns") && puts != 0 {
				t.Fatal("invalid input reached storage")
			}
		})
	}
}
