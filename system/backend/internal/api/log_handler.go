package api

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"time"

	commonapi "github.com/addp/common/api"
	commoni18n "github.com/addp/common/middleware/i18n"
	sysi18n "github.com/addp/system/i18n"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
)

type LogHandler struct {
	logService *service.LogService
}

func NewLogHandler(logService *service.LogService) *LogHandler {
	return &LogHandler{logService: logService}
}

// List godoc
// @Summary      获取日志列表 | List audit logs
// @Description  分页获取审计日志（支持多条件过滤）| Get paginated audit logs with multi-condition filtering
// @Tags         日志管理 | Log Management
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        page query int false "页码 | Page number" default(1)
// @Param        page_size query int false "每页数量 | Page size" default(10)
// @Param        start_time query string false "开始时间 | Start time"
// @Param        end_time query string false "结束时间 | End time"
// @Param        http_method query string false "HTTP方法 | HTTP method"
// @Success      200 {object} object{data=[]models.AuditLog,total=int,page=int,page_size=int}
// @Failure      500 {object} models.ErrorResponse
// @Router       /logs [get]
func (h *LogHandler) List(c *gin.Context) {
	page, pageSize := commonapi.ParsePagination(c)

	// 构建过滤条件
	filters := &models.AuditLogFilters{
		StartTime:  c.Query("start_time"),
		EndTime:    c.Query("end_time"),
		HTTPMethod: c.Query("http_method"),
		EntityType: c.Query("entity_type"),
		Username:   c.Query("username"),
		IPAddress:  c.Query("ip"),
		ModuleName: c.Query("module_name"),
		StatusCode: c.Query("status_code"),
	}

	// 用户ID过滤
	if userIDStr := c.Query("user_id"); userIDStr != "" {
		id, err := strconv.ParseUint(userIDStr, 10, 32)
		if err == nil {
			uid := uint(id)
			filters.UserID = &uid
		}
	}

	// 获取当前用户ID
	currentUserID, _ := commonapi.GetCurrentUserID(c)

	logs, total, err := h.logService.List(page, pageSize, filters, currentUserID)
	if err != nil {
		commonapi.RespondError(c, 500, err.Error())
		return
	}

	commonapi.RespondPaginated(c, logs, total, page, pageSize)
}

// GetByID godoc
// @Summary      获取日志详情 | Get audit log detail
// @Tags         日志管理 | Log Management
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "日志ID | Log ID"
// @Success      200 {object} models.AuditLog
// @Failure      404 {object} models.ErrorResponse
// @Router       /logs/{id} [get]
func (h *LogHandler) GetByID(c *gin.Context) {
	id, err := commonapi.BindIDParam(c, "id")
	if err != nil {
		return
	}

	log, err := h.logService.GetByID(id)
	if err != nil {
		commonapi.RespondError(c, 404, commoni18n.T(c, sysi18n.MsgLogNotFound))
		return
	}

	commonapi.RespondSuccess(c, log)
}

