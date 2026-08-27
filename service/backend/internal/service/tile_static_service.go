package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	enginePlugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/format/pmtiles"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/service/internal/models"
)

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
		return emptyStaticVectorTile(), nil
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
	modelProvider, ok := pl.(enginePlugin.EngineCatalogModelProvider)
	if !ok {
		return nil, fmt.Errorf("engine %s does not expose a catalog model", engine.EngineType)
	}
	rangeReader, ok := pl.(enginePlugin.RangeReadableProvider)
	if !ok {
		return nil, fmt.Errorf("engine %s does not support range reads", engine.EngineType)
	}
	providerPath, err := resourcetree.EngineCatalogPathFromLocator(modelProvider.EngineCatalogModel(), loc)
	if err != nil {
		return nil, fmt.Errorf("build static PMTiles provider path: %w", err)
	}
	connInfo := enginePlugin.ConnectionInfo(engine.ConnectionInfo)
	headerBytes, err := readStaticPMTilesRange(ctx, rangeReader, connInfo, providerPath, 0, pmtiles.HeaderSize)
	if err != nil {
		return nil, fmt.Errorf("read static PMTiles header: %w", err)
	}
	header, err := pmtiles.ParseHeaderBytes(headerBytes)
	if err != nil {
		return nil, err
	}
	headerHash, err := pmtiles.HeaderHash(headerBytes)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(headerHash, commonJSON.InterfaceString(snapshot["header_hash"])) {
		return nil, errors.New("static PMTiles header no longer matches the published dependency snapshot")
	}
	archive, err := pmtiles.NewArchive(header, func(readCtx context.Context, offset, length int64) ([]byte, error) {
		return readStaticPMTilesRange(readCtx, rangeReader, connInfo, providerPath, offset, length)
	})
	if err != nil {
		return nil, err
	}
	data, err := archive.GetTile(ctx, uint8(z), uint32(x), uint32(y))
	if errors.Is(err, pmtiles.ErrTileNotFound) {
		return emptyStaticVectorTile(), nil
	}
	if err != nil {
		return nil, err
	}
	return &StaticTile{
		Data:            data,
		ContentType:     "application/vnd.mapbox-vector-tile",
		ContentEncoding: "gzip",
	}, nil
}

var emptyGzipMVT = func() []byte {
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	if err := writer.Close(); err != nil {
		panic(fmt.Sprintf("build empty gzip MVT: %v", err))
	}
	return buffer.Bytes()
}()

func emptyStaticVectorTile() *StaticTile {
	return &StaticTile{
		Data:            emptyGzipMVT,
		ContentType:     "application/vnd.mapbox-vector-tile",
		ContentEncoding: "gzip",
	}
}

func readStaticPMTilesRange(ctx context.Context, reader enginePlugin.RangeReadableProvider, connInfo enginePlugin.ConnectionInfo, path enginePlugin.EngineCatalogPath, offset, length int64) ([]byte, error) {
	rc, err := reader.OpenRange(ctx, connInfo, path, enginePlugin.ReadOptions{Offset: offset, Length: length})
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, length+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != length {
		return nil, fmt.Errorf("range read returned %d bytes, want %d", len(data), length)
	}
	return data, nil
}

func normalizeTileFormat(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "jpg" {
		return "jpeg"
	}
	return value
}
