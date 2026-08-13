package api

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"

	commoni18n "github.com/addp/common/middleware/i18n"
	sysi18n "github.com/addp/standard/i18n"
	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
	"github.com/addp/standard/internal/service"
	"github.com/gin-gonic/gin"
)

// DocumentHandler 标准文档 Handler
type DocumentHandler struct {
	svc *service.DocumentService
}

func NewDocumentHandler(svc *service.DocumentService) *DocumentHandler {
	return &DocumentHandler{svc: svc}
}

// @Summary 获取标准文档列表 | List standard documents
// @Tags Standard
// @Produce json
// @Success 200 {object} models.PaginatedDocumentResponse
// @Failure 401 {object} map[string]string "需要登录 | Authentication required"
// @Failure 403 {object} map[string]string "无权访问 | Access denied"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.document.read"]
// @Router /documents [get]
// @Security BearerAuth
func (h *DocumentHandler) ListDocuments(c *gin.Context) {
	tenantID := getTenantID(c)
	opts := repository.ListDocumentOptions{
		DocType: c.Query("doc_type"),
		Keyword: c.Query("keyword"),
	}
	if pageStr := c.Query("page"); pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil {
			opts.Page = p
		}
	}
	if psStr := c.Query("page_size"); psStr != "" {
		if ps, err := strconv.Atoi(psStr); err == nil {
			opts.PageSize = ps
		}
	}
	docs, total, err := h.svc.ListDocuments(tenantID, opts)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	page := opts.Page
	pageSize := opts.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	totalPages := int((total + int64(pageSize) - 1) / int64(pageSize))
	if totalPages < 1 {
		totalPages = 1
	}
	c.JSON(http.StatusOK, gin.H{"data": docs, "total": total, "page": page, "page_size": pageSize, "total_pages": totalPages})
}

// @Summary 获取标准文档详情 | Get standard document detail
// @Tags Standard
// @Produce json
// @Success 200 {object} models.Document
// @Failure 401 {object} map[string]string "需要登录 | Authentication required"
// @Failure 403 {object} map[string]string "无权访问 | Access denied"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.document.read"]
// @Router /documents/{id} [get]
// @Security BearerAuth
func (h *DocumentHandler) GetDocument(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}
	tenantID := getTenantID(c)
	doc, err := h.svc.GetDocument(id, tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, sysi18n.MsgDocumentNotFound)})
		return
	}
	c.JSON(http.StatusOK, doc)
}

// @Summary 创建标准文档 | Create standard document
// @Tags Standard
// @Produce json
// @Success 201 {object} models.Document
// @Failure 401 {object} map[string]string "需要登录 | Authentication required"
// @Failure 403 {object} map[string]string "无权访问 | Access denied"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.document.create"]
// @Router /documents [post]
// @Security BearerAuth
func (h *DocumentHandler) CreateDocument(c *gin.Context) {
	var req models.CreateDocumentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	tenantID := getTenantID(c)
	userID := getUserID(c)
	doc, err := h.svc.CreateDocument(&req, tenantID, userID)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusCreated, doc)
}

// @Summary 更新标准文档 | Update standard document
// @Tags Standard
// @Param request body models.UpdateDocumentRequest true "更新标准文档 | Update standard document"
// @Failure 409 {object} map[string]string "资源版本冲突 | Resource version conflict"
// @Produce json
// @Success 200 {object} models.Document
// @Failure 401 {object} map[string]string "需要登录 | Authentication required"
// @Failure 403 {object} map[string]string "无权访问 | Access denied"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.document.update"]
// @Router /documents/{id} [put]
// @Security BearerAuth
func (h *DocumentHandler) UpdateDocument(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}
	var req models.UpdateDocumentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	tenantID := getTenantID(c)
	userID := getUserID(c)
	doc, err := h.svc.UpdateDocument(id, tenantID, userID, &req)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, doc)
}

// @Summary 删除标准文档 | Delete standard document
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} map[string]string "需要登录 | Authentication required"
// @Failure 403 {object} map[string]string "无权访问 | Access denied"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.document.delete"]
// @Router /documents/{id} [delete]
// @Security BearerAuth
func (h *DocumentHandler) DeleteDocument(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}
	tenantID := getTenantID(c)
	if err := h.svc.DeleteDocument(id, tenantID); err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": commoni18n.T(c, sysi18n.MsgDeleteSuccess)})
}

