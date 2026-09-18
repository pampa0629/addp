package api

import (
	"context"
	"errors"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	commonapi "github.com/addp/common/api"
	commonauth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	modulei18n "github.com/addp/ontology/i18n"
	"github.com/addp/ontology/internal/models"
	"github.com/addp/ontology/internal/repository"
	"github.com/addp/ontology/internal/semantic"
	"github.com/addp/ontology/internal/service"
	"github.com/gin-gonic/gin"
)

type RevisionCommands interface {
	Trial(context.Context, models.Actor, string, string, uint64, string, uint64, map[string]semantic.Fact) (*service.TrialResult, error)
	ListClasses(context.Context, models.Actor, string) (*service.ClassDirectory, error)
	ClassContext(context.Context, models.Actor, string, string, uint64, string, uint64) (*service.SemanticContext, error)
	ListOntologies(context.Context, models.Actor, models.ListPage) ([]models.Ontology, int64, error)
	ListRevisions(context.Context, models.Actor, string, models.ListPage) ([]models.RevisionSummary, int64, error)
	CreateDraft(context.Context, models.Actor, semantic.Definition) (*models.Revision, error)
	SaveDraft(context.Context, models.Actor, uint64, semantic.Definition) (*models.Revision, error)
	Get(context.Context, models.Actor, semantic.Scope) (*models.Revision, error)
	Head(context.Context, models.Actor, string) (*models.Ontology, error)
	Projection(context.Context, models.Actor, string, string) (*models.Projection, error)
	LatestProjection(context.Context, models.Actor, semantic.Scope) (*models.Projection, error)
	Transition(context.Context, models.Actor, semantic.Scope, uint64, string) (*models.Revision, error)
	RebuildProjection(context.Context, models.Actor, semantic.Scope, uint64, string, uint64) (*models.Projection, error)
	AdmitProjection(context.Context, models.Actor, semantic.Scope, uint64, string, string, service.ExecutionAuthorizationIssuer) error
}

type Handler struct {
	revisions RevisionCommands
	issuer    service.ExecutionAuthorizationIssuer
}

var identifier = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

func requestActor(c *gin.Context) models.Actor {
	a, _ := commonauth.AuthContextFromGin(c)
	tenant, _ := commonauth.TenantIDFromGin(c)
	principal, _ := commonauth.PrincipalIDFromGin(c)
	membership, _ := strconv.ParseInt(*a.Context.TenantMembershipID, 10, 64)
	version, _ := strconv.ParseInt(a.Authorization.AuthorizationVersion, 10, 64)
	return models.Actor{TenantID: uint64(tenant), PrincipalID: principal, MembershipID: membership, AuthorizationVersion: version}
}

func scope(c *gin.Context) (models.Actor, semantic.Scope, bool) {
	a := requestActor(c)
	s := semantic.Scope{TenantID: a.TenantID, OntologyID: c.Param("ontology_id"), Revision: 1}
	if raw := c.Param("revision"); raw != "" {
		n, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || !positive(n) || strconv.FormatUint(n, 10) != raw {
			fail(c, repository.ErrInvalid)
			return a, s, false
		}
		s.Revision = n
	}
	if !identifier.MatchString(s.OntologyID) || repository.ValidateActor(a, s) != nil {
		fail(c, repository.ErrInvalid)
		return a, s, false
	}
	return a, s, true
}

func positive(n uint64) bool { return n > 0 && n < math.MaxInt64 }

func bind(c *gin.Context, request any) bool {
	if c.ContentType() != "application/json" {
		fail(c, repository.ErrInvalid)
		return false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, (1<<20)+4096)
	if commonapi.BindOptionalJSONStrict(c, request) != nil {
		fail(c, repository.ErrInvalid)
		return false
	}
	return true
}

func fail(c *gin.Context, err error) {
	status, code, key := http.StatusInternalServerError, "ontology_operation_failed", modulei18n.Failed
	switch {
	case errors.Is(err, repository.ErrNotActive):
		status, code, key = http.StatusConflict, "ontology_not_active", modulei18n.NotActive
	case errors.Is(err, service.ErrActivationChanged):
		status, code, key = http.StatusConflict, "ontology_activation_changed", modulei18n.ActivationChanged
	case errors.Is(err, service.ErrResultTooLarge):
		status, code, key = http.StatusRequestEntityTooLarge, "result_too_large", modulei18n.ResultTooLarge
	case errors.Is(err, repository.ErrInvalid):
		status, code, key = http.StatusBadRequest, "invalid_ontology_request", modulei18n.Invalid
	case errors.Is(err, repository.ErrConflict):
		status, code, key = http.StatusConflict, "resource_version_conflict", modulei18n.Conflict
	case errors.Is(err, repository.ErrNotFound):
		status, code, key = http.StatusNotFound, "ontology_not_found", modulei18n.NotFound
	}
	c.AbortWithStatusJSON(status, ErrorResponse{Error: commoni18n.T(c, key), ErrorCode: code})
}

func (h *Handler) transition(c *gin.Context, action string) {
	a, s, ok := scope(c)
	if !ok {
		return
	}
	var req VersionRequest
	if !bind(c, &req) {
		return
	}
	if !positive(req.Version) {
		fail(c, repository.ErrInvalid)
		return
	}
	r, err := h.revisions.Transition(c.Request.Context(), a, s, req.Version, action)
	if err != nil {
		fail(c, err)
		return
	}
	if action == "publish" {
		if r.Generation == nil || r.BuildExecutionID == nil {
			fail(c, repository.ErrIntegrity)
			return
		}
		h.admit(c, a, s, AcceptedResponse{s.OntologyID, s.Revision, r.Version, *r.Generation, *r.BuildExecutionID})
		return
	}
	c.JSON(http.StatusOK, revisionResponse(r))
}

func (h *Handler) admit(c *gin.Context, a models.Actor, s semantic.Scope, result AcceptedResponse) {
	// The authentication middleware has already verified this exact Bearer.
	token := strings.Fields(c.GetHeader("Authorization"))[1]
	if err := h.revisions.AdmitProjection(c.Request.Context(), a, s, result.Version, result.Generation, token, h.issuer); err != nil {
		c.JSON(http.StatusBadGateway, ErrorResponse{Error: commoni18n.T(c, modulei18n.AdmissionFailed), ErrorCode: "projection_admission_failed", Intent: &result})
		return
	}
	c.JSON(http.StatusAccepted, result)
}
