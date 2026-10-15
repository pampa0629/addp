package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type ManagerContentClient struct{ tenantHTTPClient }

func NewManagerContentClient(baseURL string, tokenSource ServiceTokenProvider, httpClient *http.Client) *ManagerContentClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Second}
	}
	return &ManagerContentClient{tenantHTTPClient: newTenantHTTPClient(baseURL, tokenSource, httpClient)}
}

func (c *ManagerContentClient) WithTenantID(tenantID uint) *ManagerContentClient {
	if c == nil {
		return nil
	}
	return &ManagerContentClient{tenantHTTPClient: c.tenantHTTPClient.withTenantID(tenantID)}
}

type ManagerContentField struct {
	Name            string `json:"name"`
	DataType        string `json:"data_type,omitempty"`
	ColumnType      string `json:"column_type,omitempty"`
	Comment         string `json:"comment,omitempty"`
	OrdinalPosition int    `json:"ordinal_position,omitempty"`
	IsNullable      bool   `json:"is_nullable,omitempty"`
	IsPrimaryKey    bool   `json:"is_primary_key,omitempty"`
	IsUniqueKey     bool   `json:"is_unique_key,omitempty"`
}

const (
	ManagerContentPayloadTechnicalMetadata = "technical_metadata"
	ManagerContentPayloadExtractedContent  = "extracted_content"
)

