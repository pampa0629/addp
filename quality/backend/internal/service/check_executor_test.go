package service

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	commonAPI "github.com/addp/common/api"
	commonClient "github.com/addp/common/client"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
)

func TestRunCheckRejectsNonManualTriggerAsBadRequest(t *testing.T) {
	executor := NewCheckExecutor(nil, nil, nil, nil, time.Minute, 1)
	_, err := executor.RunCheckWithContext(context.Background(), 1, 7, 11, "", commonExecution.TriggerTypeScheduled, "quality", nil)
	if !errors.Is(err, commonAPI.ErrBadRequest) {
		t.Fatalf("RunCheckWithContext error = %v, want bad request", err)
	}
}

func TestEvaluateCheckCounts(t *testing.T) {
	tests := []struct {
		name       string
		counts     CheckCounts
		wantRate   float64
		wantPassed bool
		wantErr    bool
	}{
		{name: "empty table", counts: CheckCounts{}, wantRate: 100, wantPassed: true},
		{name: "all passed", counts: CheckCounts{TotalCount: 10}, wantRate: 100, wantPassed: true},
		{name: "partially failed", counts: CheckCounts{TotalCount: 10, FailedCount: 2}, wantRate: 80, wantPassed: false},
		{name: "failed exceeds total", counts: CheckCounts{TotalCount: 1, FailedCount: 2}, wantErr: true},
		{name: "negative count", counts: CheckCounts{TotalCount: -1}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rate, passed, err := evaluateCheckCounts(tt.counts)
			if (err != nil) != tt.wantErr {
				t.Fatalf("evaluateCheckCounts() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if math.Abs(rate-tt.wantRate) > 0.000001 || passed != tt.wantPassed {
				t.Fatalf("evaluateCheckCounts() = (%v, %v), want (%v, %v)", rate, passed, tt.wantRate, tt.wantPassed)
			}
		})
	}
}

func TestAggregateExecutionResult(t *testing.T) {
	details := []RuleResult{
		{Column: "z_col", PassRate: 50, Passed: false},
		{Column: "a_col", PassRate: 100, Passed: true},
		{Column: "z_col", PassRate: 100, Passed: true},
	}
	result, err := aggregateExecutionResult(details)
	if err != nil {
		t.Fatalf("aggregateExecutionResult() error = %v", err)
	}
	if result.QualityScore != 250.0/3.0 || result.TotalRules != 3 || result.PassedRules != 2 || result.FailedRules != 1 {
		t.Fatalf("aggregateExecutionResult() = %#v", result)
	}
	if len(result.FieldScores) != 2 || result.FieldScores[0].Column != "a_col" || result.FieldScores[1].Column != "z_col" {
		t.Fatalf("field score order = %#v", result.FieldScores)
	}
	if result.FieldScores[1].Score != 75 || result.FieldScores[1].RuleCount != 2 {
		t.Fatalf("z_col score = %#v", result.FieldScores[1])
	}

	metadata := executionResultMetadata(result)
	if metadata["schema_version"] != qualityExecutionResultSchemaVersion {
		t.Fatalf("metadata schema_version = %v", metadata["schema_version"])
	}
	if _, exists := metadata["result"]; exists {
		t.Fatal("metadata must not contain legacy result wrapper")
	}
}

func TestAggregateExecutionResultRejectsNoRules(t *testing.T) {
	if _, err := aggregateExecutionResult(nil); err == nil {
		t.Fatal("aggregateExecutionResult() error = nil, want no-rules error")
	}
}

func TestExecutionFailureCodeKeepsStableDomainReasons(t *testing.T) {
	wrapped := failExecution(qualityExecutionNoRules, errors.New("internal detail"))
	if got := executionFailureCode(wrapped); got != qualityExecutionNoRules {
		t.Fatalf("executionFailureCode() = %q, want %q", got, qualityExecutionNoRules)
	}
	if got := executionFailureCode(errors.New("unknown")); got != qualityExecutionFailedCode {
		t.Fatalf("unknown executionFailureCode() = %q, want %q", got, qualityExecutionFailedCode)
	}
}

