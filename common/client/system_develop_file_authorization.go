package client

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strconv"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/addp/common/models"
)

// IssuedDevelopFileAuthorization carries System's sealed value and original
// user origin. Neither the scope digest nor an old response is a fresh Allow.
type IssuedDevelopFileAuthorization struct {
	Authorization IssuedExecutionAuthorization    `json:"authorization"`
	Scope         execution.DevelopBoundFileScope `json:"scope"`
	ScopeDigest   string                          `json:"scope_digest"`
}

type DevelopFileAdmissionRequest struct {
	ExecutionID     string `json:"execution_id"`
	ExpectedAttempt int    `json:"expected_attempt"`
	ScopeDigest     string `json:"scope_digest"`
}

type DevelopFileAdmissionObservation struct {
	AuthorizationID string    `json:"authorization_id"`
	ExecutionID     string    `json:"execution_id"`
	ExpectedAttempt int       `json:"expected_attempt"`
	ScopeDigest     string    `json:"scope_digest"`
	State           string    `json:"state"`
	ApprovalID      string    `json:"approval_id,omitempty"`
	ObservedAt      time.Time `json:"observed_at"`
	ExpiresAt       time.Time `json:"expires_at"`
}

type DevelopFileSourceAccessRequest struct {
	ExecutionID string                   `json:"execution_id"`
	Attempt     int                      `json:"attempt"`
	LeaseToken  string                   `json:"lease_token"`
	ScopeDigest string                   `json:"scope_digest"`
	Source      plugin.EngineCatalogPath `json:"source"`
}

type DevelopFileSourceAccessObservation struct {
	AuthorizationID string                   `json:"authorization_id"`
	ExecutionID     string                   `json:"execution_id"`
	Attempt         int                      `json:"attempt"`
	ScopeDigest     string                   `json:"scope_digest"`
	Source          plugin.EngineCatalogPath `json:"source"`
	ObservedAt      time.Time                `json:"observed_at"`
	ExpiresAt       time.Time                `json:"expires_at"`
	Engine          *models.Engine           `json:"engine"`
	// BusinessDeadline is sealed once from original DB StartedAt and human/source
	// eligibility. Renewable machine credentials and leases never extend it.
	BusinessDeadline time.Time `json:"business_deadline" binding:"required"`
}

// IssueDevelopFile sends only an execution and expected intent digest. System
// must fetch the trusted intent and independently prove Meta/IAM facts. The User
// credential lives only in this synchronous call and is never retained.
func (c *SystemExecutionAuthorizationClient) IssueDevelopFile(ctx context.Context, credential, executionID string, intent execution.DevelopFileIntent) (*IssuedDevelopFileAuthorization, error) {
	frozen := intent.Clone()
	digest, err := frozen.Digest()
	if c == nil || c.system == nil || !validExecutionBearer(credential) || !canonicalExecutionUUID(executionID) || err != nil {
		return nil, errors.New("invalid Develop file authorization issuance")
	}
	request := struct {
		ExecutionID  string `json:"execution_id"`
		IntentDigest string `json:"intent_digest"`
	}{executionID, digest}
	var result IssuedDevelopFileAuthorization
	endpoint := "/api/v1/system/auth/execution-authorizations/develop-file-workflows"
	if frozen.Parent != nil {
		endpoint = "/api/v1/system/runtime/execution-authorizations/develop-file-workflows"
	}
	if err := c.system.executionAuthorizationJSON(ctx, endpoint, credential, request, &result, http.StatusCreated); err != nil {
		return nil, err
	}
	if result.Validate() != nil || result.Authorization.ExecutionID != executionID || result.Scope.ValidateAgainstIntent(*frozen) != nil {
		return nil, errors.New("invalid sealed Develop file authorization")
	}
	return result.Clone(), nil
}

func (r *IssuedDevelopFileAuthorization) Validate() error {
	if r == nil || !r.Authorization.ExpiresAt.After(time.Now().UTC()) {
		return errors.New("invalid Develop file authorization window")
	}
	return r.validateBinding()
}