// Export 导出审计日志
// @Summary      导出审计日志 | Export audit logs
// @Tags         日志管理 | Log Management
// @Produce      json
// @Security     BearerAuth
// @Param        format query string false "导出格式 csv/json | Export format" default(csv)
// @Param        start_time query string false "开始时间 | Start time"
// @Param        end_time query string false "结束时间 | End time"
// @Param        http_method query string false "HTTP方法 | HTTP method"
// @Success      200 {object} map[string]interface{}
// @Failure      500 {object} models.ErrorResponse
// @Router       /logs/export [get]
func (h *LogHandler) Export(c *gin.Context) {
	format := c.DefaultQuery("format", "csv") // csv 或 json

	// 构建过滤条件（与 List 接口相同）
	filters := &models.AuditLogFilters{
		StartTime:  c.Query("start_time"),
		EndTime:    c.Query("end_time"),
		HTTPMethod: c.Query("http_method"),
		EntityType: c.Query("entity_type"),
		Username:   c.Query("username"),
		IPAddress:  c.Query("ip"),
		ModuleName: c.Query("module_name"),
		StatusCode: c.Query("status_code"),
	}

	if userIDStr := c.Query("user_id"); userIDStr != "" {
		id, err := strconv.ParseUint(userIDStr, 10, 32)
		if err == nil {
			uid := uint(id)
			filters.UserID = &uid
		}
	}

	// 获取当前用户ID
	currentUserID, _ := commonapi.GetCurrentUserID(c)

	// 导出所有匹配的日志（不分页，限制最多 10000 条）
	logs, _, err := h.logService.List(1, 10000, filters, currentUserID)
	if err != nil {
		commonapi.RespondError(c, 500, err.Error())
		return
	}

	// 生成文件名
	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("audit_logs_%s.%s", timestamp, format)

	if format == "json" {
		// JSON 格式导出
		c.Header("Content-Type", "application/json")
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
		c.JSON(http.StatusOK, logs)
	} else {
		// CSV 格式导出
		c.Header("Content-Type", "text/csv; charset=utf-8")
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))

		writer := csv.NewWriter(c.Writer)
		defer writer.Flush()

		// 写入 UTF-8 BOM（解决 Excel 中文乱码）
		c.Writer.Write([]byte{0xEF, 0xBB, 0xBF})

		// 写入表头
		headers := []string{"ID", "用户ID", "用户名", "租户ID", "HTTP方法", "路径", "资源类型", "资源ID", "IP地址", "时间"}
		if err := writer.Write(headers); err != nil {
			commonapi.RespondError(c, 500, commoni18n.T(c, sysi18n.MsgExportFailed))
			return
		}

		// 写入数据
		for _, log := range logs {
			var userID, tenantID string
			if log.UserID != nil {
				userID = fmt.Sprintf("%d", *log.UserID)
			}
			if log.TenantID != nil {
				tenantID = fmt.Sprintf("%d", *log.TenantID)
			}

			record := []string{
				fmt.Sprintf("%d", log.ID),
				userID,
				log.Username,
				tenantID,
				log.HTTPMethod,
				log.ResourcePath,
				log.EntityType,
				log.EntityID,
				log.IPAddress,
				log.CreatedAt.Format("2006-01-02 15:04:05"),
			}
			if err := writer.Write(record); err != nil {
				continue
			}
		}
	}
}

// CreateFromInternal 接收来自其他模块的审计日志（内部 API）
func (h *LogHandler) CreateFromInternal(c *gin.Context) {
	var req models.AuditLogCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// 输出详细错误信息便于调试
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, commoni18n.MsgInvalidParams), "details": err.Error()})
		return
	}

	// 构建审计日志对象
	auditLog := &models.AuditLog{
		UserID:       req.UserID,
		TenantID:     req.TenantID,
		HTTPMethod:   req.HTTPMethod,
		ResourcePath: req.ResourcePath,
		HTTPStatus:   req.HTTPStatus,
		DurationMs:   req.DurationMs,
		EntityType:   req.EntityType,
		EntityID:     req.EntityID,
		RequestBody:  req.RequestBody,
		QueryParams:  req.QueryParams,
		UserAgent:    req.UserAgent,
		IPAddress:    req.IPAddress,
		ModuleName:   req.ModuleName,
		LogLevel:     req.LogLevel,
		ErrorMessage: req.ErrorMessage,
		RequestID:    req.RequestID,
	}

	// 设置用户名
	if req.Username != nil {
		auditLog.Username = *req.Username
	}

	// 创建日志
	if err := h.logService.Create(auditLog); err != nil {
		commonapi.RespondError(c, 500, commoni18n.T(c, sysi18n.MsgAuditLogCreateFailed))
		return
	}

	commonapi.RespondCreated(c, gin.H{"message": commoni18n.T(c, sysi18n.MsgAuditLogCreated)})
}

// GetStats 获取日志统计
// @Summary      获取日志统计 | Get log stats
// @Tags         日志管理 | Log Management
// @Produce      json
// @Security     BearerAuth
// @Success      200 {object} map[string]interface{}
// @Failure      500 {object} models.ErrorResponse
// @Router       /logs/stats [get]
func (h *LogHandler) GetStats(c *gin.Context) {
	currentUserID, _ := commonapi.GetCurrentUserID(c)

	stats, err := h.logService.GetStats(currentUserID)
	if err != nil {
		commonapi.RespondError(c, 500, err.Error())
		return
	}

	commonapi.RespondSuccess(c, stats)
}

// GetTrends 获取时间趋势
// @Summary      获取日志趋势 | Get log trends
// @Tags         日志管理 | Log Management
// @Produce      json
// @Security     BearerAuth
// @Success      200 {object} map[string]interface{}
// @Failure      500 {object} models.ErrorResponse
// @Router       /logs/trends [get]
func (h *LogHandler) GetTrends(c *gin.Context) {
	currentUserID, _ := commonapi.GetCurrentUserID(c)

	trends, err := h.logService.GetTrends(currentUserID)
	if err != nil {
		commonapi.RespondError(c, 500, err.Error())
		return
	}

	commonapi.RespondSuccess(c, trends)
}
