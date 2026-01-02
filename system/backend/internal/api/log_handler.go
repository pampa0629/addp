package api

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"time"

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

func (h *LogHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	// 构建过滤条件
	filters := &models.AuditLogFilters{
		StartTime:  c.Query("start_time"),
		EndTime:    c.Query("end_time"),
		Action:     c.Query("action"),
		EntityType: c.Query("entity_type"),
		Username:   c.Query("username"),
		IPAddress:  c.Query("ip"),
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
	currentUserID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未授权"})
		return
	}

	logs, total, err := h.logService.List(page, pageSize, filters, currentUserID.(uint))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  logs,
		"total": total,
		"page":  page,
		"page_size": pageSize,
	})
}

func (h *LogHandler) GetByID(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的日志ID"})
		return
	}

	log, err := h.logService.GetByID(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "日志不存在"})
		return
	}

	c.JSON(http.StatusOK, log)
}

// Export 导出审计日志
func (h *LogHandler) Export(c *gin.Context) {
	format := c.DefaultQuery("format", "csv") // csv 或 json

	// 构建过滤条件（与 List 接口相同）
	filters := &models.AuditLogFilters{
		StartTime:  c.Query("start_time"),
		EndTime:    c.Query("end_time"),
		Action:     c.Query("action"),
		EntityType: c.Query("entity_type"),
		Username:   c.Query("username"),
		IPAddress:  c.Query("ip"),
	}

	if userIDStr := c.Query("user_id"); userIDStr != "" {
		id, err := strconv.ParseUint(userIDStr, 10, 32)
		if err == nil {
			uid := uint(id)
			filters.UserID = &uid
		}
	}

	// 获取当前用户ID
	currentUserID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未授权"})
		return
	}

	// 导出所有匹配的日志（不分页，限制最多 10000 条）
	logs, _, err := h.logService.List(1, 10000, filters, currentUserID.(uint))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
		headers := []string{"ID", "用户ID", "用户名", "租户ID", "操作", "资源类型", "资源ID", "IP地址", "时间"}
		if err := writer.Write(headers); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "导出失败"})
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
				log.Action,
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求参数"})
		return
	}

	// 构建审计日志对象
	auditLog := &models.AuditLog{
		UserID:     req.UserID,
		TenantID:   req.TenantID,
		Action:     req.Action,
		EntityType: req.EntityType,
		EntityID:   req.EntityID,
		Details:    req.Details,
		IPAddress:  req.IPAddress,
		ModuleName: req.ModuleName,
	}

	// 设置用户名
	if req.Username != nil {
		auditLog.Username = *req.Username
	}

	// 创建日志
	if err := h.logService.Create(auditLog); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建审计日志失败"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "审计日志已创建"})
}
