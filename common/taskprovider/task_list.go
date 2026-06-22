package taskprovider

import (
	"encoding/json"
	"fmt"
	"io"
)

type TaskListResponse struct {
	Items    []interface{}
	Total    int
	Page     int
	PageSize int
}

func ParseTaskListResponse(body io.Reader) (*TaskListResponse, error) {
	payload, err := io.ReadAll(io.LimitReader(body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("failed to read task list response: %w", err)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, fmt.Errorf("task list response must be a JSON object: %w", err)
	}
	return ParseTaskListObject(result)
}

func ParseTaskListObject(result map[string]interface{}) (*TaskListResponse, error) {
	if result == nil {
		return nil, fmt.Errorf("TaskProvider task list response must be an object with items")
	}
	for key := range result {
		switch key {
		case "items", "total", "page", "page_size":
		default:
			return nil, fmt.Errorf("TaskProvider task list response contains non-standard field %q", key)
		}
	}

	itemsRaw, hasItems := result["items"]
	if !hasItems {
		return nil, fmt.Errorf("TaskProvider task list response missing standard items field")
	}
	items, ok := itemsRaw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("TaskProvider task list response items must be an array")
	}
	total, err := requiredTaskListInteger(result, "total")
	if err != nil {
		return nil, err
	}
	page, err := requiredTaskListInteger(result, "page")
	if err != nil {
		return nil, err
	}
	pageSize, err := requiredTaskListInteger(result, "page_size")
	if err != nil {
		return nil, err
	}
	return &TaskListResponse{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

func requiredTaskListInteger(result map[string]interface{}, field string) (int, error) {
	value, ok := result[field]
	if !ok {
		return 0, fmt.Errorf("TaskProvider task list response missing standard %s field", field)
	}
	switch v := value.(type) {
	case float64:
		if v != float64(int(v)) {
			return 0, fmt.Errorf("TaskProvider task list response %s must be an integer", field)
		}
		return int(v), nil
	case int:
		return v, nil
	case int64:
		return int(v), nil
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			return 0, fmt.Errorf("TaskProvider task list response %s must be an integer", field)
		}
		return int(n), nil
	default:
		return 0, fmt.Errorf("TaskProvider task list response %s must be an integer", field)
	}
}
