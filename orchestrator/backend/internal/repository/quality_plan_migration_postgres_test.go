package repository

import (
	"encoding/json"
	commonExecution "github.com/addp/common/execution"
	migrations "github.com/addp/orchestrator/migrations"
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestIntegrationPostgresQualityPlanReferences(t *testing.T) {
	db := openOrchestratorMigrationIntegrationDB(t)
	if err := commonExecution.EnsureStore(db); err != nil {
		t.Fatal(err)
	}
	raw, err := migrations.FS.ReadFile("005_quality_plan_references.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(string(raw), "BEGIN;")), "COMMIT;"))
	for _, active := range []bool{false, true} {
		t.Run(map[bool]string{false: "rewrites_only_quality_steps", true: "blocks_active_workflow"}[active], func(t *testing.T) {
			tx := db.Begin()
			defer tx.Rollback()
			if tx.Error != nil {
				t.Fatal(tx.Error)
			}
			ensureOrchestratorMigrationTestTables(t, tx)
			name := "quality-plan-" + uuid.NewString()
			insertOrchestratorMigrationTestRow(t, tx, name, `[{"id":"compute","provider":"develop","task_type":"query","task_id":4},{"id":"check","provider":"quality","task_type":"check","task_id":4,"depends_on":["compute"],"parameters":{}},{"id":"validation","provider":"quality","task_type":"data_validation","task_id":4,"depends_on":["check"]}]`)
			if active {
				if err := tx.Exec(`INSERT INTO common.task_executions(tenant_id,execution_id,module,task_type,source,status,trigger_type,created_at,updated_at) VALUES (7,?,'orchestrator','orchestration','orchestrator','running','manual',now(),now())`, uuid.NewString()).Error; err != nil {
					t.Fatal(err)
				}
				if err := tx.Exec(sql).Error; err == nil {
					t.Fatal("active workflow allowed migration")
				}
				return
			}
			if err := tx.Exec(sql).Error; err != nil {
				t.Fatal(err)
			}
			if err := tx.Exec(sql).Error; err != nil {
				t.Fatalf("not idempotent: %v", err)
			}
			var text string
			if err := tx.Raw("SELECT steps::text FROM orchestrator.orchestrations WHERE name=?", name).Scan(&text).Error; err != nil {
				t.Fatal(err)
			}
			var steps []struct {
				ID       string
				Provider string
				TaskType string   `json:"task_type"`
				TaskID   int64    `json:"task_id"`
				Depends  []string `json:"depends_on"`
			}
			if err := json.Unmarshal([]byte(text), &steps); err != nil {
				t.Fatal(err)
			}
			if len(steps) != 3 || steps[0].TaskType != "query" || steps[0].TaskID != 4 || steps[1].TaskType != "quality_plan" || steps[1].TaskID != 8 || steps[2].TaskID != 9 || steps[1].Depends[0] != "compute" || steps[2].Depends[0] != "check" {
				t.Fatalf("steps=%s", text)
			}
		})
	}
}
