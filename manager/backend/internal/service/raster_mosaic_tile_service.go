package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	commonClient "github.com/addp/common/client"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/rastermosaic"
	"github.com/addp/common/resourcetree"
)

const (
	RasterMosaicTileFormatPNG  = "png"
	RasterMosaicTileFormatWebP = "webp"
	DefaultRasterMosaicGamma   = 0.6
)

var (
	ErrRasterMosaicTileInvalidLocator = errors.New("raster mosaic tile locator is invalid")
	ErrRasterMosaicTileUnsupported    = errors.New("raster mosaic tile preview is unsupported")
	ErrRasterMosaicRuntimeUnavailable = errors.New("raster mosaic runtime is unavailable")
)

type RasterMosaicMetaClient interface {
	GetItemByIDForTenant(tenantID, itemID uint) (*commonModels.MetaItem, error)
}

type RasterMosaicRuntime interface {
	RenderTile(ctx context.Context, req RasterMosaicRuntimeRenderRequest) (*RasterMosaicRuntimeTile, error)
}

type RasterMosaicRuntimeRenderRequest struct {
	TenantID uint                       `json:"tenant_id,omitempty"`
	Dataset  RasterMosaicRuntimeDataset `json:"dataset"`
	Tile     RasterMosaicRuntimeTileReq `json:"tile"`
	Render   RasterMosaicRuntimeOptions `json:"render"`
}

type RasterMosaicRuntimeDataset struct {
	DatasetRootURI string               `json:"dataset_root_uri"`
	ManifestRef    string               `json:"manifest_ref,omitempty"`
	IndexRef       string               `json:"index_ref,omitempty"`
	OverviewRef    string               `json:"overview_ref"`
	GDALEnv        commonModels.JSONMap `json:"gdal_env,omitempty"`
}

type RasterMosaicRuntimeTileReq struct {
	Z        int    `json:"z"`
	X        int    `json:"x"`
	Y        int    `json:"y"`
	TileSize int    `json:"tile_size"`
	CRS      string `json:"crs"`
}

type RasterMosaicRuntimeOptions struct {
	Format     string   `json:"format"`
	Resampling string   `json:"resampling,omitempty"`
	Gamma      float64  `json:"gamma,omitempty"`
	DisplayMin *float64 `json:"display_min,omitempty"`
	DisplayMax *float64 `json:"display_max,omitempty"`
	Invert     bool     `json:"invert,omitempty"`
}

type RasterMosaicRuntimeTile struct {
	Data        []byte
	ContentType string
	Source      string
}

type HTTPRasterMosaicRuntimeClient struct {
	baseURL     string
	tokenSource commonClient.ServiceTokenProvider
	httpClient  *http.Client
}

func NewHTTPRasterMosaicRuntimeClient(baseURL string, tokenSource commonClient.ServiceTokenProvider, timeout time.Duration) *HTTPRasterMosaicRuntimeClient {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &HTTPRasterMosaicRuntimeClient{
		baseURL:     strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		tokenSource: tokenSource,
		httpClient:  &http.Client{Timeout: timeout},
	}
}

func (c *HTTPRasterMosaicRuntimeClient) RenderTile(ctx context.Context, req RasterMosaicRuntimeRenderRequest) (*RasterMosaicRuntimeTile, error) {
	if c == nil || c.baseURL == "" {
		return nil, ErrRasterMosaicRuntimeUnavailable
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal raster mosaic runtime request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/internal/raster-mosaic/render-tile", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.tokenSource == nil || req.TenantID == 0 {
		return nil, errors.New("raster mosaic runtime requires a tenant service token")
	}
	token, err := c.tokenSource.Token(ctx, req.TenantID)
	if err != nil {
		return nil, fmt.Errorf("get raster mosaic runtime token: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRasterMosaicRuntimeUnavailable, err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("raster mosaic runtime returned status %d: %s", resp.StatusCode, string(payload))
	}
	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = contentTypeForRasterMosaicTile(req.Render.Format)
	}
	return &RasterMosaicRuntimeTile{
		Data:        payload,
		ContentType: contentType,
		Source:      normalizeRasterMosaicTileSource(resp.Header.Get("X-ADDP-Mosaic-Tile-Source")),
	}, nil
}

type RasterMosaicTileService struct {
	systemClient UploadSystemClient
	metaClient   RasterMosaicMetaClient
	runtime      RasterMosaicRuntime
	tileSize     int
}

type RasterMosaicTileRequest struct {
	Locator    string
	TenantID   *uint
	Z          int
	X          int
	Y          int
	Format     string
	Gamma      float64
	DisplayMin *float64
	DisplayMax *float64
	Invert     bool
}

func NewRasterMosaicTileService(systemClient UploadSystemClient, metaClient RasterMosaicMetaClient, runtime RasterMosaicRuntime, tileSize int) *RasterMosaicTileService {
	if tileSize != 512 {
		tileSize = 256
	}
	return &RasterMosaicTileService{systemClient: systemClient, metaClient: metaClient, runtime: runtime, tileSize: tileSize}
}

