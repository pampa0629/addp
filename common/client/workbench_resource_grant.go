package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	WorkbenchResourceTypeDataApplication      = "data_application"
	WorkbenchResourceGrantSubjectUser         = "user"
	WorkbenchDataApplicationExecutePermission = "workbench.data_application.execute"
)

type WorkbenchResourceGrantClient struct{ tenantHTTPClient }

func NewWorkbenchResourceGrantClient(baseURL string, tokenSource ServiceTokenProvider, httpClient *http.Client) *WorkbenchResourceGrantClient {
	return &WorkbenchResourceGrantClient{tenantHTTPClient: newTenantHTTPClient(baseURL, tokenSource, httpClient)}
}

func (c *WorkbenchResourceGrantClient) WithTenantID(tenantID uint) *WorkbenchResourceGrantClient {
	if c == nil {
		return nil
	}
	return &WorkbenchResourceGrantClient{tenantHTTPClient: c.tenantHTTPClient.withTenantID(tenantID)}
}

type WorkbenchAssetResourceGrantRequest struct {
	ResourceType string     `json:"resource_type"`
	ResourceID   string     `json:"resource_id"`
	SubjectType  string     `json:"subject_type"`
	SubjectID    string     `json:"subject_id"`
	Permission   string     `json:"permission"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
}

type WorkbenchAssetResourceGrantResponse struct {
	ID             string     `json:"id"`
	SourceIdentity string     `json:"source_identity"`
	Status         string     `json:"status"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	RevokedAt      *time.Time `json:"revoked_at,omitempty"`
}

func NewWorkbenchDataApplicationGrantRequest(resourceID string, subjectID int64, expiresAt *time.Time) (WorkbenchAssetResourceGrantRequest, error) {
	parsed, err := uuid.Parse(strings.TrimSpace(resourceID))
	if err != nil || parsed == uuid.Nil || parsed.String() != resourceID || subjectID <= 0 {
		return WorkbenchAssetResourceGrantRequest{}, errors.New("invalid Workbench data application resource grant target")
	}
	return WorkbenchAssetResourceGrantRequest{
		ResourceType: WorkbenchResourceTypeDataApplication, ResourceID: parsed.String(),
		SubjectType: WorkbenchResourceGrantSubjectUser, SubjectID: strconv.FormatInt(subjectID, 10),
		Permission: WorkbenchDataApplicationExecutePermission, ExpiresAt: expiresAt,
	}, nil
}

func (c *WorkbenchResourceGrantClient) FulfillAssetGrant(ctx context.Context, sourceIdentity int64, request WorkbenchAssetResourceGrantRequest) (*WorkbenchAssetResourceGrantResponse, error) {
	return c.writeAssetGrant(ctx, http.MethodPut, sourceIdentity, request)
}

func (c *WorkbenchResourceGrantClient) RevokeAssetGrant(ctx context.Context, sourceIdentity int64, request WorkbenchAssetResourceGrantRequest) (*WorkbenchAssetResourceGrantResponse, error) {
	return c.writeAssetGrant(ctx, http.MethodDelete, sourceIdentity, request)
}

func (c *WorkbenchResourceGrantClient) writeAssetGrant(ctx context.Context, method string, sourceIdentity int64, request WorkbenchAssetResourceGrantRequest) (*WorkbenchAssetResourceGrantResponse, error) {
	if c == nil || sourceIdentity <= 0 {
		return nil, errors.New("Workbench resource grant client requires a positive Asset authorization ID")
	}
	path := "/api/v1/workbench/runtime/resource-grants/" + url.PathEscape(strconv.FormatInt(sourceIdentity, 10))
	var response WorkbenchAssetResourceGrantResponse
	if err := c.doJSON(ctx, method, path, request, &response); err != nil {
		return nil, fmt.Errorf("Workbench resource grant %s: %w", strings.ToLower(method), err)
	}
	if response.SourceIdentity != strconv.FormatInt(sourceIdentity, 10) || response.ID == "" ||
		(response.Status != "effective" && response.Status != "revoked") {
		return nil, errors.New("Workbench resource grant returned an invalid response")
	}
	return &response, nil
}
