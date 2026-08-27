package iam

import (
	"errors"
	"reflect"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	"github.com/google/uuid"
)

func TestNormalizeExecutionAuthorizationRequestCanonicalizesBoundary(t *testing.T) {
	executionID := uuid.MustParse("9a21ab1a-2900-42a5-ae91-821339b3fcdd")
	audience, accesses, effects, ttl, err := normalizeExecutionAuthorizationRequest(
		"develop", executionID, []ExecutionEngineAccessScope{
			{EngineID: 12, Effects: []string{"external_effect", "read"}},
			{EngineID: 3, Effects: []string{"read"}},
		}, 20*time.Minute,
	)
	if err != nil {
		t.Fatal(err)
	}
	if audience != "develop" || !reflect.DeepEqual(accesses, []ExecutionEngineAccessScope{
		{EngineID: 3, Effects: []string{"read"}},
		{EngineID: 12, Effects: []string{"read", "external_effect"}},
	}) ||
		!reflect.DeepEqual(effects, []string{"read", "external_effect"}) || ttl != 20*time.Minute {
		t.Fatalf("normalized boundary audience=%q accesses=%v effects=%v ttl=%v", audience, accesses, effects, ttl)
	}
}

func TestNormalizeExecutionAuthorizationRequestAcceptsQualityAudience(t *testing.T) {
	executionID := uuid.MustParse("2bc80c2c-1ca7-479c-b6bb-b0d9d57ca226")
	audience, accesses, effects, ttl, err := normalizeExecutionAuthorizationRequest(
		commonExecution.AudienceQuality, executionID,
		[]ExecutionEngineAccessScope{{EngineID: 2, Effects: []string{"read"}}}, time.Hour,
	)
	if err != nil {
		t.Fatal(err)
	}
	if audience != commonExecution.AudienceQuality || !reflect.DeepEqual(accesses, []ExecutionEngineAccessScope{{EngineID: 2, Effects: []string{"read"}}}) ||
		!reflect.DeepEqual(effects, []string{"read"}) || ttl != time.Hour {
		t.Fatalf("normalized quality boundary audience=%q accesses=%v effects=%v ttl=%v", audience, accesses, effects, ttl)
	}
}

func TestNormalizeExecutionAuthorizationRequestAcceptsTransferAudience(t *testing.T) {
	executionID := uuid.New()
	audience, _, effects, _, err := normalizeExecutionAuthorizationRequest(
		commonExecution.AudienceTransfer, executionID, []ExecutionEngineAccessScope{
			{EngineID: 2, Effects: []string{"write"}}, {EngineID: 1, Effects: []string{"read"}},
		}, time.Minute,
	)
	if err != nil {
		t.Fatal(err)
	}
	if audience != commonExecution.AudienceTransfer || !reflect.DeepEqual(effects, []string{"read", "write"}) {
		t.Fatalf("normalized transfer boundary audience=%q effects=%v", audience, effects)
	}
}

func TestNormalizeExecutionAuthorizationRequestRejectsUnknownOrDuplicateBoundary(t *testing.T) {
	executionID := uuid.New()
	testCases := []struct {
		name     string
		audience string
		accesses []ExecutionEngineAccessScope
	}{
		{name: "unknown audience", audience: "meta", accesses: []ExecutionEngineAccessScope{{EngineID: 1, Effects: []string{"read"}}}},
		{name: "duplicate engine", audience: "develop", accesses: []ExecutionEngineAccessScope{{EngineID: 1, Effects: []string{"read"}}, {EngineID: 1, Effects: []string{"write"}}}},
		{name: "duplicate effect", audience: "develop", accesses: []ExecutionEngineAccessScope{{EngineID: 1, Effects: []string{"read", "read"}}}},
		{name: "unknown effect", audience: "develop", accesses: []ExecutionEngineAccessScope{{EngineID: 1, Effects: []string{"admin"}}}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, _, _, _, err := normalizeExecutionAuthorizationRequest(
				testCase.audience, executionID, testCase.accesses, time.Minute,
			)
			if !errors.Is(err, commonapi.ErrBadRequest) {
				t.Fatalf("error = %v, want bad request", err)
			}
		})
	}
}

