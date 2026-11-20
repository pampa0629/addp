package client

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/addp/common/models"
	"github.com/google/uuid"
)

// TransferExportSessionReference identifies immutable owner facts, never an actor claim.
type TransferExportSessionReference struct {
	SessionID   uint   `json:"session_id" binding:"required"`
	ExecutionID string `json:"execution_id" binding:"required"`
}

func (r TransferExportSessionReference) Validate() error {
	if r.SessionID == 0 || !validExportExecutionID(r.ExecutionID) {
		return errors.New("invalid export session reference")
	}
	return nil
}

func validExportExecutionID(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id != uuid.Nil && id.String() == value
}

type ExportExecutionSourceRequest struct {
	ExecutionID   string `json:"execution_id" binding:"required"`
	RequestDigest string `json:"request_digest" binding:"required"`
}

func (r ExportExecutionSourceRequest) Validate() error {
	if !validExportExecutionID(r.ExecutionID) {
		return errors.New("invalid export execution UUID")
	}
	digest, err := hex.DecodeString(r.RequestDigest)
	if err != nil || len(digest) != sha256.Size || hex.EncodeToString(digest) != r.RequestDigest {
		return errors.New("invalid export request digest")
	}
	return nil
}

// ExportExecutionSource carries observation provenance, not execution authority.
type ExportExecutionSource struct {
	TenantID uint `json:"tenant_id"`
	UserID   uint `json:"user_id"`
}

// ExportRequestDigest freezes every executable request field. The reference is
// excluded because the owning session is created after computing the digest.
func ExportRequestDigest(request *CreateTransferExecutionRequest) (string, error) {
	if request == nil {
		return "", errors.New("export execution request is required")
	}
	encoded, err := json.Marshal(struct {
		Name                   string                        `json:"name"`
		Config                 TransferExecutionConfig       `json:"config"`
		BatchSize              int                           `json:"batch_size"`
		AutoScanMetadata       bool                          `json:"auto_scan_metadata"`
		ExecutionAuthorization *IssuedExecutionAuthorization `json:"execution_authorization,omitempty"`
		FileGDBActor           *FileGDBExportActor           `json:"filegdb_actor,omitempty"`
	}{request.Name, request.Config, request.BatchSize, request.AutoScanMetadata, request.ExecutionAuthorization, request.FileGDBActor})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

type ModuleEndpointResolver interface {
	ListModuleEndpoints(context.Context, string) ([]models.ModuleRoutingEndpoint, error)
}

type ExportExecutionSourceClient struct {
	registry   ModuleEndpointResolver
	tokens     ServiceTokenProvider
	httpClient *http.Client
}

func NewExportExecutionSourceClient(registry ModuleEndpointResolver, tokens ServiceTokenProvider) *ExportExecutionSourceClient {
	return &ExportExecutionSourceClient{registry: registry, tokens: tokens, httpClient: &http.Client{
		Timeout:       30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (c *ExportExecutionSourceClient) ResolveExportExecutionSource(ctx context.Context, owner string, tenantID uint, ref TransferExportSessionReference, digest string) (*ExportExecutionSource, error) {
	if c == nil || c.registry == nil || c.tokens == nil || tenantID == 0 || (owner != "manager" && owner != "develop") {
		return nil, errors.New("export source owner is unavailable")
	}
	request := ExportExecutionSourceRequest{ExecutionID: ref.ExecutionID, RequestDigest: digest}
	if err := ref.Validate(); err != nil {
		return nil, err
	}
	if err := request.Validate(); err != nil {
		return nil, err
	}
	transport, err := ownerArtifactTransport(ctx, c.registry, c.tokens, c.httpClient, owner, tenantID)
	if err != nil {
		return nil, err
	}
	var result ExportExecutionSource
	path := fmt.Sprintf("/api/v1/%s/runtime/export-sessions/%d/execution-source", owner, ref.SessionID)
	if err := transport.doJSON(ctx, http.MethodPost, path, request, &result); err != nil {
		return nil, err
	}
	if result.TenantID != tenantID || result.UserID == 0 {
		return nil, errors.New("export source identity is invalid")
	}
	return &result, nil
}

// ownerArtifactTransport selects one live Owner backend for private artifact facts.
func ownerArtifactTransport(ctx context.Context, registry ModuleEndpointResolver, tokens ServiceTokenProvider, httpClient *http.Client, owner string, tenantID uint) (tenantHTTPClient, error) {
	endpoints, err := registry.ListModuleEndpoints(ctx, owner)
	if err != nil {
		return tenantHTTPClient{}, err
	}
	base := ownerEndpointURL(endpoints, owner, time.Now())
	if base == "" {
		return tenantHTTPClient{}, errors.New("artifact source backend is unavailable")
	}
	return newTenantHTTPClient(base, tokens, httpClient).withTenantID(tenantID), nil
}

func ownerEndpointURL(endpoints []models.ModuleRoutingEndpoint, owner string, now time.Time) string {
	for _, ep := range endpoints {
		if ep.ModuleName != owner || !ep.ExpiresAt.After(now) {
			continue
		}
		base, err := url.Parse(ep.URL)
		if err == nil && (base.Scheme == "http" || base.Scheme == "https") && base.Hostname() != "" && base.User == nil && !base.ForceQuery && base.RawQuery == "" && base.Fragment == "" && (base.Path == "" || base.Path == "/") {
			return base.Scheme + "://" + base.Host
		}
	}
	return ""
}
