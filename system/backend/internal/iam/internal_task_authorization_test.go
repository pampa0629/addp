package iam

import (
	"strings"
	"testing"
	"time"

	"github.com/addp/common/execution"
	"github.com/google/uuid"
)

func TestInternalTaskAuthorizationDoesNotRelaxEngineAuthorization(t *testing.T) {
	scope := execution.InternalTaskScope{TaskType: "semantic_projection", ResourceID: "beijing_outdoor", Revision: "1", Digest: strings.Repeat("a", 64), Generation: uuid.NewString()}
	input := IssueExecutionAuthorizationInput{Audience: "ontology", ExecutionID: uuid.New(), InternalTask: &scope}
	if _, _, _, ttl, err := normalizeUserExecutionAuthorizationRequest(input); err != nil || ttl != 15*time.Minute {
		t.Fatalf("valid scope: %v %v", ttl, err)
	}
	for _, mutate := range []func(*IssueExecutionAuthorizationInput){
		func(i *IssueExecutionAuthorizationInput) { i.InternalTask = nil }, func(i *IssueExecutionAuthorizationInput) { i.Audience = "develop" },
		func(i *IssueExecutionAuthorizationInput) { i.ExpiresIn = time.Hour + time.Second },
		func(i *IssueExecutionAuthorizationInput) {
			i.Accesses = []ExecutionEngineAccessScope{{EngineID: 1, Effects: []string{"read"}}}
		},
	} {
		copy := input
		mutate(&copy)
		if _, _, _, _, err := normalizeUserExecutionAuthorizationRequest(copy); err == nil {
			t.Fatal("invalid scope accepted")
		}
	}
	if _, _, _, _, err := normalizeExecutionAuthorizationRequest("ontology", input.ExecutionID, []ExecutionEngineAccessScope{{EngineID: 1, Effects: []string{"read"}}}, time.Minute); err == nil {
		t.Fatal("internal audience accepted by engine/derived path")
	}
	rows := []RoleAssignmentPermissionProjection{{PermissionKey: "ontology.revision.publish"}}
	if !containsAllExecutionPermissions(rows, "ontology", nil) || containsAllExecutionPermissions(nil, "ontology", nil) || containsAllExecutionPermissions(rows, "ontology", []string{"read"}) {
		t.Fatal("internal permission widened")
	}
}
