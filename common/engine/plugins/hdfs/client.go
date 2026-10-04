package hdfs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/engine/plugin"
)

const maxMetadataBytes = 4 << 20
const maxCatalogEntries = 10000

var httpClient = &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

type fileStatus struct {
	Name     string `json:"pathSuffix"`
	Type     string `json:"type"`
	Length   int64  `json:"length"`
	Modified int64  `json:"modificationTime"`
}

type client struct{ config *Config }

func newClient(info plugin.ConnectionInfo) (*client, error) {
	c, err := ParseConnectionInfo(info)
	if err != nil {
		return nil, err
	}
	return &client{c}, nil
}

func (c *client) operationURL(relative, op string, query url.Values) (string, error) {
	physical, err := c.config.PhysicalPath(relative)
	if err != nil {
		return "", err
	}
	u := *c.config.Endpoint
	u.Path, u.RawPath = "/webhdfs/v1"+physical, ""
	if query == nil {
		query = make(url.Values)
	}
	query.Set("op", op)
	query.Set("user.name", c.config.User)
	u.RawQuery = query.Encode()
	return u.String(), nil
}

func request(ctx context.Context, address string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, plugin.WrapEngineCatalogError(plugin.EngineCatalogErrorUnavailable, fmt.Errorf("HDFS request failed"))
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		kind := plugin.EngineCatalogErrorUnavailable
		if resp.StatusCode == http.StatusNotFound {
			kind = plugin.EngineCatalogErrorNotFound
		}
		return nil, plugin.WrapEngineCatalogError(kind, fmt.Errorf("HDFS request returned HTTP %d", resp.StatusCode))
	}
	return resp, nil
}

func (c *client) metadata(ctx context.Context, relative, op string, query url.Values, target interface{}) error {
	address, err := c.operationURL(relative, op, query)
	if err != nil {
		return err
	}
	resp, err := request(ctx, address)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxMetadataBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxMetadataBytes {
		return fmt.Errorf("HDFS metadata response exceeds budget")
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("invalid HDFS metadata response")
	}
	return nil
}

func (c *client) stat(ctx context.Context, relative string) (*fileStatus, error) {
	// Check each ancestor: WebHDFS may resolve symlinks server-side.
	parts := strings.Split(relative, "/")
	var result *fileStatus
	for i := 0; i <= len(parts); i++ {
		current := strings.Join(parts[:i], "/")
		if i == len(parts) && current == "" && result != nil {
			break
		}
		var payload struct {
			Status *fileStatus `json:"FileStatus"`
		}
		if err := c.metadata(ctx, current, "GETFILESTATUS", nil, &payload); err != nil {
			return nil, err
		}
		result = payload.Status
		if result == nil || result.Length < 0 || (result.Type != "FILE" && result.Type != "DIRECTORY") {
			return nil, plugin.WrapEngineCatalogError(plugin.EngineCatalogErrorUnsupported, fmt.Errorf("HDFS supports ordinary files and directories only"))
		}
		if i < len(parts) && result.Type != "DIRECTORY" {
			return nil, plugin.WrapEngineCatalogError(plugin.EngineCatalogErrorInvalidPath, fmt.Errorf("HDFS ancestor must be a directory"))
		}
	}
	return result, nil
}

func (c *client) list(ctx context.Context, relative string) ([]fileStatus, error) {
	s, err := c.stat(ctx, relative)
	if err != nil {
		return nil, err
	}
	if s.Type != "DIRECTORY" {
		return nil, plugin.WrapEngineCatalogError(plugin.EngineCatalogErrorInvalidPath, fmt.Errorf("HDFS listing requires a directory"))
	}
	var result []fileStatus
	cursor := ""
	scanned := 0
	for {
		var page struct {
			Listing *struct {
				Partial *struct {
					Statuses struct {
						Files []fileStatus `json:"FileStatus"`
					} `json:"FileStatuses"`
				} `json:"partialListing"`
				Remaining int `json:"remainingEntries"`
			} `json:"DirectoryListing"`
		}
		if err := c.metadata(ctx, relative, "LISTSTATUS_BATCH", url.Values{"startafter": {cursor}}, &page); err != nil {
			return nil, err
		}
		if page.Listing == nil || page.Listing.Partial == nil || page.Listing.Remaining < 0 {
			return nil, fmt.Errorf("invalid HDFS directory page")
		}
		files := page.Listing.Partial.Statuses.Files
		scanned += len(files)
		if scanned > maxCatalogEntries {
			return nil, fmt.Errorf("HDFS catalog exceeds entry budget")
		}
		for _, f := range files {
			if err := validateName(f.Name); err != nil {
				return nil, err
			}
			if f.Length < 0 {
				return nil, fmt.Errorf("invalid HDFS file length")
			}
			if f.Type == "FILE" || f.Type == "DIRECTORY" {
				result = append(result, f)
			}
		}
		if page.Listing.Remaining == 0 {
			return result, nil
		}
		if len(files) == 0 || files[len(files)-1].Name <= cursor {
			return nil, fmt.Errorf("HDFS directory cursor did not advance")
		}
		cursor = files[len(files)-1].Name
	}
}

type boundedReader struct {
	io.Reader
	body io.Closer
}

func (r *boundedReader) Close() error { return r.body.Close() }

func (c *client) open(ctx context.Context, relative string, offset, length int64) (io.ReadCloser, error) {
	if length == 0 {
		return io.NopCloser(strings.NewReader("")), nil
	}
	q := url.Values{"noredirect": {"true"}, "offset": {strconv.FormatInt(offset, 10)}, "length": {strconv.FormatInt(length, 10)}}
	var location struct {
		Location string `json:"Location"`
	}
	if err := c.metadata(ctx, relative, "OPEN", q, &location); err != nil {
		return nil, err
	}
	u, err := url.Parse(location.Location)
	expected, _ := c.operationURL(relative, "OPEN", nil)
	e, _ := url.Parse(expected)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") || (e.Scheme == "https" && u.Scheme != "https") || u.Path != e.Path || u.Query().Get("op") != "OPEN" || u.Query().Get("user.name") != c.config.User || u.Query().Get("offset") != q.Get("offset") || u.Query().Get("length") != q.Get("length") {
		return nil, fmt.Errorf("invalid HDFS DataNode read location")
	}
	resp, err := request(ctx, u.String())
	if err != nil {
		return nil, err
	}
	return &boundedReader{io.LimitReader(resp.Body, length), resp.Body}, nil
}
