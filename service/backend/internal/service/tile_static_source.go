package service

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/common/resourcetree"
	"github.com/addp/common/tilepyramid"
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
		commonJSON.String(item.Attributes, "item", "format") != string(format.FormatTilePyramid) ||
		commonJSON.String(item.Attributes, "item", "layout") != format.LayoutWhole {
		return errors.New("static tile source must be a media/tile_pyramid/whole item")
	}
	info := commonJSON.Section(item.Attributes, "format_info.tile_pyramid")
	if info == nil {
		return errors.New("static tile item is missing format_info.tile_pyramid")
	}
	manifest := tilepyramid.Manifest{
		SchemaVersion:   tilepyramid.ManifestSchemaVersion,
		DataType:        "media",
		Format:          string(format.FormatTilePyramid),
		Layout:          format.LayoutWhole,
		TileKind:        commonJSON.InterfaceString(info["tile_kind"]),
		TileFormat:      commonJSON.InterfaceString(info["tile_format"]),
		Scheme:          commonJSON.InterfaceString(info["scheme"]),
		TileMatrixSet:   commonJSON.InterfaceString(info["tile_matrix_set"]),
		TileTemplate:    commonJSON.InterfaceString(info["tile_template"]),
		ContentType:     commonJSON.InterfaceString(info["content_type"]),
		ContentEncoding: commonJSON.InterfaceString(info["content_encoding"]),
		MinZoom:         int(commonJSON.InterfaceInt64(info["min_zoom"])),
		MaxZoom:         int(commonJSON.InterfaceInt64(info["max_zoom"])),
		TileCount:       commonJSON.InterfaceInt64(info["tile_count"]),
	}
	if err := tilepyramid.ValidateManifest(manifest); err != nil {
		return fmt.Errorf("invalid static tile item metadata: %w", err)
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
		"tile_kind":        manifest.TileKind,
		"tile_format":      strings.ToLower(strings.TrimSpace(manifest.TileFormat)),
		"scheme":           tilepyramid.SchemeXYZ,
		"tile_matrix_set":  tilepyramid.DefaultMatrixSet,
		"tile_template":    strings.TrimSpace(manifest.TileTemplate),
		"content_type":     tilepyramid.ContentType(manifest.TileFormat, manifest.ContentType),
		"content_encoding": strings.ToLower(strings.TrimSpace(manifest.ContentEncoding)),
		"min_zoom":         manifest.MinZoom,
		"max_zoom":         manifest.MaxZoom,
		"tile_count":       manifest.TileCount,
		"captured_at":      time.Now().UTC().Format(time.RFC3339Nano),
	}
	return nil
}
