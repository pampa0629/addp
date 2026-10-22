package service

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/engineaccess"
)

var ErrModel3DGLBSourceAuthorizationUnavailable = errors.New("model GLB source authorization unavailable")

// Only the owner constructor can bind the official System consumer to the
// exact running claim. There is no caller-supplied Allow callback or credential.
type Model3DGLBSourceAuthorization struct {
	client             *commonClient.SystemServiceClient
	scope              commonExecution.ManagerModel3DReadScope
	lease              commonExecution.Lease
	authorizationID    string
	expiresAt          time.Time
	declarations       []string
	expectedConversion Model3DGLBExecutionConfig
}

func newModel3DGLBSourceAuthorization(ctx context.Context, client *commonClient.SystemServiceClient, item *commonExecution.TaskExecution, raw json.RawMessage, candidate *model3DGLBSourceCandidate) (*Model3DGLBSourceAuthorization, error) {
	if err := validateModel3DGLBClaim(ctx, item); err != nil {
		return nil, err
	}
	if client == nil {
		return nil, ErrModel3DGLBSourceAuthorizationUnavailable
	}
	header, err := decodeModel3DGLBFrozenHeader(raw)
	if err != nil || candidate == nil || candidate.composition == nil || !reflect.DeepEqual(header.Composition, candidate.composition) {
		return nil, ErrModel3DGLBSourceAuthorizationRequired
	}
	scope, err := commonExecution.NewManagerModel3DReadScope(raw, header.OwnerTask.ID, header.OwnerTask.Version, header.EngineVersion, header.Composition)
	if err != nil || item.SourceTaskID == nil || *item.SourceTaskID != scope.OwnerTaskID {
		return nil, ErrModel3DGLBSourceAuthorizationRequired
	}
	lease, _ := commonExecution.LeaseFromContext(ctx)
	conversion, err := decodeFileModel3DGLBConversion(raw)
	if err != nil {
		return nil, err
	}
	result := &Model3DGLBSourceAuthorization{client: client, scope: *scope.Clone(), lease: lease, authorizationID: strconv.FormatInt(*item.ExecutionAuthorizationID, 10), expiresAt: *item.AuthorizationExpiresAt, expectedConversion: conversion}
	if scope.FileComposition.Format == "3ds" {
		if candidate.declarations == nil {
			return nil, ErrModel3DGLBSourceAuthorizationRequired
		}
		result.declarations = append([]string{}, candidate.declarations...)
	}
	return result, nil
}

// ValidateConversion is a pure consistency check, never an Allow. The private
// independent raw decode must equal the exact values handed to the executor.
func (a *Model3DGLBSourceAuthorization) ValidateConversion(actual Model3DGLBExecutionConfig) error {
	if a == nil || !reflect.DeepEqual(a.expectedConversion, actual) {
		return ErrModel3DGLBSourceAuthorizationRequired
	}
	return nil
}

func (a *Model3DGLBSourceAuthorization) Composition() *authorizationfacts.FileCompositionBoundary {
	if a == nil {
		return nil
	}
	return a.scope.FileComposition.Clone()
}

func (a *Model3DGLBSourceAuthorization) DeclaredTextureRefs() ([]string, error) {
	if a == nil || a.scope.Validate() != nil || (a.scope.FileComposition.Format == "3ds" && a.declarations == nil) {
		return nil, ErrModel3DGLBSourceAuthorizationRequired
	}
	return append([]string{}, a.declarations...), nil
}

func (a *Model3DGLBSourceAuthorization) BeforeSourceRead(ctx context.Context, target plugin.EngineCatalogPath) (*commonModels.Engine, error) {
	if a == nil {
		return nil, ErrModel3DGLBSourceAuthorizationRequired
	}
	matched := false
	for _, ref := range a.scope.FileComposition.Refs {
		if reflect.DeepEqual(ref.Target, target) {
			matched = true
			break
		}
	}
	if !matched {
		return nil, ErrModel3DGLBSourceAuthorizationRequired
	}
	return a.consume(ctx)
}

func (a *Model3DGLBSourceAuthorization) Checkpoint(ctx context.Context) error {
	_, err := a.consume(ctx)
	return err
}
func (a *Model3DGLBSourceAuthorization) BeforePublish(ctx context.Context) error {
	_, err := a.consume(ctx)
	return err
}

func (a *Model3DGLBSourceAuthorization) consume(ctx context.Context) (*commonModels.Engine, error) {
	lease, ok := commonExecution.LeaseFromContext(ctx)
	if a == nil || !ok || lease != a.lease || !a.expiresAt.After(time.Now()) || a.scope.Validate() != nil {
		return nil, ErrModel3DGLBSourceAuthorizationRequired
	}
	if ctx.Err() != nil || a.client == nil {
		return nil, ErrModel3DGLBSourceAuthorizationUnavailable
	}
	observation, err := a.client.WithTenantID(uint(lease.TenantID)).CheckManagerModel3DAccess(ctx, a.authorizationID, commonClient.ManagerModel3DAccessRequest{ExecutionID: lease.ExecutionID, Attempt: lease.Attempt, LeaseToken: lease.Token, SourceReadScope: *a.scope.Clone()})
	if err != nil {
		var upstream *commonClient.SystemAPIError
		if errors.As(err, &upstream) && (upstream.StatusCode == 400 || upstream.StatusCode == 401 || upstream.StatusCode == 403 || upstream.StatusCode == 409) {
			return nil, ErrModel3DGLBSourceAuthorizationRequired
		}
		return nil, ErrModel3DGLBSourceAuthorizationUnavailable
	}
	if observation == nil || observation.ObservedAt.IsZero() || ctx.Err() != nil {
		return nil, ErrModel3DGLBSourceAuthorizationUnavailable
	}
	engine := observation.Engine
	primary, err := a.scope.FileComposition.PrimaryTarget()
	if err != nil || engine == nil || !commonExecution.ManagerModel3DSourceEngineType(engine.EngineType) || engine.ID != primary.EngineID || engine.TenantID == nil || int(*engine.TenantID) != lease.TenantID ||
		strconv.FormatInt(engine.Version, 10) != a.scope.EngineVersion || engineaccess.EnsureAvailable(engine) != nil {
		return nil, ErrModel3DGLBSourceAuthorizationRequired
	}
	return engine, nil
}
