package plugin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/google/uuid"
)

const WorkflowPrivateFilesAPIV2 = "addp.workflow.private-files/v2"

// Runtime file transport is not a content grant or a publication provider.
// The callback supplies the current tenant Develop service token per request;
// credentials never enter a connection, manifest, task or receipt.
type WorkflowRuntimeCredential func(context.Context) (string, error)

type WorkflowPrivateFile struct {
	NodeID string `json:"node_id"`
	Format string `json:"format,omitempty"`
	Size   int64  `json:"size_bytes,string"`
	SHA256 string `json:"sha256"`
}

type WorkflowPrivateFileManifest struct {
	SchemaVersion   string                `json:"schema_version"`
	TenantID        uint                  `json:"tenant_id,string"`
	ExecutionID     string                `json:"execution_id"`
	Attempt         int                   `json:"attempt"`
	AuthorizationID int64                 `json:"authorization_id,string"`
	ScopeDigest     string                `json:"scope_digest"`
	Deadline        time.Time             `json:"deadline"`
	WorkflowJSON    string                `json:"workflow_json"`
	Inputs          []WorkflowPrivateFile `json:"inputs"`
	Outputs         []string              `json:"outputs"`
}

type WorkflowPrivateFileStatus struct {
	RuntimeExecutionID string                `json:"runtime_execution_id"`
	ExecutionID        string                `json:"execution_id"`
	Attempt            int                   `json:"attempt"`
	ScopeDigest        string                `json:"scope_digest"`
	Inputs             []string              `json:"inputs"`
	Execution          json.RawMessage       `json:"execution"`
	Outputs            []WorkflowPrivateFile `json:"outputs"`
	CleanupPending     *bool                 `json:"cleanup_pending"`
}

type WorkflowPrivateFileCleanup struct {
	Cleanup   string `json:"cleanup"`
	ErrorCode string `json:"error_code,omitempty"`
}

// The optional HTTP facet uses the same Runtime and canonical execution API.
type WorkflowPrivateFileRuntimeProvider interface {
	WorkflowRuntimeProvider
	PrivateFilesExchange(ConnectionInfo, string, WorkflowPrivateFileManifest, WorkflowRuntimeCredential) (*WorkflowPrivateFilesClient, error)
}

var _ WorkflowPrivateFileRuntimeProvider = (*HTTPWorkflowRuntimeProvider)(nil)

func (p *HTTPWorkflowRuntimeProvider) PrivateFilesExchange(conn ConnectionInfo, runtimeID string, manifest WorkflowPrivateFileManifest, credential WorkflowRuntimeCredential) (*WorkflowPrivateFilesClient, error) {
	return NewWorkflowPrivateFilesClient(conn, runtimeID, manifest, credential)
}

type WorkflowPrivateFilesClient struct {
	connection ConnectionInfo
	baseURL    string
	runtimeID  string
	manifest   WorkflowPrivateFileManifest
	credential WorkflowRuntimeCredential
}

func workflowPrivateUUID(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id != uuid.Nil && id.String() == value
}

func workflowPrivateDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && strings.ToLower(value) == value
}

func NewWorkflowPrivateFilesClient(conn ConnectionInfo, runtimeID string, manifest WorkflowPrivateFileManifest, credential WorkflowRuntimeCredential) (*WorkflowPrivateFilesClient, error) {
	base, err := RuntimeBaseURL(conn)
	if err != nil || credential == nil || !workflowPrivateUUID(runtimeID) || manifest.SchemaVersion != WorkflowPrivateFilesAPIV2 ||
		manifest.TenantID == 0 || !workflowPrivateUUID(manifest.ExecutionID) || manifest.Attempt < 1 || manifest.AuthorizationID <= 0 ||
		!workflowPrivateDigest(manifest.ScopeDigest) || manifest.Deadline.IsZero() || !json.Valid([]byte(manifest.WorkflowJSON)) ||
		len(manifest.Inputs) == 0 || len(manifest.Outputs) > 1 {
		return nil, errors.New("invalid workflow private-file binding")
	}
	seen := map[string]bool{}
	for _, input := range manifest.Inputs {
		if (input.Format != "geopackage" && input.Format != "geojson") || strings.TrimSpace(input.NodeID) == "" || seen[input.NodeID] || input.Size < 0 || !workflowPrivateDigest(input.SHA256) {
			return nil, errors.New("invalid workflow private-file input")
		}
		seen[input.NodeID] = true
	}
	for _, output := range manifest.Outputs {
		if strings.TrimSpace(output) == "" || seen[output] {
			return nil, errors.New("invalid workflow private-file output")
		}
		seen[output] = true
	}
	manifest.Inputs = append([]WorkflowPrivateFile(nil), manifest.Inputs...)
	manifest.Outputs = append([]string{}, manifest.Outputs...)
	// Only endpoint identity is retained, never a business storage connection.
	endpoint := ConnectionInfo{"host": GetString(conn, "host"), "port": GetInt(conn, "port"), "protocol": GetString(conn, "protocol")}
	return &WorkflowPrivateFilesClient{endpoint, base, runtimeID, manifest, credential}, nil
}

