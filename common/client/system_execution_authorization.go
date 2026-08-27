package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/models"
	"github.com/google/uuid"
)

type IssueExecutionAuthorizationRequest struct {
	Audience    string                       `json:"audience"`
	ExecutionID string                       `json:"execution_id"`
	Accesses    []ExecutionEngineAccessScope `json:"accesses"`
	ExpiresIn   int64                        `json:"expires_in"`
}

type ExecutionEngineAccessScope struct {
	EngineID string   `json:"engine_id"`
	Effects  []string `json:"effects"`
}

type IssueExecutionAuthorizationFromExecutionRequest struct {
	ParentExecutionID string                       `json:"parent_execution_id"`
	Audience          string                       `json:"audience"`
	ExecutionID       string                       `json:"execution_id"`
	Attempt           int                          `json:"attempt,omitempty"`
	LeaseToken        string                       `json:"lease_token,omitempty"`
	Accesses          []ExecutionEngineAccessScope `json:"accesses"`
	ExpiresIn         int64                        `json:"expires_in"`
}

type IssueExecutionAuthorizationFromServiceDefinitionRequest struct {
	ExecutionID       string                       `json:"execution_id"`
	Accesses          []ExecutionEngineAccessScope `json:"accesses"`
	DefinitionID      string                       `json:"definition_id"`
	DefinitionVersion string                       `json:"definition_version"`
	ExpiresIn         int64                        `json:"expires_in"`
}

type IssuedExecutionAuthorization struct {
	ID                         string                       `json:"id"`
	ExecutionID                string                       `json:"execution_id"`
	Audience                   string                       `json:"audience"`
	Accesses                   []ExecutionEngineAccessScope `json:"accesses"`
	ExpiresAt                  time.Time                    `json:"expires_at"`
	ActorPrincipalID           string                       `json:"actor_principal_id"`
	TenantID                   string                       `json:"tenant_id"`
	TenantMembershipID         string                       `json:"tenant_membership_id"`
	IssuedAuthorizationVersion string                       `json:"issued_authorization_version"`
	SourceType                 string                       `json:"source_type"`
	SourceDefinitionID         *string                      `json:"source_definition_id,omitempty"`
	SourceDefinitionVersion    *string                      `json:"source_definition_version,omitempty"`
}

type ExecutionEngineAccessRequest struct {
	ExecutionID     string   `json:"execution_id"`
	EngineID        string   `json:"engine_id"`
	RequiredEffects []string `json:"required_effects"`
}

type ExecutionEngineAccess struct {
	AuthorizationID string         `json:"authorization_id"`
	ExecutionID     string         `json:"execution_id"`
	Audience        string         `json:"audience"`
	EngineID        string         `json:"engine_id"`
	Effects         []string       `json:"effects"`
	ExpiresAt       time.Time      `json:"expires_at"`
	Engine          *models.Engine `json:"engine"`
}

// TaskExecutionAuthorizationFields converts a validated System response into
// the complete authorization fact set stored on common.task_executions.
func TaskExecutionAuthorizationFields(issued *IssuedExecutionAuthorization) (map[string]interface{}, error) {
	if issued == nil {
		return nil, errors.New("execution authorization is empty")
	}
	parse := func(value string) (*int64, error) {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed <= 0 {
			return nil, fmt.Errorf("invalid IAM ID %q", value)
		}
		return &parsed, nil
	}
	authorizationID, err := parse(issued.ID)
	if err != nil {
		return nil, err
	}
	actorID, err := parse(issued.ActorPrincipalID)
	if err != nil {
		return nil, err
	}
	membershipID, err := parse(issued.TenantMembershipID)
	if err != nil {
		return nil, err
	}
	version, err := parse(issued.IssuedAuthorizationVersion)
	if err != nil {
		return nil, err
	}
	if !issued.ExpiresAt.After(time.Now().UTC()) {
		return nil, errors.New("execution authorization expiry is invalid")
	}
	return map[string]interface{}{
		"execution_authorization_id":   authorizationID,
		"actor_principal_id":           actorID,
		"actor_tenant_membership_id":   membershipID,
		"issued_authorization_version": version,
		"authorization_expires_at":     issued.ExpiresAt,
	}, nil
}

