package service

import (
	"context"
	"encoding/json"
	"reflect"
	"strconv"
	"strings"

	commonClient "github.com/addp/common/client"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/manager/internal/engineaccess"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
)

func model3DGLBPublishedScope(result *models.Model3DGLB) (*commonExecution.ManagerModel3DReadScope, error) {
	if !repository.Model3DGLBUsesExecutionOutput(result) {
		return nil, ErrModel3DGLBSourceAuthorizationRequired
	}
	raw, err := json.Marshal(result.Metadata["model3d_source_scope"])
	var scope commonExecution.ManagerModel3DReadScope
	if err != nil || json.Unmarshal(raw, &scope) != nil || scope.Validate() != nil {
		return nil, ErrModel3DGLBSourceAuthorizationRequired
	}
	primary, err := scope.FileComposition.PrimaryTarget()
	if err != nil || primary.EngineID != result.SourceEngineID || scope.FileComposition.Format != result.SourceFormat {
		return nil, ErrModel3DGLBSourceAuthorizationRequired
	}
	return &scope, nil
}

// The result must come from GetByID's atomic pointer/scope projection. Current
// composition is facts only; current Human read permission is checked below.
func (s *Model3DGLBTaskService) CheckResultRead(ctx context.Context, result *models.Model3DGLB, credential string, human bool) error {
	if s == nil || result == nil {
		return engineaccess.ErrUnavailable
	}
	raw, _ := json.Marshal(map[string]any{"source": map[string]any{"source_engine_id": result.SourceEngineID, "item_locator": result.Locator}})
	engine, err := s.sourceEngineForSnapshot(ctx, &repository.Model3DGLBTaskSnapshot{TenantID: result.TenantID, Config: raw})
	if err != nil {
		return err
	}
	if !managedModel3DFileSource(engine.EngineType, result.SourceFormat) {
		if repository.Model3DGLBUsesExecutionOutput(result) {
			return ErrModel3DGLBSourceAuthorizationRequired
		}
		return nil
	}
	if !human || !strings.HasPrefix(credential, "addp_at_") {
		return commonClient.ErrManagerPreviewCredentialRejected
	}
	scope, err := model3DGLBPublishedScope(result)
	if err != nil || !commonExecution.ManagerModel3DSourceComposition(engine.EngineType, &scope.FileComposition) || strconv.FormatInt(engine.Version, 10) != scope.EngineVersion {
		return ErrModel3DGLBSourceAuthorizationRequired
	}
	if result.ItemID == nil {
		return ErrModel3DGLBSourceAuthorizationRequired
	}
	candidate, err := s.sourceCandidate(ctx, result.TenantID, Model3DGLBExecutionConfig{Source: Model3DGLBSourceConfig{ItemID: *result.ItemID, SourceEngineID: result.SourceEngineID, ItemFingerprint: result.ItemFingerprint, ItemLocator: result.Locator, Format: result.SourceFormat}})
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(candidate.composition, &scope.FileComposition) {
		return ErrModel3DGLBSourceAuthorizationRequired
	}
	if s.sourceConsumer == nil {
		return engineaccess.ErrUnavailable
	}
	return s.sourceConsumer.CheckManagerModel3DResultRead(ctx, credential, commonClient.ManagerModel3DExpectedSource{
		EngineVersion:   scope.EngineVersion,
		FileComposition: *scope.FileComposition.Clone(),
	})
}

func (s *QuickViewService) model3DGLBCompositionCurrent(ctx context.Context, result *models.Model3DGLB) (bool, error) {
	scope, err := model3DGLBPublishedScope(result)
	if err != nil {
		return false, nil
	}
	if s.metaClient == nil || result.ItemID == nil {
		return false, nil
	}
	item, err := s.metaClient.GetItemByIDForTenant(ctx, result.TenantID, *result.ItemID)
	if err != nil {
		return false, err
	}
	candidate, err := model3DGLBCandidateFromMeta(result.TenantID, Model3DGLBSourceConfig{ItemID: *result.ItemID, SourceEngineID: result.SourceEngineID, ItemFingerprint: result.ItemFingerprint, ItemLocator: result.Locator, Format: result.SourceFormat}, item)
	return err == nil && reflect.DeepEqual(candidate.composition, &scope.FileComposition), nil
}
