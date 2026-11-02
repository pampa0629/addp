package service

import (
	"context"
	"encoding/json"
	"reflect"
	"strconv"

	commonClient "github.com/addp/common/client"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/format/plugins/obj"
)

// Only the owner constructor can bind the official System consumer to the
// exact running claim. There is no caller-supplied Allow callback or credential.
type Model3DGLBSourceAuthorization struct {
	*fileSourceAuthorization
	declarations       []string
	libraries          []obj.MaterialLibraryDeclaration
	expectedConversion Model3DGLBExecutionConfig
}

func newModel3DGLBSourceAuthorization(ctx context.Context, client *commonClient.SystemServiceClient, item *commonExecution.TaskExecution, raw json.RawMessage, candidate *model3DGLBSourceCandidate) (*Model3DGLBSourceAuthorization, error) {
	if err := validateModel3DGLBClaim(ctx, item); err != nil {
		return nil, err
	}
	if client == nil {
		return nil, ErrFileSourceAuthorizationUnavailable
	}
	header, err := decodeModel3DGLBFrozenHeader(raw)
	if err != nil || candidate == nil || candidate.composition == nil || !reflect.DeepEqual(header.Composition, candidate.composition) {
		return nil, ErrFileSourceAuthorizationRequired
	}
	scope, err := commonExecution.NewManagerFileReadScope(raw, header.OwnerTask.ID, header.OwnerTask.Version, header.EngineVersion, header.Composition)
	if err != nil || item.SourceTaskID == nil || *item.SourceTaskID != scope.OwnerTaskID {
		return nil, ErrFileSourceAuthorizationRequired
	}
	lease, _ := commonExecution.LeaseFromContext(ctx)
	conversion, err := decodeFileModel3DGLBConversion(raw)
	if err != nil {
		return nil, err
	}
	result := &Model3DGLBSourceAuthorization{fileSourceAuthorization: &fileSourceAuthorization{client: client, scope: *scope.Clone(), lease: lease, authorizationID: strconv.FormatInt(*item.ExecutionAuthorizationID, 10), expiresAt: *item.AuthorizationExpiresAt}, expectedConversion: conversion}
	if scope.FileComposition.Format == "3ds" || scope.FileComposition.Format == "dae" {
		if candidate.declarations == nil {
			return nil, ErrFileSourceAuthorizationRequired
		}
		result.declarations = append([]string{}, candidate.declarations...)
	}
	if scope.FileComposition.Format == "obj" {
		if candidate.libraries == nil {
			return nil, ErrFileSourceAuthorizationRequired
		}
		result.libraries = cloneOBJLibraries(candidate.libraries)
	}
	return result, nil
}

// ValidateConversion is a pure consistency check, never an Allow. The private
// independent raw decode must equal the exact values handed to the executor.
func (a *Model3DGLBSourceAuthorization) ValidateConversion(actual Model3DGLBExecutionConfig) error {
	if a == nil || !reflect.DeepEqual(a.expectedConversion, actual) {
		return ErrFileSourceAuthorizationRequired
	}
	return nil
}

func (a *Model3DGLBSourceAuthorization) DeclaredTextureRefs() ([]string, error) {
	if a == nil || a.fileSourceAuthorization == nil || a.scope.Validate() != nil || ((a.scope.FileComposition.Format == "3ds" || a.scope.FileComposition.Format == "dae") && a.declarations == nil) {
		return nil, ErrFileSourceAuthorizationRequired
	}
	return append([]string{}, a.declarations...), nil
}

func (a *Model3DGLBSourceAuthorization) OBJMaterialLibraries() ([]obj.MaterialLibraryDeclaration, error) {
	if a == nil || a.fileSourceAuthorization == nil || a.scope.Validate() != nil || a.scope.FileComposition.Format != "obj" || a.libraries == nil {
		return nil, ErrFileSourceAuthorizationRequired
	}
	return cloneOBJLibraries(a.libraries), nil
}

func cloneOBJLibraries(libraries []obj.MaterialLibraryDeclaration) []obj.MaterialLibraryDeclaration {
	cloned := make([]obj.MaterialLibraryDeclaration, len(libraries))
	for i, library := range libraries {
		cloned[i] = obj.MaterialLibraryDeclaration{Reference: library.Reference, TextureRefs: append([]string{}, library.TextureRefs...)}
	}
	return cloned
}
