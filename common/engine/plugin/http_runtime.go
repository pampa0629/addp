package plugin

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

func RuntimeBaseURL(connInfo ConnectionInfo) (string, error) {
	host := NormalizeHost(GetString(connInfo, "host"))
	port := GetInt(connInfo, "port")
	protocol := GetString(connInfo, "protocol")
	if protocol == "" {
		protocol = "http"
	}
	if host == "" || port == 0 {
		return "", fmt.Errorf("missing required fields: host, port")
	}
	return fmt.Sprintf("%s://%s:%d", protocol, host, port), nil
}

func HTTPListOperators(ctx context.Context, connInfo ConnectionInfo) ([]OperatorDescriptor, error) {
	baseURL, err := RuntimeBaseURL(connInfo)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/operators", nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list operators failed with status %d: %s", resp.StatusCode, string(body))
	}

	var payload struct {
		Operators []OperatorDescriptor `json:"operators"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	return payload.Operators, nil
}

func HTTPExecuteWorkflow(ctx context.Context, connInfo ConnectionInfo, req WorkflowExecuteRequest) (*WorkflowExecuteResult, error) {
	baseURL, err := RuntimeBaseURL(connInfo)
	if err != nil {
		return nil, err
	}
	payload := map[string]interface{}{
		"workflow_def": req.WorkflowDef,
		"input_data":   req.InputData,
	}
	for key, value := range req.Runtime {
		payload[key] = value
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/workflow", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, err
	}
	result := &WorkflowExecuteResult{
		Result: map[string]interface{}{},
	}
	if status, _ := raw["status"].(string); status != "" {
		result.Status = status
	}
	if executionID, _ := raw["execution_id"].(string); executionID != "" {
		result.ExecutionID = executionID
	}
	if errText, _ := raw["error"].(string); errText != "" {
		result.Error = errText
	}
	if errorCode, _ := raw["error_code"].(string); errorCode != "" {
		result.ErrorCode = errorCode
	}
	if details, _ := raw["details"].(string); details != "" {
		result.Details = details
	}
	if executionTimeMs, ok := floatFromRaw(raw["execution_time_ms"]); ok {
		result.ExecutionTimeMs = &executionTimeMs
	}
	for key, value := range raw {
		if key != "status" && key != "execution_id" && key != "error" && key != "error_code" && key != "details" && key != "execution_time_ms" {
			result.Result[key] = value
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return result, fmt.Errorf("execute workflow failed with status %d: %s", resp.StatusCode, string(respBody))
	}
	if err := runtimeFailureFromStandardFields("execute workflow", result.Status, result.Error, result.ErrorCode, result.Details); err != nil {
		return result, err
	}
	return result, nil
}

func HTTPInvokeOperator(ctx context.Context, connInfo ConnectionInfo, operatorName string, req OperatorInvokeRequest) (*OperatorInvokeResult, error) {
	baseURL, err := RuntimeBaseURL(connInfo)
	if err != nil {
		return nil, err
	}
	payload := map[string]interface{}{
		"params": req.Params,
	}
	for key, value := range req.Runtime {
		payload[key] = value
	}
	if req.BinaryPayload != nil {
		payload["binary_payload"] = req.BinaryPayload
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/operators/"+operatorName+"/invoke", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, err
	}
	result := &OperatorInvokeResult{
		Result: map[string]interface{}{},
	}
	if status, _ := raw["status"].(string); status != "" {
		result.Status = status
	}
	if executionID, _ := raw["execution_id"].(string); executionID != "" {
		result.ExecutionID = executionID
	}
	if errText, _ := raw["error"].(string); errText != "" {
		result.Error = errText
	}
	if errorCode, _ := raw["error_code"].(string); errorCode != "" {
		result.ErrorCode = errorCode
	}
	if details, _ := raw["details"].(string); details != "" {
		result.Details = details
	}
	if executionTimeMs, ok := floatFromRaw(raw["execution_time_ms"]); ok {
		result.ExecutionTimeMs = &executionTimeMs
	}
	if binaryPayload, ok := binaryPayloadFromRaw(raw["binary_payload"]); ok {
		result.BinaryPayload = binaryPayload
	}
	for key, value := range raw {
		if key != "status" && key != "execution_id" && key != "error" && key != "error_code" && key != "details" && key != "execution_time_ms" && key != "binary_payload" {
			result.Result[key] = value
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return result, fmt.Errorf("invoke operator failed with status %d: %s", resp.StatusCode, string(respBody))
	}
	if err := runtimeFailureFromStandardFields("invoke operator", result.Status, result.Error, result.ErrorCode, result.Details); err != nil {
		return result, err
	}
	return result, nil
}

func binaryPayloadFromRaw(value interface{}) (*BinaryPayload, bool) {
	if value == nil {
		return nil, false
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, false
	}
	var payload BinaryPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, false
	}
	if len(payload.Data) == 0 && payload.ContentType == "" && payload.Encoding == "" && payload.Name == "" && len(payload.Metadata) == 0 {
		return nil, false
	}
	return &payload, true
}

func runtimeFailureFromStandardFields(operation string, status string, errorText string, errorCode string, details string) error {
	if status != "failed" && status != "error" {
		return nil
	}
	parts := []string{operation + " failed"}
	if errorCode != "" {
		parts = append(parts, "code="+errorCode)
	}
	if errorText != "" {
		parts = append(parts, errorText)
	}
	if details != "" {
		parts = append(parts, details)
	}
	return fmt.Errorf("%s", strings.Join(parts, ": "))
}

func HTTPGetExecutionStatus(ctx context.Context, connInfo ConnectionInfo, executionID string) (*WorkflowExecutionStatus, error) {
	baseURL, err := RuntimeBaseURL(connInfo)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/executions/"+url.PathEscape(executionID), nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, err
	}
	status := workflowExecutionStatusFromRaw(raw)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return status, fmt.Errorf("get workflow execution status failed with status %d: %s", resp.StatusCode, string(respBody))
	}
	return status, nil
}

func workflowExecutionStatusFromRaw(raw map[string]interface{}) *WorkflowExecutionStatus {
	status := &WorkflowExecutionStatus{
		Raw: map[string]interface{}{},
	}
	if value, _ := raw["status"].(string); value != "" {
		status.Status = value
	}
	if value, _ := raw["execution_id"].(string); value != "" {
		status.ExecutionID = value
	}
	if value, ok := raw["result"]; ok {
		status.Result = value
	}
	if value, ok := raw["all_results"].(map[string]interface{}); ok {
		status.AllResults = value
	}
	if value, _ := raw["message"].(string); value != "" {
		status.Message = value
	}
	if value, ok := stringSliceFromRaw(raw["task_order"]); ok {
		status.TaskOrder = value
	}
	if value, _ := raw["error"].(string); value != "" {
		status.Error = value
	}
	if value, _ := raw["error_code"].(string); value != "" {
		status.ErrorCode = value
	}
	if value, _ := raw["details"].(string); value != "" {
		status.Details = value
	}
	if value, ok := intFromRaw(raw["progress"]); ok {
		status.Progress = value
	}
	if value, _ := raw["started_at"].(string); value != "" {
		status.StartedAt = value
	}
	if value, ok := floatFromRaw(raw["execution_time_ms"]); ok {
		status.ExecutionTimeMs = &value
	}
	for key, value := range raw {
		if key != "status" && key != "execution_id" && key != "result" && key != "all_results" && key != "message" && key != "task_order" && key != "error" && key != "error_code" && key != "details" && key != "progress" && key != "started_at" && key != "execution_time_ms" {
			status.Raw[key] = value
		}
	}
	return status
}

func floatFromRaw(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		parsed, err := v.Float64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

func intFromRaw(value interface{}) (int, bool) {
	switch v := value.(type) {
	case float64:
		return int(v), v == float64(int(v))
	case int:
		return v, true
	case int64:
		return int(v), true
	case json.Number:
		parsed, err := v.Int64()
		return int(parsed), err == nil
	default:
		return 0, false
	}
}

func stringSliceFromRaw(value interface{}) ([]string, bool) {
	switch v := value.(type) {
	case []string:
		return v, true
	case []interface{}:
		result := make([]string, 0, len(v))
		for _, item := range v {
			text, ok := item.(string)
			if !ok {
				return nil, false
			}
			result = append(result, text)
		}
		return result, true
	default:
		return nil, false
	}
}
