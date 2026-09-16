package service

import (
	"encoding/json"
	"github.com/addp/quality/internal/models"
	"github.com/addp/quality/internal/testsupport"
	"gorm.io/gorm"
	"testing"
)

func planRequestFixtures(t *testing.T, db *gorm.DB, tenantID int64, raw json.RawMessage) []PlanCheckRequest {
	t.Helper()
	plan := models.QualityPlan{TenantID: tenantID, Rules: raw}
	testsupport.SeedPlanRules(t, db, &plan)
	items := make([]PlanCheckRequest, len(plan.CheckItems))
	for i, c := range plan.CheckItems {
		items[i] = PlanCheckRequest{RuleKey: c.RuleKey, RuleID: c.RuleID, RevisionNo: c.RevisionNo, Severity: c.Severity, Disabled: c.Disabled, Bindings: c.Bindings}
	}
	return items
}
