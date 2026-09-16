package migration

import (
	"encoding/json"
	"fmt"
	"github.com/addp/common/dataquality"
	"github.com/addp/common/resourcetree"
	"github.com/addp/quality/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"time"
)

// IDs are deterministic across owners: check id -> 2*id,
// data_validation id -> 2*id+1. Orchestrator migrates its own references.
// Migration 10 owns its historical schema, independently of today's model.
func insertPlanV10(tx *gorm.DB, plan *models.QualityPlan, rules json.RawMessage) error {
	return tx.Table("quality.plans").Create(map[string]interface{}{
		"id": plan.ID, "tenant_id": plan.TenantID, "code": plan.Code, "name": plan.Name, "description": plan.Description, "version": plan.Version,
		"table_bindings": plan.TableBindings, "rules": rules, "created_by": plan.CreatedBy, "updated_by": plan.UpdatedBy, "created_at": plan.CreatedAt, "updated_at": plan.UpdatedAt,
	}).Error
}

func migrateQualityPlans(tx *gorm.DB) error {
	type oldCheck struct {
		ID, TenantID, EngineID, CreatedBy        int64
		Name, Description, SchemaName, TableName string
		CreatedAt, UpdatedAt                     time.Time
	}
	type oldApplication struct {
		ID, TenantID, ElementID, ElementRevisionID, EngineID, CreatedBy int64
		SchemaName, TableName, ColumnName                               string
		RuleConfig                                                      json.RawMessage
		Enabled                                                         bool
	}
	var checks []oldCheck
	var applications []oldApplication
	if err := tx.Table("quality.check_tasks").Order("id").Find(&checks).Error; err != nil {
		return err
	}
	if err := tx.Table("quality.rule_applications").Order("id").Find(&applications).Error; err != nil {
		return err
	}
	scope := func(tenant, engine int64, schema, table string) string {
		v, _ := json.Marshal([]interface{}{tenant, engine, schema, table})
		return string(v)
	}
	checkByScope := map[string]int{}
	var maxID int64
	for i, c := range checks {
		checkByScope[scope(c.TenantID, c.EngineID, c.SchemaName, c.TableName)] = i
		if c.ID > maxID {
			maxID = c.ID
		}
	}
	for _, a := range applications {
		key := scope(a.TenantID, a.EngineID, a.SchemaName, a.TableName)
		if _, ok := checkByScope[key]; ok {
			continue
		}
		maxID++
		checkByScope[key] = len(checks)
		checks = append(checks, oldCheck{ID: maxID, TenantID: a.TenantID, EngineID: a.EngineID, CreatedBy: a.CreatedBy, Name: a.TableName, SchemaName: a.SchemaName, TableName: a.TableName, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()})
	}
	rulesByCheck := map[int64][]map[string]interface{}{}
	for _, a := range applications {
		check := checks[checkByScope[scope(a.TenantID, a.EngineID, a.SchemaName, a.TableName)]]
		document, err := dataquality.Parse(a.RuleConfig)
		if err != nil {
			return fmt.Errorf("application %d: %w", a.ID, err)
		}
		for _, rule := range document.Rules {
			key := uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("quality-plan:%d:%s", a.ID, rule.RuleKey))).String()
			kind := rule.Type
			params := map[string]interface{}{"table": "target", "column": a.ColumnName}
			var source interface{}
			switch kind {
			case "not_null":
			case "allowed_values":
				params["values"] = rule.Params.Values
			case "format", "length", "value_range":
				params["constraint"] = rule.Params
			case "unique":
				kind = "unique_key"
				params = map[string]interface{}{"table": "target", "columns": []string{a.ColumnName}}
			default:
				return fmt.Errorf("unsupported rule type %q", kind)
			}
			if kind != "unique_key" {
				source = map[string]interface{}{"element_id": a.ElementID, "element_revision_id": a.ElementRevisionID, "rule_key": rule.RuleKey}
			}
			item := map[string]interface{}{"rule_key": key, "type": kind, "severity": rule.Severity, "params": params, "name": rule.Message, "disabled": !a.Enabled || !rule.Enabled}
			if source != nil {
				item["source"] = source
			}
			rulesByCheck[check.ID] = append(rulesByCheck[check.ID], item)
			if err := tx.Table("quality.issues").Where("tenant_id=? AND plan_id=? AND rule_key=?", a.TenantID, a.ID, rule.RuleKey).Updates(map[string]interface{}{"plan_id": check.ID * 2, "rule_key": key, "rule_type": kind}).Error; err != nil {
				return err
			}
		}
	}
	for _, c := range checks {
		locator := (&resourcetree.ResourceLocator{EngineID: uint(c.EngineID), Type: resourcetree.TypeTable, Path: []string{c.SchemaName, c.TableName}}).ToURI()
		bindings, _ := json.Marshal([]map[string]string{{"alias": "target", "locator": locator}})
		rules := rulesByCheck[c.ID]
		if rules == nil {
			rules = []map[string]interface{}{}
		}
		raw, _ := json.Marshal(map[string]interface{}{"schema_version": "addp.quality.plan-rules/v1", "rules": rules})
		plan := models.QualityPlan{ID: c.ID * 2, TenantID: c.TenantID, Code: fmt.Sprintf("migrated_check_%d", c.ID), Name: c.Name, Description: c.Description, Version: 1, TableBindings: bindings, Rules: raw, CreatedBy: c.CreatedBy, UpdatedBy: c.CreatedBy, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt}
		if err := insertPlanV10(tx, &plan, raw); err != nil {
			return err
		}
	}
	type oldValidation struct {
		models.QualityPlan
		Assertions json.RawMessage
	}
	var validations []oldValidation
	if err := tx.Table("quality.data_validation_tasks").Find(&validations).Error; err != nil {
		return err
	}
	for _, v := range validations {
		var document struct {
			Assertions []map[string]interface{} `json:"assertions"`
		}
		if err := json.Unmarshal(v.Assertions, &document); err != nil {
			return err
		}
		for _, rule := range document.Assertions {
			rule["rule_key"] = rule["assertion_key"]
			delete(rule, "assertion_key")
		}
		raw, _ := json.Marshal(map[string]interface{}{"schema_version": "addp.quality.plan-rules/v1", "rules": document.Assertions})
		plan := v.QualityPlan
		plan.ID = v.ID*2 + 1
		plan.Rules = raw
		plan.LastExecutionID = ""
		plan.LastExecutionStatus = ""
		plan.LastRunAt = nil
		if err := insertPlanV10(tx, &plan, raw); err != nil {
			return err
		}
	}
	for _, sql := range []string{
		`ALTER TABLE quality.issues ADD CONSTRAINT fk_quality_issue_plan FOREIGN KEY (tenant_id,plan_id) REFERENCES quality.plans(tenant_id,id) ON DELETE CASCADE`,
		`CREATE UNIQUE INDEX uq_quality_issue_rule ON quality.issues(tenant_id,plan_id,rule_key)`,
		`SELECT setval(pg_get_serial_sequence('quality.plans','id'),COALESCE((SELECT max(id) FROM quality.plans),1),(SELECT count(*)>0 FROM quality.plans))`,
		`DROP TABLE quality.rule_applications`, `DROP TABLE quality.check_tasks`, `DROP TABLE quality.data_validation_tasks`,
	} {
		if err := tx.Exec(sql).Error; err != nil {
			return err
		}
	}
	return nil
}
