package service

import (
	"context"
	"encoding/json"
	"github.com/addp/common/resourcetree"
	"reflect"
	"strconv"
	"strings"

	commonClient "github.com/addp/common/client"
	commonExecution "github.com/addp/common/execution"
	commonjson "github.com/addp/common/jsonmap"
	"github.com/addp/manager/internal/engineaccess"
	"github.com/addp/manager/internal/models"
)

func enviRasterCOGVersionMatches(result *models.RasterCOG, raster *RasterQuickViewSource) bool {
	if result == nil || raster == nil || raster.HeaderSHA256 == "" || raster.RasterSHA256 == "" {
		return false
	}
	return commonjson.String(result.Metadata, "source_version", "header_sha256") == raster.HeaderSHA256 && commonjson.String(result.Metadata, "source_version", "raster_sha256") == raster.RasterSHA256
}

// COG bytes are a derived copy, not an independent permission grant. ENVI
// results bind the generated pair and version to the current Human decision.
func (s *RasterCOGTaskService) CheckResultRead(ctx context.Context, result *models.RasterCOG, credential string, human bool) error {
	if s == nil || result == nil {
		return engineaccess.ErrUnavailable
	}
	itemID := uint(0)
	if result.ItemID != nil {
		itemID = *result.ItemID
	}
	locator, err := resourcetree.ParseURI(result.Locator)
	if err != nil || locator.EngineID != result.SourceEngineID {
		return ErrFileSourceAuthorizationRequired
	}
	if s.sourceMeta == nil {
		return engineaccess.ErrUnavailable
	}
	item, err := s.sourceMeta.GetItemSnapshot(ctx, result.TenantID, result.SourceEngineID, itemID, locator.FullName())
	if err != nil {
		return engineaccess.ErrUnavailable
	}
	if item == nil {
		return engineaccess.ErrUnavailable
	}
	actual := commonjson.String(item.Attributes, "item", "format")
	_, hasScope := result.Metadata["file_source_scope"]
	if actual != "envi" {
		if hasScope {
			return ErrFileSourceAuthorizationRequired
		}
		return nil
	}
	if !human || !strings.HasPrefix(credential, "addp_at_") {
		return commonClient.ErrManagerPreviewCredentialRejected
	}
	var scope commonExecution.ManagerFileReadScope
	raw, err := json.Marshal(result.Metadata["file_source_scope"])
	if err != nil || json.Unmarshal(raw, &scope) != nil || scope.Purpose() != commonExecution.TaskTypeRasterCOGGeneration {
		return ErrFileSourceAuthorizationRequired
	}
	composition, err := enviCompositionFromMeta(result.TenantID, result.SourceEngineID, itemID, result.ItemFingerprint, result.Locator, item)
	if err != nil || !reflect.DeepEqual(composition, &scope.FileComposition) {
		return ErrFileSourceAuthorizationRequired
	}
	header, raster, err := enviRasterVersion(item)
	if err != nil || !enviRasterCOGVersionMatches(result, &RasterQuickViewSource{HeaderSHA256: header, RasterSHA256: raster}) {
		return ErrFileSourceAuthorizationRequired
	}
	if s.sourceConsumer == nil || s.sourceEngines == nil {
		return engineaccess.ErrUnavailable
	}
	engine, err := s.sourceEngines.GetEngineForTenant(ctx, result.TenantID, result.SourceEngineID)
	if err != nil {
		return engineaccess.ErrUnavailable
	}
	if engine == nil || !commonExecution.ManagerFileSourceComposition(engine.EngineType, composition) || strconv.FormatInt(engine.Version, 10) != scope.EngineVersion {
		return ErrFileSourceAuthorizationRequired
	}
	return s.sourceConsumer.CheckManagerFileRead(ctx, credential, commonClient.ManagerFileExpectedSource{EngineVersion: scope.EngineVersion, FileComposition: *composition})
}
