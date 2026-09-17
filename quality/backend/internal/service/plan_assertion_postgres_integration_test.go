package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	commonModels "github.com/addp/common/models"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationPostgresRelationalAssertions(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("requires standard PostgreSQL gate")
	}
	db, err := gorm.Open(postgres.Open(qualityServiceIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	schema := fmt.Sprintf("quality_assertion_%d", time.Now().UnixNano())
	if err = db.Exec(`CREATE SCHEMA "` + schema + `"`).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Exec(`DROP SCHEMA "` + schema + `" CASCADE`).Error })
	statements := []string{
		`CREATE TABLE "` + schema + `".people(person_id TEXT, actual BIGINT, "odd""column" TEXT)`,
		`CREATE TABLE "` + schema + `".details(person_id TEXT, activity_id TEXT, active BOOLEAN)`,
		`CREATE TABLE "` + schema + `".activities(activity_id TEXT, active BOOLEAN)`,
		`INSERT INTO "` + schema + `".people VALUES ('p1',2,'ok'),('p2',2,'ok'),('p3',0,NULL),('p4',NULL,'ok')`,
		`INSERT INTO "` + schema + `".details VALUES ('p1','a1',TRUE),('p1','a1',TRUE),('p1','a2',FALSE),('p2','a3',TRUE),('p2',NULL,NULL)`,
		`INSERT INTO "` + schema + `".activities VALUES ('a1',TRUE),('a2',FALSE),('a3',FALSE)`,
	}
	for _, sql := range statements {
		if err = db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	bindings := []PlanTableBinding{}
	for _, name := range []string{"people", "details", "activities"} {
		bindings = append(bindings, PlanTableBinding{Alias: name, Locator: fmt.Sprintf("addp://engine/1/path/%s/%s?type=table", schema, name)})
	}
	cases := []struct {
		name, params  string
		total, failed int64
	}{
		{"distinct_count", string(assertionPlanRule(t, countAssertion).Params), 4, 2},
		{"row_count", `{"table":"people","fields":{"key":"person_id"},"relations":{"detail":{"table":"details","fields":{"key":"person_id"}}},"assertion":{"op":"eq","args":[{"op":"count","relation":"detail","where":{"op":"eq","args":[{"op":"field","field":"key"},{"op":"field","relation":"detail","field":"key"}]}},{"op":"number","value":"0"}]}}`, 4, 2},
		{"nested_missing_or_mismatched", `{"table":"people","fields":{"key":"person_id"},"relations":{"detail":{"table":"details","fields":{"key":"person_id","item":"activity_id","active":"active"}},"activity":{"table":"activities","fields":{"item":"activity_id","active":"active"}}},"assertion":{"op":"not","args":[{"op":"exists","relation":"detail","where":{"op":"and","args":[{"op":"eq","args":[{"op":"field","field":"key"},{"op":"field","relation":"detail","field":"key"}]},{"op":"not","args":[{"op":"exists","relation":"activity","where":{"op":"and","args":[{"op":"eq","args":[{"op":"field","relation":"detail","field":"item"},{"op":"field","relation":"activity","field":"item"}]},{"op":"eq","args":[{"op":"field","relation":"detail","field":"active"},{"op":"field","relation":"activity","field":"active"}]}]}}]}]}}]}}`, 4, 1},
		{"null_unknown_fails", `{"table":"people","fields":{"actual":"actual"},"assertion":{"op":"gte","args":[{"op":"field","field":"actual"},{"op":"number","value":"0"}]}}`, 4, 1},
		{"numeric_not_lexical", `{"table":"people","assertion":{"op":"gt","args":[{"op":"number","value":"10"},{"op":"number","value":"2"}]}}`, 4, 0},
		{"big_integer_preserved", `{"table":"people","assertion":{"op":"ne","args":[{"op":"number","value":"9007199254740993"},{"op":"number","value":"9007199254740992"}]}}`, 4, 0},
		{"null_safe", `{"table":"people","fields":{"actual":"actual"},"assertion":{"op":"eq","args":[{"op":"field","field":"actual"},{"op":"value","value":null}]}}`, 4, 3},
		{"today", `{"table":"people","assertion":{"op":"eq","args":[{"op":"today"},{"op":"today"}]}}`, 4, 0},
		{"quoted_column", `{"table":"people","fields":{"value":"odd\"column"},"assertion":{"op":"eq","args":[{"op":"field","field":"value"},{"op":"value","value":"ok"}]}}`, 4, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rule := PlanRule{RuleKey: "00000000-0000-4000-8000-000000000001", Type: "relational_assertion", Severity: "warning", Params: json.RawMessage(tc.params)}
			// Include the actual persisted execution JSONMap decoding boundary.
			stored, err := (commonModels.JSONMap{"params": rule.Params}).Value()
			if err != nil {
				t.Fatal(err)
			}
			var reloaded commonModels.JSONMap
			if err := reloaded.Scan(stored); err != nil {
				t.Fatal(err)
			}
			rule.Params, err = json.Marshal(reloaded["params"])
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(PlanRuleDocument{SchemaVersion: planSchemaVersion, Rules: []PlanRule{rule}})
			if _, err := validatePlanContract(bindings, raw); err != nil {
				t.Fatal(err)
			}
			config := &planExecutionConfig{TableBindings: bindings, Rules: PlanRuleDocument{Rules: []PlanRule{rule}}}
			result, err := runPlan(context.Background(), db, config)
			if err != nil {
				t.Fatal(err)
			}
			if !result.Passed || result.Rules[0].TotalCount != tc.total || result.Rules[0].FailedCount != tc.failed {
				t.Fatalf("result=%#v rules=%#v", result, result.Rules)
			}
			if tc.failed > 0 {
				config.Rules.Rules[0].Severity = "error"
				result, err = runPlan(context.Background(), db, config)
				if err == nil || result == nil || result.Passed || result.Rules[0].FailedCount != tc.failed {
					t.Fatalf("missing error evidence: %#v %v", result, err)
				}
			}
		})
	}
	if err = db.Exec(`TRUNCATE "` + schema + `".people`).Error; err != nil {
		t.Fatal(err)
	}
	result, err := runPlan(context.Background(), db, &planExecutionConfig{TableBindings: bindings, Rules: PlanRuleDocument{Rules: []PlanRule{assertionPlanRule(t, countAssertion)}}})
	if err != nil || !result.Passed || result.Rules[0].TotalCount != 0 {
		t.Fatalf("empty table: %#v %v", result, err)
	}
}
