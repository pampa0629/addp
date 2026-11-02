package service

import (
	"context"
	"encoding/json"
	"reflect"
	"strconv"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/manager/internal/repository"
)

type rasterCOGSourceAuthorization struct {
	*fileSourceAuthorization
	expected RasterCOGExecutionConfig
}

func newRasterCOGSourceAuthorization(ctx context.Context, client *commonClient.SystemServiceClient, item *commonExecution.TaskExecution, raw json.RawMessage, config RasterCOGExecutionConfig, composition *authorizationfacts.FileCompositionBoundary) (*rasterCOGSourceAuthorization, error) {
	if validateFileSourceClaim(ctx, item, commonExecution.TaskTypeRasterCOGGeneration) != nil || client == nil || composition == nil {
		return nil, ErrFileSourceAuthorizationRequired
	}
	var header struct {
		Owner         struct{ ID, Version string } `json:"owner_task"`
		EngineVersion string                       `json:"source_engine_version"`
	}
	if json.Unmarshal(raw, &header) != nil {
		return nil, ErrFileSourceAuthorizationRequired
	}
	scope, err := commonExecution.NewManagerFileReadScope(raw, header.Owner.ID, header.Owner.Version, header.EngineVersion, composition)
	if err != nil || scope.Purpose() != commonExecution.TaskTypeRasterCOGGeneration || item.SourceTaskID == nil || *item.SourceTaskID != scope.OwnerTaskID {
		return nil, ErrFileSourceAuthorizationRequired
	}
	lease, _ := commonExecution.LeaseFromContext(ctx)
	return &rasterCOGSourceAuthorization{fileSourceAuthorization: &fileSourceAuthorization{client: client, scope: *scope.Clone(), lease: lease, authorizationID: strconv.FormatInt(*item.ExecutionAuthorizationID, 10), expiresAt: *item.AuthorizationExpiresAt}, expected: config}, nil
}

func (a *rasterCOGSourceAuthorization) validateConfig(config RasterCOGExecutionConfig) error {
	if a == nil || a.fileSourceAuthorization == nil || !reflect.DeepEqual(a.expected, config) {
		return ErrFileSourceAuthorizationRequired
	}
	return nil
}

type rasterCOGInputOwner struct {
	repo                *repository.RasterCOGRepository
	resultID, tenantID  uint
	executionID, bucket string
}

func (o *rasterCOGInputOwner) validate(p repository.FileInputPreparation) error {
	if o == nil || o.repo == nil || o.resultID == 0 || p.ExecutionID != o.executionID || p.ValidateForTask(o.tenantID, commonExecution.TaskTypeRasterCOGGeneration) != nil {
		return ErrFileSourceAuthorizationRequired
	}
	for _, object := range p.Objects {
		if object.Bucket != o.bucket {
			return ErrFileSourceAuthorizationRequired
		}
	}
	return nil
}
func (o *rasterCOGInputOwner) prepare(ctx context.Context, p repository.FileInputPreparation) error {
	if err := o.validate(p); err != nil {
		return err
	}
	return o.repo.PrepareExecutionInputs(ctx, o.resultID, p)
}
func (o *rasterCOGInputOwner) finish(ctx context.Context, p repository.FileInputPreparation, settled, cleaned bool) error {
	if err := o.validate(p); err != nil {
		return err
	}
	return o.repo.FinishExecutionInputs(ctx, o.resultID, p, settled, cleaned)
}