// UploadFile 上传文档文件（multipart/form-data, field: "file"）
// @Summary 上传文档文件 | Upload document file
// @Tags Standard
// @Accept multipart/form-data
// @Param version formData int true "当前文档资源版本 | Current document resource version"
// @Param file formData file true "文档文件 | Document file"
// @Failure 409 {object} map[string]string "资源版本冲突 | Resource version conflict"
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 413 {object} map[string]string
// @Failure 502 {object} map[string]string
// @Failure 401 {object} map[string]string "需要登录 | Authentication required"
// @Failure 403 {object} map[string]string "无权访问 | Access denied"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.document.update"]
// @Router /documents/{id}/upload [post]
// @Security BearerAuth
func (h *DocumentHandler) UploadFile(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}
	tenantID := getTenantID(c)
	version, err := strconv.ParseInt(c.PostForm("version"), 10, 64)
	if err != nil || version <= 0 {
		respondError(c, http.StatusBadRequest, errors.New("version is required"))
		return
	}
	// 为 multipart 边界预留少量头部空间，文件本体仍由 Service 按精确大小校验。
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, h.svc.MaxFileSize()+1024*1024)

	file, err := c.FormFile("file")
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			respondDocumentFileError(c, service.ErrDocumentFileTooLarge)
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgFileRequired)})
		return
	}

	f, err := file.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": commoni18n.T(c, sysi18n.MsgFileOpenFailed)})
		return
	}
	defer f.Close()

	contentType := file.Header.Get("Content-Type")
	doc, err := h.svc.UploadFile(id, tenantID, version, file.Filename, f, file.Size, contentType)
	if err != nil {
		respondDocumentFileError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message":   commoni18n.T(c, sysi18n.MsgUploadSuccess),
		"file_name": doc.FileName,
		"file_size": doc.FileSize,
		"version":   doc.Version,
	})
}

// DownloadFile 下载文档文件（通过后端代理流式传输）
// @Summary 下载文档文件 | Download document file
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 404 {object} map[string]string
// @Failure 502 {object} map[string]string
// @Failure 401 {object} map[string]string "需要登录 | Authentication required"
// @Failure 403 {object} map[string]string "无权访问 | Access denied"
// @x-addp-auth-mode "resource_ticket"
// @x-addp-required-permissions ["standard.document.read"]
// @Router /documents/{id}/download [get]
// @Security BearerAuth
func (h *DocumentHandler) DownloadFile(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}
	tenantID := getTenantID(c)

	reader, fileName, fileSize, err := h.svc.DownloadFile(id, tenantID)
	if err != nil {
		respondDocumentFileError(c, err)
		return
	}
	defer reader.Close()

	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": fileName}))
	c.Header("Content-Type", "application/octet-stream")
	if fileSize > 0 {
		c.Header("Content-Length", strconv.FormatInt(fileSize, 10))
	}
	c.Status(http.StatusOK)
	if _, err := io.Copy(c.Writer, reader); err != nil {
		c.Error(err)
	}
}

// @Summary 获取文档关联映射 | Get document mappings
// @Tags Standard
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} map[string]string "需要登录 | Authentication required"
// @Failure 403 {object} map[string]string "无权访问 | Access denied"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.document.read"]
// @Router /documents/{id}/mappings [get]
// @Security BearerAuth
func (h *DocumentHandler) GetMappings(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}
	mappings, err := h.svc.GetMappings(id, getTenantID(c))
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, mappings)
}

// @Summary 设置文档关联映射 | Set document mappings
// @Tags Standard
// @Param request body models.SetDocumentMappingsRequest true "关联映射及当前文档版本 | Mappings with current document version"
// @Failure 409 {object} map[string]string "资源版本冲突 | Resource version conflict"
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} map[string]string "需要登录 | Authentication required"
// @Failure 403 {object} map[string]string "无权访问 | Access denied"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.document.update"]
// @Router /documents/{id}/mappings [put]
// @Security BearerAuth
func (h *DocumentHandler) SetMappings(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}
	var req models.SetDocumentMappingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.SetMappings(id, getTenantID(c), &req); err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	doc, err := h.svc.GetDocument(id, getTenantID(c))
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, models.ResourceVersionResponse{Version: doc.Version})
}

// ===== 反向查询：从标准项维度列出关联文档 =====

// @Summary 查询数据元关联的文档 | List documents by element
// @Tags Standard
// @Produce json
// @Success 200 {array} models.Document
// @Failure 401 {object} map[string]string "需要登录 | Authentication required"
// @Failure 403 {object} map[string]string "无权访问 | Access denied"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.element.read","standard.document.read"]
// @Router /elements/{id}/documents [get]
// @Security BearerAuth
func (h *DocumentHandler) ListDocsByElement(c *gin.Context) {
	entityID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}
	docs, err := h.svc.ListByElement(getTenantID(c), entityID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, docs)
}

