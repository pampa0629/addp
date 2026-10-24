package service

import (
	"context"
	"io"
	"reflect"
	"strconv"
	"strings"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/datatype"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/manager/internal/engineaccess"
	"github.com/addp/manager/internal/models"
)

type modelStorageReadChecker interface {
	CheckManagerModel3DRead(context.Context, string, commonClient.ManagerModel3DExpectedSource) error
}

func (s *MetadataService) SetModelSourceReadChecker(checker modelStorageReadChecker) {
	s.modelSourceChecker = checker
}

// storageRead is confined to one synchronous request. It never persists the
// User credential or upgrades a resource ticket to a User source permission.
type storageRead struct {
	service    *MetadataService
	target     *StorageDataItem
	tenantID   *uint
	credential string
	expected   *commonClient.ManagerModel3DExpectedSource
}

func (s *MetadataService) prepareStorageRead(target *StorageDataItem, tenantID *uint, credential string, human bool, plan *models.DownloadPlan) (*storageRead, error) {
	if target == nil || tenantID == nil || *tenantID == 0 || target.EngineID == 0 {
		return nil, ErrEngineAccessDenied
	}
	read := &storageRead{service: s, target: target, tenantID: tenantID}
	if target.Descriptor.DataType != datatype.Model3D {
		return read, nil
	}
	if !human || !strings.HasPrefix(credential, "addp_at_") || strings.ContainsAny(credential, " \t\r\n") {
		return nil, commonClient.ErrManagerPreviewCredentialRejected
	}
	if s.modelSourceChecker == nil {
		return nil, commonClient.ErrManagerPreviewReadUnavailable
	}
	engine, item := target.engine, target.item
	if engine == nil || item == nil || engine.ID != target.EngineID || engine.EngineType != target.EngineType ||
		engine.TenantID == nil || *engine.TenantID != *tenantID || engine.Version <= 0 ||
		!commonExecution.ManagerModel3DSourceFormat(engine.EngineType, target.Descriptor.Format) {
		return nil, commonClient.ErrManagerPreviewReadDenied
	}
	if err := engineaccess.EnsureAvailable(engine); err != nil {
		return nil, err
	}
	candidate, err := model3DGLBCandidateFromMeta(*tenantID, Model3DGLBSourceConfig{
		ItemID: item.ID, SourceEngineID: engine.ID, ItemFingerprint: target.ItemFingerprint,
		ItemLocator: target.locator, Format: target.Descriptor.Format,
	}, item)
	if err != nil || !commonExecution.ManagerModel3DSourceComposition(engine.EngineType, candidate.composition) {
		return nil, commonClient.ErrManagerPreviewReadDenied
	}
	expected := commonClient.ManagerModel3DExpectedSource{EngineVersion: strconv.FormatInt(engine.Version, 10), FileComposition: *candidate.composition.Clone()}
	if expected.Validate() != nil {
		return nil, commonClient.ErrManagerPreviewReadDenied
	}
	if plan != nil {
		if len(plan.Refs) != len(expected.FileComposition.Refs) {
			return nil, commonClient.ErrManagerPreviewReadDenied
		}
		seen := map[string]bool{}
		for _, ref := range plan.Refs {
			var match bool
			for _, allowed := range expected.FileComposition.Refs {
				if ref.StorageRef == allowed.Target.StringPath() && ref.Primary == allowed.Primary && ref.Required == allowed.Required && ref.Role == allowed.Role {
					match = true
					break
				}
			}
			if !match || seen[ref.StorageRef] {
				return nil, commonClient.ErrManagerPreviewReadDenied
			}
			seen[ref.StorageRef] = true
		}
	}
	read.expected = &expected
	read.credential = credential
	return read, nil
}

func (r *storageRead) resource(ctx context.Context) (*models.Engine, error) {
	if r.expected == nil {
		return r.service.getResourceForTenant(ctx, r.target.EngineID, r.tenantID)
	}
	// System compares this exact Engine version in the same source decision.
	// Re-fetching a possibly different connection after that decision is forbidden.
	return r.target.engine, nil
}

func (r *storageRead) check(ctx context.Context, storageRef string) error {
	if r.expected == nil {
		return nil
	}
	target, exact, err := streamStorageRefPath(r.target.EngineType, r.target.EngineID, storageRef)
	if err != nil || exact != storageRef {
		return commonClient.ErrManagerPreviewReadDenied
	}
	found := false
	for _, ref := range r.expected.FileComposition.Refs {
		if reflect.DeepEqual(ref.Target, target) {
			found = true
			break
		}
	}
	if !found {
		return commonClient.ErrManagerPreviewReadDenied
	}
	return r.service.modelSourceChecker.CheckManagerModel3DRead(ctx, r.credential, *r.expected)
}

func (r *storageRead) stream(ctx context.Context, storageRef, rangeHeader string) (io.ReadCloser, int64, string, string, error) {
	resource, err := r.resource(ctx)
	if err != nil {
		return nil, 0, "", "", err
	}
	return r.service.streamStorageContent(ctx, resource, storageRef, rangeHeader, func() error { return r.check(ctx, storageRef) })
}
