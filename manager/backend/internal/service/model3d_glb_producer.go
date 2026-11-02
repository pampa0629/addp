package service

import (
	"context"
	"encoding/json"

	commonClient "github.com/addp/common/client"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/engineaccess"
	"github.com/addp/manager/internal/repository"
)

type model3DGLBMetaClient interface {
	GetItemByIDForTenant(context.Context, uint, uint) (*commonModels.MetaItem, error)
}

func (s *Model3DGLBTaskService) SetSourceAuthorizationClients(meta model3DGLBMetaClient, issuer *commonClient.SystemExecutionAuthorizationClient, consumer *commonClient.SystemServiceClient) {
	s.sourceMeta, s.sourceIssuer, s.sourceConsumer = meta, issuer, consumer
}

func (s *Model3DGLBTaskService) sourceCandidate(ctx context.Context, tenant uint, config Model3DGLBExecutionConfig) (*model3DGLBSourceCandidate, error) {
	if s.sourceMeta == nil {
		return nil, engineaccess.ErrUnavailable
	}
	item, err := s.sourceMeta.GetItemByIDForTenant(ctx, tenant, config.Source.ItemID)
	if err != nil {
		return nil, engineaccess.ErrUnavailable
	}
	return model3DGLBCandidateFromMeta(tenant, config.Source, item)
}

// Classify with System before creating pending. A caller cannot opt out by
// omitting a file-source marker or claiming a different engine_type in task config.
func (s *Model3DGLBTaskService) freezeSourceExecution(ctx context.Context, snapshot *repository.FileTaskSnapshot, item *commonExecution.TaskExecution, credential string) (bool, error) {
	var keys map[string]json.RawMessage
	if json.Unmarshal(snapshot.Config, &keys) != nil {
		return false, ErrFileSourceAuthorizationRequired
	}
	for _, key := range []string{"config_version", "owner_task", "source_engine_version", "source_composition"} {
		if _, exists := keys[key]; exists {
			return false, ErrFileSourceAuthorizationRequired
		}
	}
	engine, err := s.sourceEngineForSnapshot(ctx, snapshot)
	var header struct {
		Source struct {
			Format string `json:"format"`
		} `json:"source"`
	}
	if json.Unmarshal(snapshot.Config, &header) != nil {
		return false, ErrFileSourceAuthorizationRequired
	}
	if err != nil || !managedModel3DFileSource(engine.EngineType, header.Source.Format) {
		return false, err
	}
	if s.sourceIssuer == nil || s.sourceConsumer == nil || s.sourceMeta == nil {
		return true, engineaccess.ErrUnavailable
	}
	if credential == "" {
		return true, ErrFileSourceAuthorizationRequired
	}
	config, err := decodeFileModel3DGLBConversion(snapshot.Config)
	if err != nil {
		return true, err
	}
	candidate, err := s.sourceCandidate(ctx, snapshot.TenantID, config)
	if err != nil {
		return true, err
	}
	item.ExecutionConfig, err = freezeFileModel3DGLBConfig(snapshot, engine, candidate, item.ExecutionID, s.bucket)
	return true, err
}

// Only the execution UUID goes to System. The current User bearer exists in
// this request stack only; System re-reads pending/config/Actor/Meta/Grant.
func (s *Model3DGLBTaskService) issueSourceAuthorization(ctx context.Context, expected *commonExecution.TaskExecution, credential string) error {
	return issueFileExecutionAuthorization(ctx, s.repo, s.sourceIssuer, expected, credential, commonExecution.TaskTypeModel3DGLBGeneration)
}