func TestContainsAllExecutionPermissionsRequiresFeatureAndEveryEffect(t *testing.T) {
	rows := []RoleAssignmentPermissionProjection{
		{PermissionKey: developExecutionPermission},
		{PermissionKey: executionEffectPermissions["read"]},
		{PermissionKey: executionEffectPermissions["write"]},
	}
	if !containsAllExecutionPermissions(rows, "develop", []string{"read", "write"}) {
		t.Fatal("complete permission set was rejected")
	}
	if containsAllExecutionPermissions(rows[1:], "develop", []string{"read"}) {
		t.Fatal("effect permission without feature permission was accepted")
	}
	if containsAllExecutionPermissions(rows, "develop", []string{"ddl"}) {
		t.Fatal("missing effect permission was accepted")
	}
}

func TestContainsAllExecutionPermissionsAcceptsServiceReadSampleBoundary(t *testing.T) {
	rows := []RoleAssignmentPermissionProjection{
		{PermissionKey: serviceQuerySamplePermission},
		{PermissionKey: serviceDataReadPermission},
	}
	for _, audience := range []string{"service", "duckdb"} {
		if !containsAllExecutionPermissions(rows, audience, []string{"read"}) {
			t.Fatalf("service sample boundary was rejected for audience %q", audience)
		}
	}
	if containsAllExecutionPermissions(rows, "service", []string{"write"}) {
		t.Fatal("service sample boundary accepted a write effect")
	}
	if containsAllExecutionPermissions(rows[:1], "service", []string{"read"}) {
		t.Fatal("service feature permission without data read permission was accepted")
	}
}

func TestContainsAllExecutionPermissionsRequiresModelMaterializationAndEveryEffect(t *testing.T) {
	rows := []RoleAssignmentPermissionProjection{
		{PermissionKey: modelMaterializationExecutionPermission},
		{PermissionKey: executionEffectPermissions["read"]},
		{PermissionKey: executionEffectPermissions["ddl"]},
	}
	if !containsAllExecutionPermissions(rows, commonExecution.AudienceModel, []string{"read", "ddl"}) {
		t.Fatal("complete Model materialization permission set was rejected")
	}
	if containsAllExecutionPermissions(rows[1:], commonExecution.AudienceModel, []string{"ddl"}) {
		t.Fatal("DDL effect permission without Model materialization permission was accepted")
	}
	if containsAllExecutionPermissions(rows[:2], commonExecution.AudienceModel, []string{"read", "ddl"}) {
		t.Fatal("Model materialization permission without every effect permission was accepted")
	}
}

func TestContainsAllExecutionPermissionsRequiresQualityExecuteAndDataRead(t *testing.T) {
	rows := []RoleAssignmentPermissionProjection{
		{PermissionKey: "quality.check_task.execute"},
		{PermissionKey: executionEffectPermissions["read"]},
	}
	if !containsAllExecutionPermissions(rows, commonExecution.AudienceQuality, []string{"read"}) {
		t.Fatal("complete quality read boundary was rejected")
	}
	if containsAllExecutionPermissions(rows[:1], commonExecution.AudienceQuality, []string{"read"}) {
		t.Fatal("quality execute permission without data read was accepted")
	}
	if containsAllExecutionPermissions(rows[1:], commonExecution.AudienceQuality, []string{"read"}) {
		t.Fatal("data read permission without quality execute was accepted")
	}
	if containsAllExecutionPermissions(rows, commonExecution.AudienceQuality, []string{"write"}) {
		t.Fatal("quality audience accepted a non-read effect")
	}
}

func TestContainsAllExecutionPermissionsRestrictsTransferToReadWrite(t *testing.T) {
	rows := []RoleAssignmentPermissionProjection{
		{PermissionKey: transferExecutionPermission},
		{PermissionKey: executionEffectPermissions["read"]},
		{PermissionKey: executionEffectPermissions["write"]},
		{PermissionKey: executionEffectPermissions["ddl"]},
		{PermissionKey: executionEffectPermissions["external_effect"]},
	}
	if !containsAllExecutionPermissions(rows, commonExecution.AudienceTransfer, []string{"read", "write"}) {
		t.Fatal("complete Transfer read/write boundary was rejected")
	}
	if containsAllExecutionPermissions(rows[1:], commonExecution.AudienceTransfer, []string{"read"}) {
		t.Fatal("Transfer data effect without task execution permission was accepted")
	}
	if containsAllExecutionPermissions(rows, commonExecution.AudienceTransfer, []string{"ddl"}) ||
		containsAllExecutionPermissions(rows, commonExecution.AudienceTransfer, []string{"external_effect"}) {
		t.Fatal("Transfer audience accepted DDL or external effect")
	}
}
