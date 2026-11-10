package client

import (
	"context"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/models"
)

const (
	DevelopFilePublicationPrepare = "prepare"
	DevelopFilePublicationRelease = "release"
)

type DevelopFilePublicationRequest struct {
	ExecutionID       string `json:"execution_id"`
	Attempt           int    `json:"attempt"`
	LeaseToken        string `json:"lease_token"`
	ScopeDigest       string `json:"scope_digest"`
	ApprovalID        string `json:"approval_id"`
	OperationID       string `json:"operation_id"`
	Stage             string `json:"stage"`
	PreparationDigest string `json:"preparation_digest,omitempty"`
}

func (r DevelopFilePublicationRequest) Validate() error {
	digest := func(value string) bool {
		decoded, err := hex.DecodeString(value)
		return err == nil && len(decoded) == 32 && strings.ToLower(value) == value
	}
	if !canonicalExecutionUUID(r.ExecutionID) || r.Attempt <= 0 || !canonicalExecutionUUID(r.LeaseToken) ||
		!canonicalExecutionUUID(r.ApprovalID) || !canonicalExecutionUUID(r.OperationID) || !digest(r.ScopeDigest) {
		return errors.New("invalid Develop file publication binding")
	}
	if (r.Stage == DevelopFilePublicationPrepare && r.PreparationDigest == "") ||
		(r.Stage == DevelopFilePublicationRelease && digest(r.PreparationDigest)) {
		return nil
	}
	return errors.New("invalid Develop file publication stage")
}

type DevelopFilePublicationObservation struct {
	AuthorizationID   string         `json:"authorization_id"`
	ExecutionID       string         `json:"execution_id"`
	Attempt           int            `json:"attempt"`
	ScopeDigest       string         `json:"scope_digest"`
	ApprovalID        string         `json:"approval_id"`
	OperationID       string         `json:"operation_id"`
	Stage             string         `json:"stage"`
	PreparationDigest string         `json:"preparation_digest,omitempty"`
	ObservedAt        time.Time      `json:"observed_at"`
	ExpiresAt         time.Time      `json:"expires_at"`
	BusinessDeadline  time.Time      `json:"business_deadline"`
	Engine            *models.Engine `json:"engine,omitempty"`
}

func (c *SystemServiceClient) AuthorizeDevelopFilePublication(ctx context.Context, issued IssuedDevelopFileAuthorization, request DevelopFilePublicationRequest) (*DevelopFilePublicationObservation, error) {
	issued = *issued.Clone()
	if request.Validate() != nil || issued.Scope.Target == nil || request.ExecutionID != issued.Authorization.ExecutionID ||
		request.Attempt != issued.Scope.ExpectedAttempt || request.ScopeDigest != issued.ScopeDigest {
		return nil, errors.New("invalid Develop file publication request")
	}
	credential, err := c.developFileCredential(ctx, issued)
	if err != nil {
		return nil, err
	}
	var result DevelopFilePublicationObservation
	path := "/api/v1/system/execution-authorizations/" + issued.Authorization.ID + "/develop-geopackage-publications"
	if err := c.executionAuthorizationJSON(ctx, path, credential, request, &result, http.StatusOK); err != nil {
		return nil, err
	}
	if result.AuthorizationID != issued.Authorization.ID || result.ExecutionID != request.ExecutionID ||
		result.Attempt != request.Attempt || result.ScopeDigest != request.ScopeDigest || result.ApprovalID != request.ApprovalID ||
		result.OperationID != request.OperationID || result.Stage != request.Stage || result.PreparationDigest != request.PreparationDigest ||
		!developFileObservationWindow(result.ObservedAt, result.ExpiresAt, issued.Authorization.ExpiresAt) ||
		result.BusinessDeadline.IsZero() || result.ExpiresAt.After(result.BusinessDeadline) ||
		result.BusinessDeadline.After(issued.Authorization.ExpiresAt) {
		return nil, errors.New("invalid current Develop file publication observation")
	}
	if request.Stage == DevelopFilePublicationPrepare {
		target, engine := issued.Scope.Target, result.Engine
		version, _ := strconv.ParseInt(target.EngineVersion, 10, 64)
		if engine == nil || engine.ID != target.Target.EngineID || engine.TenantID == nil || *engine.TenantID != *c.tenantID ||
			engine.EngineType != "nfs" || engine.Version != version || engine.LifecycleState != models.EngineLifecycleActive ||
			engine.DeletedAt != nil || len(engine.ConnectionInfo) == 0 {
			return nil, errors.New("invalid Develop file target engine")
		}
	} else if result.Engine != nil {
		return nil, errors.New("Develop file release must not return a connection")
	}
	return &result, nil
}