// @Summary 查询术语关联的文档 | List documents by glossary
// @Tags Standard
// @Produce json
// @Success 200 {array} models.Document
// @Failure 401 {object} map[string]string "需要登录 | Authentication required"
// @Failure 403 {object} map[string]string "无权访问 | Access denied"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.glossary.read","standard.document.read"]
// @Router /glossaries/{id}/documents [get]
// @Security BearerAuth
func (h *DocumentHandler) ListDocsByGlossary(c *gin.Context) {
	entityID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}
	docs, err := h.svc.ListByGlossary(getTenantID(c), entityID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, docs)
}

// @Summary 查询指标关联的文档 | List documents by metric
// @Tags Standard
// @Produce json
// @Success 200 {array} models.Document
// @Failure 401 {object} map[string]string "需要登录 | Authentication required"
// @Failure 403 {object} map[string]string "无权访问 | Access denied"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.metric.read","standard.document.read"]
// @Router /metrics/{id}/documents [get]
// @Security BearerAuth
func (h *DocumentHandler) ListDocsByMetric(c *gin.Context) {
	entityID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}
	docs, err := h.svc.ListByMetric(getTenantID(c), entityID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, docs)
}

// ===== 创建文档并关联到标准项 =====

// @Summary 创建文档并关联到数据元 | Create and link document to element
// @Tags Standard
// @Param request body models.CreateLinkedDocumentRequest true "文档信息及当前数据元版本 | Document with current element version"
// @Failure 409 {object} map[string]string "资源版本冲突 | Resource version conflict"
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} map[string]string "需要登录 | Authentication required"
// @Failure 403 {object} map[string]string "无权访问 | Access denied"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.element.update","standard.document.create"]
// @Router /elements/{id}/documents [post]
// @Security BearerAuth
func (h *DocumentHandler) CreateAndLinkElement(c *gin.Context) {
	entityID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}
	var req models.CreateLinkedDocumentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	doc, err := h.svc.CreateAndLinkElement(&req, getTenantID(c), getUserID(c), entityID)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusCreated, doc)
}

// @Summary 创建文档并关联到术语 | Create and link document to glossary
// @Tags Standard
// @Param request body models.CreateLinkedDocumentRequest true "文档信息及当前术语版本 | Document with current glossary version"
// @Failure 409 {object} map[string]string "资源版本冲突 | Resource version conflict"
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} map[string]string "需要登录 | Authentication required"
// @Failure 403 {object} map[string]string "无权访问 | Access denied"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.glossary.update","standard.document.create"]
// @Router /glossaries/{id}/documents [post]
// @Security BearerAuth
func (h *DocumentHandler) CreateAndLinkGlossary(c *gin.Context) {
	entityID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}
	var req models.CreateLinkedDocumentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	doc, err := h.svc.CreateAndLinkGlossary(&req, getTenantID(c), getUserID(c), entityID)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusCreated, doc)
}

// @Summary 创建文档并关联到指标 | Create and link document to metric
// @Tags Standard
// @Param request body models.CreateLinkedDocumentRequest true "文档信息及当前指标版本 | Document with current metric version"
// @Failure 409 {object} map[string]string "资源版本冲突 | Resource version conflict"
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} map[string]string "需要登录 | Authentication required"
// @Failure 403 {object} map[string]string "无权访问 | Access denied"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.metric.update","standard.document.create"]
// @Router /metrics/{id}/documents [post]
// @Security BearerAuth
func (h *DocumentHandler) CreateAndLinkMetric(c *gin.Context) {
	entityID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}
	var req models.CreateLinkedDocumentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	doc, err := h.svc.CreateAndLinkMetric(&req, getTenantID(c), getUserID(c), entityID)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusCreated, doc)
}

// ===== 关联已有文档到标准项 =====

// @Summary 关联已有文档到数据元 | Link document to element
// @Tags Standard
// @Param request body models.LinkDocumentRequest true "文档 ID 及当前数据元版本 | Document ID with current element version"
// @Failure 409 {object} map[string]string "资源版本冲突 | Resource version conflict"
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} map[string]string "需要登录 | Authentication required"
// @Failure 403 {object} map[string]string "无权访问 | Access denied"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.element.update","standard.document.update"]
// @Router /elements/{id}/documents/link [post]
// @Security BearerAuth
func (h *DocumentHandler) LinkDocToElement(c *gin.Context) {
	entityID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}
	var body models.LinkDocumentRequest
	if err := c.ShouldBindJSON(&body); err != nil || body.DocID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgDocIDRequired)})
		return
	}
	if err := h.svc.LinkDocToElement(body.DocID, getTenantID(c), entityID, body.Version); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, models.ResourceVersionResponse{Version: body.Version + 1})
}

