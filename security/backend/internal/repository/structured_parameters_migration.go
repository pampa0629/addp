package repository

import (
	"encoding/json"
	"fmt"

	"github.com/addp/common/dataprotection"
	"github.com/addp/security/internal/models"
	"gorm.io/gorm"
)

// One-way startup convergence; runtime code never reads the removed columns.
func migrateStructuredProtectionParameters(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		for _, statement := range []struct{ table, column, definition string }{
			{"protection_baselines", "parameters", "TEXT"}, {"protection_baselines", "allowed_algorithms", "TEXT"},
			{"protection_policy_revisions", "algorithm", "VARCHAR(80)"}, {"protection_policy_revisions", "parameters", "TEXT"}, {"protection_policy_revisions", "invalid_value_effect", "VARCHAR(20)"},
		} {
			exists, err := securityColumnExists(tx, statement.table, statement.column)
			if err != nil {
				return err
			}
			if !exists {
				if err := tx.Exec("ALTER TABLE security." + statement.table + " ADD COLUMN " + statement.column + " " + statement.definition).Error; err != nil {
					return err
				}
			}
		}
		legacy, err := securityColumnExists(tx, "protection_baselines", "keep_prefix")
		if err != nil {
			return err
		}
		if legacy {
			var old []struct {
				ID         int64
				KeepPrefix int
				KeepSuffix int
				Effect     string
				Algorithm  string
			}
			if err := tx.Table("security.protection_baselines").Select("id, keep_prefix, keep_suffix, effect, algorithm").Scan(&old).Error; err != nil {
				return err
			}
			for _, row := range old {
				var parameters map[string]any
				var allowed []string
				if row.Effect == dataprotection.EffectMask {
					parameters = map[string]any{"prefix_runes": row.KeepPrefix, "suffix_runes": row.KeepSuffix, "mask_rune": "*"}
					allowed = []string{row.Algorithm}
				}
				params, _ := json.Marshal(parameters)
				algorithms, _ := json.Marshal(allowed)
				if err := tx.Table("security.protection_baselines").Where("id = ?", row.ID).Updates(map[string]any{"parameters": string(params), "allowed_algorithms": string(algorithms)}).Error; err != nil {
					return err
				}
			}
			for _, column := range []string{"keep_prefix", "keep_suffix"} {
				if err := tx.Exec("ALTER TABLE security.protection_baselines DROP COLUMN " + column).Error; err != nil {
					return err
				}
			}
		}
		var revisions []models.ProtectionPolicyRevision
		if err := tx.Select("id, tenant_id, policy_id, revision, state, effect, algorithm, parameters, invalid_value_effect, rationale, created_by, created_at").Where("effect = ? AND (algorithm IS NULL OR algorithm = '')", dataprotection.EffectMask).Find(&revisions).Error; err != nil {
			return err
		}
		for _, revision := range revisions {
			var baseline models.ProtectionBaseline
			err := tx.Table("security.protection_baselines AS baseline").Select("baseline.*").
				Joins("JOIN security.resource_security_assessment_revisions AS assessment ON assessment.tenant_id = baseline.tenant_id AND assessment.sensitive_data_type_id = baseline.sensitive_data_type_id AND assessment.security_grade_id = baseline.security_grade_id").
				Joins("JOIN security.resource_security_assessments AS root ON root.id = assessment.assessment_id AND root.current_revision = assessment.revision").
				Joins("JOIN security.protection_policies AS policy ON policy.assessment_id = root.id AND policy.tenant_id = root.tenant_id").
				Where("policy.id = ? AND baseline.tenant_id = ?", revision.PolicyID, revision.TenantID).First(&baseline).Error
			if err != nil {
				return fmt.Errorf("resolve legacy policy baseline: %w", err)
			}
			params, _ := json.Marshal(baseline.Parameters)
			if err := tx.Model(&revision).Updates(map[string]any{"algorithm": baseline.Algorithm, "parameters": string(params), "invalid_value_effect": baseline.InvalidValueEffect}).Error; err != nil {
				return err
			}
		}
		return tx.Model(&models.ProtectionPolicyRevision{}).Where("effect IN ? AND (invalid_value_effect IS NULL OR invalid_value_effect = '')", []string{dataprotection.EffectSuppress, dataprotection.EffectDeny}).Update("invalid_value_effect", gorm.Expr("effect")).Error
	})
}

func securityColumnExists(db *gorm.DB, table, column string) (bool, error) {
	if db.Dialector.Name() != "sqlite" {
		return db.Migrator().HasColumn("security."+table, column), nil
	}
	var columns []struct{ Name string }
	if err := db.Raw("PRAGMA security.table_info(" + table + ")").Scan(&columns).Error; err != nil {
		return false, err
	}
	for _, entry := range columns {
		if entry.Name == column {
			return true, nil
		}
	}
	return false, nil
}
