package plugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"time"
)

// WorkflowExecutionLog records an observed runtime event, never arbitrary process output.
type WorkflowExecutionLog struct {
	Timestamp string `json:"timestamp" format:"date-time" binding:"required"`
	Level     string `json:"level" enums:"INFO,ERROR" binding:"required"`
	Event     string `json:"event" enums:"workflow_started,task_progress,workflow_succeeded,workflow_failed" binding:"required"`
	TaskID    string `json:"task_id,omitempty"`
	Progress  *int   `json:"progress,omitempty"`
}

// DecodeWorkflowExecutionLogs validates the external or persisted JSON boundary.
// Missing logs represent an execution which did not record them.
func DecodeWorkflowExecutionLogs(value interface{}) ([]WorkflowExecutionLog, error) {
	logs := []WorkflowExecutionLog{}
	if value == nil {
		return logs, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, errors.New("invalid workflow execution logs")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&logs); err != nil || logs == nil {
		return nil, errors.New("invalid workflow execution logs")
	}
	for _, entry := range logs {
		timestamp, err := time.Parse(time.RFC3339Nano, entry.Timestamp)
		_, offset := timestamp.Zone()
		if err != nil || offset != 0 {
			return nil, errors.New("invalid workflow execution log timestamp")
		}
		switch entry.Event {
		case "workflow_started", "workflow_succeeded":
			if entry.Level != "INFO" || entry.TaskID != "" || entry.Progress != nil {
				return nil, errors.New("invalid workflow execution log")
			}
		case "workflow_failed":
			if entry.Level != "ERROR" || entry.TaskID != "" || entry.Progress != nil {
				return nil, errors.New("invalid workflow execution log")
			}
		case "task_progress":
			if entry.Level != "INFO" || entry.TaskID == "" || entry.Progress == nil || *entry.Progress < 0 || *entry.Progress > 100 {
				return nil, errors.New("invalid workflow execution log")
			}
		default:
			return nil, errors.New("invalid workflow execution log event")
		}
	}
	return logs, nil
}
