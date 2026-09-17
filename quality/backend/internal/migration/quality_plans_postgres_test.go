package migration

import (
	"context"
	"encoding/json"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/quality/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"os"
	"testing"
)

// Every legacy-schema fixture is rolled back; this never resets a development database.
func TestIntegrationPostgresQualityPlanMigration(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("PostgreSQL integration is disabled")
	}
	db, err := gorm.Open(postgres.Open(qualityMigrationIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = commonExecution.EnsureStore(db); err != nil {
		t.Fatal(err)
	}
	if err = NewRunner(db).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	catalog, err := ReadCatalog(EmbeddedSQL, DefaultMigrationsRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, active := range []bool{false, true} {
		t.Run(map[bool]string{false: "preserves_definitions_issues_history", true: "refuses_active_execution"}[active], func(t *testing.T) {
			tx := db.Begin()
			if tx.Error != nil {
				t.Fatal(tx.Error)
			}
			defer tx.Rollback()
			exec := func(q string, args ...interface{}) {
				t.Helper()
				if err := tx.Exec(q, args...).Error; err != nil {
					t.Fatal(err)
				}
			}
			exec("DROP SCHEMA quality CASCADE")
			exec("CREATE SCHEMA quality")
			for _, file := range catalog.Files[:9] {
				exec(file.Contents)
			}
			const key = "00000000-0000-4000-8000-000000000001"
			exec(`INSERT INTO quality.check_tasks(id,tenant_id,name,engine_id,schema_name,table_name,created_by) VALUES (4,7,'legacy check',2,'public','customers',1)`)
			rules := `{"schema_version":"addp.quality.rules/v1","rules":[{"rule_key":"` + key + `","type":"not_null","enabled":true,"severity":"error","message":"required","params":{}}]}`
			exec(`INSERT INTO quality.rule_applications(id,tenant_id,element_id,element_revision_id,engine_id,schema_name,table_name,column_name,rule_config,enabled,created_by) VALUES (10,7,3,31,2,'public','customers','id',?::jsonb,true,1),(11,7,3,31,2,'public','orphan','id',?::jsonb,false,1)`, rules, rules)
			exec(`INSERT INTO quality.issues(tenant_id,execution_id,last_execution_id,rule_application_id,rule_key,rule_type,severity,column_name,table_name,schema_name,engine_id,failed_count,total_count,pass_rate) VALUES (7,'legacy-history','legacy-history',10,?::uuid,'not_null','error','id','customers','public',2,1,2,50)`, key)
			exec(`INSERT INTO quality.data_validation_tasks(id,tenant_id,code,name,version,table_bindings,assertions,created_by,updated_by) VALUES (4,7,'legacy_validation','validation',3,?::jsonb,?::jsonb,1,1)`, `[{"alias":"target","locator":"addp://engine/2/path/public/customers?type=table"}]`, `{"schema_version":"addp.quality.data-validation/v1","assertions":[{"assertion_key":"`+key+`","type":"row_count","severity":"error","params":{"table":"target","min":1}}]}`)
			historyID := uuid.NewString()
			status := "success"
			if active {
				status = "pending"
			}
			exec(`INSERT INTO common.task_executions(tenant_id,execution_id,module,task_type,source,source_task_id,status,trigger_type,execution_config,metadata,created_at,updated_at) VALUES (7,?,'quality','check', 'quality', '4',?,'manual','{"legacy":true}','{"score":50}',now(),now())`, historyID, status)
			if active {
				if err := tx.Exec(catalog.Files[9].Contents).Error; err == nil {
					t.Fatal("active execution allowed destructive migration")
				}
				return
			}
			exec(catalog.Files[9].Contents)
			if err := migrateQualityPlans(tx); err != nil {
				t.Fatal(err)
			}
			var plans []struct {
				models.QualityPlan
				Rules json.RawMessage
			}
			if err := tx.Table("quality.plans").Order("id").Find(&plans).Error; err != nil {
				t.Fatal(err)
			}
			if len(plans) != 3 || plans[0].ID != 8 || plans[1].ID != 9 || plans[2].ID != 10 || plans[1].Version != 3 {
				t.Fatalf("plans=%+v", plans)
			}
			var doc struct {
				Rules []struct {
					RuleKey  string `json:"rule_key"`
					Disabled bool
					Source   struct {
						ElementRevisionID int64 `json:"element_revision_id"`
					}
				}
			}
			if err := json.Unmarshal(plans[0].Rules, &doc); err != nil {
				t.Fatal(err)
			}
			expected := uuid.NewSHA1(uuid.NameSpaceOID, []byte("quality-plan:10:"+key)).String()
			if len(doc.Rules) != 1 || doc.Rules[0].RuleKey != expected || doc.Rules[0].Source.ElementRevisionID != 31 {
				t.Fatalf("rules=%s", plans[0].Rules)
			}
			if err := json.Unmarshal(plans[2].Rules, &doc); err != nil || !doc.Rules[0].Disabled {
				t.Fatalf("disabled orphan=%s err=%v", plans[2].Rules, err)
			}
			var issue models.Issue
			if err := tx.First(&issue).Error; err != nil {
				t.Fatal(err)
			}
			if issue.PlanID != 8 || issue.RuleKey != expected || issue.LastExecutionID != "legacy-history" {
				t.Fatalf("issue=%+v", issue)
			}
			var history commonExecution.TaskExecution
			if err := tx.Where("execution_id=?", historyID).First(&history).Error; err != nil {
				t.Fatal(err)
			}
			if history.TaskType != "check" || history.ExecutionConfig["legacy"] != true || history.Metadata["score"] != float64(50) {
				t.Fatalf("history was rewritten: %+v", history)
			}
			var count int64
			if err := tx.Raw(`SELECT count(*) FROM information_schema.tables WHERE table_schema='quality' AND table_name IN ('check_tasks','rule_applications','data_validation_tasks')`).Scan(&count).Error; err != nil || count != 0 {
				t.Fatalf("old tables=%d err=%v", count, err)
			}
			// The second transition extracts reusable definitions without changing
			// check identities, source evidence or historical execution payloads.
			exec("SAVEPOINT active_rule_migration")
			exec("UPDATE common.task_executions SET status='pending' WHERE execution_id=?", historyID)
			if err := tx.Exec(catalog.Files[10].Contents).Error; err == nil {
				t.Fatal("active execution allowed rule extraction")
			}
			exec("ROLLBACK TO SAVEPOINT active_rule_migration")
			exec(catalog.Files[10].Contents)
			var extracted []models.QualityRule
			if err := tx.Order("id").Find(&extracted).Error; err != nil {
				t.Fatal(err)
			}
			if len(extracted) != 3 || extracted[0].RevisionNo != 1 || extracted[0].Source.ElementRevisionID != 31 {
				t.Fatalf("extracted rules=%+v", extracted)
			}
			var check models.PlanCheckItem
			if err := tx.Where("tenant_id=7 AND plan_id=8").First(&check).Error; err != nil {
				t.Fatal(err)
			}
			if check.RuleKey != expected || check.RuleID != extracted[0].ID || check.Bindings.Column != "id" || check.RevisionNo != 1 {
				t.Fatalf("check identity changed=%+v", check)
			}
			if string(extracted[0].Params) != "{}" {
				t.Fatalf("target leaked into reusable rule: %s", extracted[0].Params)
			}
			if err := tx.Raw(`SELECT count(*) FROM information_schema.columns WHERE table_schema='quality' AND table_name='plans' AND column_name='rules'`).Scan(&count).Error; err != nil || count != 0 {
				t.Fatalf("old embedded column remains: %d %v", count, err)
			}
			exec(catalog.Files[11].Contents)
			exec(`UPDATE quality.plans SET owner_domain_id=42 WHERE tenant_id=7 AND id=8`)
			var before models.Issue
			if err := tx.Select("id, owner_domain_id, updated_at, last_execution_id").First(&before).Error; err != nil {
				t.Fatal(err)
			}
			// The forward-only calibration must be idempotent and preserve audit facts.
			exec(catalog.Files[12].Contents)
			exec(catalog.Files[12].Contents)
			var after models.Issue
			if err := tx.Select("id, owner_domain_id, updated_at, last_execution_id").First(&after).Error; err != nil {
				t.Fatal(err)
			}
			if after.OwnerDomainID == nil || *after.OwnerDomainID != 42 || !after.UpdatedAt.Equal(before.UpdatedAt) || after.LastExecutionID != before.LastExecutionID {
				t.Fatalf("issue ownership calibration lost audit facts: %+v", after)
			}
			if err := tx.Where("execution_id=?", historyID).First(&history).Error; err != nil {
				t.Fatal(err)
			}
			if _, recorded := history.ExecutionConfig["owner_domain_id"]; recorded {
				t.Fatal("historical ownership was fabricated")
			}
			exec(`UPDATE quality.plans SET owner_domain_id=NULL WHERE tenant_id=7 AND id=8`)
			exec(catalog.Files[12].Contents)
			after = models.Issue{}
			if err := tx.Select("id, owner_domain_id, updated_at, last_execution_id").First(&after).Error; err != nil || after.OwnerDomainID != nil {
				t.Fatalf("public calibration: %+v %v", after, err)
			}
		})
	}
}
