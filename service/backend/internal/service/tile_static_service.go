package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	enginePlugin "github.com/addp/common/engine/plugin"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/common/tilepyramid"
	"github.com/addp/service/internal/models"
)

var ErrStaticTileOutOfRange = errors.New("static tile coordinates are outside the published range")

type StaticTileEngineClient interface {
	GetEngine(engineID uint) (*commonModels.Engine, error)
}

type StaticTile struct {
	Data            []byte
	ContentType     string
	ContentEncoding string
}

type StaticTileService struct {
	systemClient StaticTileEngineClient
}

func NewStaticTileService(systemClient StaticTileEngineClient) *StaticTileService {
	return &StaticTileService{systemClient: systemClient}
}

func (s *StaticTileService) GetStaticTile(
	ctx context.Context,
	tenantID uint,
	layer *models.TileServiceLayer,
	z, x, y int,
	requestedFormat string,
) (*StaticTile, error) {
	if s == nil || s.systemClient == nil {
		return nil, errors.New("system client is required for static tile access")
	}
	if layer == nil || layer.LayerConfig == nil {
		return nil, errors.New("static tile layer config is required")
	}
	source := commonJSON.InterfaceMap(layer.LayerConfig["source"])
	snapshot := commonJSON.InterfaceMap(layer.LayerConfig["source_snapshot"])
	locatorValue := strings.TrimSpace(commonJSON.InterfaceString(source["locator"]))
	if locatorValue == "" || snapshot == nil {
		return nil, errors.New("static tile source locator and snapshot are required")
	}
	loc, err := resourcetree.ParseURI(locatorValue)
	if err != nil {
		return nil, fmt.Errorf("parse static tile source locator: %w", err)
	}
	tileFormat := normalizeTileFormat(commonJSON.InterfaceString(snapshot["tile_format"]))
	if normalizeTileFormat(requestedFormat) != tileFormat {
		return nil, fmt.Errorf("requested format %q does not match published format %q", requestedFormat, tileFormat)
	}
	minZoom := int(commonJSON.InterfaceInt64(snapshot["min_zoom"]))
	maxZoom := int(commonJSON.InterfaceInt64(snapshot["max_zoom"]))
	if z < minZoom || z > maxZoom || x < 0 || y < 0 {
		return nil, ErrStaticTileOutOfRange
	}
	tilePath, err := tilepyramid.ResolveTilePath(commonJSON.InterfaceString(snapshot["tile_template"]), z, x, y)
	if err != nil {
		return nil, fmt.Errorf("resolve static tile path: %w", err)
	}

	engine, err := s.systemClient.GetEngine(loc.EngineID)
	if err != nil {
		return nil, fmt.Errorf("get static tile engine %d: %w", loc.EngineID, err)
	}
	if engine == nil || engine.ID != loc.EngineID || engine.TenantID == nil || *engine.TenantID != tenantID {
		return nil, errors.New("static tile engine is outside the service tenant")
	}
	pl, err := enginePlugin.Get(engine.EngineType)
	if err != nil {
		return nil, err
	}
	modelProvider, ok := pl.(enginePlugin.CatalogModelProvider)
	if !ok {
		return nil, fmt.Errorf("engine %s does not expose a catalog model", engine.EngineType)
	}
	contentReader, ok := pl.(enginePlugin.ContentReadableProvider)
	if !ok {
		return nil, fmt.Errorf("engine %s does not support content reads", engine.EngineType)
	}
	tileLocator := &resourcetree.ResourceLocator{
		EngineID: loc.EngineID,
		Path:     append(append([]string(nil), loc.Path...), strings.Split(tilePath, "/")...),
		Type:     loc.Type,
	}
	providerPath, err := resourcetree.ProviderCatalogPathFromLocator(modelProvider.CatalogModel(), tileLocator)
	if err != nil {
		return nil, fmt.Errorf("build static tile provider path: %w", err)
	}
	reader, err := contentReader.OpenContent(ctx, enginePlugin.ConnectionInfo(engine.ConnectionInfo), providerPath, enginePlugin.ReadOptions{})
	if err != nil {
		return nil, fmt.Errorf("open static tile content: %w", err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read static tile content: %w", err)
	}
	if len(data) == 0 {
		return nil, errors.New("static tile content is empty")
	}
	return &StaticTile{
		Data:            data,
		ContentType:     tilepyramid.ContentType(tileFormat, commonJSON.InterfaceString(snapshot["content_type"])),
		ContentEncoding: strings.ToLower(strings.TrimSpace(commonJSON.InterfaceString(snapshot["content_encoding"]))),
	}, nil
}

func normalizeTileFormat(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "jpg" {
		return "jpeg"
	}
	return value
}
