package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

func HTTPListOperators(ctx context.Context, connInfo ConnectionInfo) ([]OperatorMetadata, error) {
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
		Operators []OperatorMetadata `json:"operators"`
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
	if req.InputData != nil {
		if engineID, ok := req.InputData["engine_id"]; ok {
			payload["engine_id"] = engineID
		}
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
	for key, value := range raw {
		if key != "status" && key != "execution_id" && key != "error" {
			result.Result[key] = value
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return result, fmt.Errorf("execute workflow failed with status %d: %s", resp.StatusCode, string(respBody))
	}
	return result, nil
}
