package service

import (
	"context"
	"errors"

	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
)

type NotebookSessionControlPlane interface {
	Issue(context.Context, string, commonClient.IssueNotebookSessionAuthorizationRequest) (*commonClient.IssuedNotebookSessionAuthorization, error)
	ListChildren(context.Context, uint, string, commonClient.NotebookEngineCatalogChildrenRequest) ([]commonClient.EngineCatalogEntry, error)
	ListEngineDescriptors(context.Context, uint, string, string) ([]commonModels.EngineRuntimeDescriptor, error)
	DeriveExecutionEngineAccess(context.Context, uint, string, commonClient.NotebookExecutionEngineAccessRequest) (*commonClient.ExecutionEngineAccess, error)
	ValidateExecutionEngineAccess(context.Context, uint, string, commonClient.ExecutionEngineAccessRequest) (*commonClient.ExecutionEngineAccess, error)
	Revoke(context.Context, uint, string, commonClient.RevokeNotebookSessionAuthorizationRequest) error
}

type systemNotebookSessionControlPlane struct {
	issuer *commonClient.SystemNotebookSessionAuthorizationClient
	system *commonClient.SystemServiceClient
}

func NewNotebookSessionControlPlane(
	issuer *commonClient.SystemNotebookSessionAuthorizationClient,
	system *commonClient.SystemServiceClient,
) (NotebookSessionControlPlane, error) {
	if issuer == nil || system == nil {
		return nil, errors.New("Notebook Catalog control plane clients are required")
	}
	return &systemNotebookSessionControlPlane{issuer: issuer, system: system}, nil
}

func (c *systemNotebookSessionControlPlane) Issue(
	ctx context.Context,
	userAccessToken string,
	request commonClient.IssueNotebookSessionAuthorizationRequest,
) (*commonClient.IssuedNotebookSessionAuthorization, error) {
	return c.issuer.Issue(ctx, userAccessToken, request)
}

func (c *systemNotebookSessionControlPlane) ListChildren(
	ctx context.Context,
	tenantID uint,
	authorizationID string,
	request commonClient.NotebookEngineCatalogChildrenRequest,
) ([]commonClient.EngineCatalogEntry, error) {
	return c.system.WithTenantID(tenantID).ListNotebookEngineCatalogChildren(ctx, authorizationID, request)
}

func (c *systemNotebookSessionControlPlane) ListEngineDescriptors(
	ctx context.Context,
	tenantID uint,
	authorizationID, sessionID string,
) ([]commonModels.EngineRuntimeDescriptor, error) {
	return c.system.WithTenantID(tenantID).ListNotebookEngineDescriptors(ctx, authorizationID, sessionID)
}

func (c *systemNotebookSessionControlPlane) Revoke(
	ctx context.Context,
	tenantID uint,
	authorizationID string,
	request commonClient.RevokeNotebookSessionAuthorizationRequest,
) error {
	return c.system.WithTenantID(tenantID).RevokeNotebookSessionAuthorization(ctx, authorizationID, request)
}

func (c *systemNotebookSessionControlPlane) DeriveExecutionEngineAccess(
	ctx context.Context,
	tenantID uint,
	authorizationID string,
	request commonClient.NotebookExecutionEngineAccessRequest,
) (*commonClient.ExecutionEngineAccess, error) {
	return c.system.WithTenantID(tenantID).DeriveNotebookExecutionEngineAccess(ctx, authorizationID, request)
}

func (c *systemNotebookSessionControlPlane) ValidateExecutionEngineAccess(
	ctx context.Context,
	tenantID uint,
	authorizationID string,
	request commonClient.ExecutionEngineAccessRequest,
) (*commonClient.ExecutionEngineAccess, error) {
	return c.system.WithTenantID(tenantID).GetExecutionEngineAccess(ctx, authorizationID, request)
}
