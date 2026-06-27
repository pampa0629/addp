package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	commonAPI "github.com/addp/common/api"
	"github.com/addp/manager/internal/preview"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

type RasterMosaicTileHandler struct {
	service *service.RasterMosaicTileService
}

func NewRasterMosaicTileHandler(service *service.RasterMosaicTileService) *RasterMosaicTileHandler {
	return &RasterMosaicTileHandler{service: service}
}

// GetRasterMosaicTile 返回 raster_mosaic 图片瓦片。
// @Summary 获取 raster mosaic 图片瓦片 | Get raster mosaic image tile
// @Description 按 raster_mosaic item locator 返回 PNG/WebP 图片瓦片；全局 overview COG 分辨率不足时切换 leaf COG 合成。| Return PNG/WebP image tiles for a raster_mosaic item locator; switches to leaf COG composition when the global overview COG is not detailed enough.
// @Tags Manager
// @Produce image/png
// @Produce image/webp
// @Param locator query string true "raster_mosaic item locator"
// @Param gamma query number false "single-band display gamma, default 0.6"
// @Param display_min query number false "single-band display minimum"
// @Param display_max query number false "single-band display maximum"
// @Param invert query boolean false "invert grayscale display"
// @Param z path int true "zoom"
// @Param x path int true "tile x"
// @Param y path string true "tile y with extension, e.g. 12.webp"
// @Success 200 "图片瓦片 | Image tile"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 403 {object} map[string]interface{} "无权访问 | Access denied"
// @Failure 404 {object} map[string]interface{} "资源不存在 | Not found"
// @Failure 503 {object} map[string]interface{} "runtime 不可用 | Runtime unavailable"
// @Router /raster_mosaic/tiles/{z}/{x}/{y} [get]
// @Security BearerAuth
func (h *RasterMosaicTileHandler) GetRasterMosaicTile(c *gin.Context) {
	if h == nil || h.service == nil {
		commonAPI.InternalServerError(c, "raster mosaic tile service is not available")
		return
	}
	locator := strings.TrimSpace(c.Query("locator"))
	if locator == "" {
		missingLocator(c)
		return
	}
	z, ok := parseTileIntParam(c, "z")
	if !ok {
		return
	}
	x, ok := parseTileIntParam(c, "x")
	if !ok {
		return
	}
	y, format, ok := parseTileYFormat(c.Param("y"))
	if !ok {
		commonAPI.BadRequestError(c, "invalid tile y")
		return
	}
	gamma, ok := parseOptionalPositiveFloat(c.Query("gamma"))
	if !ok {
		commonAPI.BadRequestError(c, "invalid gamma")
		return
	}
	displayMin, displayMax, ok := parseOptionalDisplayRange(c.Query("display_min"), c.Query("display_max"))
	if !ok {
		commonAPI.BadRequestError(c, "invalid display range")
		return
	}
	invert, ok := parseOptionalBool(c.Query("invert"))
	if !ok {
		commonAPI.BadRequestError(c, "invalid invert")
		return
	}
	tile, err := h.service.RenderTile(c.Request.Context(), service.RasterMosaicTileRequest{
		Locator:    locator,
		TenantID:   tenantIDFromContext(c),
		Z:          z,
		X:          x,
		Y:          y,
		Format:     format,
		Gamma:      gamma,
		DisplayMin: displayMin,
		DisplayMax: displayMax,
		Invert:     invert,
	})
	if err != nil {
		switch {
		case errors.Is(err, service.ErrEngineAccessDenied), errors.Is(err, preview.ErrEngineAccessDenied):
			accessDeniedToEngine(c)
		case errors.Is(err, service.ErrRasterMosaicTileInvalidLocator), errors.Is(err, service.ErrRasterMosaicTileUnsupported):
			commonAPI.BadRequestError(c, err.Error())
		case errors.Is(err, service.ErrRasterMosaicRuntimeUnavailable):
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		default:
			commonAPI.InternalServerError(c, err.Error())
		}
		return
	}
	c.Header("Content-Type", tile.ContentType)
	c.Header("Cache-Control", "public, max-age=60")
	if tile.Source != "" {
		c.Header("X-ADDP-Mosaic-Tile-Source", tile.Source)
	}
	c.Data(http.StatusOK, tile.ContentType, tile.Data)
}

func parseOptionalDisplayRange(rawMin string, rawMax string) (*float64, *float64, bool) {
	minText := strings.TrimSpace(rawMin)
	maxText := strings.TrimSpace(rawMax)
	if minText == "" && maxText == "" {
		return nil, nil, true
	}
	if minText == "" || maxText == "" {
		return nil, nil, false
	}
	minValue, err := strconv.ParseFloat(minText, 64)
	if err != nil {
		return nil, nil, false
	}
	maxValue, err := strconv.ParseFloat(maxText, 64)
	if err != nil || maxValue <= minValue {
		return nil, nil, false
	}
	return &minValue, &maxValue, true
}

func parseOptionalBool(raw string) (bool, bool) {
	text := strings.ToLower(strings.TrimSpace(raw))
	if text == "" {
		return false, true
	}
	switch text {
	case "1", "true", "yes", "y", "on":
		return true, true
	case "0", "false", "no", "n", "off":
		return false, true
	default:
		return false, false
	}
}

func parseOptionalPositiveFloat(raw string) (float64, bool) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return 0, true
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil || value <= 0 {
		return 0, false
	}
	return value, true
}

func parseTileIntParam(c *gin.Context, key string) (int, bool) {
	value, err := strconv.Atoi(strings.TrimSpace(c.Param(key)))
	if err != nil || value < 0 {
		commonAPI.BadRequestError(c, "invalid tile "+key)
		return 0, false
	}
	return value, true
}

func parseTileYFormat(raw string) (int, string, bool) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return 0, "", false
	}
	format := service.RasterMosaicTileFormatWebP
	if dot := strings.LastIndex(text, "."); dot >= 0 {
		format = text[dot+1:]
		text = text[:dot]
	}
	y, err := strconv.Atoi(text)
	if err != nil || y < 0 {
		return 0, "", false
	}
	switch strings.ToLower(strings.TrimSpace(format)) {
	case service.RasterMosaicTileFormatPNG, service.RasterMosaicTileFormatWebP:
		return y, strings.ToLower(strings.TrimSpace(format)), true
	default:
		return 0, "", false
	}
}
