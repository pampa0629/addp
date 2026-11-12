package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"strconv"
	"strings"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/format"
	"github.com/addp/common/format/plugins/envi"
	"github.com/addp/common/format/plugins/fbx"
	"github.com/addp/common/format/plugins/glb"
	commonjson "github.com/addp/common/jsonmap"
	"github.com/addp/manager/internal/engineaccess"
	"github.com/addp/manager/internal/models"
)

type fileStorageReadChecker interface {
	CheckManagerFileRead(context.Context, string, commonClient.ManagerFileExpectedSource) error
}

// This credential requirement is also exposed by resource-actions so the page
// sends the current User through the same authenticated source read path.
func storageFileSourceRequiresUser(target *StorageDataItem) bool {
	return target.Descriptor.DataType == datatype.Model3D || target.Descriptor.Format == "envi"
}

// NFS raw access is limited to formats with a source authorization path.
func storageRawReadSupported(target *StorageDataItem) bool {
	return target.EngineType != "nfs" ||
		target.Descriptor.Format == "filegdb" || target.Descriptor.Format == "envi" ||
		(target.Descriptor.DataType == datatype.Model3D &&
			commonExecution.ManagerModel3DSourceFormat(target.EngineType, target.Descriptor.Format))
}

func (s *MetadataService) SetFileSourceReadChecker(checker fileStorageReadChecker) {
	s.fileSourceChecker = checker
}

// storageRead is confined to one synchronous request. It never persists the
// User credential or upgrades a resource ticket to a User source permission.
type storageRead struct {
	service    *MetadataService
	target     *StorageDataItem
	tenantID   *uint
	credential string
	expected   *commonClient.ManagerFileExpectedSource
	wholeRoot  *plugin.EngineCatalogPath
	headerSHA  string
}

func (s *MetadataService) prepareStorageRead(ctx context.Context, target *StorageDataItem, tenantID *uint, credential string, human bool, plan *models.DownloadPlan) (*storageRead, error) {
	if target == nil || tenantID == nil || *tenantID == 0 || target.EngineID == 0 {
		return nil, ErrEngineAccessDenied
	}
	tiffPreview := plan == nil && target.tiffRangePreview
	if !tiffPreview && !storageRawReadSupported(target) {
		return nil, commonClient.ErrManagerPreviewReadDenied
	}
	read := &storageRead{service: s, target: target, tenantID: tenantID}
	if target.Descriptor.Format == "filegdb" {
		return s.prepareWholeStorageRead(read, credential, human)
	}
	if !tiffPreview && !storageFileSourceRequiresUser(target) {
		return read, nil
	}
	if !human || !strings.HasPrefix(credential, "addp_at_") || strings.ContainsAny(credential, " \t\r\n") {
		return nil, commonClient.ErrManagerPreviewCredentialRejected
	}
	if s.fileSourceChecker == nil {
		return nil, commonClient.ErrManagerPreviewReadUnavailable
	}
	engine, item := target.engine, target.item
	if engine == nil || item == nil || engine.ID != target.EngineID || engine.EngineType != target.EngineType ||
		engine.TenantID == nil || *engine.TenantID != *tenantID || engine.Version <= 0 ||
		(!tiffPreview && !commonExecution.ManagerModel3DSourceFormat(engine.EngineType, target.Descriptor.Format) && target.Descriptor.Format != "envi") {
		return nil, commonClient.ErrManagerPreviewReadDenied
	}
	if err := engineaccess.EnsureAvailable(engine); err != nil {
		return nil, err
	}
	var composition *authorizationfacts.FileCompositionBoundary
	var err error
	switch {
	case tiffPreview:
		composition, err = readTIFFCompositionFromMeta(ctx, s.metaClient, *tenantID, target.ItemFingerprint, item)
		if errors.Is(err, engineaccess.ErrUnavailable) {
			return nil, commonClient.ErrManagerPreviewReadUnavailable
		}
	case target.Descriptor.Format == "envi":
		composition, err = enviCompositionFromMeta(*tenantID, engine.ID, item.ID, target.ItemFingerprint, target.locator, item)
		read.headerSHA = commonjson.String(item.Attributes, "format_info.envi", "header_sha256")
	default:
		var candidate *model3DGLBSourceCandidate
		candidate, err = model3DGLBCandidateFromMeta(*tenantID, Model3DGLBSourceConfig{ItemID: item.ID, SourceEngineID: engine.ID, ItemFingerprint: target.ItemFingerprint, ItemLocator: target.locator, Format: target.Descriptor.Format}, item)
		if err == nil {
			composition = candidate.composition
		}
	}
	if err != nil || !commonExecution.ManagerFileSourceComposition(engine.EngineType, composition) {
		return nil, commonClient.ErrManagerPreviewReadDenied
	}
	expected := commonClient.ManagerFileExpectedSource{EngineVersion: strconv.FormatInt(engine.Version, 10), FileComposition: *composition.Clone()}
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
	if r.expected == nil && r.wholeRoot == nil {
		return r.service.getResourceForTenant(ctx, r.target.EngineID, r.tenantID)
	}
	// Keep the server-verified request snapshot. Model decisions additionally
	// compare its exact Engine version; do not substitute another connection.
	return r.target.engine, nil
}

func (r *storageRead) check(ctx context.Context, storageRef string) error {
	if r.wholeRoot != nil {
		return r.checkWhole(ctx, storageRef)
	}
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
	return r.service.fileSourceChecker.CheckManagerFileRead(ctx, r.credential, *r.expected)
}

