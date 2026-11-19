// Package elasticsearch provides the HTTP connection for platform search owners.
package elasticsearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	endpoint string
	username string
	password string
	http     *http.Client
}

func New(endpoint, username, password string) (*Client, error) {
	endpoint = strings.TrimSpace(endpoint)
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("invalid platform Elasticsearch endpoint")
	}
	return &Client{
		endpoint: strings.TrimRight(endpoint, "/"), username: username, password: password,
		http: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}, nil
}

type ResponseError struct {
	Status int
	Type   string
}

func (e *ResponseError) Error() string {
	return fmt.Sprintf("platform Elasticsearch returned HTTP %d (%s)", e.Status, e.Type)
}

func (c *Client) Request(ctx context.Context, method, path string, input, output any) error {
	var body bytes.Buffer
	if input != nil {
		if err := json.NewEncoder(&body).Encode(input); err != nil {
			return err
		}
	}
	return c.request(ctx, method, path, "application/json", &body, output)
}

// RequestNDJSON sends a prepared bulk request without wrapping its lines in JSON.
func (c *Client) RequestNDJSON(ctx context.Context, method, path string, body io.Reader, output any) error {
	return c.request(ctx, method, path, "application/x-ndjson", body, output)
}

func (c *Client) request(ctx context.Context, method, path, contentType string, body io.Reader, output any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.endpoint+path, body)
	if err != nil {
		return err
	}
	if req.ContentLength != 0 {
		req.Header.Set("Content-Type", contentType)
	}
	if c.username != "" {
		req.SetBasicAuth(c.username, c.password)
	}
	response, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var failure struct {
			Error struct {
				Type string `json:"type"`
			} `json:"error"`
		}
		_ = json.NewDecoder(response.Body).Decode(&failure)
		return &ResponseError{Status: response.StatusCode, Type: failure.Error.Type}
	}
	if output != nil {
		return json.NewDecoder(response.Body).Decode(output)
	}
	_, err = io.Copy(io.Discard, response.Body)
	return err
}
