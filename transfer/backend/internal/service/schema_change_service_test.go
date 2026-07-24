package service

import (
	"reflect"
	"testing"

	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
)

func TestAdditiveUnexpectedFieldsRequiresOnlyUnexpectedFields(t *testing.T) {
	unexpected, eligible, err := additiveUnexpectedFields(models.JSONMap{
		"missing_fields": []string{}, "unexpected_fields": []string{"added"}, "incompatible_fields": []string{},
	})
	if err != nil || !eligible || !reflect.DeepEqual(unexpected, []string{"added"}) {
		t.Fatalf("unexpected=%v eligible=%v err=%v", unexpected, eligible, err)
	}
	for _, diff := range []models.JSONMap{
		{"missing_fields": []string{"removed"}, "unexpected_fields": []string{"added"}},
		{"unexpected_fields": []string{"added"}, "incompatible_fields": []string{"changed"}},
		{"unexpected_fields": []string{}},
	} {
		if _, eligible, err := additiveUnexpectedFields(diff); err != nil || eligible {
			t.Fatalf("diff=%#v eligible=%v err=%v, want ineligible", diff, eligible, err)
		}
	}
}

func TestValidateSchemaChangeApprovalRequiresExactSafeMapping(t *testing.T) {
	config := validPostgreSQLCDCTaskConfig()
	inspected := []models.SchemaChangeField{{Source: "added", Target: "added", TargetType: "string", Nullable: true}}
	approved, err := validateSchemaChangeApproval(config, inspected, []models.SchemaChangeField{{
		Source: " added ", Target: " renamed ", TargetType: "string", Nullable: true,
	}})
	if err != nil || !reflect.DeepEqual(approved, []models.SchemaChangeField{{
		Source: "added", Target: "renamed", TargetType: "string", Nullable: true,
	}}) {
		t.Fatalf("approved=%#v err=%v", approved, err)
	}
	invalid := [][]models.SchemaChangeField{
		{{Source: "added", Target: "renamed", TargetType: "string", Nullable: false}},
		{{Source: "added", Target: "renamed", TargetType: "bigint", Nullable: true}},
		{{Source: "added", Target: "id", TargetType: "string", Nullable: true}},
		{{Source: "other", Target: "renamed", TargetType: "string", Nullable: true}},
	}
	for _, fields := range invalid {
		if _, err := validateSchemaChangeApproval(config, inspected, fields); err == nil {
			t.Fatalf("approval %#v succeeded, want conflict", fields)
		}
	}
}

func TestAppendSchemaChangeMappingsProducesSingleCDCConfigPath(t *testing.T) {
	config := validPostgreSQLCDCTaskConfig()
	original, err := planner.ParseDatabaseCDCTaskSpec(config)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := appendSchemaChangeMappings(config, []models.SchemaChangeField{{
		Source: "added", Target: "renamed", TargetType: "string", Nullable: true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Transforms) != 1 || spec.Transforms[0].Type != "field_mapping" {
		t.Fatalf("updated transforms=%#v", spec.Transforms)
	}
	fields := spec.Transforms[0].Fields
	if len(fields) != len(original.Transforms[0].Fields)+1 {
		t.Fatalf("updated fields=%#v", fields)
	}
	added := fields[len(fields)-1]
	if added.Source != "added" || added.Target != "renamed" ||
		added.TargetType != "string" || added.Nullable == nil || !*added.Nullable {
		t.Fatalf("updated fields=%#v", fields)
	}
	encodedConfig, err := databaseCDCSpecConfig(spec)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := planner.ParseDatabaseCDCTaskSpec(encodedConfig); err != nil {
		t.Fatalf("updated config does not parse through canonical CDC path: %v", err)
	}
}