type ManagerContentDocument struct {
	DocumentID       string                 `json:"document_id"`
	PayloadKind      string                 `json:"payload_kind" enums:"technical_metadata,extracted_content"`
	ContentHash      string                 `json:"content_hash,omitempty"`
	Locator          string                 `json:"locator,omitempty"`
	EngineID         uint                   `json:"engine_id"`
	EngineName       string                 `json:"engine_name,omitempty"`
	EngineType       string                 `json:"engine_type,omitempty"`
	DataItemType     string                 `json:"data_item_type"`
	Name             string                 `json:"name"`
	FullName         string                 `json:"full_name,omitempty"`
	Description      string                 `json:"description,omitempty"`
	Tags             []string               `json:"tags,omitempty"`
	Schema           string                 `json:"schema,omitempty"`
	TableKind        string                 `json:"table_kind,omitempty"`
	Fields           []ManagerContentField  `json:"fields,omitempty"`
	RowCount         *int64                 `json:"row_count,omitempty"`
	Bucket           string                 `json:"bucket,omitempty"`
	Path             string                 `json:"path,omitempty"`
	SizeBytes        *int64                 `json:"size_bytes,omitempty"`
	ContentType      string                 `json:"content_type,omitempty"`
	DataUpdatedAt    *time.Time             `json:"data_updated_at,omitempty"`
	Content          string                 `json:"content,omitempty"`
	ContentPreview   string                 `json:"content_preview,omitempty"`
	ContentTruncated bool                   `json:"content_truncated,omitempty"`
	DocumentType     string                 `json:"document_type,omitempty"`
	Title            string                 `json:"title,omitempty"`
	Author           string                 `json:"author,omitempty"`
	Keywords         []string               `json:"keywords,omitempty"`
	WordCount        int                    `json:"word_count,omitempty"`
	PageCount        int                    `json:"page_count,omitempty"`
	CreatedDate      *time.Time             `json:"created_date,omitempty"`
	ModifiedDate     *time.Time             `json:"modified_date,omitempty"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
	ProjectionTime   time.Time              `json:"projection_time"`
}

func (d ManagerContentDocument) Validate() error {
	if strings.TrimSpace(d.DocumentID) == "" || d.EngineID == 0 || strings.TrimSpace(d.DataItemType) == "" || strings.TrimSpace(d.Name) == "" {
		return errors.New("Manager content document is incomplete")
	}
	switch d.PayloadKind {
	case ManagerContentPayloadTechnicalMetadata:
		if d.ContentHash != "" || d.Content != "" || d.ContentPreview != "" || d.ContentTruncated || d.Title != "" || d.Author != "" || len(d.Keywords) > 0 || d.WordCount != 0 || d.PageCount != 0 || d.CreatedDate != nil || d.ModifiedDate != nil || len(d.Metadata) > 0 || len(d.Tags) > 0 {
			return errors.New("Manager technical metadata document contains extracted content")
		}
	case ManagerContentPayloadExtractedContent:
		if d.EngineName != "" || d.EngineType != "" || d.Locator != "" || d.FullName != "" || d.Description != "" || d.Schema != "" || d.TableKind != "" || len(d.Fields) > 0 || d.RowCount != nil || d.Bucket != "" || d.Path != "" || d.SizeBytes != nil || d.ContentType != "" || d.DocumentType != "" || d.DataUpdatedAt != nil {
			return errors.New("Manager extracted content document contains technical metadata")
		}
	default:
		return errors.New("Manager content document payload kind is invalid")
	}
	return nil
}

const ManagerContentBatchLimit = 100

type ManagerContentBatch struct {
	Documents []ManagerContentDocument `json:"documents"`
}

func (b ManagerContentBatch) Validate() error {
	if len(b.Documents) == 0 || len(b.Documents) > ManagerContentBatchLimit {
		return errors.New("Manager content batch must contain 1 to 100 documents")
	}
	seen := make(map[string]bool, len(b.Documents))
	for _, document := range b.Documents {
		id := document.DocumentID
		if id != strings.TrimSpace(id) || len(id) > 128 || strings.Contains(id, "/") || seen[id] {
			return errors.New("Manager content batch contains an invalid or duplicate document ID")
		}
		if err := document.Validate(); err != nil {
			return err
		}
		seen[id] = true
	}
	return nil
}

func (c *ManagerContentClient) UpsertDocuments(ctx context.Context, documents []ManagerContentDocument) error {
	batch := ManagerContentBatch{Documents: documents}
	if err := batch.Validate(); err != nil {
		return err
	}
	path := "/api/v1/manager/runtime/content-documents/batch_update"
	if err := c.doJSON(ctx, http.MethodPost, path, batch, nil); err != nil {
		return fmt.Errorf("Manager content document upsert: %w", err)
	}
	return nil
}

type ManagerContentDeleteScope struct {
	DocumentIDs  []string
	EngineID     uint
	DataItemType string
	Schema       string
	Bucket       string
	PathPrefix   string
}

// Validate guards the one deletion contract before any transport or side effect.
func (s ManagerContentDeleteScope) Validate() error {
	if s.EngineID == 0 {
		return errors.New("Manager content deletion requires an engine ID")
	}
	if len(s.DocumentIDs) > ManagerContentBatchLimit {
		return errors.New("Manager content deletion exceeds batch limit")
	}
	if len(s.DocumentIDs) > 0 && (s.DataItemType != "" || s.Schema != "" || s.Bucket != "" || s.PathPrefix != "") {
		return errors.New("Manager exact deletion cannot mix range selectors")
	}
	seen := make(map[string]bool, len(s.DocumentIDs))
	for _, id := range s.DocumentIDs {
		if id == "" || id != strings.TrimSpace(id) || len(id) > 128 || strings.ContainsAny(id, "/\\") || seen[id] {
			return errors.New("Manager content deletion has an invalid or duplicate fingerprint")
		}
		for _, r := range id {
			if unicode.IsControl(r) {
				return errors.New("Manager content deletion has an invalid fingerprint")
			}
		}
		seen[id] = true
	}
	return nil
}

func (c *ManagerContentClient) DeleteDocuments(ctx context.Context, scope ManagerContentDeleteScope) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	query := url.Values{}
	query.Set("engine_id", strconv.FormatUint(uint64(scope.EngineID), 10))
	for _, id := range scope.DocumentIDs {
		query.Add("document_id", id)
	}
	if value := strings.TrimSpace(scope.DataItemType); value != "" {
		query.Set("data_item_type", value)
	}
	if value := strings.TrimSpace(scope.Schema); value != "" {
		query.Set("schema", value)
	}
	if value := strings.TrimSpace(scope.Bucket); value != "" {
		query.Set("bucket", value)
	}
	if value := strings.TrimSpace(scope.PathPrefix); value != "" {
		query.Set("path_prefix", value)
	}
	path := "/api/v1/manager/runtime/content-documents?" + query.Encode()
	if err := c.doJSON(ctx, http.MethodDelete, path, nil, nil); err != nil {
		return fmt.Errorf("Manager content document deletion: %w", err)
	}
	return nil
}
