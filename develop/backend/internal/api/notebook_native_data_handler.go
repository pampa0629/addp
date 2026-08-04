package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	commonClient "github.com/addp/common/client"
	developi18n "github.com/addp/develop/backend/i18n"
	"github.com/addp/develop/backend/internal/service"
	"github.com/gin-gonic/gin"
)

type notebookRecordScanRequest struct {
	EngineID  uint                           `json:"engine_id"`
	Path      commonClient.EngineCatalogPath `json:"path"`
	BatchSize int                            `json:"batch_size"`
	MaxRows   int64                          `json:"max_rows,omitempty"`
}

type notebookGraphSampleRequest struct {
	EngineID uint                           `json:"engine_id"`
	Path     commonClient.EngineCatalogPath `json:"path"`
	Limit    int                            `json:"limit"`
	Timeout  int64                          `json:"timeout"`
}

type notebookGraphQueryRequest struct {
	EngineID uint   `json:"engine_id"`
	Query    string `json:"query"`
	MaxRows  int    `json:"max_rows"`
	Timeout  int64  `json:"timeout"`
}

type notebookByteRange struct {
	Offset int64 `json:"offset"`
	Length int64 `json:"length"`
}

type notebookContentReadRequest struct {
	EngineID uint                           `json:"engine_id"`
	Path     commonClient.EngineCatalogPath `json:"path"`
	Range    *notebookByteRange             `json:"range,omitempty"`
}

type notebookChangeStreamRequest struct {
	EngineID        uint                           `json:"engine_id"`
	Path            commonClient.EngineCatalogPath `json:"path"`
	InitialPosition string                         `json:"initial_position"`
	Positions       map[string]int64               `json:"positions,omitempty"`
	BatchSize       int                            `json:"batch_size"`
	PollTimeout     int64                          `json:"poll_timeout"`
}

// StreamSessionRecords streams a native collection cursor as Arrow JSON records.
// @Summary 流式读取 Notebook collection | Stream a Notebook collection
// @Description 每条动态 schema 文档以稳定 document JSON 字段传输，SDK 还原为原生记录。 | Each dynamic-schema document is transported in a stable document JSON field and restored by the SDK.
// @Tags Notebook
// @Accept json
// @Produce application/vnd.apache.arrow.stream
// @Param session_id path string true "Notebook 会话 ID | Notebook session ID"
// @Param request body notebookRecordScanRequest true "Collection 路径与批大小 | Collection path and batch size"
// @Success 200 {file} binary
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 422 {object} map[string]string
// @Failure 502 {object} map[string]string
// @Failure 503 {object} map[string]string
// @Failure 504 {object} map[string]string
// @x-addp-auth-mode "authenticated"
// @Router /notebook-kernel-sessions/{session_id}/record-scans [post]
func (h *NotebookHandler) StreamSessionRecords(c *gin.Context) {
	token, ok := notebookKernelBearer(c.GetHeader("Authorization"))
	if !ok {
		respondDevelopCatalogError(c, http.StatusUnauthorized, "notebook_session_unavailable", developi18n.MsgNotebookSessionUnavailable)
		return
	}
	var request notebookRecordScanRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		respondDevelopCatalogError(c, http.StatusBadRequest, "record_scan_request_invalid", developi18n.MsgNotebookCatalogRequestInvalid)
		return
	}
	ready := false
	err := h.notebookSessionService.StreamRecords(c.Request.Context(), c.Param("session_id"), token,
		service.NotebookRecordScanRequest{EngineID: request.EngineID, Path: request.Path, BatchSize: request.BatchSize, MaxRows: request.MaxRows},
		c.Writer, func() {
			ready = true
			c.Header("Content-Type", "application/vnd.apache.arrow.stream")
			setNotebookStreamHeaders(c)
			c.Status(http.StatusOK)
		})
	if err == nil || ready {
		return
	}
	respondNotebookDataError(c, err, service.ErrNotebookRecordScanInvalid, service.ErrNotebookRecordScanUnsupported,
		"record_scan_request_invalid", "record_scan_unsupported", "record_scan_timeout")
}