func TestIssueExecutionAuthorizationFromParentUsesQualityBoundary(t *testing.T) {
	parentExecutionID := "74d980cf-3ced-41ef-81fc-271f89249110"
	childExecutionID := "2aaeb79d-2bbd-47a2-a8d4-a607ce6d51a5"
	expiresAt := time.Now().UTC().Add(10 * time.Minute)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/system/oauth/token":
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if r.Form.Get("tenant_id") != "7" {
				t.Fatalf("service token tenant_id = %q", r.Form.Get("tenant_id"))
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "addp_at_quality_service", "token_type": "Bearer", "expires_in": 300, "scope": "addp.api",
			})
		case "/api/v1/system/runtime/execution-authorizations":
			if r.Header.Get("Authorization") != "Bearer addp_at_quality_service" {
				t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
			}
			var request commonClient.IssueExecutionAuthorizationFromExecutionRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			if request.ParentExecutionID != parentExecutionID || request.ExecutionID != childExecutionID ||
				request.Audience != commonExecution.AudienceQuality || request.ExpiresIn != 3600 ||
				len(request.Accesses) != 1 || request.Accesses[0].EngineID != "12" ||
				len(request.Accesses[0].Effects) != 1 || request.Accesses[0].Effects[0] != "read" {
				t.Fatalf("authorization request = %#v", request)
			}
			_ = json.NewEncoder(w).Encode(commonClient.IssuedExecutionAuthorization{
				ID: "91", ExecutionID: childExecutionID, Audience: commonExecution.AudienceQuality,
				Accesses: []commonClient.ExecutionEngineAccessScope{{EngineID: "12", Effects: []string{"read"}}}, ExpiresAt: expiresAt, ActorPrincipalID: "21", TenantID: "7",
				TenantMembershipID: "22", IssuedAuthorizationVersion: "3", SourceType: "user",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	tokenSource, err := commonClient.NewOAuthServiceTokenSource(server.URL, "addp-quality", "0123456789abcdef0123456789abcdef", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	executor := NewCheckExecutor(commonClient.NewSystemServiceClient(server.URL, tokenSource, server.Client()), nil, nil, nil, time.Minute, 1)
	facts, err := executor.issueExecutionAuthorization(context.Background(), 7, childExecutionID, 12, "", &parentExecutionID)
	if err != nil {
		t.Fatalf("issueExecutionAuthorization: %v", err)
	}
	if facts.authorizationID == nil || *facts.authorizationID != 91 || facts.actorPrincipalID == nil || *facts.actorPrincipalID != 21 ||
		facts.tenantMembershipID == nil || *facts.tenantMembershipID != 22 || facts.authorizationVersion == nil || *facts.authorizationVersion != 3 ||
		facts.expiresAt == nil || !facts.expiresAt.Equal(expiresAt) || len(facts.effects) != 1 || facts.effects[0] != "read" {
		t.Fatalf("authorization facts = %#v", facts)
	}
}

func TestIssueExecutionAuthorizationRejectsInvalidSystemResponse(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*commonClient.IssuedExecutionAuthorization)
	}{
		{name: "expired", mutate: func(response *commonClient.IssuedExecutionAuthorization) {
			response.ExpiresAt = time.Now().UTC().Add(-time.Minute)
		}},
		{name: "audience mismatch", mutate: func(response *commonClient.IssuedExecutionAuthorization) { response.Audience = "addp-develop" }},
		{name: "engine expansion", mutate: func(response *commonClient.IssuedExecutionAuthorization) {
			response.Accesses = append(response.Accesses, commonClient.ExecutionEngineAccessScope{EngineID: "13", Effects: []string{"read"}})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			childExecutionID := "2aaeb79d-2bbd-47a2-a8d4-a607ce6d51a5"
			parentExecutionID := "74d980cf-3ced-41ef-81fc-271f89249110"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/system/oauth/token":
					_ = json.NewEncoder(w).Encode(map[string]interface{}{"access_token": "addp_at_quality_service", "token_type": "Bearer", "expires_in": 300, "scope": "addp.api"})
				case "/api/v1/system/runtime/execution-authorizations":
					response := commonClient.IssuedExecutionAuthorization{
						ID: "91", ExecutionID: childExecutionID, Audience: commonExecution.AudienceQuality,
						Accesses:  []commonClient.ExecutionEngineAccessScope{{EngineID: "12", Effects: []string{"read"}}},
						ExpiresAt: time.Now().UTC().Add(time.Hour), ActorPrincipalID: "21", TenantID: "7", TenantMembershipID: "22", IssuedAuthorizationVersion: "3",
					}
					test.mutate(&response)
					_ = json.NewEncoder(w).Encode(response)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			tokenSource, err := commonClient.NewOAuthServiceTokenSource(server.URL, "addp-quality", "0123456789abcdef0123456789abcdef", server.Client())
			if err != nil {
				t.Fatal(err)
			}
			executor := NewCheckExecutor(commonClient.NewSystemServiceClient(server.URL, tokenSource, server.Client()), nil, nil, nil, time.Minute, 1)
			if _, err := executor.issueExecutionAuthorization(context.Background(), 7, childExecutionID, 12, "", &parentExecutionID); err == nil {
				t.Fatal("issueExecutionAuthorization unexpectedly accepted invalid response")
			}
		})
	}
}

