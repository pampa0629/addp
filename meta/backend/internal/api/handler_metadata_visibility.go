package api

import (
	"context"
	"errors"
	commonClient "github.com/addp/common/client"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/authorization"
	"github.com/addp/common/engine/plugin"
	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/addp/meta/internal/service"
	"github.com/gin-gonic/gin"
)

type metadataVisibilityClient interface {
	CheckMetaMetadataRead(context.Context, string, []plugin.EngineCatalogPath) (*authorization.SourceMetadataCheck, error)
}

func (h *Handler) metadataBrowsingContext(c *gin.Context) context.Context {
	current, ok := commonAuth.AuthContextFromGin(c)
	if ok && current.Principal.Type == "service_principal" && current.Token.Type == "service_access_token" {
		return c.Request.Context()
	}
	c.Header("Cache-Control", "no-store")
	return service.WithMetadataVisibility(c.Request.Context(), func(ctx context.Context, targets []plugin.EngineCatalogPath) (*authorization.SourceMetadataCheck, error) {
		if !ok || current.Principal.Type != "user" || current.Context.Type != "tenant" || h.metadataVisibility == nil {
			return nil, commonapi.ErrForbidden
		}
		return h.metadataVisibility.CheckMetaMetadataRead(ctx, commonAuth.CanonicalBearerToken(c.GetHeader("Authorization")), targets)
	})
}

func (h *Handler) visibleMetadataItem(c *gin.Context, item *models.MetaItemLite) (*models.MetaItemLite, error) {
	if item == nil {
		return nil, gorm.ErrRecordNotFound
	}
	items, err := h.resourceTreeService.FilterMetadataItems(h.metadataBrowsingContext(c), commonAuth.GetTenantID(c), []models.MetaItemLite{*item})
	if err != nil {
		return nil, err
	}
	if len(items) != 1 {
		return nil, gorm.ErrRecordNotFound
	}
	return &items[0], nil
}

func (h *Handler) visibleMetadataNode(c *gin.Context, node *models.MetaNodeLite) (*models.MetaNodeLite, error) {
	if node == nil {
		return nil, gorm.ErrRecordNotFound
	}
	nodes, err := h.resourceTreeService.FilterMetadataNodes(h.metadataBrowsingContext(c), commonAuth.GetTenantID(c), []models.MetaNodeLite{*node})
	if err != nil {
		return nil, err
	}
	if len(nodes) != 1 {
		return nil, gorm.ErrRecordNotFound
	}
	return &nodes[0], nil
}

func (h *Handler) respondMetadataQueryError(c *gin.Context, err error) {
	c.Header("Cache-Control", "no-store")
	if errors.Is(err, commonapi.ErrForbidden) || errors.Is(err, commonClient.ErrMetadataQueryDenied) || errors.Is(err, commonClient.ErrMetadataQueryUnavailable) || errors.Is(err, commonClient.ErrMetadataCredentialRejected) {
		h.handleServiceError(c, err)
		return
	}
	// Do not echo database errors, target paths or names from hidden candidates.
	respondItemSnapshotReadError(c, err)
}

type lineageVisibilityClient interface {
	CheckMetaLineageRead(context.Context, string, []plugin.EngineCatalogPath) (*authorization.SourceMetadataCheck, error)
}

func (h *Handler) lineageBrowsingContext(c *gin.Context) context.Context {
	current, ok := commonAuth.AuthContextFromGin(c)
	if ok && current.Principal.Type == "service_principal" && current.Token.Type == "service_access_token" {
		return c.Request.Context()
	}
	c.Header("Cache-Control", "no-store")
	return service.WithLineageVisibility(c.Request.Context(), func(ctx context.Context) ([]uint, error) {
		if !ok || current.Principal.Type != "user" || current.Context.Type != "tenant" || current.Token.Type == "delegated_access_token" || h.lineageVisibility == nil {
			return nil, commonapi.ErrForbidden
		}
		ctx = service.WithMetadataVisibility(ctx, func(ctx context.Context, targets []plugin.EngineCatalogPath) (*authorization.SourceMetadataCheck, error) {
			return h.lineageVisibility.CheckMetaLineageRead(ctx, commonAuth.CanonicalBearerToken(c.GetHeader("Authorization")), targets)
		})
		return h.resourceTreeService.VisibleLineageItems(ctx, commonAuth.GetTenantID(c))
	})
}
