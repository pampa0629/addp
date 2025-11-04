package api

import (
    "net/http"
    "strconv"
    "strings"

    commonAPI "github.com/addp/common/api"
    "github.com/addp/common/logger"
    "github.com/addp/manager/internal/service"
    "github.com/gin-gonic/gin"
)

type TilesHandler struct {
    mvt *service.MVTService
}

func NewTilesHandler(mvt *service.MVTService) *TilesHandler {
    return &TilesHandler{mvt: mvt}
}

// GET /api/tiles/:z/:x/:y.pbf?resource_id=...&schema=...&table=...&geom=geom&cols=a,b,c&srid=4326
func (h *TilesHandler) GetTile(c *gin.Context) {
    // path params
    z, err := strconv.Atoi(c.Param("z"))
    if err != nil {
        commonAPI.BadRequestError(c, "invalid z")
        return
    }
    x, err := strconv.Atoi(c.Param("x"))
    if err != nil {
        commonAPI.BadRequestError(c, "invalid x")
        return
    }
    y, err := strconv.Atoi(c.Param("y"))
    if err != nil {
        commonAPI.BadRequestError(c, "invalid y")
        return
    }

    // query params
    resIDStr := c.Query("resource_id")
    schema := c.Query("schema")
    table := c.Query("table")
    if resIDStr == "" || schema == "" || table == "" {
        commonAPI.BadRequestError(c, "missing required parameters")
        return
    }
    resID64, err := strconv.ParseUint(resIDStr, 10, 64)
    if err != nil {
        commonAPI.BadRequestError(c, "invalid resource_id")
        return
    }
    resourceID := uint(resID64)

    geom := c.DefaultQuery("geom", "geom")
    srid := 4326
    if sr := c.Query("srid"); sr != "" {
        if v, e := strconv.Atoi(sr); e == nil && v > 0 {
            srid = v
        }
    }
    var cols []string
    if colStr := c.Query("cols"); colStr != "" {
        // keep at most 8 columns to ensure preview性能
        parts := strings.Split(colStr, ",")
        for _, p := range parts {
            p = strings.TrimSpace(p)
            if p == "" {
                continue
            }
            cols = append(cols, p)
            if len(cols) >= 8 {
                break
            }
        }
    }

    tenantID := tenantIDFromContext(c)

    tile, err := h.mvt.GetTile(c.Request.Context(), tenantID, resourceID, schema, table, geom, cols, z, x, y, srid)
    if err != nil {
        if err == service.ErrResourceAccessDenied {
            commonAPI.ForbiddenError(c, "resource not accessible")
            return
        }
        logger.L().Error("MVT: failed to generate tile", "error", err)
        commonAPI.InternalServerError(c, err.Error())
        return
    }

    // Return protobuf (MVT). Many clients accept either content-type.
    c.Header("Content-Type", "application/x-protobuf")
    c.Header("Cache-Control", "public, max-age=60")
    c.Data(http.StatusOK, "application/x-protobuf", tile)
}