func TestExecutionCheckTimeoutRequiresVersionedPositiveBudget(t *testing.T) {
	valid := commonModels.JSONMap{
		"schema_version":   qualityExecutionConfigSchemaVersion,
		"check_timeout_ms": int64(45000),
	}
	got, err := executionCheckTimeout(valid)
	if err != nil || got != 45*time.Second {
		t.Fatalf("executionCheckTimeout() = %v, %v, want 45s", got, err)
	}
	for _, config := range []commonModels.JSONMap{
		{"schema_version": "addp.quality.execution-config/v0", "check_timeout_ms": int64(45000)},
		{"schema_version": qualityExecutionConfigSchemaVersion, "check_timeout_ms": int64(0)},
		{"schema_version": qualityExecutionConfigSchemaVersion},
	} {
		if _, err := executionCheckTimeout(config); err == nil {
			t.Fatalf("executionCheckTimeout(%#v) unexpectedly succeeded", config)
		}
	}
}

func TestExecutionTerminalFieldsDistinguishesTimeout(t *testing.T) {
	timedOut := executionTerminalFields(failExecution(qualityExecutionSQLFailed, context.DeadlineExceeded), true)
	if timedOut["status"] != commonExecution.ExecutionStatusTimeout {
		t.Fatalf("timeout status = %v", timedOut["status"])
	}
	timeoutDetails, ok := timedOut["error_details"].(commonModels.JSONMap)
	if !ok || timeoutDetails["code"] != qualityExecutionTimeout {
		t.Fatalf("timeout error details = %#v", timedOut["error_details"])
	}

	failed := executionTerminalFields(failExecution(qualityExecutionSQLFailed, errors.New("query failed")), false)
	if failed["status"] != commonExecution.ExecutionStatusFailed {
		t.Fatalf("failed status = %v", failed["status"])
	}
	failedDetails, ok := failed["error_details"].(commonModels.JSONMap)
	if !ok || failedDetails["code"] != qualityExecutionSQLFailed {
		t.Fatalf("failed error details = %#v", failed["error_details"])
	}
}

func TestExecutionDeadlineWinsOverCompletedRuleResult(t *testing.T) {
	execErr := executionErrorForDeadline(nil, true)
	if !errors.Is(execErr, context.DeadlineExceeded) {
		t.Fatalf("deadline error = %v, want context deadline exceeded", execErr)
	}
	fields := executionTerminalFields(execErr, true)
	if fields["status"] != commonExecution.ExecutionStatusTimeout {
		t.Fatalf("deadline status = %v, want timeout", fields["status"])
	}
}
