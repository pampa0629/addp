package service

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	commoni18n "github.com/addp/common/middleware/i18n"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	developi18n "github.com/addp/develop/backend/i18n"
)

// Resolve only persisted source facts; public parameters cannot override extent or CRS.
func (s *WorkflowEngineService) workflowVectorTileFacts(ctx context.Context, tenantID uint, locator *resourcetree.ResourceLocator, params map[string]interface{}) (string, commonModels.JSONMap, error) {
	invalid := func() (string, commonModels.JSONMap, error) {
		return "", nil, fmt.Errorf("%s", commoni18n.ForLanguage("", developi18n.MsgVectorTileFactsInvalid))
	}
	if s.metaClient == nil || locator.ItemID == nil || (locator.Type != resourcetree.TypeFile && locator.Type != resourcetree.TypeObject) {
		return invalid()
	}
	client := s.metaClient.WithTenantID(tenantID)
	item, err := client.GetItemByCatalogPath(locator.EngineID, locator.FullName())
	if err != nil {
		return invalid()
	}
	if item == nil || item.ID != *locator.ItemID || item.TenantID != tenantID || item.EngineID != locator.EngineID || item.FullName != locator.FullName() {
		return invalid()
	}
	descriptor := dataitem.DescriptorFromAttributes(item.Attributes)
	if descriptor.DataType != datatype.Table || !slices.Contains([]string{"shapefile", "geojson"}, descriptor.Format) || (descriptor.Layout != format.LayoutSingle && descriptor.Layout != format.LayoutMulti) {
		return invalid()
	}
	spatial, err := client.GetItemSpatialMetadataByID(item.ID)
	if err != nil || spatial == nil || spatial.SRID <= 0 || spatial.ExtentSRID <= 0 || len(spatial.Extent) != 4 {
		return invalid()
	}
	for _, value := range spatial.Extent {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return invalid()
		}
	}
	if spatial.Extent[0] > spatial.Extent[2] || spatial.Extent[1] > spatial.Extent[3] {
		return invalid()
	}
	zoom := func(name string) (int, bool) {
		raw := params[name]
		switch value := raw.(type) {
		case int:
			return value, value >= 0 && value <= 24
		case float64:
			return int(value), value >= 0 && value <= 24 && math.Trunc(value) == value
		default:
			return 0, false
		}
	}
	minZoom, minOK := zoom("min_zoom")
	maxZoom, maxOK := zoom("max_zoom")
	if !minOK || !maxOK || maxZoom < minZoom || strings.TrimSpace(stringParam(params, "layer_name")) == "" {
		return invalid()
	}
	fields := map[string]bool{}
	for _, field := range spatial.Fields {
		if field.Name != spatial.GeometryColumn {
			fields[field.Name] = true
		}
	}
	if raw, exists := params["attributes"]; exists {
		selected, ok := stringSliceFromInterface(raw)
		if !ok {
			return invalid()
		}
		seen := map[string]bool{}
		for _, field := range selected {
			if !fields[field] || seen[field] {
				return invalid()
			}
			seen[field] = true
		}
	}
	return descriptor.Format, commonModels.JSONMap{"min_zoom": minZoom, "max_zoom": maxZoom,
		"extent": spatial.Extent, "extent_srid": spatial.ExtentSRID, "source_srs": fmt.Sprintf("EPSG:%d", spatial.SRID)}, nil
}
