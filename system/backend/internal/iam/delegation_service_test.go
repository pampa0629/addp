package iam

import (
	"errors"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	commonauth "github.com/addp/common/authorization"
)

type staticDelegationCatalog map[string]commonauth.ToolAuthorization

func (catalog staticDelegationCatalog) FindToolAuthorization(name string) (commonauth.ToolAuthorization, bool) {
	tool, ok := catalog[name]
	return tool, ok
}

func TestDelegationServiceValidatesRequestAgainstCatalog(t *testing.T) {
	service, err := NewDelegationService(
		NewRepository(nil),
		staticDelegationCatalog{
			"workflow.run": {
				Name:                "workflow.run",
				Owner:               "develop",
				RequiredScopes:      []string{"workflow.run"},
				RequiredPermissions: []string{"develop.task.execute"},
			},
		},
		DelegationServiceConfig{},
	)
	if err != nil {
		t.Fatal(err)
	}
	valid := IssueDelegatedAccessTokenInput{
		SourceAccessToken: "addp_at_source",
		Audience:          "develop",
		Scopes:            []string{"workflow.run"},
		AgentRunID:        "run-1",
		ToolCallID:        "call-1",
	}
	tool, err := service.validateRequest(valid)
	if err != nil || tool.Name != "workflow.run" {
		t.Fatalf("validateRequest() tool=%#v error=%v", tool, err)
	}

	tests := []struct {
		name   string
		mutate func(*IssueDelegatedAccessTokenInput)
		want   error
	}{
		{name: "source token", mutate: func(input *IssueDelegatedAccessTokenInput) { input.SourceAccessToken = "addp_dat_source" }, want: commonapi.ErrUnauthorized},
		{name: "unknown Tool", mutate: func(input *IssueDelegatedAccessTokenInput) { input.Scopes = []string{"workflow.unknown"} }, want: commonapi.ErrBadRequest},
		{name: "multiple scopes", mutate: func(input *IssueDelegatedAccessTokenInput) {
			input.Scopes = []string{"workflow.run", "workflow.validate"}
		}, want: commonapi.ErrBadRequest},
		{name: "audience mismatch", mutate: func(input *IssueDelegatedAccessTokenInput) { input.Audience = "manager" }, want: commonapi.ErrBadRequest},
		{name: "binding whitespace", mutate: func(input *IssueDelegatedAccessTokenInput) { input.AgentRunID = " run-1" }, want: commonapi.ErrBadRequest},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			input := valid
			input.Scopes = append([]string(nil), valid.Scopes...)
			testCase.mutate(&input)
			if _, err := service.validateRequest(input); !errors.Is(err, testCase.want) {
				t.Fatalf("validateRequest() error=%v, want %v", err, testCase.want)
			}
		})
	}
}

func TestNewDelegationServiceRejectsUnsafeConfiguration(t *testing.T) {
	catalog := staticDelegationCatalog{"tool": {Name: "tool"}}
	if _, err := NewDelegationService(nil, catalog, DelegationServiceConfig{}); !errors.Is(err, commonapi.ErrBadRequest) {
		t.Fatalf("nil repository error=%v", err)
	}
	if _, err := NewDelegationService(NewRepository(nil), catalog, DelegationServiceConfig{
		AccessTokenTTL: 2*time.Minute + time.Nanosecond,
	}); !errors.Is(err, commonapi.ErrBadRequest) {
		t.Fatalf("unsafe TTL error=%v", err)
	}
}

func TestContainsAllDelegationPermissions(t *testing.T) {
	rows := []RoleAssignmentPermissionProjection{
		{PermissionKey: "develop.task.execute"},
		{PermissionKey: "develop.task.read"},
	}
	if !containsAllDelegationPermissions(rows, []string{"develop.task.execute", "develop.task.read"}) {
		t.Fatal("all-of Permission set was rejected")
	}
	if containsAllDelegationPermissions(rows, []string{"develop.task.execute", "develop.task.cancel"}) {
		t.Fatal("incomplete all-of Permission set was accepted")
	}
}