// SystemExecutionAuthorizationClient derives an execution authorization from
// the current request's User Access Token. The token is method-scoped and is
// never retained by the client.
type SystemExecutionAuthorizationClient struct {
	system *SystemServiceClient
}

func NewSystemExecutionAuthorizationClient(baseURL string, httpClient *http.Client) *SystemExecutionAuthorizationClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &SystemExecutionAuthorizationClient{system: &SystemServiceClient{
		baseURL:    strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		httpClient: httpClient,
	}}
}

func (c *SystemExecutionAuthorizationClient) Issue(
	ctx context.Context,
	userAccessToken string,
	request IssueExecutionAuthorizationRequest,
) (*IssuedExecutionAuthorization, error) {
	if c == nil || c.system == nil || c.system.baseURL == "" ||
		!strings.HasPrefix(userAccessToken, "addp_at_") || len(userAccessToken) == len("addp_at_") {
		return nil, errors.New("execution authorization issue requires a System URL and User Access Token")
	}
	var response IssuedExecutionAuthorization
	_, err := c.system.doJSON(
		ctx, http.MethodPost, "/api/v1/system/auth/execution-authorizations",
		userAccessToken, request, &response,
	)
	if err != nil {
		return nil, err
	}
	if err := validateIssuedExecutionAuthorization(&response, request); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *SystemServiceClient) GetExecutionEngineAccess(
	ctx context.Context,
	authorizationID string,
	request ExecutionEngineAccessRequest,
) (*ExecutionEngineAccess, error) {
	if _, err := parseCanonicalPositiveID(authorizationID); err != nil {
		return nil, errors.New("execution engine access requires a canonical authorization ID")
	}
	var response ExecutionEngineAccess
	path := fmt.Sprintf("/api/v1/system/execution-authorizations/%s/engine-accesses", authorizationID)
	if err := c.doTenantJSON(ctx, http.MethodPost, path, request, &response); err != nil {
		return nil, err
	}
	if response.AuthorizationID != authorizationID || response.ExecutionID != request.ExecutionID ||
		response.EngineID != request.EngineID || response.Engine == nil || response.Engine.ID == 0 ||
		strconv.FormatUint(uint64(response.Engine.ID), 10) != request.EngineID || !response.ExpiresAt.After(time.Now().UTC()) ||
		!sameStringSet(response.Effects, request.RequiredEffects) {
		return nil, errors.New("System execution engine access returned an invalid response")
	}
	return &response, nil
}

func (c *SystemServiceClient) IssueExecutionAuthorizationFromExecution(
	ctx context.Context,
	request IssueExecutionAuthorizationFromExecutionRequest,
) (*IssuedExecutionAuthorization, error) {
	if c == nil {
		return nil, errors.New("System service client is required")
	}
	hasAttempt := request.Attempt != 0
	hasLeaseToken := strings.TrimSpace(request.LeaseToken) != ""
	if hasAttempt != hasLeaseToken || request.Attempt < 0 {
		return nil, errors.New("execution authorization lease attempt and token must be provided together")
	}
	if hasLeaseToken {
		parsed, err := uuid.Parse(request.LeaseToken)
		if err != nil || parsed == uuid.Nil || parsed.String() != request.LeaseToken {
			return nil, errors.New("execution authorization lease token must be a canonical UUID")
		}
	}
	var response IssuedExecutionAuthorization
	if err := c.doTenantJSON(
		ctx, http.MethodPost, "/api/v1/system/runtime/execution-authorizations",
		request, &response,
	); err != nil {
		return nil, err
	}
	if response.ExecutionID != request.ExecutionID || response.Audience != request.Audience ||
		!sameExecutionEngineAccessScopes(response.Accesses, request.Accesses) || !response.ExpiresAt.After(time.Now().UTC()) {
		return nil, errors.New("System execution authorization returned an invalid response")
	}
	for _, value := range []string{
		response.ID, response.ActorPrincipalID, response.TenantID,
		response.TenantMembershipID, response.IssuedAuthorizationVersion,
	} {
		if _, err := parseCanonicalPositiveID(value); err != nil {
			return nil, errors.New("System execution authorization returned an invalid IAM ID")
		}
	}
	return &response, nil
}

