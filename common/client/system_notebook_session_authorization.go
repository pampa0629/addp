package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/addp/common/models"
	"github.com/google/uuid"
)

type IssueNotebookSessionAuthorizationRequest struct {
	SessionID string `json:"session_id"`
	TaskID    uint   `json:"task_id"`
	ExpiresIn int64  `json:"expires_in"`
}

type IssuedNotebookSessionAuthorization struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	TaskID    uint      `json:"task_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

type NotebookCatalogChildrenRequest struct {
	SessionID string                   `json:"session_id"`
	EngineID  uint                     `json:"engine_id"`
	Path      EngineCatalogPath        `json:"path"`
	Options   EngineCatalogListOptions `json:"options,omitempty"`
}

type RevokeNotebookSessionAuthorizationRequest struct {
	SessionID string `json:"session_id"`
}

type NotebookExecutionEngineAccessRequest struct {
	SessionID   string `json:"session_id"`
	ExecutionID string `json:"execution_id"`
	EngineID    uint   `json:"engine_id"`
	ExpiresIn   int64  `json:"expires_in"`
}

// SystemNotebookSessionAuthorizationClient derives authorization facts from
// the current request's User Access Token without retaining that token.
type SystemNotebookSessionAuthorizationClient struct {
	system *SystemServiceClient
}

func NewSystemNotebookSessionAuthorizationClient(
	baseURL string,
	httpClient *http.Client,
) *SystemNotebookSessionAuthorizationClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &SystemNotebookSessionAuthorizationClient{system: &SystemServiceClient{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"), httpClient: httpClient,
	}}
}

func (c *SystemNotebookSessionAuthorizationClient) Issue(
	ctx context.Context,
	userAccessToken string,
	request IssueNotebookSessionAuthorizationRequest,
) (*IssuedNotebookSessionAuthorization, error) {
	if c == nil || c.system == nil || c.system.baseURL == "" ||
		!strings.HasPrefix(userAccessToken, "addp_at_") || len(userAccessToken) == len("addp_at_") {
		return nil, errors.New("notebook session authorization issue requires a System URL and User Access Token")
	}
	if !canonicalNotebookUUID(request.SessionID) || request.TaskID == 0 || request.ExpiresIn <= 0 {
		return nil, errors.New("notebook session authorization request is invalid")
	}
	var response IssuedNotebookSessionAuthorization
	_, err := c.system.doJSON(
		ctx, http.MethodPost, "/api/v1/system/auth/notebook-session-authorizations",
		userAccessToken, request, &response,
	)
	if err != nil {
		return nil, err
	}
	if !canonicalNotebookUUID(response.ID) || response.SessionID != request.SessionID ||
		response.TaskID != request.TaskID || !response.ExpiresAt.After(time.Now().UTC()) {
		return nil, errors.New("System notebook session authorization returned an invalid response")
	}
	return &response, nil
}

func (c *SystemServiceClient) ListNotebookCatalogChildren(
	ctx context.Context,
	authorizationID string,
	request NotebookCatalogChildrenRequest,
) ([]EngineCatalogEntry, error) {
	if !canonicalNotebookUUID(authorizationID) || !canonicalNotebookUUID(request.SessionID) || request.EngineID == 0 {
		return nil, errors.New("notebook catalog request is invalid")
	}
	var response EngineCatalogListChildrenResponse
	path := fmt.Sprintf("/api/v1/system/notebook-session-authorizations/%s/catalog/children", authorizationID)
	if err := c.doTenantJSON(ctx, http.MethodPost, path, request, &response); err != nil {
		return nil, err
	}
	if response.Nodes == nil {
		return nil, errors.New("System notebook catalog returned null nodes")
	}
	for _, node := range response.Nodes {
		if node.Path.EngineID != request.EngineID || node.Path.Version != "catalog.path/v1" {
			return nil, errors.New("System notebook catalog returned an invalid path")
		}
	}
	return response.Nodes, nil
}

func (c *SystemServiceClient) ListNotebookEngineDescriptors(
	ctx context.Context,
	authorizationID, sessionID string,
) ([]models.EngineRuntimeDescriptor, error) {
	if !canonicalNotebookUUID(authorizationID) || !canonicalNotebookUUID(sessionID) {
		return nil, errors.New("notebook engine descriptor request is invalid")
	}
	path := fmt.Sprintf(
		"/api/v1/system/notebook-session-authorizations/%s/engine-descriptors?session_id=%s",
		authorizationID,
		url.QueryEscape(sessionID),
	)
	var response []models.EngineRuntimeDescriptor
	if err := c.doTenantJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return nil, err
	}
	if response == nil {
		return nil, errors.New("System notebook engine descriptor list returned null")
	}
	return response, nil
}

func (c *SystemServiceClient) RevokeNotebookSessionAuthorization(
	ctx context.Context,
	authorizationID string,
	request RevokeNotebookSessionAuthorizationRequest,
) error {
	if !canonicalNotebookUUID(authorizationID) || !canonicalNotebookUUID(request.SessionID) {
		return errors.New("notebook catalog revocation is invalid")
	}
	path := fmt.Sprintf("/api/v1/system/notebook-session-authorizations/%s/revocations", authorizationID)
	return c.doTenantJSON(ctx, http.MethodPost, path, request, nil)
}

func (c *SystemServiceClient) DeriveNotebookExecutionEngineAccess(
	ctx context.Context,
	authorizationID string,
	request NotebookExecutionEngineAccessRequest,
) (*ExecutionEngineAccess, error) {
	if !canonicalNotebookUUID(authorizationID) || !canonicalNotebookUUID(request.SessionID) ||
		!canonicalNotebookUUID(request.ExecutionID) || request.EngineID == 0 || request.ExpiresIn <= 0 {
		return nil, errors.New("notebook execution engine access request is invalid")
	}
	path := fmt.Sprintf("/api/v1/system/notebook-session-authorizations/%s/execution-engine-accesses", authorizationID)
	var response ExecutionEngineAccess
	if err := c.doTenantJSON(ctx, http.MethodPost, path, request, &response); err != nil {
		return nil, err
	}
	if response.ExecutionID != request.ExecutionID || response.EngineID != fmt.Sprint(request.EngineID) ||
		response.Audience != "develop" || len(response.Effects) != 1 || response.Effects[0] != "read" ||
		!response.ExpiresAt.After(time.Now().UTC()) {
		return nil, errors.New("System notebook execution engine access returned an invalid response")
	}
	return &response, nil
}

func canonicalNotebookUUID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed != uuid.Nil && parsed.String() == value
}
