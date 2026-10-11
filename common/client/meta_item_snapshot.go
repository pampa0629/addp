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
	"github.com/addp/common/models"
)

// GetItemSnapshot reads only persisted Meta facts. NotFound means no metadata
// snapshot, never that the physical resource is absent. Upstream auth failures
// are dependency failures; callers must authorize their own current principal.
func (c *MetaClient) GetItemSnapshot(ctx context.Context, tenantID, engineID, itemID uint, catalogPath string) (*models.MetaItem, error) {
	if tenantID == 0 || engineID == 0 || (itemID == 0 && catalogPath == "") {
		return nil, commonapi.ErrBadRequest
	}
	path := fmt.Sprintf("/api/v1/meta/items/%d", itemID)
	if itemID == 0 {
		path = fmt.Sprintf("/api/v1/meta/items/by-catalog-path?engine_id=%d&catalog_path=%s", engineID, url.QueryEscape(catalogPath))
	}
	bound := c.WithTenantID(tenantID)
	if bound == nil {
		return nil, fmt.Errorf("Meta snapshot client unavailable")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, bound.baseURL+path, nil)
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
		return nil, fmt.Errorf("Meta snapshot dependency status %d", response.StatusCode)
	}
	var item models.MetaItem
	body, err := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	if err != nil || len(body) > 2<<20 || !utf8.Valid(body) {
		return nil, fmt.Errorf("Meta snapshot response too large or unreadable")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&item); err != nil {
		return nil, err
	}
	if decoder.Decode(new(interface{})) != io.EOF {
		return nil, fmt.Errorf("Meta snapshot response must contain one item")
	}
	if item.ID == 0 || (itemID != 0 && item.ID != itemID) || item.TenantID != tenantID || item.EngineID != engineID || item.FullName != catalogPath {
		return nil, fmt.Errorf("Meta snapshot identity mismatch")
	}
	return &item, nil
}