// SampleSessionGraph returns a bounded native graph sample.
// @Summary 获取 Notebook 图样本 | Get a Notebook graph sample
// @Tags Notebook
// @Accept json
// @Produce json
// @Param session_id path string true "Notebook 会话 ID | Notebook session ID"
// @Param request body notebookGraphSampleRequest true "图路径与样本边界 | Graph path and sample bounds"
// @Success 200 {object} plugin.GraphData
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 422 {object} map[string]string
// @Failure 502 {object} map[string]string
// @Failure 503 {object} map[string]string
// @Failure 504 {object} map[string]string
// @x-addp-auth-mode "authenticated"
// @Router /notebook-kernel-sessions/{session_id}/graph-samples [post]
func (h *NotebookHandler) SampleSessionGraph(c *gin.Context) {
	token, ok := notebookKernelBearer(c.GetHeader("Authorization"))
	if !ok {
		respondDevelopCatalogError(c, http.StatusUnauthorized, "notebook_session_unavailable", developi18n.MsgNotebookSessionUnavailable)
		return
	}
	var request notebookGraphSampleRequest
	if err := c.ShouldBindJSON(&request); err != nil || request.Timeout <= 0 || request.Timeout > 300 {
		respondDevelopCatalogError(c, http.StatusBadRequest, "graph_request_invalid", developi18n.MsgNotebookCatalogRequestInvalid)
		return
	}
	result, err := h.notebookSessionService.SampleGraph(c.Request.Context(), c.Param("session_id"), token,
		service.NotebookGraphSampleRequest{EngineID: request.EngineID, Path: request.Path, Limit: request.Limit, Timeout: time.Duration(request.Timeout) * time.Second})
	if err != nil {
		respondNotebookDataError(c, err, service.ErrNotebookGraphRequestInvalid, service.ErrNotebookGraphUnsupported,
			"graph_request_invalid", "graph_operation_unsupported", "graph_timeout")
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, result)
}

// QuerySessionGraph executes bounded read-only Cypher and preserves graph structure.
// @Summary 执行 Notebook 图查询 | Execute a Notebook graph query
// @Tags Notebook
// @Accept json
// @Produce json
// @Param session_id path string true "Notebook 会话 ID | Notebook session ID"
// @Param request body notebookGraphQueryRequest true "Cypher 与执行边界 | Cypher and execution bounds"
// @Success 200 {object} plugin.GraphQueryResult
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 422 {object} map[string]string
// @Failure 502 {object} map[string]string
// @Failure 503 {object} map[string]string
// @Failure 504 {object} map[string]string
// @x-addp-auth-mode "authenticated"
// @Router /notebook-kernel-sessions/{session_id}/graph-queries [post]
func (h *NotebookHandler) QuerySessionGraph(c *gin.Context) {
	token, ok := notebookKernelBearer(c.GetHeader("Authorization"))
	if !ok {
		respondDevelopCatalogError(c, http.StatusUnauthorized, "notebook_session_unavailable", developi18n.MsgNotebookSessionUnavailable)
		return
	}
	var request notebookGraphQueryRequest
	if err := c.ShouldBindJSON(&request); err != nil || request.Timeout <= 0 || request.Timeout > 300 {
		respondDevelopCatalogError(c, http.StatusBadRequest, "graph_request_invalid", developi18n.MsgNotebookCatalogRequestInvalid)
		return
	}
	result, err := h.notebookSessionService.QueryGraph(c.Request.Context(), c.Param("session_id"), token,
		service.NotebookGraphQueryRequest{EngineID: request.EngineID, Query: request.Query, MaxRows: request.MaxRows, Timeout: time.Duration(request.Timeout) * time.Second})
	if err != nil {
		respondNotebookDataError(c, err, service.ErrNotebookGraphRequestInvalid, service.ErrNotebookGraphUnsupported,
			"graph_request_invalid", "graph_operation_unsupported", "graph_timeout")
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, result)
}

// StreamSessionContent streams a full object/file or one explicit byte range.
// @Summary 读取 Notebook 对象或文件内容 | Read Notebook object or file content
// @Tags Notebook
// @Accept json
// @Produce application/octet-stream
// @Param session_id path string true "Notebook 会话 ID | Notebook session ID"
// @Param request body notebookContentReadRequest true "内容路径与可选范围 | Content path and optional range"
// @Success 200 {file} binary
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 422 {object} map[string]string
// @Failure 502 {object} map[string]string
// @Failure 503 {object} map[string]string
// @Failure 504 {object} map[string]string
// @x-addp-auth-mode "authenticated"
// @Router /notebook-kernel-sessions/{session_id}/content-reads [post]
func (h *NotebookHandler) StreamSessionContent(c *gin.Context) {
	token, ok := notebookKernelBearer(c.GetHeader("Authorization"))
	if !ok {
		respondDevelopCatalogError(c, http.StatusUnauthorized, "notebook_session_unavailable", developi18n.MsgNotebookSessionUnavailable)
		return
	}
	var request notebookContentReadRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		respondDevelopCatalogError(c, http.StatusBadRequest, "content_read_request_invalid", developi18n.MsgNotebookCatalogRequestInvalid)
		return
	}
	serviceRequest := service.NotebookContentReadRequest{EngineID: request.EngineID, Path: request.Path}
	if request.Range != nil {
		serviceRequest.Range = &service.NotebookByteRange{Offset: request.Range.Offset, Length: request.Range.Length}
	}
	ready := false
	err := h.notebookSessionService.StreamContent(c.Request.Context(), c.Param("session_id"), token, serviceRequest, c.Writer, func() {
		ready = true
		c.Header("Content-Type", "application/octet-stream")
		setNotebookStreamHeaders(c)
		c.Status(http.StatusOK)
	})
	if err == nil || ready {
		return
	}
	respondNotebookDataError(c, err, service.ErrNotebookContentReadInvalid, service.ErrNotebookContentReadUnsupported,
		"content_read_request_invalid", "content_read_unsupported", "content_read_timeout")
}

