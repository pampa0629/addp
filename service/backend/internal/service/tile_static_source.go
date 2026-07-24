package service

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/common/resourcetree"
)

func (s *TileServiceService) normalizeStaticTileSource(tenantID uint, config map[string]interface{}) error {
	if s.metaClient == nil {
		return errors.New("meta client is required for static tile publication")
	}
	source := commonJSON.InterfaceMap(config["source"])
	locatorValue := strings.TrimSpace(commonJSON.InterfaceString(source["locator"]))
	if locatorValue == "" {
		return errors.New("static tile source.locator is required")
	}
	loc, err := resourcetree.ParseURI(locatorValue)
	if err != nil {
		return fmt.Errorf("invalid static tile source locator: %w", err)
	}
	if loc.ItemID == nil || *loc.ItemID == 0 {
		return errors.New("static tile source locator must include item_id")
	}
	if loc.Type != resourcetree.TypeObject && loc.Type != resourcetree.TypeFile {
		return fmt.Errorf("static tile source must be an object or file item, got %s", loc.Type)
	}
	item, err := s.metaClient.WithTenantID(tenantID).GetItemByID(*loc.ItemID)
	if err != nil {
		return fmt.Errorf("get static tile meta item %d: %w", *loc.ItemID, err)
	}
	if item == nil || item.ID != *loc.ItemID || item.TenantID != tenantID || item.EngineID != loc.EngineID {
		return errors.New("static tile meta item identity does not match locator")
	}
	if strings.Trim(strings.Join(loc.Path, "/"), "/") != strings.Trim(item.FullName, "/") {
		return errors.New("static tile locator path does not match meta item full_name")
	}
	if commonJSON.String(item.Attributes, "item", "data_type") != "media" ||
		commonJSON.String(item.Attributes, "item", "format") != string(format.FormatPMTiles) ||
		commonJSON.String(item.Attributes, "item", "layout") != format.LayoutSingle {
		return errors.New("static tile source must be a media/pmtiles/single item")
	}
	info := commonJSON.Section(item.Attributes, "format_info.pmtiles")
	if info == nil {
		return errors.New("static tile item is missing format_info.pmtiles")
	}
	specVersion := int(commonJSON.InterfaceInt64(info["spec_version"]))
	tileType := strings.ToLower(strings.TrimSpace(commonJSON.InterfaceString(info["tile_type"])))
	tileCompression := strings.ToLower(strings.TrimSpace(commonJSON.InterfaceString(info["tile_compression"])))
	headerHash := strings.ToLower(strings.TrimSpace(commonJSON.InterfaceString(info["header_hash"])))
	minZoom := int(commonJSON.InterfaceInt64(info["min_zoom"]))
	maxZoom := int(commonJSON.InterfaceInt64(info["max_zoom"]))
	center := commonJSON.InterfaceFloat64Slice(info["center"])
	spatial := commonJSON.Section(item.Attributes, "capabilities.spatial")
	extent := commonJSON.InterfaceFloat64Slice(spatial["extent"])
	srid := int(commonJSON.InterfaceInt64(spatial["srid"]))
	crsRef := strings.TrimSpace(commonJSON.InterfaceString(spatial["crs_ref"]))
	if specVersion != 3 || tileType != "mvt" || tileCompression != "gzip" || len(headerHash) != 64 || minZoom < 0 || maxZoom < minZoom || maxZoom > 31 ||
		!validPMTilesCenter(center, minZoom, maxZoom) || !validWGS84Extent(extent) || srid != 4326 || crsRef != "EPSG:4326" {
		return errors.New("static tile item has invalid PMTiles v3 metadata")
	}

	for key := range config {
		delete(config, key)
	}
	config["source"] = map[string]interface{}{
		"locator":   loc.ToURI(),
		"engine_id": loc.EngineID,
		"item_id":   *loc.ItemID,
	}
	config["source_snapshot"] = map[string]interface{}{
		"item_id":          item.ID,
		"fingerprint":      item.Fingerprint,
		"scope_path":       item.FullName,
		"archive_format":   "pmtiles",
		"spec_version":     specVersion,
		"header_hash":      headerHash,
		"tile_format":      tileType,
		"tile_compression": tileCompression,
		"min_zoom":         minZoom,
		"max_zoom":         maxZoom,
		"center":           append([]float64(nil), center...),
		"spatial": map[string]interface{}{
			"srid": srid, "crs_ref": crsRef, "extent": append([]float64(nil), extent...),
		},
		"captured_at": time.Now().UTC().Format(time.RFC3339Nano),
	}
	return nil
}

func validPMTilesCenter(center []float64, minZoom, maxZoom int) bool {
	return len(center) == 3 && finite(center...) &&
		center[0] >= -180 && center[0] <= 180 && center[1] >= -90 && center[1] <= 90 &&
		center[2] >= float64(minZoom) && center[2] <= float64(maxZoom)
}

func validWGS84Extent(extent []float64) bool {
	return len(extent) == 4 && finite(extent...) &&
		extent[0] >= -180 && extent[2] <= 180 && extent[1] >= -90 && extent[3] <= 90 &&
		extent[0] <= extent[2] && extent[1] <= extent[3]
}

func finite(values ...float64) bool {
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
	}
	return true
}
