package preview

import (
	"context"
	"fmt"
	"io"

	"github.com/addp/common/engine/plugin"
	commonPMTiles "github.com/addp/common/format/pmtiles"
)

// ReadPMTilesTileFromURI reads one Business PMTiles tile through the source
// engine's range provider. It deliberately does not expose the source storage
// address or create a Manager-owned cache record.
func (r *PreviewResolver) ReadPMTilesTileFromURI(ctx context.Context, locatorURI string, z, x, y int, tenantID *uint) ([]byte, bool, error) {
	req, err := r.ResolveRequestFromURIWithSelection(ctx, locatorURI, 1, 1, "", "", "", plugin.GraphSampleFilter{}, tenantID)
	if err != nil {
		return nil, false, err
	}
	if req == nil || (req.ItemType != "object" && req.ItemType != "file") {
		return nil, false, nil
	}
	if formatNameFromMetaAttributes(req.MetadataAttributes()) != "pmtiles" {
		return nil, false, nil
	}
	providerReq, err := r.buildProviderRequest(ctx, req)
	if err != nil {
		return nil, true, err
	}
	rangeReader, ok := providerReq.EnginePlugin.(plugin.RangeReadableProvider)
	if !ok {
		return nil, true, fmt.Errorf("engine %s does not support PMTiles range reads", providerReq.Engine.EngineType)
	}
	catalog, ok := providerReq.EnginePlugin.(plugin.EngineCatalogProvider)
	if !ok {
		return nil, true, fmt.Errorf("engine %s does not support PMTiles catalog resolution", providerReq.Engine.EngineType)
	}
	entry, err := catalog.ResolvePath(ctx, plugin.ConnectionInfo(providerReq.Engine.ConnectionInfo), providerReq.ProviderPath)
	if err != nil {
		return nil, true, err
	}
	if entry == nil || entry.Storage == nil || entry.Storage.SizeBytes == nil || *entry.Storage.SizeBytes <= 0 {
		return nil, true, fmt.Errorf("PMTiles object size is unavailable")
	}
	size := *entry.Storage.SizeBytes
	readRange := func(readCtx context.Context, offset, length int64) ([]byte, error) {
		if offset < 0 || length <= 0 || offset > size || length > size-offset {
			return nil, fmt.Errorf("invalid PMTiles range offset=%d length=%d size=%d", offset, length, size)
		}
		rc, err := rangeReader.OpenRange(readCtx, plugin.ConnectionInfo(providerReq.Engine.ConnectionInfo), providerReq.ProviderPath, plugin.ReadOptions{Offset: offset, Length: length})
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		data, err := io.ReadAll(rc)
		if err != nil {
			return nil, err
		}
		if int64(len(data)) != length {
			return nil, io.ErrUnexpectedEOF
		}
		return data, nil
	}
	data, err := readPMTilesTileFromRange(ctx, size, z, x, y, readRange)
	return data, true, err
}

func readPMTilesTileFromRange(ctx context.Context, size int64, z, x, y int, readRange commonPMTiles.RangeReadFunc) ([]byte, error) {
	if z < 0 || z > 31 || x < 0 || y < 0 {
		return nil, commonPMTiles.ErrTileNotFound
	}
	headerData, err := readRange(ctx, 0, commonPMTiles.HeaderSize)
	if err != nil {
		return nil, fmt.Errorf("read PMTiles header: %w", err)
	}
	header, err := commonPMTiles.ParseHeaderBytes(headerData)
	if err != nil {
		return nil, err
	}
	if err := commonPMTiles.ValidateHeader(header, size); err != nil {
		return nil, err
	}
	archive, err := commonPMTiles.NewArchive(header, readRange)
	if err != nil {
		return nil, err
	}
	data, err := archive.GetTile(ctx, uint8(z), uint32(x), uint32(y))
	if err == commonPMTiles.ErrTileNotFound {
		return nil, nil
	}
	return data, err
}
