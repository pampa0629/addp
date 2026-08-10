package iam

import (
	"errors"
	"reflect"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/google/uuid"
)

func TestNormalizeExecutionAuthorizationRequestCanonicalizesBoundary(t *testing.T) {
	executionID := uuid.MustParse("9a21ab1a-2900-42a5-ae91-821339b3fcdd")
	audience, engineIDs, effects, ttl, err := normalizeExecutionAuthorizationRequest(
		"develop", executionID, []int64{12, 3}, []string{"external_effect", "read"}, 20*time.Minute,
	)
	if err != nil {
		t.Fatal(err)
	}
	if audience != "develop" || !reflect.DeepEqual(engineIDs, []int64{3, 12}) ||
		!reflect.DeepEqual(effects, []string{"read", "external_effect"}) || ttl != 20*time.Minute {
		t.Fatalf("normalized boundary audience=%q engines=%v effects=%v ttl=%v", audience, engineIDs, effects, ttl)
	}
}

func TestNormalizeExecutionAuthorizationRequestAcceptsQualityAudience(t *testing.T) {
	executionID := uuid.MustParse("2bc80c2c-1ca7-479c-b6bb-b0d9d57ca226")
	audience, engineIDs, effects, ttl, err := normalizeExecutionAuthorizationRequest(
		"addp-quality", executionID, []int64{2}, []string{"read"}, time.Hour,
	)
	if err != nil {
		t.Fatal(err)
	}
	if audience != "addp-quality" || !reflect.DeepEqual(engineIDs, []int64{2}) ||
		!reflect.DeepEqual(effects, []string{"read"}) || ttl != time.Hour {
		t.Fatalf("normalized quality boundary audience=%q engines=%v effects=%v ttl=%v", audience, engineIDs, effects, ttl)
	}
}

func TestNormalizeExecutionAuthorizationRequestRejectsUnknownOrDuplicateBoundary(t *testing.T) {
	executionID := uuid.New()
	testCases := []struct {
		name      string
		audience  string
		engineIDs []int64
		effects   []string
	}{
		{name: "unknown audience", audience: "meta", engineIDs: []int64{1}, effects: []string{"read"}},
		{name: "duplicate engine", audience: "develop", engineIDs: []int64{1, 1}, effects: []string{"read"}},
		{name: "duplicate effect", audience: "develop", engineIDs: []int64{1}, effects: []string{"read", "read"}},
		{name: "unknown effect", audience: "develop", engineIDs: []int64{1}, effects: []string{"admin"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, _, _, _, err := normalizeExecutionAuthorizationRequest(
				testCase.audience, executionID, testCase.engineIDs, testCase.effects, time.Minute,
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

func TestContainsAllExecutionPermissionsRequiresQualityExecuteAndDataRead(t *testing.T) {
	rows := []RoleAssignmentPermissionProjection{
		{PermissionKey: "quality.check_task.execute"},
		{PermissionKey: executionEffectPermissions["read"]},
	}
	if !containsAllExecutionPermissions(rows, "addp-quality", []string{"read"}) {
		t.Fatal("complete quality read boundary was rejected")
	}
	if containsAllExecutionPermissions(rows[:1], "addp-quality", []string{"read"}) {
		t.Fatal("quality execute permission without data read was accepted")
	}
	if containsAllExecutionPermissions(rows[1:], "addp-quality", []string{"read"}) {
		t.Fatal("data read permission without quality execute was accepted")
	}
	if containsAllExecutionPermissions(rows, "addp-quality", []string{"write"}) {
		t.Fatal("quality audience accepted a non-read effect")
	}
}