func (s *RasterMosaicTileService) RenderTile(ctx context.Context, req RasterMosaicTileRequest) (*RasterMosaicRuntimeTile, error) {
	if s == nil || s.systemClient == nil || s.metaClient == nil {
		return nil, errors.New("raster mosaic tile service is not configured")
	}
	if s.runtime == nil {
		return nil, ErrRasterMosaicRuntimeUnavailable
	}
	loc, err := resourcetree.ParseURI(strings.TrimSpace(req.Locator))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRasterMosaicTileInvalidLocator, err)
	}
	if loc.ItemID == nil || *loc.ItemID == 0 {
		return nil, fmt.Errorf("%w: locator must include item_id", ErrRasterMosaicTileInvalidLocator)
	}
	if req.TenantID == nil || *req.TenantID == 0 {
		return nil, ErrEngineAccessDenied
	}
	item, err := s.metaClient.GetItemByIDForTenant(*req.TenantID, *loc.ItemID)
	if err != nil {
		return nil, err
	}
	if item == nil || item.EngineID == 0 {
		return nil, ErrRasterMosaicTileUnsupported
	}
	if req.TenantID != nil && item.TenantID != 0 && item.TenantID != *req.TenantID {
		return nil, ErrEngineAccessDenied
	}
	if item.EngineID != loc.EngineID {
		return nil, fmt.Errorf("%w: locator engine_id does not match item", ErrRasterMosaicTileInvalidLocator)
	}
	mosaicInfo, err := rasterMosaicTileInfo(item.Attributes)
	if err != nil {
		return nil, err
	}
	engine, err := s.systemClient.GetEngineForTenant(ctx, *req.TenantID, item.EngineID)
	if err != nil {
		return nil, err
	}
	if !engine.IsUsable() {
		return nil, errors.New("engine is not active")
	}
	if !resourceAccessible(engine, req.TenantID) {
		return nil, ErrEngineAccessDenied
	}
	datasetLoc := &resourcetree.ResourceLocator{
		EngineID: item.EngineID,
		Path:     resourcetree.ParseFullNamePath(engine.EngineType, string(resourcetree.TypeDirectory), item.FullName),
		Type:     resourcetree.TypeDirectory,
	}
	datasetRootURI, gdalEnv, _, err := rasterMosaicGDALRoot(engine, datasetLoc, "")
	if err != nil {
		return nil, err
	}
	format := normalizeRasterMosaicTileFormat(req.Format)
	gamma := normalizeRasterMosaicGamma(req.Gamma)
	return s.runtime.RenderTile(ctx, RasterMosaicRuntimeRenderRequest{
		TenantID: *req.TenantID,
		Dataset: RasterMosaicRuntimeDataset{
			DatasetRootURI: datasetRootURI,
			ManifestRef:    mosaicInfo.ManifestRef,
			IndexRef:       mosaicInfo.IndexRef,
			OverviewRef:    mosaicInfo.OverviewRef,
			GDALEnv:        gdalEnv,
		},
		Tile: RasterMosaicRuntimeTileReq{
			Z:        req.Z,
			X:        req.X,
			Y:        req.Y,
			TileSize: s.tileSize,
			CRS:      "EPSG:3857",
		},
		Render: RasterMosaicRuntimeOptions{
			Format:     format,
			Resampling: "bilinear",
			Gamma:      gamma,
			DisplayMin: req.DisplayMin,
			DisplayMax: req.DisplayMax,
			Invert:     req.Invert,
		},
	})
}

type rasterMosaicPreviewInfo struct {
	ManifestRef string
	IndexRef    string
	OverviewRef string
}

func rasterMosaicTileInfo(attrs map[string]interface{}) (rasterMosaicPreviewInfo, error) {
	if !strings.EqualFold(commonJSON.String(attrs, "item", "format"), "raster_mosaic") {
		return rasterMosaicPreviewInfo{}, ErrRasterMosaicTileUnsupported
	}
	info := commonJSON.Section(attrs, "format_info.raster_mosaic")
	manifestRef := strings.TrimSpace(commonJSON.InterfaceString(info["manifest_ref"]))
	if manifestRef == "" {
		manifestRef = rastermosaic.ManifestFileName
	}
	indexRef := strings.TrimSpace(commonJSON.InterfaceString(info["index_ref"]))
	if indexRef == "" {
		indexRef = rastermosaic.SourceIndexRef
	}
	overviewRef := strings.TrimSpace(commonJSON.InterfaceString(info["overview_ref"]))
	if overviewRef == "" {
		return rasterMosaicPreviewInfo{}, fmt.Errorf("%w: raster mosaic overview_ref is missing", ErrRasterMosaicTileUnsupported)
	}
	return rasterMosaicPreviewInfo{ManifestRef: manifestRef, IndexRef: indexRef, OverviewRef: overviewRef}, nil
}

func normalizeRasterMosaicTileFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case RasterMosaicTileFormatPNG:
		return RasterMosaicTileFormatPNG
	default:
		return RasterMosaicTileFormatWebP
	}
}

func contentTypeForRasterMosaicTile(format string) string {
	switch normalizeRasterMosaicTileFormat(format) {
	case RasterMosaicTileFormatPNG:
		return "image/png"
	default:
		return "image/webp"
	}
}

func normalizeRasterMosaicTileSource(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "overview", "leaf":
		return strings.ToLower(strings.TrimSpace(source))
	default:
		return ""
	}
}

func normalizeRasterMosaicGamma(gamma float64) float64 {
	if gamma > 0 {
		return gamma
	}
	return DefaultRasterMosaicGamma
}
