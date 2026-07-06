package api

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"

	commonapi "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

type RasterMosaicProgressEventRequest struct {
	Phase           string               `json:"phase"`
	Event           string               `json:"event"`
	Message         string               `json:"message,omitempty"`
	TotalFiles      int64                `json:"total_files,omitempty"`
	ProcessedFiles  int64                `json:"processed_files,omitempty"`
	FailedFiles     int64                `json:"failed_files,omitempty"`
	CurrentFile     string               `json:"current_file,omitempty"`
	FileProgress    *int                 `json:"file_progress,omitempty"`
	OverallProgress *float64             `json:"overall_progress,omitempty"`
	CurrentZoom     int                  `json:"current_zoom,omitempty"`
	MaxZoom         int                  `json:"max_zoom,omitempty"`
	TilesProcessed  int                  `json:"tiles_processed,omitempty"`
	TilesTotal      int                  `json:"tiles_total_estimate,omitempty"`
	ProgressPercent *float64             `json:"progress_percent,omitempty"`
	ElapsedSeconds  *float64             `json:"elapsed_seconds,omitempty"`
	RemainingSec    *float64             `json:"estimated_remaining_seconds,omitempty"`
	Metadata        commonModels.JSONMap `json:"metadata,omitempty"`
}

type RasterMosaicProgressEventResponse struct {
	ExecutionID string `json:"execution_id"`
	Status      string `json:"status"`
}

func (h *TaskProviderHandler) RecordRasterMosaicExecutionProgressEvent(c *gin.Context) {
	h.RecordManagerExecutionProgressEvent(c)
}

func (h *TaskProviderHandler) RecordManagerExecutionProgressEvent(c *gin.Context) {
	if h == nil || h.taskExecRepo == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "task execution repository is unavailable"})
		return
	}
	req, err := decodeRasterMosaicProgressEventRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	executionID := c.Param("execution_id")
	tenantID := c.GetUint("tenant_id")
	exec, err := h.taskExecRepo.GetByExecutionID(c.Request.Context(), executionID, int(tenantID))
	if err != nil {
		if errors.Is(err, commonapi.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "执行记录不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	switch exec.TaskType {
	case commonExecution.TaskTypeVectorTileCacheGeneration:
		h.recordTileCacheProgressEvent(c, tenantID, executionID, req)
	case commonExecution.TaskTypeRasterMosaicGeneration:
		h.recordRasterMosaicProgressEvent(c, tenantID, executionID, req)
	case commonExecution.TaskTypePointCloudCOPCGeneration:
		h.recordPointCloudCOPCProgressEvent(c, tenantID, executionID, req)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "execution task_type does not accept progress events"})
	}
}

func (h *TaskProviderHandler) recordRasterMosaicProgressEvent(c *gin.Context, tenantID uint, executionID string, req RasterMosaicProgressEventRequest) {
	if h.rasterMosaicTaskSvc == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "raster mosaic generation task service is unavailable"})
		return
	}
	event := service.RasterMosaicProgressEvent{
		Phase:           req.Phase,
		Event:           req.Event,
		Message:         req.Message,
		TotalFiles:      req.TotalFiles,
		ProcessedFiles:  req.ProcessedFiles,
		FailedFiles:     req.FailedFiles,
		CurrentFile:     req.CurrentFile,
		FileProgress:    req.FileProgress,
		OverallProgress: progressIntPointer(req.OverallProgress),
		Metadata:        req.Metadata,
	}
	if err := h.rasterMosaicTaskSvc.RecordProgressEvent(c.Request.Context(), tenantID, executionID, event); err != nil {
		switch {
		case errors.Is(err, commonapi.ErrNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "执行记录不存在"})
		case errors.Is(err, service.ErrRasterMosaicProgressTargetMismatch):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		case errors.Is(err, service.ErrRasterMosaicExecutionCompleted):
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	c.JSON(http.StatusAccepted, RasterMosaicProgressEventResponse{
		ExecutionID: executionID,
		Status:      "accepted",
	})
}

func (h *TaskProviderHandler) recordTileCacheProgressEvent(c *gin.Context, tenantID uint, executionID string, req RasterMosaicProgressEventRequest) {
	if h.tileCacheTaskSvc == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "vector tile cache generation task service is unavailable"})
		return
	}
	event := service.TileCacheProgressEvent{
		Phase:              req.Phase,
		Event:              req.Event,
		Message:            req.Message,
		CurrentZoom:        req.CurrentZoom,
		MaxZoom:            req.MaxZoom,
		TilesProcessed:     req.TilesProcessed,
		TilesTotalEstimate: req.TilesTotal,
		ProgressPercent:    req.ProgressPercent,
		OverallProgress:    req.OverallProgress,
		ElapsedSeconds:     req.ElapsedSeconds,
		RemainingSeconds:   req.RemainingSec,
		Metadata:           req.Metadata,
	}
	if err := h.tileCacheTaskSvc.RecordProgressEvent(c.Request.Context(), tenantID, executionID, event); err != nil {
		switch {
		case errors.Is(err, commonapi.ErrNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "执行记录不存在"})
		case errors.Is(err, service.ErrTileCacheProgressTargetMismatch):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		case errors.Is(err, service.ErrTileCacheExecutionCompleted):
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	c.JSON(http.StatusAccepted, RasterMosaicProgressEventResponse{
		ExecutionID: executionID,
		Status:      "accepted",
	})
}

func (h *TaskProviderHandler) recordPointCloudCOPCProgressEvent(c *gin.Context, tenantID uint, executionID string, req RasterMosaicProgressEventRequest) {
	if h.pointCloudCOPCTaskSvc == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "point cloud COPC generation task service is unavailable"})
		return
	}
	event := service.PointCloudCOPCProgressEvent{
		Phase:           req.Phase,
		Event:           req.Event,
		Message:         req.Message,
		OverallProgress: progressIntPointer(req.OverallProgress),
		Metadata:        req.Metadata,
	}
	if err := h.pointCloudCOPCTaskSvc.RecordProgressEvent(c.Request.Context(), tenantID, executionID, event); err != nil {
		switch {
		case errors.Is(err, commonapi.ErrNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "执行记录不存在"})
		case errors.Is(err, service.ErrPointCloudCOPCProgressTargetMismatch):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		case errors.Is(err, service.ErrPointCloudCOPCExecutionCompleted):
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	c.JSON(http.StatusAccepted, RasterMosaicProgressEventResponse{
		ExecutionID: executionID,
		Status:      "accepted",
	})
}

func decodeRasterMosaicProgressEventRequest(c *gin.Context) (RasterMosaicProgressEventRequest, error) {
	var req RasterMosaicProgressEventRequest
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		return req, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return req, errors.New("request body must contain a single JSON object")
	}
	if req.Metadata == nil {
		req.Metadata = commonModels.JSONMap{}
	}
	return req, nil
}

func progressIntPointer(value *float64) *int {
	if value == nil {
		return nil
	}
	rounded := int(math.Round(*value))
	if rounded < 0 {
		rounded = 0
	}
	if rounded > 100 {
		rounded = 100
	}
	return &rounded
}
