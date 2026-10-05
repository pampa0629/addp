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
	"sort"
	"time"

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
		Name             string                  `json:"name"`
		Config           TransferExecutionConfig `json:"config"`
		BatchSize        int                     `json:"batch_size"`
		AutoScanMetadata bool                    `json:"auto_scan_metadata"`
	}{request.Name, request.Config, request.BatchSize, request.AutoScanMetadata})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

type ExportSourceRegistry interface {
	ListActiveModules(context.Context) ([]*ModuleInfo, error)
}

type ExportExecutionSourceClient struct {
	registry   ExportSourceRegistry
	tokens     ServiceTokenProvider
	httpClient *http.Client
}

func NewExportExecutionSourceClient(registry ExportSourceRegistry, tokens ServiceTokenProvider) *ExportExecutionSourceClient {
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
	modules, err := c.registry.ListActiveModules(ctx)
	if err != nil {
		return nil, err
	}
	var instances []ModuleRuntimeInstanceInfo
	for _, module := range modules {
		if module == nil || !module.Enabled || module.ModuleName != owner {
			continue
		}
		for _, instance := range module.Instances {
			if instance.Role == ModuleRuntimeRoleBackend && instance.Status == "up" && instance.LeaseExpiresAt.After(time.Now()) {
				instances = append(instances, instance)
			}
		}
	}
	if len(instances) == 0 {
		return nil, errors.New("export source backend is unavailable")
	}
	sort.Slice(instances, func(i, j int) bool { return instances[i].InstanceID < instances[j].InstanceID })
	base, err := url.Parse(instances[0].ModuleURL)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || (base.Path != "" && base.Path != "/") {
		return nil, errors.New("export source backend URL is invalid")
	}
	transport := newTenantHTTPClient(base.Scheme+"://"+base.Host, c.tokens, c.httpClient).withTenantID(tenantID)
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