type workflowPrivateCredentialKey struct{}

func authorizeWorkflowRequest(ctx context.Context, request *http.Request, required bool) error {
	credential, ok := ctx.Value(workflowPrivateCredentialKey{}).(WorkflowRuntimeCredential)
	if !ok {
		if required {
			return errors.New("workflow private-file service credential required")
		}
		return nil
	}
	token, err := credential(ctx)
	if err != nil || token == "" || strings.ContainsAny(token, " \t\r\n") {
		return errors.New("workflow service credential unavailable")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	// Do not permit net/http to replay a request body after an unknown outcome.
	request.GetBody = nil
	return nil
}

func (c *WorkflowPrivateFilesClient) bounded(ctx context.Context) (context.Context, context.CancelFunc, error) {
	if !time.Now().Before(c.manifest.Deadline) {
		return nil, nil, errors.New("workflow private-file execution expired")
	}
	ctx, cancel := context.WithDeadline(ctx, c.manifest.Deadline)
	return ctx, cancel, nil
}

func (c *WorkflowPrivateFilesClient) call(ctx context.Context, method, suffix string, body io.Reader, contentType string, expected int, result any) error {
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+"/api/private-files/"+c.runtimeID+suffix, body)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("Accept-Language", commoni18n.Language(ctx))
	ctx = context.WithValue(ctx, workflowPrivateCredentialKey{}, c.credential)
	if err := authorizeWorkflowRequest(ctx, request, true); err != nil {
		return err
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != expected {
		return fmt.Errorf("workflow private-file request returned HTTP %d", response.StatusCode)
	}
	decoder := json.NewDecoder(response.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(result); err != nil {
		return errors.New("invalid workflow private-file response")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return errors.New("invalid workflow private-file response")
	}
	return nil
}

func (c *WorkflowPrivateFilesClient) validateStatus(status *WorkflowPrivateFileStatus) error {
	if status.RuntimeExecutionID != c.runtimeID || status.ExecutionID != c.manifest.ExecutionID || status.Attempt != c.manifest.Attempt ||
		status.ScopeDigest != c.manifest.ScopeDigest || status.CleanupPending == nil || status.Inputs == nil || status.Outputs == nil ||
		len(status.Inputs) != len(c.manifest.Inputs) || len(status.Outputs) > len(c.manifest.Outputs) {
		return errors.New("workflow private-file response changed execution binding")
	}
	for _, state := range status.Inputs {
		if state != "missing" && state != "receiving" && state != "ready" && state != "failed" {
			return errors.New("invalid workflow private-file input state")
		}
	}
	for i, output := range status.Outputs {
		if output.NodeID != c.manifest.Outputs[i] || output.Size <= 0 || !workflowPrivateDigest(output.SHA256) {
			return errors.New("invalid workflow private-file output receipt")
		}
	}
	var execution struct {
		Status string `json:"status"`
	}
	if json.Unmarshal(status.Execution, &execution) != nil || (execution.Status != "pending" && execution.Status != "running" && execution.Status != "success" && execution.Status != "failed") {
		return errors.New("invalid workflow private-file execution state")
	}
	if execution.Status == "success" {
		if len(status.Outputs) != len(c.manifest.Outputs) {
			return errors.New("successful workflow has no complete private-file receipts")
		}
		for _, input := range status.Inputs {
			if input != "ready" {
				return errors.New("successful workflow has incomplete private-file inputs")
			}
		}
	}
	return nil
}

func (c *WorkflowPrivateFilesClient) Create(ctx context.Context) (*WorkflowPrivateFileStatus, error) {
	ctx, cancel, err := c.bounded(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	raw, err := json.Marshal(c.manifest)
	if err != nil {
		return nil, err
	}
	var result WorkflowPrivateFileStatus
	if err := c.call(ctx, http.MethodPut, "", bytes.NewReader(raw), "application/json", http.StatusCreated, &result); err != nil {
		return nil, err
	}
	return &result, c.validateStatus(&result)
}

func (c *WorkflowPrivateFilesClient) Upload(ctx context.Context, index int, input io.Reader) error {
	if index < 0 || index >= len(c.manifest.Inputs) || input == nil {
		return errors.New("invalid workflow private-file input ordinal")
	}
	ctx, cancel, err := c.bounded(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	var receipt struct {
		RuntimeExecutionID string `json:"runtime_execution_id"`
		InputIndex         int    `json:"input_index"`
		State              string `json:"state"`
	}
	// Keep unknown Content-Length: the Runtime consumes the complete framed
	// stream and verifies declared size, EOF and SHA, including surplus bytes.
	if err := c.call(ctx, http.MethodPut, "/inputs/"+strconv.Itoa(index), struct{ io.Reader }{input}, "application/octet-stream", http.StatusOK, &receipt); err != nil {
		return err
	}
	if receipt.RuntimeExecutionID != c.runtimeID || receipt.InputIndex != index || receipt.State != "ready" {
		return errors.New("invalid workflow private-file input receipt")
	}
	return nil
}

func (c *WorkflowPrivateFilesClient) Status(ctx context.Context) (*WorkflowPrivateFileStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var result WorkflowPrivateFileStatus
	if err := c.call(ctx, http.MethodGet, "", nil, "application/json", http.StatusOK, &result); err != nil {
		return nil, err
	}
	return &result, c.validateStatus(&result)
}

func (c *WorkflowPrivateFilesClient) Execute(ctx context.Context, language string) (*WorkflowExecuteResult, error) {
	ctx, cancel, err := c.bounded(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	ctx = context.WithValue(ctx, workflowPrivateCredentialKey{}, c.credential)
	result, err := HTTPExecuteWorkflow(ctx, c.connection, WorkflowExecuteRequest{DisplayLanguage: language,
		Runtime: &WorkflowRuntimeContext{TenantID: c.manifest.TenantID,
			ExecutionAuthorization: WorkflowExecutionAuthorization{ID: c.manifest.AuthorizationID, Effects: []string{"read"}},
			PrivateFiles:           &WorkflowPrivateFilesReference{ExecutionID: c.runtimeID}}})
	if err == nil && result.ExecutionID != c.runtimeID {
		err = errors.New("workflow Runtime changed private execution identity")
	}
	return result, err
}

func (c *WorkflowPrivateFilesClient) Execution(ctx context.Context) (*WorkflowExecutionStatus, error) {
	ctx = context.WithValue(ctx, workflowPrivateCredentialKey{}, c.credential)
	result, err := HTTPGetExecutionStatus(ctx, c.connection, c.runtimeID)
	if err == nil && result.ExecutionID != c.runtimeID {
		err = errors.New("workflow Runtime changed private execution identity")
	}
	return result, err
}

func (c *WorkflowPrivateFilesClient) CopyOutput(ctx context.Context, index int, receipt WorkflowPrivateFile, target io.Writer) error {
	if index < 0 || index >= len(c.manifest.Outputs) || target == nil || receipt.NodeID != c.manifest.Outputs[index] || receipt.Size <= 0 || !workflowPrivateDigest(receipt.SHA256) {
		return errors.New("invalid workflow private-file output binding")
	}
	ctx, cancel, err := c.bounded(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/private-files/"+c.runtimeID+"/outputs/"+strconv.Itoa(index), nil)
	if err != nil {
		return err
	}
	ctx = context.WithValue(ctx, workflowPrivateCredentialKey{}, c.credential)
	if err := authorizeWorkflowRequest(ctx, request, true); err != nil {
		return err
	}
	response, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "application/geopackage+sqlite3" {
		return errors.New("workflow private-file output unavailable")
	}
	digest := sha256.New()
	if _, err := io.CopyN(io.MultiWriter(target, digest), response.Body, receipt.Size); err != nil {
		return err
	}
	var extra [1]byte
	if n, err := response.Body.Read(extra[:]); n != 0 || err != io.EOF || hex.EncodeToString(digest.Sum(nil)) != receipt.SHA256 {
		return errors.New("workflow private-file output integrity mismatch")
	}
	return nil
}

func (c *WorkflowPrivateFilesClient) Cleanup(ctx context.Context) (*WorkflowPrivateFileCleanup, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+"/api/private-files/"+c.runtimeID, nil)
	if err != nil {
		return nil, err
	}
	ctx = context.WithValue(ctx, workflowPrivateCredentialKey{}, c.credential)
	if err := authorizeWorkflowRequest(ctx, request, true); err != nil {
		return nil, err
	}
	response, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusAccepted {
		return nil, fmt.Errorf("workflow private-file cleanup returned HTTP %d", response.StatusCode)
	}
	var result WorkflowPrivateFileCleanup
	decoder := json.NewDecoder(response.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil || decoder.Decode(new(any)) != io.EOF ||
		(response.StatusCode == http.StatusOK && (result.Cleanup != "passed" || result.ErrorCode != "")) ||
		(response.StatusCode == http.StatusAccepted && result.Cleanup != "pending") {
		return nil, errors.New("invalid workflow private-file cleanup observation")
	}
	return &result, nil
}
