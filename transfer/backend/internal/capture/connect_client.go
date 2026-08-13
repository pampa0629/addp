package capture

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var ErrConnectorNotFound = errors.New("Kafka Connect connector not found")

type ConnectorStatus struct {
	Name           string
	ConnectorState string
	WorkerID       string
	TaskStates     []string
	Error          string
}

type ConnectorOffset struct {
	Partition map[string]json.RawMessage `json:"partition"`
	Offset    map[string]json.RawMessage `json:"offset"`
}

type ConnectorOffsets struct {
	Offsets []ConnectorOffset `json:"offsets"`
}

type ConnectControl interface {
	PutConfig(ctx context.Context, name string, config map[string]string) error
	Status(ctx context.Context, name string) (*ConnectorStatus, error)
	Offsets(ctx context.Context, name string) (*ConnectorOffsets, error)
	Pause(ctx context.Context, name string) error
	Resume(ctx context.Context, name string) error
	Delete(ctx context.Context, name string) error
}

func (c *ConnectClient) Offsets(ctx context.Context, name string) (*ConnectorOffsets, error) {
	var response ConnectorOffsets
	if err := c.do(ctx, http.MethodGet, "/connectors/"+url.PathEscape(name)+"/offsets", nil, &response, http.StatusOK); err != nil {
		return nil, err
	}
	return &response, nil
}

type ConnectClient struct {
	baseURL  string
	username string
	password string
	client   *http.Client
}

func NewConnectClient(baseURL, username, password string, timeout time.Duration) (*ConnectClient, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("Kafka Connect URL is invalid")
	}
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &ConnectClient{baseURL: baseURL, username: username, password: password, client: &http.Client{Timeout: timeout}}, nil
}

func (c *ConnectClient) PutConfig(ctx context.Context, name string, config map[string]string) error {
	return c.do(ctx, http.MethodPut, "/connectors/"+url.PathEscape(name)+"/config", config, nil, http.StatusOK, http.StatusCreated)
}

func (c *ConnectClient) Status(ctx context.Context, name string) (*ConnectorStatus, error) {
	var response struct {
		Name      string `json:"name"`
		Connector struct {
			State    string `json:"state"`
			WorkerID string `json:"worker_id"`
			Trace    string `json:"trace"`
		} `json:"connector"`
		Tasks []struct {
			State string `json:"state"`
			Trace string `json:"trace"`
		} `json:"tasks"`
	}
	err := c.do(ctx, http.MethodGet, "/connectors/"+url.PathEscape(name)+"/status", nil, &response, http.StatusOK)
	if err != nil {
		return nil, err
	}
	status := &ConnectorStatus{Name: response.Name, ConnectorState: strings.ToUpper(response.Connector.State), WorkerID: response.Connector.WorkerID}
	if response.Connector.Trace != "" {
		status.Error = response.Connector.Trace
	}
	for _, task := range response.Tasks {
		state := strings.ToUpper(task.State)
		status.TaskStates = append(status.TaskStates, state)
		if status.Error == "" && task.Trace != "" {
			status.Error = task.Trace
		}
	}
	return status, nil
}

func (c *ConnectClient) Pause(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodPut, "/connectors/"+url.PathEscape(name)+"/pause", nil, nil, http.StatusAccepted, http.StatusNoContent)
}

func (c *ConnectClient) Resume(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodPut, "/connectors/"+url.PathEscape(name)+"/resume", nil, nil, http.StatusAccepted, http.StatusNoContent)
}

func (c *ConnectClient) Delete(ctx context.Context, name string) error {
	err := c.do(ctx, http.MethodDelete, "/connectors/"+url.PathEscape(name), nil, nil, http.StatusNoContent, http.StatusOK)
	if errors.Is(err, ErrConnectorNotFound) {
		return nil
	}
	return err
}

func (c *ConnectClient) do(ctx context.Context, method, path string, body interface{}, target interface{}, expected ...int) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.username != "" {
		req.SetBasicAuth(c.username, c.password)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("Kafka Connect %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return ErrConnectorNotFound
	}
	ok := false
	for _, status := range expected {
		if resp.StatusCode == status {
			ok = true
			break
		}
	}
	if !ok {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return fmt.Errorf("Kafka Connect %s %s returned %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if target != nil {
		if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
			return fmt.Errorf("decode Kafka Connect response: %w", err)
		}
	}
	return nil
}
