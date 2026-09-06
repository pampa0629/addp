package api

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"

	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	sysi18n "github.com/addp/standard/i18n"
	standardauthorization "github.com/addp/standard/internal/authorization"
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
// @Param scope_type query string false "适用范围 | Scope" Enums(platform,tenant_common,domain)
// @Param owner_domain_id query int false "归属业务域 ID | Owning domain ID"
// @Param status query string false "修订状态 | Revision status" Enums(draft,in_review,published,withdrawn)
// @Param as_of query string false "生效时点（RFC3339） | Effective point in time (RFC3339)"
// @Success 200 {object} models.PaginatedDocumentResponse
// @Failure 401 {object} map[string]string "需要登录 | Authentication required"
// @Failure 403 {object} map[string]string "无权访问 | Access denied"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.document.read"]
// @Router /documents [get]
// @Security BearerAuth
func (h *DocumentHandler) ListDocuments(c *gin.Context) {
	tenantID := getTenantID(c)
	status, err := parseOptionalRevisionStatus(c)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	opts := repository.ListDocumentOptions{
		DocType: c.Query("doc_type"), Status: status, Keyword: c.Query("keyword"),
	}
	if opts.ScopeType, err = parseOptionalStandardScope(c); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	if opts.AsOf, err = parseOptionalAsOf(c); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	if value := c.Query("owner_domain_id"); value != "" {
		id, parseErr := strconv.ParseInt(value, 10, 64)
		if parseErr != nil || id <= 0 {
			respondError(c, http.StatusBadRequest, fmt.Errorf("invalid owner_domain_id"))
			return
		}
		opts.OwnerDomainID = &id
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
// @Param as_of query string false "生效时点（RFC3339） | Effective point in time (RFC3339)"
// @Success 200 {object} models.DocumentAggregate
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
	asOf, err := parseOptionalAsOf(c)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	doc, err := h.svc.GetDocumentAt(id, tenantID, asOf)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, sysi18n.MsgDocumentNotFound)})
		return
	}
	c.JSON(http.StatusOK, doc)
}

// @Summary 创建标准文档 | Create standard document
// @Tags Standard
// @Produce json
// @Param request body models.CreateDocumentRequest true "文档身份和首个草稿修订 | Document identity and initial draft revision"
// @Success 201 {object} models.DocumentAggregate
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
// @Success 200 {object} models.DocumentAggregate
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
// @Summary 上传文档修订文件 | Upload document revision file
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
// @Router /documents/{id}/revisions/{revision_id}/file [post]
// @Security BearerAuth
func (h *DocumentHandler) UploadFile(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}
	revisionID, err := strconv.ParseInt(c.Param("revision_id"), 10, 64)
	if err != nil || revisionID <= 0 {
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
	doc, err := h.svc.UploadFile(id, revisionID, tenantID, getUserID(c), version, file.Filename, f, file.Size, contentType)
	if err != nil {
		respondDocumentFileError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message":   commoni18n.T(c, sysi18n.MsgUploadSuccess),
		"file_name": doc.DraftRevision.FileName,
		"file_size": doc.DraftRevision.FileSize,
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
// @Router /documents/{id}/revisions/{revision_id}/file [get]
// @Security BearerAuth
func (h *DocumentHandler) DownloadFile(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}
	revisionID, err := strconv.ParseInt(c.Param("revision_id"), 10, 64)
	if err != nil || revisionID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidID)})
		return
	}
	tenantID := getTenantID(c)

	reader, fileName, mediaType, fileSize, err := h.svc.DownloadFile(id, revisionID, tenantID)
	if err != nil {
		respondDocumentFileError(c, err)
		return
	}
	defer reader.Close()

	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": fileName}))
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	c.Header("Content-Type", mediaType)
	if fileSize > 0 {
		c.Header("Content-Length", strconv.FormatInt(fileSize, 10))
	}
	c.Status(http.StatusOK)
	if _, err := io.Copy(c.Writer, reader); err != nil {
		c.Error(err)
	}
}

