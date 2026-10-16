package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"unicode/utf8"

	commonapi "github.com/addp/common/api"
	"github.com/google/uuid"
)

// MetaMosaicScanResult is the closed, source-bound machine result contract.
// It is neither a general execution response nor an authorization grant.
type MetaMosaicScanResult struct {
	ExecutionID       string            `json:"execution_id"`
	ParentExecutionID string            `json:"parent_execution_id"`
	Status            string            `json:"status"`
	Output            *MetaMosaicOutput `json:"output,omitempty"`
}

// MetaMosaicOutput identifies the whole item, never its manifest file.
type MetaMosaicOutput struct {
	Locator     string `json:"locator"`
	ItemID      uint   `json:"item_id"`
	Fingerprint string `json:"fingerprint"`
}

func (c *MetaClient) GetMosaicScanResult(ctx context.Context, tenantID uint, executionID, parentExecutionID string) (*MetaMosaicScanResult, error) {
	if tenantID == 0 || !nonzeroExecutionUUID(executionID) || !nonzeroExecutionUUID(parentExecutionID) {
		return nil, commonapi.ErrBadRequest
	}
	bound := c.WithTenantID(tenantID)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, bound.baseURL+"/api/v1/meta/runtime/mosaic-scans/"+url.PathEscape(executionID)+"/result?parent_execution_id="+url.QueryEscape(parentExecutionID), nil)
	if err != nil {
		return nil, err
	}
	response, err := bound.do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return nil, commonapi.ErrNotFound
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Meta mosaic result dependency status %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (64<<10)+1))
	if err != nil || len(body) > 64<<10 || !utf8.Valid(body) {
		return nil, fmt.Errorf("Meta mosaic result response too large or unreadable")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var result MetaMosaicScanResult
	if err := decoder.Decode(&result); err != nil {
		return nil, err
	}
	if decoder.Decode(new(interface{})) != io.EOF || result.ExecutionID != executionID || result.ParentExecutionID != parentExecutionID {
		return nil, fmt.Errorf("Meta mosaic result identity mismatch")
	}
	return &result, nil
}

func nonzeroExecutionUUID(raw string) bool {
	id, err := uuid.Parse(raw)
	return err == nil && id != uuid.Nil && id.String() == raw
}