func (c *SystemServiceClient) IssueExecutionAuthorizationFromServiceDefinition(
	ctx context.Context,
	request IssueExecutionAuthorizationFromServiceDefinitionRequest,
) (*IssuedExecutionAuthorization, error) {
	if c == nil {
		return nil, errors.New("System service client is required")
	}
	var response IssuedExecutionAuthorization
	if err := c.doTenantJSON(
		ctx, http.MethodPost, "/api/v1/system/runtime/execution-authorizations/service-definitions",
		request, &response,
	); err != nil {
		return nil, err
	}
	if response.ExecutionID != request.ExecutionID || response.Audience != "duckdb" ||
		!sameExecutionEngineAccessScopes(response.Accesses, request.Accesses) || response.SourceType != "service_definition" ||
		response.SourceDefinitionID == nil || *response.SourceDefinitionID != request.DefinitionID ||
		response.SourceDefinitionVersion == nil || *response.SourceDefinitionVersion != request.DefinitionVersion ||
		!response.ExpiresAt.After(time.Now().UTC()) {
		return nil, errors.New("System service definition authorization returned an invalid response")
	}
	for _, value := range []string{
		response.ID, response.ActorPrincipalID, response.TenantID,
		response.TenantMembershipID, response.IssuedAuthorizationVersion,
	} {
		if _, err := parseCanonicalPositiveID(value); err != nil {
			return nil, errors.New("System service definition authorization returned an invalid IAM ID")
		}
	}
	return &response, nil
}

func sameStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	values := make(map[string]struct{}, len(left))
	for _, value := range left {
		if value == "" {
			return false
		}
		values[value] = struct{}{}
	}
	if len(values) != len(left) {
		return false
	}
	for _, value := range right {
		if _, exists := values[value]; !exists {
			return false
		}
	}
	return true
}

func sameExecutionEngineAccessScopes(left, right []ExecutionEngineAccessScope) bool {
	if len(left) == 0 || len(left) != len(right) {
		return false
	}
	leftByEngine := make(map[string][]string, len(left))
	for _, access := range left {
		if _, err := parseCanonicalPositiveID(access.EngineID); err != nil {
			return false
		}
		if _, duplicate := leftByEngine[access.EngineID]; duplicate || len(access.Effects) == 0 {
			return false
		}
		leftByEngine[access.EngineID] = access.Effects
	}
	for _, access := range right {
		effects, exists := leftByEngine[access.EngineID]
		if !exists || !sameStringSet(effects, access.Effects) {
			return false
		}
		delete(leftByEngine, access.EngineID)
	}
	return len(leftByEngine) == 0
}

func validateIssuedExecutionAuthorization(
	response *IssuedExecutionAuthorization,
	request IssueExecutionAuthorizationRequest,
) error {
	if response == nil || response.Audience != request.Audience || response.ExecutionID != request.ExecutionID ||
		!response.ExpiresAt.After(time.Now().UTC()) ||
		!sameExecutionEngineAccessScopes(response.Accesses, request.Accesses) {
		return errors.New("System execution authorization returned an invalid response")
	}
	for _, value := range []string{
		response.ID, response.ActorPrincipalID, response.TenantID,
		response.TenantMembershipID, response.IssuedAuthorizationVersion,
	} {
		if _, err := parseCanonicalPositiveID(value); err != nil {
			return errors.New("System execution authorization returned an invalid IAM ID")
		}
	}
	if _, err := uuid.Parse(response.ExecutionID); err != nil {
		return errors.New("System execution authorization returned an invalid execution ID")
	}
	return nil
}

func parseCanonicalPositiveID(value string) (int64, error) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 || strconv.FormatInt(parsed, 10) != value {
		return 0, errors.New("invalid canonical positive ID")
	}
	return parsed, nil
}