func (r *storageRead) stream(ctx context.Context, storageRef, rangeHeader string) (io.ReadCloser, int64, string, string, error) {
	resource, err := r.resource(ctx)
	if err != nil {
		return nil, 0, "", "", err
	}
	check := func() error { return r.check(ctx, storageRef) }
	if r.expected != nil && r.expected.FileComposition.Format == "envi" {
		headerName := ""
		for _, ref := range r.expected.FileComposition.Refs {
			if ref.Role == "header" {
				headerName = ref.Target.StringPath()
			}
		}
		reader, size, _, mime, err := r.service.streamStorageContent(ctx, resource, headerName, "", envi.MaxHeaderBytes, func() error { return r.check(ctx, headerName) })
		if err != nil {
			return nil, 0, "", "", err
		}
		raw, readErr := io.ReadAll(io.LimitReader(reader, envi.MaxHeaderBytes+1))
		closeErr := reader.Close()
		header, parseErr := envi.ParseHeader(ctx, raw)
		if readErr != nil || closeErr != nil || int64(len(raw)) != size || parseErr != nil || header.SHA256 != r.headerSHA {
			return nil, 0, "", "", commonClient.ErrManagerPreviewReadDenied
		}
		if err := check(); err != nil {
			return nil, 0, "", "", err
		}
		if storageRef == headerName {
			options, length, contentRange, err := parseStorageRange(rangeHeader, int64(len(raw)))
			if err != nil {
				return nil, 0, "", "", err
			}
			return io.NopCloser(bytes.NewReader(raw[options.Offset : options.Offset+length])), length, contentRange, mime, nil
		}
		stream, length, contentRange, contentType, err := r.service.streamStorageContent(ctx, resource, storageRef, rangeHeader, envi.MaxRasterBytes, check)
		if err != nil {
			return nil, 0, "", "", err
		}
		validSize := length == header.FileBytes
		if contentRange != "" {
			_, total, found := strings.Cut(contentRange, "/")
			validSize = found && total == strconv.FormatInt(header.FileBytes, 10)
		}
		if !validSize {
			closeErr := stream.Close()
			if closeErr != nil {
				return nil, 0, "", "", closeErr
			}
			return nil, 0, "", "", commonClient.ErrManagerPreviewReadDenied
		}
		return stream, length, contentRange, contentType, nil
	}
	if r.expected != nil && r.expected.FileComposition.Format == "gltf" {
		primary, err := r.expected.FileComposition.PrimaryTarget()
		if err != nil {
			return nil, 0, "", "", commonClient.ErrManagerPreviewReadDenied
		}
		primaryName := primary.StringPath()
		// Every raw member request first re-proves the exact manifest. No new
		// declaration may cause a dependent file to be opened at an old boundary.
		reader, size, _, contentType, err := r.service.streamStorageContent(ctx, resource, primaryName, "", format.MaxGLTFManifestBytes, func() error { return r.check(ctx, primaryName) })
		if err != nil {
			return nil, 0, "", "", err
		}
		raw, readErr := io.ReadAll(io.LimitReader(reader, format.MaxGLTFManifestBytes+1))
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil || int64(len(raw)) != size || int64(len(raw)) > format.MaxGLTFManifestBytes || validateGLTFCompositionBytes(ctx, raw, &r.expected.FileComposition) != nil {
			return nil, 0, "", "", commonClient.ErrManagerPreviewReadDenied
		}
		if err := check(); err != nil {
			return nil, 0, "", "", err
		}
		if storageRef != primaryName {
			return r.service.streamStorageContent(ctx, resource, storageRef, rangeHeader, 0, check)
		}
		options, length, contentRange, err := parseStorageRange(rangeHeader, int64(len(raw)))
		if err != nil {
			return nil, 0, "", "", err
		}
		return io.NopCloser(bytes.NewReader(raw[options.Offset : options.Offset+length])), length, contentRange, contentType, nil
	}
	if r.expected == nil || (r.expected.FileComposition.Format != "glb" && r.expected.FileComposition.Format != "fbx") {
		return r.service.streamStorageContent(ctx, resource, storageRef, rangeHeader, 0, check)
	}
	// Self-contained models may change at the same path after basic observation. Validate
	// exactly the bytes that will be returned, before exposing even a Range.
	budget := glb.MaxCompositionBytes
	if r.expected.FileComposition.Format == "fbx" {
		budget = fbx.MaxCompositionBytes
	}
	reader, size, _, contentType, err := r.service.streamStorageContent(ctx, resource, storageRef, "", budget, check)
	if err != nil {
		return nil, 0, "", "", err
	}
	raw, readErr := io.ReadAll(io.LimitReader(reader, budget+1))
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || int64(len(raw)) != size || int64(len(raw)) > budget {
		return nil, 0, "", "", commonClient.ErrManagerPreviewReadDenied
	}
	provider, err := format.GetModel3DCompositionProvider(format.FormatType(r.expected.FileComposition.Format))
	if err != nil {
		return nil, 0, "", "", commonClient.ErrManagerPreviewReadUnavailable
	}
	refs, err := provider.ReadModel3DComposition(ctx, bytes.NewReader(raw), int64(len(raw)))
	if err != nil || refs == nil || len(refs) != 0 {
		return nil, 0, "", "", commonClient.ErrManagerPreviewReadDenied
	}
	if err := check(); err != nil {
		return nil, 0, "", "", err
	}
	options, length, contentRange, err := parseStorageRange(rangeHeader, int64(len(raw)))
	if err != nil {
		return nil, 0, "", "", err
	}
	return io.NopCloser(bytes.NewReader(raw[options.Offset : options.Offset+length])), length, contentRange, contentType, nil
}
