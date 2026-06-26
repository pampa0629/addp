package plugin

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const WorkflowRuntimeAPIAddpV1 = "addp.workflow/v1"

// HTTPWorkflowRuntimeProvider adapts any engine instance that implements the
// ADDP workflow HTTP runtime protocol. It is intentionally not registered by
// engine_type, because user-defined extension engines have their own stable
// engine_type but share the same runtime protocol.
type HTTPWorkflowRuntimeProvider struct {
	engineType  string
	displayName string
}

func NewHTTPWorkflowRuntimeProvider(engineType, displayName string) *HTTPWorkflowRuntimeProvider {
	engineType = strings.ToLower(strings.TrimSpace(engineType))
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		displayName = engineType
	}
	return &HTTPWorkflowRuntimeProvider{
		engineType:  engineType,
		displayName: displayName,
	}
}

func (p *HTTPWorkflowRuntimeProvider) Type() string {
	return p.engineType
}

func (p *HTTPWorkflowRuntimeProvider) DisplayName() string {
	return p.displayName
}

func (p *HTTPWorkflowRuntimeProvider) EngineOrigin() string {
	return "extension"
}

func (p *HTTPWorkflowRuntimeProvider) DefaultPort() int {
	return 0
}

func (p *HTTPWorkflowRuntimeProvider) RequiredFields() []string {
	return []string{"host", "port"}
}

func (p *HTTPWorkflowRuntimeProvider) SensitiveFields() []string {
	return []string{}
}

func (p *HTTPWorkflowRuntimeProvider) Capabilities() EngineCapabilities {
	return NewWorkflowCapabilities(p.Type(), WorkflowRuntimeAPIAddpV1)
}

func (p *HTTPWorkflowRuntimeProvider) ValidateConnectionInfo(connInfo ConnectionInfo) error {
	return ValidateRequiredFields(connInfo, p.RequiredFields())
}

func (p *HTTPWorkflowRuntimeProvider) TestConnection(ctx context.Context, connInfo ConnectionInfo) error {
	if err := p.ValidateConnectionInfo(connInfo); err != nil {
		return err
	}
	baseURL, err := RuntimeBaseURL(connInfo)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/health", nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("workflow runtime health check failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("workflow runtime health check failed with status %d", resp.StatusCode)
	}
	return nil
}

func (p *HTTPWorkflowRuntimeProvider) RuntimeEndpoint(ctx context.Context, connInfo ConnectionInfo) (string, error) {
	return RuntimeBaseURL(connInfo)
}

func (p *HTTPWorkflowRuntimeProvider) ListOperators(ctx context.Context, connInfo ConnectionInfo) ([]OperatorDescriptor, error) {
	return HTTPListOperators(ctx, connInfo)
}

func (p *HTTPWorkflowRuntimeProvider) ExecuteWorkflow(ctx context.Context, connInfo ConnectionInfo, req WorkflowExecuteRequest) (*WorkflowExecuteResult, error) {
	return HTTPExecuteWorkflow(ctx, connInfo, req)
}

func (p *HTTPWorkflowRuntimeProvider) InvokeOperator(ctx context.Context, connInfo ConnectionInfo, operatorName string, req OperatorInvokeRequest) (*OperatorInvokeResult, error) {
	return HTTPInvokeOperator(ctx, connInfo, operatorName, req)
}

func (p *HTTPWorkflowRuntimeProvider) GetExecutionStatus(ctx context.Context, connInfo ConnectionInfo, executionID string) (*WorkflowExecutionStatus, error) {
	return HTTPGetExecutionStatus(ctx, connInfo, executionID)
}
