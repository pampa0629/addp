package client

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/google/uuid"
)

const UploadReceiptSchema = "manager.upload-receipt/v1"
const MaxUploadFiles = 200
const MaxUploadReceiptBytes = 512 << 10
const maxUploadIdentificationBytes = MaxUploadFiles*authorizationfacts.MaxAuthorizationFactsBytes + (64 << 10)

type UploadUserOrigin struct {
	PrincipalID          int64 `json:"principal_id,string" swaggertype:"string"`
	MembershipID         int64 `json:"membership_id,string" swaggertype:"string"`
	AuthorizationVersion int64 `json:"authorization_version,string" swaggertype:"string"`
}

type UploadReceipt struct {
	SchemaVersion string                        `json:"schema_version"`
	UploadID      string                        `json:"upload_id"`
	TenantID      int64                         `json:"tenant_id,string" swaggertype:"string"`
	EngineID      uint                          `json:"engine_id,string" swaggertype:"string"`
	EngineVersion string                        `json:"engine_version"`
	Uploader      UploadUserOrigin              `json:"uploader"`
	Writes        []plugin.ContentWriteEvidence `json:"writes"`
	CommittedAt   time.Time                     `json:"committed_at"`
}

func (r UploadReceipt) Validate() error {
	id, err := uuid.Parse(r.UploadID)
	version, vErr := strconv.ParseInt(r.EngineVersion, 10, 64)
	if r.SchemaVersion != UploadReceiptSchema || err != nil || id == uuid.Nil || id.String() != r.UploadID || r.TenantID <= 0 || r.EngineID == 0 || vErr != nil || version <= 0 || strconv.FormatInt(version, 10) != r.EngineVersion || r.Uploader.PrincipalID <= 0 || r.Uploader.MembershipID <= 0 || r.Uploader.AuthorizationVersion <= 0 || r.CommittedAt.IsZero() || len(r.Writes) == 0 || len(r.Writes) > MaxUploadFiles {
		return errors.New("invalid upload receipt")
	}
	seen := map[string]bool{}
	for _, w := range r.Writes {
		file, err := plugin.RequireFileLeafPath(w.Target)
		if err != nil || w.Validate(w.Target) != nil || w.Target.EngineID != r.EngineID || seen[file] {
			return errors.New("invalid upload write evidence")
		}
		seen[file] = true
	}
	return nil
}

type ManagerUploadClient struct{ tenantHTTPClient }

func NewManagerUploadClient(base string, tokens ServiceTokenProvider, httpClient *http.Client) *ManagerUploadClient {
	return &ManagerUploadClient{newTenantHTTPClient(base, tokens, httpClient)}
}
func (c *ManagerUploadClient) ReadUploadReceipt(ctx context.Context, tenant uint, id string) (*UploadReceipt, error) {
	if c == nil || !nonzeroExecutionUUID(id) || tenant == 0 {
		return nil, errors.New("invalid upload receipt query")
	}
	var result UploadReceipt
	transport := c.tenantHTTPClient.withTenantID(tenant)
	if err := transport.doStrictJSON(ctx, http.MethodGet, "/api/v1/manager/runtime/uploads/"+id+"/receipt", &result, MaxUploadReceiptBytes); err != nil {
		return nil, err
	}
	if result.Validate() != nil || result.TenantID != int64(tenant) || result.UploadID != id {
		return nil, errors.New("upload receipt identity mismatch")
	}
	return &result, nil
}

// Meta returns its own first-identification observation, independently from the
// producer. NewItems contains bounded logical identity/boundary observations, never an Allow.
type MetaUploadIdentification struct {
	UploadID        string                       `json:"upload_id"`
	ScanExecutionID string                       `json:"scan_execution_id"`
	Status          string                       `json:"status"`
	NewItems        []authorizationfacts.Binding `json:"new_items"`
}

func (r MetaUploadIdentification) Validate() error {
	if !nonzeroExecutionUUID(r.UploadID) || !nonzeroExecutionUUID(r.ScanExecutionID) || r.UploadID == r.ScanExecutionID || len(r.NewItems) > MaxUploadFiles {
		return errors.New("invalid upload identification")
	}
	switch r.Status {
	case "pending", "running", "success", "failed", "timeout", "cancelled":
	default:
		return errors.New("invalid upload scan status")
	}
	seen := map[string]bool{}
	for _, item := range r.NewItems {
		fp := item.ItemFingerprint
		if item.Validate() != nil || seen[fp] {
			return errors.New("invalid new upload identity")
		}
		seen[fp] = true
	}
	if r.Status != "success" && len(r.NewItems) != 0 {
		return errors.New("unfinished upload scan declares new items")
	}
	return nil
}
func (c *MetaClient) EnsureUploadBasicScan(ctx context.Context, tenant uint, id string) (*MetaUploadIdentification, error) {
	if c == nil || tenant == 0 || !nonzeroExecutionUUID(id) {
		return nil, errors.New("invalid upload scan request")
	}
	var result MetaUploadIdentification
	transport := newTenantHTTPClient(c.baseURL, c.serviceTokenSource, c.httpClient).withTenantID(tenant)
	if err := transport.doStrictJSON(ctx, http.MethodPost, "/api/v1/meta/runtime/manager-uploads/"+id+"/basic-scan", &result, maxUploadIdentificationBytes); err != nil {
		return nil, err
	}
	if result.Validate() != nil || result.UploadID != id {
		return nil, errors.New("upload scan identity mismatch")
	}
	return &result, nil
}

func (c *MetaClient) ReadUploadIdentification(ctx context.Context, tenant uint, id string) (*MetaUploadIdentification, error) {
	if c == nil || tenant == 0 || !nonzeroExecutionUUID(id) {
		return nil, errors.New("invalid upload identification query")
	}
	var result MetaUploadIdentification
	transport := newTenantHTTPClient(c.baseURL, c.serviceTokenSource, c.httpClient).withTenantID(tenant)
	if err := transport.doStrictJSON(ctx, http.MethodGet, "/api/v1/meta/runtime/manager-uploads/"+id+"/basic-scan", &result, maxUploadIdentificationBytes); err != nil {
		return nil, err
	}
	if result.Validate() != nil || result.UploadID != id {
		return nil, errors.New("upload scan identity mismatch")
	}
	return &result, nil
}
