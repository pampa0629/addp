package api

import (
	"net/http"

	"github.com/addp/ontology/internal/repository"
	"github.com/gin-gonic/gin"
)

// LatestProjection resolves the last attempt even after a lost command response.
// @Summary 读取修订最新投影 | Get latest revision projection
// @Description 最新投影不等于当前激活版本 | The latest projection is not necessarily active
// @Tags Ontology
// @Produce json
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["ontology.revision.read"]
// @Param ontology_id path string true "本体标识 | Ontology identifier"
// @Param revision path int true "修订号 | Revision number"
// @Success 200 {object} ProjectionResponse "成功 | Success"
// @Failure 400 {object} ErrorResponse "请求无效 | Invalid request"
// @Failure 401 {object} ErrorResponse "未认证 | Unauthenticated"
// @Failure 403 {object} ErrorResponse "禁止访问 | Forbidden"
// @Failure 404 {object} ErrorResponse "投影不存在 | Projection not found"
// @Failure 500 {object} ErrorResponse "操作失败 | Operation failed"
// @Failure 503 {object} ErrorResponse "服务未就绪 | Module not ready"
// @Router /ontologies/{ontology_id}/revisions/{revision}/projection [get]
func (h *Handler) LatestProjection(c *gin.Context) {
	a, s, ok := scope(c)
	if !ok {
		return
	}
	r, err := h.revisions.LatestProjection(c.Request.Context(), a, s)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, projectionResponse(r))
}

// Head handles the owner command.
// @Summary 读取本体激活指针 | Get ontology activation head
// @Tags Ontology
// @Produce json
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["ontology.revision.read"]
// @Param ontology_id path string true "本体标识 | Ontology identifier"
// @Success 200 {object} HeadResponse "成功 | Success"
// @Failure 400 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 401 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 403 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 404 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 409 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 500 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 503 {object} ErrorResponse "请求失败 | Request failed"
// @Router /ontologies/{ontology_id} [get]
func (h *Handler) Head(c *gin.Context) {
	a, s, ok := scope(c)
	if !ok {
		return
	}
	r, err := h.revisions.Head(c.Request.Context(), a, s.OntologyID)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, HeadResponse{r.OntologyID, r.LastRevision, r.ActivationVersion, r.ActiveRevision, r.ActiveGeneration})
}

// Create handles the owner command.
// @Summary 创建本体草稿 | Create ontology draft
// @Tags Ontology
// @Produce json
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["ontology.revision.update"]
// @Param ontology_id path string true "本体标识 | Ontology identifier"
// @Accept json
// @Param request body CreateRequest true "请求 | Request"
// @Success 201 {object} RevisionResponse "成功 | Success"
// @Failure 400 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 401 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 403 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 404 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 409 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 500 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 503 {object} ErrorResponse "请求失败 | Request failed"
// @Router /ontologies/{ontology_id}/revisions [post]
func (h *Handler) Create(c *gin.Context) {
	a, s, ok := scope(c)
	if !ok {
		return
	}
	var req CreateRequest
	if !bind(c, &req) {
		return
	}
	if !positive(req.Revision) {
		fail(c, repository.ErrInvalid)
		return
	}
	s.Revision = req.Revision
	r, err := h.revisions.CreateDraft(c.Request.Context(), a, req.Definition.definition(s))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, revisionResponse(r))
}

// Get handles the owner command.
// @Summary 读取修订 | Get revision
// @Tags Ontology
// @Produce json
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["ontology.revision.read"]
// @Param ontology_id path string true "本体标识 | Ontology identifier"
// @Param revision path int true "修订号 | Revision number"
// @Success 200 {object} RevisionResponse "成功 | Success"
// @Failure 400 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 401 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 403 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 404 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 409 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 500 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 503 {object} ErrorResponse "请求失败 | Request failed"
// @Router /ontologies/{ontology_id}/revisions/{revision} [get]
func (h *Handler) Get(c *gin.Context) {
	a, s, ok := scope(c)
	if !ok {
		return
	}
	r, err := h.revisions.Get(c.Request.Context(), a, s)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, revisionResponse(r))
}

// Save handles the owner command.
// @Summary 保存草稿 | Save draft
// @Tags Ontology
// @Produce json
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["ontology.revision.update"]
// @Param ontology_id path string true "本体标识 | Ontology identifier"
// @Param revision path int true "修订号 | Revision number"
// @Accept json
// @Param request body SaveRequest true "请求 | Request"
// @Success 200 {object} RevisionResponse "成功 | Success"
// @Failure 400 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 401 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 403 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 404 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 409 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 500 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 503 {object} ErrorResponse "请求失败 | Request failed"
// @Router /ontologies/{ontology_id}/revisions/{revision} [put]
func (h *Handler) Save(c *gin.Context) {
	a, s, ok := scope(c)
	if !ok {
		return
	}
	var req SaveRequest
	if !bind(c, &req) {
		return
	}
	if !positive(req.Version) {
		fail(c, repository.ErrInvalid)
		return
	}
	r, err := h.revisions.SaveDraft(c.Request.Context(), a, req.Version, req.Definition.definition(s))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, revisionResponse(r))
}