// @Summary 关联已有文档到术语 | Link document to glossary
// @Tags Standard
// @Param request body models.LinkDocumentRequest true "文档 ID 及当前术语版本 | Document ID with current glossary version"
// @Failure 409 {object} map[string]string "资源版本冲突 | Resource version conflict"
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} map[string]string "需要登录 | Authentication required"
// @Failure 403 {object} map[string]string "无权访问 | Access denied"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.glossary.update","standard.document.update"]
// @Router /glossaries/{id}/documents/link [post]
// @Security BearerAuth
func (h *DocumentHandler) LinkDocToGlossary(c *gin.Context) {
	entityID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}
	var body models.LinkDocumentRequest
	if err := c.ShouldBindJSON(&body); err != nil || body.DocID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgDocIDRequired)})
		return
	}
	if err := h.svc.LinkDocToGlossary(body.DocID, getTenantID(c), entityID, body.Version); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, models.ResourceVersionResponse{Version: body.Version + 1})
}

// @Summary 关联已有文档到指标 | Link document to metric
// @Tags Standard
// @Param request body models.LinkDocumentRequest true "文档 ID 及当前指标版本 | Document ID with current metric version"
// @Failure 409 {object} map[string]string "资源版本冲突 | Resource version conflict"
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} map[string]string "需要登录 | Authentication required"
// @Failure 403 {object} map[string]string "无权访问 | Access denied"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.metric.update","standard.document.update"]
// @Router /metrics/{id}/documents/link [post]
// @Security BearerAuth
func (h *DocumentHandler) LinkDocToMetric(c *gin.Context) {
	entityID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}
	var body models.LinkDocumentRequest
	if err := c.ShouldBindJSON(&body); err != nil || body.DocID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgDocIDRequired)})
		return
	}
	if err := h.svc.LinkDocToMetric(body.DocID, getTenantID(c), entityID, body.Version); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, models.ResourceVersionResponse{Version: body.Version + 1})
}

// ===== 解除关联 =====

// @Summary 解除文档与数据元的关联 | Unlink document from element
// @Tags Standard
// @Param request body models.VersionRequest true "当前数据元版本 | Current element version"
// @Failure 409 {object} map[string]string "资源版本冲突 | Resource version conflict"
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} map[string]string "需要登录 | Authentication required"
// @Failure 403 {object} map[string]string "无权访问 | Access denied"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.element.update","standard.document.update"]
// @Router /elements/{id}/documents/{doc_id} [delete]
// @Security BearerAuth
func (h *DocumentHandler) UnlinkDocFromElement(c *gin.Context) {
	entityID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}
	docID, err := strconv.ParseInt(c.Param("doc_id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}
	var req models.VersionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.UnlinkDocFromElement(docID, getTenantID(c), entityID, req.Version); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, models.ResourceVersionResponse{Version: req.Version + 1})
}

// @Summary 解除文档与术语的关联 | Unlink document from glossary
// @Tags Standard
// @Param request body models.VersionRequest true "当前术语版本 | Current glossary version"
// @Failure 409 {object} map[string]string "资源版本冲突 | Resource version conflict"
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} map[string]string "需要登录 | Authentication required"
// @Failure 403 {object} map[string]string "无权访问 | Access denied"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.glossary.update","standard.document.update"]
// @Router /glossaries/{id}/documents/{doc_id} [delete]
// @Security BearerAuth
func (h *DocumentHandler) UnlinkDocFromGlossary(c *gin.Context) {
	entityID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}
	docID, err := strconv.ParseInt(c.Param("doc_id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}
	var req models.VersionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.UnlinkDocFromGlossary(docID, getTenantID(c), entityID, req.Version); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, models.ResourceVersionResponse{Version: req.Version + 1})
}

// @Summary 解除文档与指标的关联 | Unlink document from metric
// @Tags Standard
// @Param request body models.VersionRequest true "当前指标版本 | Current metric version"
// @Failure 409 {object} map[string]string "资源版本冲突 | Resource version conflict"
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 401 {object} map[string]string "需要登录 | Authentication required"
// @Failure 403 {object} map[string]string "无权访问 | Access denied"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.metric.update","standard.document.update"]
// @Router /metrics/{id}/documents/{doc_id} [delete]
// @Security BearerAuth
func (h *DocumentHandler) UnlinkDocFromMetric(c *gin.Context) {
	entityID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}
	docID, err := strconv.ParseInt(c.Param("doc_id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}
	var req models.VersionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.UnlinkDocFromMetric(docID, getTenantID(c), entityID, req.Version); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, models.ResourceVersionResponse{Version: req.Version + 1})
}