// StreamSessionChanges streams native Kafka records as NDJSON.
// @Summary 流式读取 Notebook Kafka topic | Stream a Notebook Kafka topic
// @Tags Notebook
// @Accept json
// @Produce application/x-ndjson
// @Param session_id path string true "Notebook 会话 ID | Notebook session ID"
// @Param request body notebookChangeStreamRequest true "Topic 路径与起始位置 | Topic path and initial positions"
// @Success 200 {file} binary
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 422 {object} map[string]string
// @Failure 502 {object} map[string]string
// @Failure 503 {object} map[string]string
// @Failure 504 {object} map[string]string
// @x-addp-auth-mode "authenticated"
// @Router /notebook-kernel-sessions/{session_id}/change-streams [post]
func (h *NotebookHandler) StreamSessionChanges(c *gin.Context) {
	token, ok := notebookKernelBearer(c.GetHeader("Authorization"))
	if !ok {
		respondDevelopCatalogError(c, http.StatusUnauthorized, "notebook_session_unavailable", developi18n.MsgNotebookSessionUnavailable)
		return
	}
	var request notebookChangeStreamRequest
	if err := c.ShouldBindJSON(&request); err != nil || request.PollTimeout <= 0 || request.PollTimeout > 60 {
		respondDevelopCatalogError(c, http.StatusBadRequest, "change_stream_request_invalid", developi18n.MsgNotebookCatalogRequestInvalid)
		return
	}
	ready := false
	err := h.notebookSessionService.StreamChanges(c.Request.Context(), c.Param("session_id"), token,
		service.NotebookChangeStreamRequest{
			EngineID: request.EngineID, Path: request.Path, InitialPosition: request.InitialPosition,
			Positions: request.Positions, BatchSize: request.BatchSize, PollTimeout: time.Duration(request.PollTimeout) * time.Second,
		}, c.Writer, func() {
			ready = true
			c.Header("Content-Type", "application/x-ndjson")
			setNotebookStreamHeaders(c)
			c.Status(http.StatusOK)
		})
	if err == nil || ready {
		return
	}
	respondNotebookDataError(c, err, service.ErrNotebookChangeStreamInvalid, service.ErrNotebookChangeStreamUnsupported,
		"change_stream_request_invalid", "change_stream_unsupported", "change_stream_timeout")
}

func setNotebookStreamHeaders(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
}

func respondNotebookDataError(
	c *gin.Context, err, invalidErr, unsupportedErr error, invalidCode, unsupportedCode, timeoutCode string,
) {
	switch {
	case errors.Is(err, service.ErrNotebookSessionNotFound):
		respondDevelopCatalogError(c, http.StatusUnauthorized, "notebook_session_unavailable", developi18n.MsgNotebookSessionUnavailable)
	case errors.Is(err, invalidErr):
		respondDevelopCatalogError(c, http.StatusBadRequest, invalidCode, developi18n.MsgNotebookCatalogRequestInvalid)
	case errors.Is(err, unsupportedErr):
		respondDevelopCatalogError(c, http.StatusUnprocessableEntity, unsupportedCode, developi18n.MsgNotebookCatalogUnsupported)
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		respondDevelopCatalogError(c, http.StatusGatewayTimeout, timeoutCode, developi18n.MsgNotebookCatalogTimeout)
	default:
		if code, ok := commonClient.SystemAPIErrorCode(err); ok {
			switch code {
			case "notebook_session_authorization_forbidden", "execution_access_forbidden":
				respondDevelopCatalogError(c, http.StatusForbidden, "notebook_data_forbidden", developi18n.MsgNotebookCatalogForbidden)
				return
			case "engine_unavailable":
				respondDevelopCatalogError(c, http.StatusServiceUnavailable, code, developi18n.MsgNotebookCatalogEngineUnavailable)
				return
			}
		}
		respondDevelopCatalogError(c, http.StatusBadGateway, "notebook_data_provider_failed", developi18n.MsgNotebookCatalogProviderFailed)
	}
}