func (r *IssuedDevelopFileAuthorization) validateBinding() error {
	if r == nil || !canonicalExecutionUUID(r.Authorization.ExecutionID) || r.Scope.Validate() != nil ||
		r.Authorization.SourceType != "user" || r.Authorization.SourceDefinitionID != nil || r.Authorization.SourceDefinitionVersion != nil {
		return errors.New("invalid Develop file authorization binding")
	}
	wanted := make(map[string]bool)
	var accesses []ExecutionEngineAccessScope
	for _, source := range r.Scope.Sources {
		id := strconv.FormatUint(uint64(source.Target.EngineID), 10)
		if !wanted[id] {
			wanted[id] = true
			accesses = append(accesses, ExecutionEngineAccessScope{EngineID: id, Effects: []string{"read"}})
		}
	}
	if err := validateIssuedExecutionAuthorizationBinding(&r.Authorization, IssueExecutionAuthorizationRequest{
		ExecutionID: r.Authorization.ExecutionID, Audience: "develop", Accesses: accesses,
	}); err != nil {
		return err
	}
	digest, err := r.Scope.Digest()
	if err != nil || digest != r.ScopeDigest {
		return errors.New("invalid Develop file sealed scope digest")
	}
	return nil
}

func (r *IssuedDevelopFileAuthorization) Clone() *IssuedDevelopFileAuthorization {
	if r == nil {
		return nil
	}
	result := *r
	result.Scope = *r.Scope.Clone()
	result.Authorization.Accesses = append([]ExecutionEngineAccessScope(nil), r.Authorization.Accesses...)
	for i := range result.Authorization.Accesses {
		result.Authorization.Accesses[i].Effects = append([]string(nil), result.Authorization.Accesses[i].Effects...)
	}
	return &result
}

// Admission only observes pending eligibility. It returns no engine, starts no
// runner, consumes no publication release and must not be cached as an Allow.
func (c *SystemServiceClient) CheckDevelopFileAdmission(ctx context.Context, issued IssuedDevelopFileAuthorization, request DevelopFileAdmissionRequest) (*DevelopFileAdmissionObservation, error) {
	issued = *issued.Clone()
	if request.ExecutionID != issued.Authorization.ExecutionID || request.ExpectedAttempt != issued.Scope.ExpectedAttempt || request.ScopeDigest != issued.ScopeDigest {
		return nil, errors.New("invalid Develop file admission request")
	}
	credential, err := c.developFileCredential(ctx, issued)
	if err != nil {
		return nil, err
	}
	var result DevelopFileAdmissionObservation
	path := "/api/v1/system/execution-authorizations/" + issued.Authorization.ID + "/develop-file-admission-checks"
	if err := c.executionAuthorizationJSON(ctx, path, credential, request, &result, http.StatusOK); err != nil {
		return nil, err
	}
	if result.AuthorizationID != issued.Authorization.ID || result.ExecutionID != request.ExecutionID || result.ExpectedAttempt != request.ExpectedAttempt || result.ScopeDigest != request.ScopeDigest ||
		!developFileObservationWindow(result.ObservedAt, result.ExpiresAt, issued.Authorization.ExpiresAt) {
		return nil, errors.New("invalid Develop file admission observation")
	}
	switch result.State {
	case "ready", "awaiting_approval", "rejected":
	default:
		return nil, errors.New("unknown Develop file admission state")
	}
	if issued.Scope.Target == nil {
		if result.State == "awaiting_approval" || result.ApprovalID != "" {
			return nil, errors.New("read-only file execution acquired write approval")
		}
	} else if (result.State == "ready" && !canonicalExecutionUUID(result.ApprovalID)) ||
		(result.ApprovalID != "" && !canonicalExecutionUUID(result.ApprovalID)) {
		return nil, errors.New("invalid Develop file approval binding")
	}
	return &result, nil
}

