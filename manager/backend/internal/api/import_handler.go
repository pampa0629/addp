package api

import (
	"io"
	"net/http"
	"strconv"

	commonAPI "github.com/addp/common/api"
	"github.com/addp/common/logger"
	"github.com/addp/common/middleware/auth"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

// ImportHandler 数据导入 API Handler
type ImportHandler struct {
	importService *service.ImportService
}

// NewImportHandler 创建 ImportHandler
func NewImportHandler(importService *service.ImportService) *ImportHandler {
	return &ImportHandler{importService: importService}
}

// ImportData 导入数据文件
// POST /api/manager/import
// Content-Type: multipart/form-data
// 参数:
//   - file: Shapefile ZIP 包
//   - target_engine_id: 目标数据库引擎 ID（必填）
//   - target_schema: 目标 schema（默认 public）
//   - target_table: 目标表名（可选，默认使用文件名）
//   - encoding: DBF 编码（可选，默认 UTF-8）
func (h *ImportHandler) ImportData(c *gin.Context) {
	log := logger.L()

	// 解析 multipart 表单
	if err := c.Request.ParseMultipartForm(100 << 20); err != nil { // 最大 100MB
		commonAPI.BadRequestError(c, "failed to parse form: "+err.Error())
		return
	}

	// 获取上传文件
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		commonAPI.BadRequestError(c, "file is required")
		return
	}
	defer file.Close()

	// 读取文件内容
	fileContent, err := io.ReadAll(file)
	if err != nil {
		commonAPI.InternalServerError(c, "failed to read file: "+err.Error())
		return
	}

	// 解析目标引擎 ID（必填）
	engineIDStr := c.PostForm("target_engine_id")
	if engineIDStr == "" {
		commonAPI.BadRequestError(c, "target_engine_id is required")
		return
	}
	engineIDUint64, err := strconv.ParseUint(engineIDStr, 10, 32)
	if err != nil {
		commonAPI.BadRequestError(c, "invalid target_engine_id")
		return
	}
	engineID := uint(engineIDUint64)

	// 获取可选参数
	targetSchema := c.PostForm("target_schema")
	targetTable := c.PostForm("target_table")
	encoding := c.PostForm("encoding")

	// 获取租户 ID
	tenantID := auth.GetTenantID(c)

	log.Info("收到导入请求",
		"filename", header.Filename,
		"size", header.Size,
		"engine_id", engineID,
		"schema", targetSchema,
		"table", targetTable,
		"tenant_id", tenantID,
	)

	// 执行导入
	req := &service.ImportShapefileRequest{
		FileContent:    fileContent,
		FileName:       header.Filename,
		TargetEngineID: engineID,
		TargetSchema:   targetSchema,
		TargetTable:    targetTable,
		Encoding:       encoding,
		TenantID:       tenantID,
	}

	result, err := h.importService.ImportShapefile(c.Request.Context(), req)
	if err != nil {
		log.Error("导入失败", "error", err, "filename", header.Filename)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	log.Info("导入任务已创建",
		"upload_uuid", result.UploadUUID,
		"transfer_task_id", result.TransferTaskID,
	)

	c.JSON(http.StatusAccepted, result)
}
