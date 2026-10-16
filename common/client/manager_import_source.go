package client

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ManagerImportSourceRequest carries only facts frozen by the Transfer Owner.
// It is never accepted from a User as execution identity.
type ManagerImportSourceRequest struct {
	ExecutionID   string `json:"execution_id"`
	TaskID        uint   `json:"task_id"`
	PrincipalID   int64  `json:"principal_id"`
	SourceLocator string `json:"source_locator"`
}

func (r ManagerImportSourceRequest) Validate() error {
	id, err := uuid.Parse(r.ExecutionID)
	source, sourceErr := url.Parse(r.SourceLocator)
	if err != nil || id == uuid.Nil || id.String() != r.ExecutionID || r.TaskID == 0 || r.PrincipalID <= 0 || sourceErr != nil || source.Scheme != "addp-infra" || source.Host != "minio" || source.User != nil || source.Fragment != "" || source.Query().Get("type") != "object" || r.SourceLocator != strings.TrimSpace(r.SourceLocator) {
		return errors.New("invalid import source verification request")
	}
	return nil
}

type ManagerImportSource struct {
	TenantID      uint   `json:"tenant_id"`
	UploaderID    int64  `json:"uploader_id"`
	TaskID        uint   `json:"task_id"`
	SourceLocator string `json:"source_locator"`
}

func (s ManagerImportSource) Matches(tenantID uint, request ManagerImportSourceRequest) bool {
	return tenantID > 0 && s.TenantID == tenantID && s.UploaderID == request.PrincipalID && s.TaskID == request.TaskID && s.SourceLocator == request.SourceLocator
}

type ManagerImportSourceClient struct {
	registry   ArtifactSourceRegistry
	tokens     ServiceTokenProvider
	httpClient *http.Client
}

func NewManagerImportSourceClient(registry ArtifactSourceRegistry, tokens ServiceTokenProvider) *ManagerImportSourceClient {
	return &ManagerImportSourceClient{registry: registry, tokens: tokens, httpClient: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (c *ManagerImportSourceClient) VerifyImportSource(ctx context.Context, tenantID uint, request ManagerImportSourceRequest) (*ManagerImportSource, error) {
	if c == nil || c.registry == nil || c.tokens == nil || tenantID == 0 {
		return nil, errors.New("import source owner is unavailable")
	}
	if err := request.Validate(); err != nil {
		return nil, err
	}
	transport, err := ownerArtifactTransport(ctx, c.registry, c.tokens, c.httpClient, "manager", tenantID)
	if err != nil {
		return nil, err
	}
	var result ManagerImportSource
	if err := transport.doJSON(ctx, http.MethodPost, "/api/v1/manager/runtime/import-sources/verify", request, &result); err != nil {
		return nil, err
	}
	if !result.Matches(tenantID, request) {
		return nil, errors.New("import source identity mismatch")
	}
	return &result, nil
}