// Submit handles the owner command.
// @Summary 提交审核 | Submit for review
// @Tags Ontology
// @Produce json
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["ontology.revision.update"]
// @Param ontology_id path string true "本体标识 | Ontology identifier"
// @Param revision path int true "修订号 | Revision number"
// @Accept json
// @Param request body VersionRequest true "请求 | Request"
// @Success 200 {object} RevisionResponse "成功 | Success"
// @Failure 400 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 401 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 403 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 404 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 409 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 500 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 503 {object} ErrorResponse "请求失败 | Request failed"
// @Router /ontologies/{ontology_id}/revisions/{revision}/submit [post]
func (h *Handler) Submit(c *gin.Context) { h.transition(c, "submit") }

// Return handles the owner command.
// @Summary 退回草稿 | Return to draft
// @Tags Ontology
// @Produce json
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["ontology.revision.update"]
// @Param ontology_id path string true "本体标识 | Ontology identifier"
// @Param revision path int true "修订号 | Revision number"
// @Accept json
// @Param request body VersionRequest true "请求 | Request"
// @Success 200 {object} RevisionResponse "成功 | Success"
// @Failure 400 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 401 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 403 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 404 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 409 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 500 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 503 {object} ErrorResponse "请求失败 | Request failed"
// @Router /ontologies/{ontology_id}/revisions/{revision}/return [post]
func (h *Handler) Return(c *gin.Context) { h.transition(c, "return") }

// Publish handles the owner command.
// @Summary 发布并准入投影 | Publish and admit projection
// @Tags Ontology
// @Produce json
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["ontology.revision.publish","system.execution_authorization.create"]
// @Param ontology_id path string true "本体标识 | Ontology identifier"
// @Param revision path int true "修订号 | Revision number"
// @Accept json
// @Param request body VersionRequest true "请求 | Request"
// @Success 202 {object} AcceptedResponse "成功 | Success"
// @Failure 400 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 401 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 403 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 404 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 409 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 500 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 503 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 502 {object} ErrorResponse "请求失败 | Request failed"
// @Router /ontologies/{ontology_id}/revisions/{revision}/publish [post]
func (h *Handler) Publish(c *gin.Context) { h.transition(c, "publish") }

// Withdraw handles the owner command.
// @Summary 撤回修订 | Withdraw revision
// @Tags Ontology
// @Produce json
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["ontology.revision.publish"]
// @Param ontology_id path string true "本体标识 | Ontology identifier"
// @Param revision path int true "修订号 | Revision number"
// @Accept json
// @Param request body VersionRequest true "请求 | Request"
// @Success 200 {object} RevisionResponse "成功 | Success"
// @Failure 400 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 401 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 403 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 404 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 409 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 500 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 503 {object} ErrorResponse "请求失败 | Request failed"
// @Router /ontologies/{ontology_id}/revisions/{revision}/withdraw [post]
func (h *Handler) Withdraw(c *gin.Context) { h.transition(c, "withdraw") }

// Rebuild handles the owner command.
// @Summary 重建失败投影 | Rebuild failed projection
// @Tags Ontology
// @Produce json
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["ontology.revision.publish","system.execution_authorization.create"]
// @Param ontology_id path string true "本体标识 | Ontology identifier"
// @Param revision path int true "修订号 | Revision number"
// @Accept json
// @Param request body RebuildRequest true "请求 | Request"
// @Success 202 {object} AcceptedResponse "成功 | Success"
// @Failure 400 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 401 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 403 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 404 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 409 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 500 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 503 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 502 {object} ErrorResponse "请求失败 | Request failed"
// @Router /ontologies/{ontology_id}/revisions/{revision}/rebuild [post]
func (h *Handler) Rebuild(c *gin.Context) {
	a, s, ok := scope(c)
	if !ok {
		return
	}
	var req RebuildRequest
	if !bind(c, &req) {
		return
	}
	if !positive(req.Version) || !positive(req.ActivationVersion) {
		fail(c, repository.ErrInvalid)
		return
	}
	r, err := h.revisions.RebuildProjection(c.Request.Context(), a, s, req.Version, req.FailedGeneration, req.ActivationVersion)
	if err != nil {
		fail(c, err)
		return
	}
	h.admit(c, a, s, AcceptedResponse{s.OntologyID, s.Revision, req.Version, r.Generation, r.ExecutionID})
}

// Projection handles the owner command.
// @Summary 读取投影状态 | Get projection status
// @Tags Ontology
// @Produce json
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["ontology.revision.read"]
// @Param ontology_id path string true "本体标识 | Ontology identifier"
// @Param generation path string true "投影标识 | Projection generation"
// @Success 200 {object} ProjectionResponse "成功 | Success"
// @Failure 400 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 401 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 403 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 404 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 409 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 500 {object} ErrorResponse "请求失败 | Request failed"
// @Failure 503 {object} ErrorResponse "请求失败 | Request failed"
// @Router /ontologies/{ontology_id}/projections/{generation} [get]
func (h *Handler) Projection(c *gin.Context) {
	a, s, ok := scope(c)
	if !ok {
		return
	}
	r, err := h.revisions.Projection(c.Request.Context(), a, s.OntologyID, c.Param("generation"))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, projectionResponse(r))
}
