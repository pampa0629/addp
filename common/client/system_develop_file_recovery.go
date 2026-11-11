package client

import (
	"context"
	"encoding/hex"
	"errors"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
)

type DevelopFilePublicationRecoveryAccess struct {
	History    DevelopFilePublicationHistory `json:"history"`
	ObservedAt time.Time                     `json:"observed_at"`
	ExpiresAt  time.Time                     `json:"expires_at"`
	Engine     *models.Engine                `json:"engine"`
}

// Recovery returns an observation for Resolve/Cleanup only. It is never a
// DevelopFilePublicationObservation and cannot claim another dispatch.
func (c *SystemServiceClient) RecoverDevelopFilePublication(ctx context.Context, issued IssuedDevelopFileAuthorization, approval string, original plugin.NewFilePublicationRequest) (*DevelopFilePublicationRecoveryAccess, error) {
	if issued.validateBinding() != nil || issued.Scope.Target == nil || !canonicalExecutionUUID(approval) ||
		original.Validate() != nil || !reflect.DeepEqual(original.Target, issued.Scope.Target.Target) {
		return nil, errors.New("invalid Develop publication recovery binding")
	}
	credential, err := c.developFileTenantCredential(ctx, issued.Authorization.TenantID)
	if err != nil {
		return nil, err
	}
	path := "/api/v1/system/execution-authorizations/" + issued.Authorization.ID + "/develop-geopackage-publications/" + original.OperationID + "/recovery-accesses"
	var result DevelopFilePublicationRecoveryAccess
	if err := c.executionAuthorizationJSON(ctx, path, credential, nil, &result, http.StatusOK); err != nil {
		return nil, err
	}
	h, e := result.History, result.Engine
	preparation, preparationErr := hex.DecodeString(h.PreparationDigest)
	digest, _ := DevelopFilePublicationRequestDigest(original)
	version, _ := strconv.ParseInt(issued.Scope.Target.EngineVersion, 10, 64)
	if h.AuthorizationID != issued.Authorization.ID || h.ExecutionID != issued.Authorization.ExecutionID || h.Attempt != issued.Scope.ExpectedAttempt ||
		h.ScopeDigest != issued.ScopeDigest || h.ApprovalID != approval || h.OperationID != original.OperationID || h.RequestDigest != digest ||
		h.PrepareAdmittedAt.IsZero() || h.PrepareAdmittedAt.After(result.ObservedAt) ||
		(h.ReleasedAt == nil && h.PreparationDigest != "") ||
		(h.ReleasedAt != nil && (h.ReleasedAt.Before(h.PrepareAdmittedAt) || h.ReleasedAt.After(result.ObservedAt) ||
			preparationErr != nil || len(preparation) != 32 || strings.ToLower(h.PreparationDigest) != h.PreparationDigest)) ||
		result.ObservedAt.IsZero() || result.ObservedAt.After(time.Now()) ||
		!result.ExpiresAt.After(time.Now()) || !result.ExpiresAt.After(result.ObservedAt) ||
		result.ExpiresAt.Sub(result.ObservedAt) > 15*time.Second || e == nil || e.TenantID == nil || *e.TenantID != *c.tenantID ||
		e.ID != original.Target.EngineID || e.Version != version || e.EngineType != "nfs" || e.LifecycleState != models.EngineLifecycleActive || e.DeletedAt != nil || len(e.ConnectionInfo) == 0 {
		return nil, errors.New("invalid Develop publication recovery observation")
	}
	return &result, nil
}