// @Summary 获取文档修订历史 | List document revisions
// @Tags Standard
// @Produce json
// @Success 200 {array} models.DocumentRevision
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.document.read"]
// @Router /documents/{id}/revisions [get]
// @Security BearerAuth
func (h *DocumentHandler) ListRevisions(c *gin.Context) {
	id, ok := elementPathID(c, "id")
	if !ok {
		return
	}
	items, err := h.svc.ListRevisions(id, getTenantID(c))
	if err != nil {
		respondError(c, http.StatusNotFound, err)
		return
	}
	c.JSON(http.StatusOK, items)
}

// @Summary 创建文档草稿修订 | Create document draft revision
// @Tags Standard
// @Accept json
// @Produce json
// @Param request body models.CreateDocumentRevisionRequest true "并发版本和变更说明 | Version and change summary"
// @Success 201 {object} models.DocumentAggregate
// @Failure 409 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.document.update"]
// @Router /documents/{id}/revisions [post]
// @Security BearerAuth
func (h *DocumentHandler) CreateRevision(c *gin.Context) {
	id, ok := elementPathID(c, "id")
	if !ok {
		return
	}
	var req models.CreateDocumentRevisionRequest
	if !bindJSON(c, &req) {
		return
	}
	result, err := h.svc.CreateRevision(id, getTenantID(c), getUserID(c), &req)
	if err != nil {
		respondError(c, http.StatusConflict, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// @Summary 获取文档修订 | Get document revision
// @Tags Standard
// @Produce json
// @Success 200 {object} models.DocumentRevision
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.document.read"]
// @Router /documents/{id}/revisions/{revision_id} [get]
// @Security BearerAuth
func (h *DocumentHandler) GetRevision(c *gin.Context) {
	id, revisionID, ok := documentRevisionPath(c)
	if !ok {
		return
	}
	result, err := h.svc.GetRevision(id, revisionID, getTenantID(c))
	if err != nil {
		respondError(c, http.StatusNotFound, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// @Summary 更新文档草稿修订 | Update document draft revision
// @Tags Standard
// @Accept json
// @Produce json
// @Param request body models.UpdateDocumentRevisionRequest true "完整草稿及并发版本 | Full draft and concurrency version"
// @Success 200 {object} models.DocumentAggregate
// @Failure 409 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.document.update"]
// @Router /documents/{id}/revisions/{revision_id} [put]
// @Security BearerAuth
func (h *DocumentHandler) UpdateRevision(c *gin.Context) {
	id, revisionID, ok := documentRevisionPath(c)
	if !ok {
		return
	}
	var req models.UpdateDocumentRevisionRequest
	if !bindJSON(c, &req) {
		return
	}
	result, err := h.svc.UpdateRevision(id, revisionID, getTenantID(c), getUserID(c), &req)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// @Summary 提交文档修订审核 | Submit document revision for review
// @Tags Standard
// @Accept json
// @Produce json
// @Param request body models.RevisionActionRequest true "当前资源版本 | Current resource version"
// @Success 200 {object} models.DocumentAggregate
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.document.update"]
// @Router /documents/{id}/revisions/{revision_id}/submit [post]
// @Security BearerAuth
func (h *DocumentHandler) SubmitRevision(c *gin.Context) { h.revisionAction(c, h.svc.SubmitRevision) }

// @Summary 退回文档修订 | Return document revision to draft
// @Tags Standard
// @Accept json
// @Produce json
// @Param request body models.RevisionActionRequest true "当前资源版本 | Current resource version"
// @Success 200 {object} models.DocumentAggregate
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.document.publish"]
// @Router /documents/{id}/revisions/{revision_id}/return [post]
// @Security BearerAuth
func (h *DocumentHandler) ReturnRevision(c *gin.Context) { h.revisionAction(c, h.svc.ReturnRevision) }

// @Summary 发布文档修订 | Publish document revision
// @Tags Standard
// @Accept json
// @Produce json
// @Param request body models.RevisionActionRequest true "当前资源版本 | Current resource version"
// @Success 200 {object} models.DocumentAggregate
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.document.publish"]
// @Router /documents/{id}/revisions/{revision_id}/publish [post]
// @Security BearerAuth
func (h *DocumentHandler) PublishRevision(c *gin.Context) { h.revisionAction(c, h.svc.PublishRevision) }

// @Summary 撤回文档发布修订 | Withdraw published document revision
// @Tags Standard
// @Accept json
// @Produce json
// @Param request body models.RevisionActionRequest true "当前资源版本 | Current resource version"
// @Success 200 {object} models.DocumentAggregate
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.document.publish"]
// @Router /documents/{id}/revisions/{revision_id}/withdraw [post]
// @Security BearerAuth
func (h *DocumentHandler) WithdrawRevision(c *gin.Context) {
	h.revisionAction(c, h.svc.WithdrawRevision)
}

// @Summary 从文档修订提炼标准候选 | Extract standard candidates from document revision
// @Tags Standard
// @Accept json
// @Produce json
// @Param request body models.CreateDocumentExtractionRequest true "当前资源版本 | Current resource version"
// @Success 201 {object} models.DocumentExtraction
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 413 {object} map[string]string
// @Failure 422 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Failure 503 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.document_extraction.create"]
// @Router /documents/{id}/revisions/{revision_id}/extractions [post]
// @Security BearerAuth
func (h *DocumentHandler) ExtractCandidates(c *gin.Context) {
	id, revisionID, ok := documentRevisionPath(c)
	if !ok {
		return
	}
	var req models.CreateDocumentExtractionRequest
	if !bindJSON(c, &req) {
		return
	}
	result, err := h.svc.ExtractCandidates(c.Request.Context(), id, revisionID, getTenantID(c), getUserID(c), req.Version)
	if err != nil {
		respondDocumentExtractionError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// @Summary 获取标准提炼候选聚合视图 | List standard extraction candidate groups
// @Description 按确定性语义指纹聚合文档历次提炼候选；total 为筛选后总数，status_counts 为不受筛选影响的文档全量状态计数；返回代表候选、全部出现记录及动态标准比对，原始候选和证据不会被改写 | Groups candidates from all document extractions by deterministic semantic fingerprint; total is the filtered count while status_counts covers all groups regardless of filters; returns the representative candidate, all occurrences, and dynamic standard comparison without rewriting raw candidates or evidence
// @Tags Standard
// @Produce json
// @Param state query string false "聚合状态 | Group state" Enums(pending,retained,rejected,formalized)
// @Param candidate_type query string false "候选类型 | Candidate type" Enums(glossary,element,code_set,metric)
// @Param page query int false "页码，默认 1 | Page number, default 1"
// @Param page_size query int false "每页数量，默认 20，最大 100 | Page size, default 20, maximum 100"
// @Success 200 {object} models.PaginatedDocumentExtractionCandidateGroupResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string "需要登录 | Authentication required"
// @Failure 403 {object} map[string]string "无权访问 | Access denied"
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.document.read"]
// @Router /documents/{id}/extraction-candidate-groups [get]
// @Security BearerAuth
func (h *DocumentHandler) ListCandidateGroups(c *gin.Context) {
	id, ok := elementPathID(c, "id")
	if !ok {
		return
	}
	opts := service.DocumentCandidateGroupListOptions{State: c.Query("state"), CandidateType: c.Query("candidate_type")}
	var err error
	if value := c.Query("page"); value != "" {
		opts.Page, err = strconv.Atoi(value)
		if err != nil {
			respondError(c, http.StatusBadRequest, service.ErrDocumentCandidateGroupQueryInvalid)
			return
		}
	}
	if value := c.Query("page_size"); value != "" {
		opts.PageSize, err = strconv.Atoi(value)
		if err != nil {
			respondError(c, http.StatusBadRequest, service.ErrDocumentCandidateGroupQueryInvalid)
			return
		}
	}
	items, err := h.svc.ListCandidateGroups(id, getTenantID(c), opts)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, service.ErrDocumentCandidateGroupQueryInvalid) {
			status = http.StatusBadRequest
		}
		respondError(c, status, err)
		return
	}
	c.JSON(http.StatusOK, items)
}

// @Summary 裁决文档提炼候选 | Review document extraction candidate
// @Tags Standard
// @Accept json
// @Produce json
// @Param request body models.UpdateDocumentExtractionCandidateRequest true "裁决状态及并发版本 | Decision and concurrency version"
// @Success 200 {object} models.DocumentExtractionCandidate
// @Failure 409 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.document.update"]
// @Router /document-extraction-candidates/{candidate_id} [put]
// @Security BearerAuth
func (h *DocumentHandler) UpdateCandidate(c *gin.Context) {
	id, ok := elementPathID(c, "candidate_id")
	if !ok {
		return
	}
	var req models.UpdateDocumentExtractionCandidateRequest
	if !bindJSON(c, &req) {
		return
	}
	result, err := h.svc.UpdateCandidateStatus(id, getTenantID(c), getUserID(c), &req)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// @Summary 正式化文档提炼候选 | Formalize document extraction candidate
// @Description 服务器根据实时比对唯一决定创建 R1 草稿、创建既有标准的新修订草稿或关联内容一致修订；不会提交审核或发布 | The server uniquely decides whether to create an R1 draft, create a new draft revision, or link an identical revision; it never submits or publishes
// @Tags Standard
// @Accept json
// @Produce json
// @Param candidate_id path int true "候选 ID | Candidate ID"
// @Param request body models.FormalizeDocumentExtractionCandidateRequest true "正式化请求 | Formalization request"
// @Success 201 {object} models.DocumentCandidateFormalizationResponse
// @Failure 400 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["standard.document.update"]
// @x-addp-conditional-permissions ["standard.glossary.create","standard.glossary.update","standard.element.create","standard.element.update","standard.code_set.create","standard.code_set.update","standard.metric.create","standard.metric.update"]
// @Router /document-extraction-candidates/{candidate_id}/formalization [post]
// @Security BearerAuth
func (h *DocumentHandler) FormalizeCandidate(c *gin.Context) {
	id, ok := elementPathID(c, "candidate_id")
	if !ok {
		return
	}
	var req models.FormalizeDocumentExtractionCandidateRequest
	if !bindJSON(c, &req) {
		return
	}
	authorization := service.CandidateFormalizationAuthorization{
		Create: map[string]bool{
			"glossary": commonAuth.HasRolePermission(c, standardauthorization.PermissionStandardGlossaryCreate),
			"element":  commonAuth.HasRolePermission(c, standardauthorization.PermissionStandardElementCreate),
			"code_set": commonAuth.HasRolePermission(c, standardauthorization.PermissionStandardCodeSetCreate),
			"metric":   commonAuth.HasRolePermission(c, standardauthorization.PermissionStandardMetricCreate),
		},
		Update: map[string]bool{
			"glossary": commonAuth.HasRolePermission(c, standardauthorization.PermissionStandardGlossaryUpdate),
			"element":  commonAuth.HasRolePermission(c, standardauthorization.PermissionStandardElementUpdate),
			"code_set": commonAuth.HasRolePermission(c, standardauthorization.PermissionStandardCodeSetUpdate),
			"metric":   commonAuth.HasRolePermission(c, standardauthorization.PermissionStandardMetricUpdate),
		},
	}
	result, err := h.svc.FormalizeCandidate(id, getTenantID(c), getUserID(c), &req, authorization)
	if err != nil {
		respondCandidateFormalizationError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

func (h *DocumentHandler) revisionAction(c *gin.Context, action func(int64, int64, int64, int64, int64) (*models.DocumentAggregate, error)) {
	id, revisionID, ok := documentRevisionPath(c)
	if !ok {
		return
	}
	var req models.RevisionActionRequest
	if !bindJSON(c, &req) {
		return
	}
	result, err := action(id, revisionID, getTenantID(c), getUserID(c), req.Version)
	if err != nil {
		respondError(c, http.StatusConflict, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func documentRevisionPath(c *gin.Context) (int64, int64, bool) {
	id, ok := elementPathID(c, "id")
	if !ok {
		return 0, 0, false
	}
	revisionID, ok := elementPathID(c, "revision_id")
	return id, revisionID, ok
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
// @Success 200 {array} models.DocumentAggregate
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
// @Success 200 {array} models.DocumentAggregate
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
// @Success 200 {array} models.DocumentAggregate
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
