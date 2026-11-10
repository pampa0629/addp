package client

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/engine/plugin"
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

// DevelopFilePublicationHistory contains committed System facts only. It grants
// no Provider access and does not establish that the file exists.
type DevelopFilePublicationHistory struct {
	AuthorizationID   string     `json:"authorization_id"`
	ExecutionID       string     `json:"execution_id"`
	Attempt           int        `json:"attempt"`
	ScopeDigest       string     `json:"scope_digest"`
	ApprovalID        string     `json:"approval_id"`
	OperationID       string     `json:"operation_id"`
	RequestDigest     string     `json:"request_digest"`
	PrepareAdmittedAt time.Time  `json:"prepare_admitted_at"`
	ReleasedAt        *time.Time `json:"released_at,omitempty"`
	PreparationDigest string     `json:"preparation_digest,omitempty"`
}

func DevelopFilePublicationRequestDigest(request plugin.NewFilePublicationRequest) (string, error) {
	raw, err := json.Marshal(request)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func (c *SystemServiceClient) ReadDevelopFilePublicationHistory(ctx context.Context, issued IssuedDevelopFileAuthorization, approvalID string, original plugin.NewFilePublicationRequest) (*DevelopFilePublicationHistory, error) {
	issued = *issued.Clone()
	if issued.validateBinding() != nil || !canonicalExecutionUUID(approvalID) || original.Validate() != nil || issued.Scope.Target == nil ||
		!reflect.DeepEqual(original.Target, issued.Scope.Target.Target) {
		return nil, errors.New("invalid Develop file publication history request")
	}
	credential, err := c.developFileTenantCredential(ctx, issued.Authorization.TenantID)
	if err != nil {
		return nil, err
	}
	digest, err := DevelopFilePublicationRequestDigest(original)
	if err != nil {
		return nil, err
	}
	path := "/api/v1/system/execution-authorizations/" + issued.Authorization.ID + "/develop-geopackage-publications/" + original.OperationID
	var history DevelopFilePublicationHistory
	if err := c.boundedAuthorizationJSON(ctx, http.MethodGet, path, credential, nil, &history, http.StatusOK); err != nil {
		return nil, err
	}
	prepared, err := hex.DecodeString(history.PreparationDigest)
	if history.AuthorizationID != issued.Authorization.ID || history.ExecutionID != issued.Authorization.ExecutionID ||
		history.Attempt != issued.Scope.ExpectedAttempt || history.ScopeDigest != issued.ScopeDigest || history.ApprovalID != approvalID ||
		history.OperationID != original.OperationID || history.RequestDigest != digest || history.PrepareAdmittedAt.IsZero() ||
		(history.ReleasedAt == nil && history.PreparationDigest != "") ||
		(history.ReleasedAt != nil && (history.ReleasedAt.Before(history.PrepareAdmittedAt) || err != nil ||
			len(prepared) != 32 || strings.ToLower(history.PreparationDigest) != history.PreparationDigest)) {
		return nil, errors.New("invalid Develop file publication history response")
	}
	return &history, nil
}
