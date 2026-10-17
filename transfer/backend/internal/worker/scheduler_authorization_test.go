package worker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/repository"
	"github.com/addp/transfer/internal/service"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type scheduleServiceToken struct{}

func (scheduleServiceToken) PlatformToken(context.Context) (string, error) {
	return "unexpected_platform_token", nil
}

func (scheduleServiceToken) Token(context.Context, uint) (string, error) {
	return "addp_at_transfer_runtime_test", nil
}

func TestSchedulerRequiresResolvedCurrentDefinitionAndFreezesUserOrigin(t *testing.T) {
	for _, scenario := range []string{"valid", "missing", "denied", "tenant", "principal", "membership", "version", "definition", "concurrent_edit", "registration_changed"} {
		t.Run(scenario, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "execution.db")), &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			pool, _ := db.DB()
			pool.SetMaxOpenConns(1)
			t.Cleanup(func() { _ = pool.Close() })
			if err := db.Exec("ATTACH DATABASE ':memory:' AS transfer").Error; err != nil {
				t.Fatal(err)
			}
			if err := executiontest.EnsureSQLiteStore(db); err != nil {
				t.Fatal(err)
			}
			if err := db.Exec(`CREATE TABLE transfer.transfer_tasks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			apply_identity TEXT NOT NULL UNIQUE,
			tenant_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			description TEXT,
			task_type TEXT NOT NULL,
			config JSON,
			schedule TEXT,
			batch_size INTEGER,
			enabled BOOLEAN,
			auto_scan_metadata BOOLEAN,
			initial_metadata_scan_status TEXT NOT NULL DEFAULT '',
			initial_metadata_scan_claim_token TEXT NOT NULL DEFAULT '',
			initial_metadata_scan_lease_until DATETIME,
			initial_metadata_scan_attempt INTEGER NOT NULL DEFAULT 0,
			initial_metadata_scan_execution_id TEXT NOT NULL DEFAULT '',
			initial_metadata_scan_error TEXT NOT NULL DEFAULT '',
			status TEXT,
			desired_state TEXT NOT NULL DEFAULT 'stopped',
			progress REAL,
			authorization_subject_id INTEGER,
 authorization_definition_hash TEXT,
 authorization_principal_id INTEGER,
 authorization_membership_id INTEGER,
 authorization_version INTEGER,
 authorized_at DATETIME,
 created_by INTEGER,
			last_execution_id TEXT,
			last_execution_status TEXT,
			last_run_at DATETIME,
			next_run_at DATETIME,
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME
		)`).Error; err != nil {
				t.Fatal(err)
			}
			due, authorized := time.Now().Add(-time.Minute), time.Now().UTC().Truncate(time.Microsecond)
			id, actor, membership, version := int64(81), int64(33), int64(34), int64(3)
			task := models.TransferTask{Name: "scheduled", TaskType: "sync", TenantID: 7, ApplyIdentity: uuid.NewString(), Schedule: "0 */5 * * * *", Enabled: true, NextRunAt: &due, Status: models.TaskStatusIdle, BatchSize: 100, Config: models.JSONMap{"runtime": map[string]interface{}{"boundary": "bounded"}, "load": map[string]interface{}{"mode": "snapshot"}}, AuthorizationSubjectID: &id, AuthorizationPrincipalID: &actor, AuthorizationMembershipID: &membership, AuthorizationVersion: &version, AuthorizedAt: &authorized}
			hash, _ := task.ScheduleAuthorizationHash()
			task.AuthorizationDefinitionHash = &hash
			if scenario == "missing" {
				task.AuthorizationSubjectID = nil
			}
			if err := db.Create(&task).Error; err != nil {
				t.Fatal(err)
			}
			calls := 0
			gateOpen := true
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Header.Get("Authorization") != "Bearer addp_at_transfer_runtime_test" || r.URL.Path != "/api/v1/system/runtime/task-authorization-subjects/81/resolve" {
					t.Error("wrong runtime credential or route")
				}
				var req commonClient.TaskAuthorizationSubjectRequest
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
				}
				if req.OwnerModule != "transfer" || req.TaskType != "sync" || req.TaskRef != task.ApplyIdentity || req.DefinitionHash != hash {
					t.Error("wrong definition binding")
				}
				if scenario == "denied" {
					w.WriteHeader(403)
					return
				}
				subject := commonClient.TaskAuthorizationSubject{ID: "81", OwnerModule: "transfer", TaskType: "sync", TaskRef: task.ApplyIdentity, DefinitionHash: hash, TenantID: "7", PrincipalID: "33", TenantMembershipID: "34", AuthorizationVersion: "3", AuthorizedAt: authorized}
				switch scenario {
				case "tenant":
					subject.TenantID = "8"
				case "principal":
					subject.PrincipalID = "35"
				case "membership":
					subject.TenantMembershipID = "35"
				case "version":
					subject.AuthorizationVersion = "4"
				case "definition":
					subject.DefinitionHash = "stale"
				case "registration_changed":
					gateOpen = false
				case "concurrent_edit":
					if err := db.Model(&task).Update("batch_size", 200).Error; err != nil {
						t.Error(err)
					}
				}
				_ = json.NewEncoder(w).Encode(subject)
			}))
			defer server.Close()
			scheduler := NewScheduler(repository.NewTaskRepository(db))
			scheduler.SetClaimGate(func() bool { return gateOpen })
			scheduler.SetExecutionService(service.NewExecutionService(db, execution.NewTaskExecutionRepository(db)))
			scheduler.SetTaskSubjectResolver(commonClient.NewSystemServiceClient(server.URL, scheduleServiceToken{}, server.Client()))
			scheduler.runDueScheduledTasks(context.Background())
			var executions []execution.TaskExecution
			if err := db.Find(&executions).Error; err != nil {
				t.Fatal(err)
			}
			var stored models.TransferTask
			db.First(&stored, task.ID)
			if scenario != "valid" {
				if len(executions) != 0 || !stored.NextRunAt.Equal(due) || stored.LastExecutionID != nil {
					t.Fatal("rejection created an execution or advanced schedule")
				}
				if scenario == "missing" && calls != 0 {
					t.Fatal("missing local authorization reached System")
				}
				return
			}
			if len(executions) != 1 || calls != 1 {
				t.Fatalf("executions=%d calls=%d", len(executions), calls)
			}
			current := executions[0]
			if current.TriggeredBy == nil || *current.TriggeredBy != 33 || *current.ActorPrincipalID != 33 || *current.ActorTenantMembershipID != 34 || *current.IssuedAuthorizationVersion != 3 || current.ParentExecutionID != nil || current.TriggerType != "scheduled" || current.ExecutionAuthorizationID != nil || current.LeaseToken != nil || current.Status != "pending" || !stored.NextRunAt.After(due) {
				t.Fatal("scheduled source or state incorrect")
			}
			scheduler.runDueScheduledTasks(context.Background())
			var count int64
			db.Model(&execution.TaskExecution{}).Count(&count)
			if count != 1 {
				t.Fatal("schedule claimed twice")
			}
		})
	}
}