// Source access is a fresh exact guarded I/O observation, not a general engine
// connection for Runtime or a replacement for current System/Meta checks.
func (c *SystemServiceClient) CheckDevelopFileSourceAccess(ctx context.Context, issued IssuedDevelopFileAuthorization, request DevelopFileSourceAccessRequest) (*DevelopFileSourceAccessObservation, error) {
	issued = *issued.Clone()
	if request.ExecutionID != issued.Authorization.ExecutionID || request.Attempt != issued.Scope.ExpectedAttempt || request.ScopeDigest != issued.ScopeDigest || !canonicalExecutionUUID(request.LeaseToken) {
		return nil, errors.New("invalid Develop file source access request")
	}
	var reference *execution.DevelopFileSourceReference
	for i := range issued.Scope.Sources {
		if reflect.DeepEqual(issued.Scope.Sources[i].Target, request.Source) {
			reference = &issued.Scope.Sources[i]
			break
		}
	}
	if reference == nil {
		return nil, errors.New("Develop file source is outside sealed scope")
	}
	request.Source = reference.Target // The frozen scope owns these segments.
	credential, err := c.developFileCredential(ctx, issued)
	if err != nil {
		return nil, err
	}
	var result DevelopFileSourceAccessObservation
	path := "/api/v1/system/execution-authorizations/" + issued.Authorization.ID + "/develop-file-source-accesses"
	if err := c.executionAuthorizationJSON(ctx, path, credential, request, &result, http.StatusOK); err != nil {
		return nil, err
	}
	engine := result.Engine
	version, _ := strconv.ParseInt(reference.EngineVersion, 10, 64)
	expectedEngineType := map[string]string{"geopackage": "nfs", "geojson": "minio"}[reference.Format]
	if result.AuthorizationID != issued.Authorization.ID || result.ExecutionID != request.ExecutionID || result.Attempt != request.Attempt || result.ScopeDigest != request.ScopeDigest ||
		!reflect.DeepEqual(result.Source, request.Source) || !developFileObservationWindow(result.ObservedAt, result.ExpiresAt, issued.Authorization.ExpiresAt) ||
		result.BusinessDeadline.IsZero() || result.ExpiresAt.After(result.BusinessDeadline) || result.BusinessDeadline.After(issued.Authorization.ExpiresAt) ||
		engine == nil || engine.ID != request.Source.EngineID || engine.TenantID == nil || *engine.TenantID != *c.tenantID || engine.EngineType != expectedEngineType ||
		engine.Version != version || engine.LifecycleState != models.EngineLifecycleActive || engine.DeletedAt != nil || len(engine.ConnectionInfo) == 0 {
		return nil, errors.New("invalid current Develop file source observation")
	}
	return &result, nil
}

func (c *SystemServiceClient) developFileCredential(ctx context.Context, issued IssuedDevelopFileAuthorization) (string, error) {
	if err := issued.Validate(); err != nil {
		return "", err
	}
	return c.developFileTenantCredential(ctx, issued.Authorization.TenantID)
}

func (c *SystemServiceClient) developFileTenantCredential(ctx context.Context, tenant string) (string, error) {
	if c == nil || c.tenantTokens == nil || c.tenantID == nil || *c.tenantID == 0 ||
		tenant != strconv.FormatUint(uint64(*c.tenantID), 10) || ctx.Err() != nil {
		return "", errors.New("Develop file access requires current bound Tenant Service context")
	}
	credential, err := c.tenantTokens.Token(ctx, *c.tenantID)
	if err != nil || !validExecutionBearer(credential) {
		return "", errors.New("Develop file Service credential unavailable")
	}
	return credential, nil
}

func developFileObservationWindow(observed, expires, originalExpiry time.Time) bool {
	return !observed.IsZero() && !observed.After(time.Now().UTC()) && expires.After(observed) &&
		expires.After(time.Now().UTC()) && !expires.After(originalExpiry)
}
