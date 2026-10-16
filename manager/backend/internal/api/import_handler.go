package api

import (
	"errors"
	"io"
	"net/http"
	"strings"

	commonAPI "github.com/addp/common/api"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/logger"
	"github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/common/resourcetree"
	manageri18n "github.com/addp/manager/i18n"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

func importError(c *gin.Context, err error) {
	var submission *commonClient.TransferTaskSubmissionError
	switch {
	case errors.Is(err, commonAPI.ErrForbidden):
		commonAPI.ForbiddenError(c, commoni18n.T(c, commoni18n.MsgForbidden))
	case errors.Is(err, service.ErrImportSubmission):
		// A downstream 401 must not replay an import after files were written.
		status := http.StatusServiceUnavailable
		if errors.As(err, &submission) && submission.StatusCode == http.StatusForbidden {
			status = http.StatusForbidden
		}
		c.JSON(status, gin.H{"error": commoni18n.T(c, manageri18n.MsgImportSubmissionFailed)})
	case errors.Is(err, service.ErrImportTableNameRequired):
		managerError(c, http.StatusBadRequest, manageri18n.MsgImportTableNameRequired)
	case errors.Is(err, service.ErrImportZipRequired):
		managerError(c, http.StatusBadRequest, manageri18n.MsgImportZipRequired)
	case errors.Is(err, service.ErrImportUnsupportedFormat):
		managerError(c, http.StatusBadRequest, manageri18n.MsgImportUnsupportedFormat)
	case errors.Is(err, service.ErrImportZipMissingShp):
		managerError(c, http.StatusBadRequest, manageri18n.MsgImportZipMissingShp)
	case errors.Is(err, service.ErrImportZipBasenameMismatch):
		managerError(c, http.StatusBadRequest, manageri18n.MsgImportZipBasenameMismatch)
	case errors.Is(err, service.ErrImportZipMissingRequiredSet):
		managerError(c, http.StatusBadRequest, manageri18n.MsgImportZipMissingRequired)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	}
}

// ImportHandler 数据导入 API Handler
type ImportHandler struct {
	importService *service.ImportService
}

// NewImportHandler 创建 ImportHandler
func NewImportHandler(importService *service.ImportService) *ImportHandler {
	return &ImportHandler{importService: importService}
}

// ImportData 导入数据文件
// POST /api/v1/manager/imports
// Content-Type: multipart/form-data
// 参数:
//   - files: Shapefile ZIP 包，或同一组 Shapefile 组件文件
//   - target_node_locator: 目标数据库 node locator（必填）
//   - target_table: 目标数据项名称（可选，默认使用文件名）
//   - encoding: DBF 编码（可选，默认 UTF-8）
//
// @Summary 导入数据文件 | Import data file
// @Description 仅租户用户；写入前检查导入及 Transfer 创建、执行权限，在当前请求内以用户身份创建并启动同步任务 | Tenant users only; check import and Transfer create/execute permissions before writes, then create and start the task with the current User credential
// @Tags Manager
// @Accept multipart/form-data
// @Produce json
// @Param files formData file true "数据文件：单个 Shapefile ZIP，或同一组 .shp/.dbf/.shx/.prj/.qpj/.cpg 文件 | Data files: one Shapefile ZIP, or a Shapefile component set"
// @Param target_node_locator formData string true "目标数据库节点 ResourceLocator | Target database node ResourceLocator"
// @Param target_table formData string false "目标数据项名称，默认使用文件名 | Target item name, default from filename"
// @Param encoding formData string false "DBF编码，默认UTF-8 | DBF encoding, default UTF-8"
// @Success 202 {object} service.ImportResult "导入请求已提交 | Import request submitted"
// @Failure 403 {object} map[string]interface{} "无权访问 | Access denied"
// @Failure 503 {object} map[string]interface{} "Transfer 提交失败 | Transfer submission failed"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.data_item.create","transfer.task.create","transfer.task.execute"]
// @Router /imports [post]
// @Security BearerAuth
func (h *ImportHandler) ImportData(c *gin.Context) {
	principal, ok := auth.PrincipalFromGin(c)
	if !ok || principal.Type != "user" {
		commonAPI.ForbiddenError(c, commoni18n.T(c, commoni18n.MsgForbidden))
		return
	}
	log := logger.L()

	// 解析 multipart 表单
	if err := c.Request.ParseMultipartForm(100 << 20); err != nil { // 最大 100MB
		managerErrorWithDetail(c, http.StatusBadRequest, manageri18n.MsgParseFormFailed, err.Error())
		return
	}

	form := c.Request.MultipartForm
	if form == nil || len(form.File["files"]) == 0 {
		managerError(c, http.StatusBadRequest, manageri18n.MsgFileRequired)
		return
	}

	files := make([]service.ImportUploadFile, 0, len(form.File["files"]))
	totalSize := int64(0)
	fileNames := make([]string, 0, len(form.File["files"]))
	for _, header := range form.File["files"] {
		file, err := header.Open()
		if err != nil {
			managerErrorWithDetail(c, http.StatusInternalServerError, manageri18n.MsgReadFileFailed, err.Error())
			return
		}
		content, readErr := io.ReadAll(file)
		closeErr := file.Close()
		if readErr != nil {
			managerErrorWithDetail(c, http.StatusInternalServerError, manageri18n.MsgReadFileFailed, readErr.Error())
			return
		}
		if closeErr != nil {
			managerErrorWithDetail(c, http.StatusInternalServerError, manageri18n.MsgReadFileFailed, closeErr.Error())
			return
		}
		files = append(files, service.ImportUploadFile{
			FileName: header.Filename,
			Content:  content,
		})
		totalSize += header.Size
		fileNames = append(fileNames, header.Filename)
	}

	targetNodeLocator := strings.TrimSpace(c.PostForm("target_node_locator"))
	if targetNodeLocator == "" {
		missingLocator(c)
		return
	}
	targetLoc, err := resourcetree.ParseURI(targetNodeLocator)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if targetLoc.Type != resourcetree.TypeSchema && targetLoc.Type != resourcetree.TypeDatabase {
		c.JSON(http.StatusBadRequest, gin.H{"error": "target_node_locator must reference a database node"})
		return
	}

	// 获取可选参数
	targetSchema := ""
	if len(targetLoc.Path) > 0 {
		targetSchema = targetLoc.Path[len(targetLoc.Path)-1]
	}
	targetTable := c.PostForm("target_table")
	encoding := c.PostForm("encoding")

	// 获取租户 ID
	tenantID := auth.GetTenantID(c)

	log.Info("收到导入请求",
		"file_count", len(files),
		"filenames", strings.Join(fileNames, ","),
		"total_size", totalSize,
		"engine_id", targetLoc.EngineID,
		"schema", targetSchema,
		"table", targetTable,
		"tenant_id", tenantID,
	)

	// 执行导入
	req := &service.ImportShapefileRequest{
		Files:             files,
		TargetNodeLocator: targetNodeLocator,
		TargetEngineID:    targetLoc.EngineID,
		TargetSchema:      targetSchema,
		TargetTable:       targetTable,
		Encoding:          encoding,
		TenantID:          tenantID,
	}

	result, err := h.importService.ImportShapefile(c.Request.Context(), req, auth.CanonicalBearerToken(c.GetHeader("Authorization")))
	if err != nil {
		log.Error("导入失败", "error", err, "filenames", strings.Join(fileNames, ","))
		importError(c, err)
		return
	}

	log.Info("导入请求已提交",
		"upload_uuid", result.UploadUUID,
		"transfer_task_id", result.TransferTaskID,
	)

	c.JSON(http.StatusAccepted, result)
}
