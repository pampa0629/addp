// Package testsupport provides explicit fixtures for the current Quality schema.
package testsupport

import (
	"encoding/json"
	"fmt"
	"github.com/addp/quality/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"strings"
	"testing"
	"time"
)

func EnsureRuleTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, q := range []string{
		`CREATE TABLE quality.standard_reference_guards (id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, resource_type TEXT NOT NULL, resource_id INTEGER NOT NULL, state TEXT NOT NULL DEFAULT 'open', created_at DATETIME, updated_at DATETIME, UNIQUE(tenant_id,resource_type,resource_id))`,
		`CREATE TABLE quality.rules (id INTEGER PRIMARY KEY AUTOINCREMENT,tenant_id INTEGER NOT NULL,owner_domain_id INTEGER,code TEXT NOT NULL,version INTEGER NOT NULL,revision_no INTEGER NOT NULL,name TEXT NOT NULL,description TEXT,type TEXT NOT NULL,params JSON NOT NULL,source JSON,created_by INTEGER,updated_by INTEGER,created_at DATETIME,updated_at DATETIME,UNIQUE(tenant_id,code))`,
		`CREATE TABLE quality.rule_revisions (tenant_id INTEGER NOT NULL,rule_id INTEGER NOT NULL,revision_no INTEGER NOT NULL,name TEXT,description TEXT,type TEXT,params JSON,source JSON,created_at DATETIME,created_by INTEGER,PRIMARY KEY(tenant_id,rule_id,revision_no))`,
		`CREATE TABLE quality.plan_check_items (tenant_id INTEGER NOT NULL,plan_id INTEGER NOT NULL,rule_key TEXT NOT NULL,position INTEGER,rule_id INTEGER NOT NULL,revision_no INTEGER NOT NULL,bindings JSON,severity TEXT,disabled BOOLEAN,PRIMARY KEY(tenant_id,plan_id,rule_key),UNIQUE(tenant_id,plan_id,position))`,
	} {
		if err := db.Exec(q).Error; err != nil {
			t.Fatal(err)
		}
	}
}

// EnsureIssueAcceptance extends hand-written SQLite issue fixtures to the
// current schema. PostgreSQL tests always use the real owner migrations.
func EnsureIssueAcceptance(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, sql := range []string{
		`ALTER TABLE quality.issues ADD COLUMN version INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE quality.issues ADD COLUMN evidence JSON`,
		`ALTER TABLE quality.issues ADD COLUMN accepted_keys JSON`,
		`ALTER TABLE quality.issues ADD COLUMN evidence_reason TEXT NOT NULL DEFAULT 'not_observed'`,
		`ALTER TABLE quality.issues ADD COLUMN accepted_count INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE quality.issues ADD COLUMN pending_count INTEGER NOT NULL DEFAULT 0`,
		`CREATE TABLE quality.issue_actions (id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, issue_id INTEGER NOT NULL, plan_id INTEGER NOT NULL, execution_id TEXT NOT NULL, action TEXT NOT NULL, actor_id INTEGER, note TEXT NOT NULL, accepted_count INTEGER NOT NULL DEFAULT 0, evidence JSON, created_at DATETIME NOT NULL)`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
}

// SeedPlanRules accepts execution-rule fixtures, splits their constraints and
// targets, and persists real independent rule revisions. No production API
// accepts this fixture shape.
func SeedPlanRules(t *testing.T, db *gorm.DB, plan *models.QualityPlan) {
	t.Helper()
	var document struct {
		Rules []struct {
			RuleKey  string                 `json:"rule_key"`
			Type     string                 `json:"type"`
			Name     string                 `json:"name"`
			Severity string                 `json:"severity"`
			Disabled bool                   `json:"disabled"`
			Source   *models.RuleSource     `json:"source"`
			Params   map[string]interface{} `json:"params"`
		}
	}
	if err := json.Unmarshal(plan.Rules, &document); err != nil {
		t.Fatal(err)
	}
	plan.CheckItems = []models.PlanCheckItem{}
	for _, r := range document.Rules {
		b := map[string]interface{}{}
		for _, key := range []string{"table", "column", "columns", "reference_table", "reference_columns"} {
			if value, ok := r.Params[key]; ok {
				b[key] = value
				delete(r.Params, key)
			}
		}
		if r.Type == "predicate_implication" {
			for _, key := range []string{"when", "then"} {
				c := r.Params[key].(map[string]interface{})
				b[key+"_column"] = c["column"]
				delete(c, "column")
			}
		}
		bindingRaw, _ := json.Marshal(b)
		var binding models.CheckBindings
		_ = json.Unmarshal(bindingRaw, &binding)
		params, _ := json.Marshal(r.Params)
		name := r.Name
		if name == "" {
			name = r.Type
		}
		now := time.Now().UTC()
		rule := models.QualityRule{TenantID: plan.TenantID, Code: "fixture_" + strings.ReplaceAll(uuid.NewString(), "-", ""), Version: 1, RevisionNo: 1, RuleContent: models.RuleContent{Name: name, Type: r.Type, Params: params, Source: r.Source}, CreatedBy: 1, UpdatedBy: 1, CreatedAt: now, UpdatedAt: now}
		if err := db.Create(&rule).Error; err != nil {
			t.Fatal(err)
		}
		revision := models.RuleRevision{TenantID: plan.TenantID, RuleID: rule.ID, RevisionNo: 1, RuleContent: rule.RuleContent, CreatedAt: now, CreatedBy: 1, LatestRevisionNo: 1}
		if err := db.Create(&revision).Error; err != nil {
			t.Fatal(err)
		}
		plan.CheckItems = append(plan.CheckItems, models.PlanCheckItem{TenantID: plan.TenantID, PlanID: plan.ID, Position: len(plan.CheckItems), RuleKey: r.RuleKey, RuleID: rule.ID, RevisionNo: 1, Bindings: binding, Severity: r.Severity, Disabled: r.Disabled, Rule: &revision})
		if db.Dialector.Name() == "postgres" {
			t.Cleanup(func() {
				_ = db.Where("tenant_id=? AND rule_id=?", plan.TenantID, rule.ID).Delete(&models.PlanCheckItem{}).Error
				_ = db.Where("tenant_id=? AND rule_id=?", plan.TenantID, rule.ID).Delete(&models.RuleRevision{}).Error
				_ = db.Where("tenant_id=? AND id=?", plan.TenantID, rule.ID).Delete(&models.QualityRule{}).Error
			})
		}
	}
	if err := plan.ResolveRules(); err != nil {
		t.Fatal(fmt.Errorf("resolve fixture: %w", err))
	}
}
