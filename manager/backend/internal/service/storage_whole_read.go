package service

import (
	"context"
	"strings"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/manager/internal/engineaccess"
)

type wholeStorageReadChecker interface {
	CheckManagerPreviewRead(context.Context, string, []plugin.EngineCatalogPath) error
}

func (s *MetadataService) SetWholeSourceReadChecker(checker wholeStorageReadChecker) {
	s.wholeSourceChecker = checker
}

func (s *MetadataService) prepareWholeStorageRead(read *storageRead, credential string, human bool) (*storageRead, error) {
	if !human || !strings.HasPrefix(credential, "addp_at_") || len(credential) <= len("addp_at_") || strings.ContainsAny(credential, " \t\r\n") {
		return nil, commonClient.ErrManagerPreviewCredentialRejected
	}
	if s.wholeSourceChecker == nil {
		return nil, commonClient.ErrManagerPreviewReadUnavailable
	}
	target := read.target
	engine, item := target.engine, target.item
	if target.EngineType != "nfs" || target.Descriptor.Layout != format.LayoutWhole || target.Descriptor.DataType != datatype.Container ||
		engine == nil || item == nil || engine.ID != target.EngineID || engine.EngineType != target.EngineType ||
		engine.TenantID == nil || *engine.TenantID != *read.tenantID || item.TenantID != *read.tenantID ||
		item.EngineID != target.EngineID || item.ItemType != "file" || item.ID == 0 {
		return nil, commonClient.ErrManagerPreviewReadDenied
	}
	if err := engineaccess.EnsureAvailable(engine); err != nil {
		return nil, err
	}
	root := plugin.FileDirectoryPath(target.EngineID, item.FullName)
	locator, err := resourcetree.ParseURI(target.locator)
	if err != nil || locator.EngineID != target.EngineID || locator.ItemID == nil || *locator.ItemID != item.ID ||
		string(locator.Type) != "file" || strings.Join(locator.Path, "/") != item.FullName ||
		!authorizationfacts.ValidWholeScopeRoot(root) || root.StringPath() != item.FullName ||
		target.ItemFingerprint != commonModels.GenerateItemFingerprint(target.EngineID, item.FullName) {
		return nil, commonClient.ErrManagerPreviewReadDenied
	}
	read.wholeRoot, read.credential = &root, credential
	return read, nil
}

func (r *storageRead) checkWhole(ctx context.Context, storageRef string) error {
	_, exact, err := streamStorageRefPath(r.target.EngineType, r.target.EngineID, storageRef)
	root := r.wholeRoot.StringPath()
	if err != nil || exact != storageRef || (storageRef != root && !strings.HasPrefix(storageRef, root+"/")) {
		return commonClient.ErrManagerPreviewReadDenied
	}
	// The root identifies one DataItem, including future internal members. A
	// prior Allow is never cached across a source operation or Range request.
	return r.service.wholeSourceChecker.CheckManagerPreviewRead(ctx, r.credential, []plugin.EngineCatalogPath{*r.wholeRoot})
}
