package service

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/addp/common/dataquality"
	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
)

func TestCompileElementRulesUsesOnlyIntrinsicConstraints(t *testing.T) {
	length := 32
	revision := &models.ElementRevision{Nullable: false, Length: &length, Format: "^[a-z]+$", ValueDomainKind: models.ValueDomainUnrestricted}
	svc := &ElementService{}
	first, err := svc.compileQualityRules(42, revision, 7)
	if err != nil {
		t.Fatal(err)
	}
	document, err := dataquality.FromValue(first)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{dataquality.RuleTypeNotNull, dataquality.RuleTypeLength, dataquality.RuleTypeFormat}
	if len(document.Rules) != len(want) {
		t.Fatalf("rules = %#v", document.Rules)
	}
	for i, rule := range document.Rules {
		if rule.Type != want[i] || rule.RuleKey != stableRuleKey(42, rule.Type) {
			t.Fatalf("rule = %#v", rule)
		}
	}
	length = 64
	second, err := svc.compileQualityRules(42, revision, 7)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := dataquality.FromValue(second)
	if err != nil {
		t.Fatal(err)
	}
	for i := range document.Rules {
		if document.Rules[i].RuleKey != changed.Rules[i].RuleKey {
			t.Fatal("changing parameters changed rule identity")
		}
	}
	if changed.Rules[1].Params.Max.String() != "64" || document.Rules[1].Params.Max.String() != "32" {
		t.Fatal("compiled snapshots are not independent")
	}
	if stableRuleKey(42, dataquality.RuleTypeLength) == stableRuleKey(43, dataquality.RuleTypeLength) {
		t.Fatal("different data elements share an identity")
	}
}

func TestElementCreationPreservesExplicitNotNull(t *testing.T) {
	db := setupStandardCleanupTestDB(t)
	svc := NewElementService(repository.NewElementRepository(db), nil, repository.NewTenantReferenceRepository(db), nil)
	result, err := svc.CreateElement(&models.CreateElementRequest{
		Code: "person_id", ScopeType: models.StandardScopeTenantCommon, Name: "Person ID", Definition: "Person identifier",
		DataType: "string", Nullable: false, ValueDomainKind: models.ValueDomainUnrestricted,
	}, 7, 1, "Initial creation")
	if err != nil {
		t.Fatal(err)
	}
	if result.DraftRevision.Nullable {
		t.Fatal("explicit nullable=false was replaced by the ORM default")
	}
	compiled, err := svc.compileQualityRules(result.ID, result.DraftRevision, 7)
	if err != nil {
		t.Fatal(err)
	}
	document, err := dataquality.FromValue(compiled)
	if err != nil || len(document.Rules) != 1 || document.Rules[0].Type != dataquality.RuleTypeNotNull {
		t.Fatalf("required element lost not-null rule: %#v, %v", document, err)
	}
}

func TestCompileElementRulesRangeAndEmptyConstraints(t *testing.T) {
	svc := &ElementService{}
	empty, err := svc.compileQualityRules(1, &models.ElementRevision{Nullable: true, ValueDomainKind: models.ValueDomainUnrestricted}, 7)
	if err != nil {
		t.Fatal(err)
	}
	document, err := dataquality.FromValue(empty)
	if err != nil || len(document.Rules) != 0 {
		t.Fatalf("empty document = %#v, error = %v", document, err)
	}
	min, max, exclusive := json.Number("0"), json.Number("100"), false
	value, err := svc.compileQualityRules(1, &models.ElementRevision{Nullable: true, ValueDomainKind: models.ValueDomainRange,
		RangeConstraint: &models.RangeConstraint{Min: &min, Max: &max, MinInclusive: &exclusive}}, 7)
	if err != nil {
		t.Fatal(err)
	}
	document, err = dataquality.FromValue(value)
	if err != nil || len(document.Rules) != 1 || document.Rules[0].Type != dataquality.RuleTypeValueRange {
		t.Fatalf("range = %#v, error = %v", document, err)
	}
	if document.Rules[0].Params.MinInclusive == nil || *document.Rules[0].Params.MinInclusive {
		t.Fatal("exclusive lower bound lost")
	}
}

func TestCompileElementEnumerationUsesExactPublishedCodeSet(t *testing.T) {
	db := openElementResolutionTestDB(t)
	for _, statement := range []string{
		`INSERT INTO standard.code_sets (id, tenant_id, scope_type, origin, code, lifecycle_state) VALUES (50, 7, 'tenant_common', 'tenant', 'member_status', 'active')`,
		`INSERT INTO standard.code_set_revisions (id, code_set_id, revision_no, status, name, description, value_type) VALUES (501, 50, 1, 'published', 'Member status', 'Member status', 'string'), (502, 50, 2, 'published', 'New status', 'New status', 'string')`,
		`INSERT INTO standard.code_set_revision_items (id, code_set_revision_id, code, label, sort_order, status) VALUES (1, 501, 'signup', 'Signup', 1, 'active'), (2, 501, 'leader', 'Leader', 2, 'active'), (3, 501, 'old', 'Old', 3, 'deprecated'), (4, 502, 'future', 'Future', 1, 'active')`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc := &ElementService{codeSets: repository.NewCodeSetRepository(db)}
	revisionID := int64(501)
	revision := &models.ElementRevision{Nullable: true, ValueDomainKind: models.ValueDomainEnumeration, CodeSetRevisionID: &revisionID}
	value, err := svc.compileQualityRules(1, revision, 7)
	if err != nil {
		t.Fatal(err)
	}
	document, err := dataquality.FromValue(value)
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Rules) != 1 || !reflect.DeepEqual(document.Rules[0].Params.Values, []string{"signup", "leader"}) {
		t.Fatalf("rules = %#v", document.Rules)
	}
	if _, err := svc.compileQualityRules(1, revision, 8); err == nil {
		t.Fatal("accepted code set from another tenant")
	}
}

func TestTextDefinitionUsesStringWithOptionalLength(t *testing.T) {
	db := setupStandardCleanupTestDB(t)
	svc := NewElementService(repository.NewElementRepository(db), nil, repository.NewTenantReferenceRepository(db), nil)
	for _, dataType := range []string{"string", "text"} {
		req := &models.CreateElementRequest{Code: "phone_" + dataType, ScopeType: models.StandardScopeTenantCommon, Name: "Phone", Definition: "Phone number", DataType: dataType, Nullable: true, ValueDomainKind: models.ValueDomainUnrestricted}
		_, err := svc.CreateElement(req, 7, 1, "Initial creation")
		if dataType == "string" && err != nil {
			t.Fatal(err)
		}
		if dataType == "text" && err == nil {
			t.Fatal("retired text type accepted")
		}
	}
}
