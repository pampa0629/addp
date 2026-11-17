package plugin

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestWorkflowExecutionLogsHTTPBoundary(t *testing.T) {
	valid := []interface{}{map[string]interface{}{"timestamp": "2026-11-17T08:00:00.123Z", "level": "INFO", "event": "task_progress", "task_id": "a", "progress": 0}}
	for _, test := range []struct {
		name    string
		value   interface{}
		invalid bool
	}{
		{"recorded", valid, false}, {"missing", nil, false}, {"empty", []interface{}{}, false},
		{"placeholder", []string{"private-placeholder"}, true},
		{"unknown_field", []interface{}{map[string]interface{}{"timestamp": "2026-11-17T08:00:00Z", "level": "INFO", "event": "workflow_started", "secret": "private"}}, true},
		{"invalid_time", []interface{}{map[string]interface{}{"timestamp": "bad", "level": "INFO", "event": "workflow_started"}}, true},
		{"unknown_event", []interface{}{map[string]interface{}{"timestamp": "2026-11-17T08:00:00Z", "level": "INFO", "event": "guessed"}}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "success", "execution_id": "run", "logs": test.value})
			}))
			defer server.Close()
			host, port, _ := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
			status, err := HTTPGetExecutionStatus(context.Background(), ConnectionInfo{"protocol": "http", "host": host, "port": port}, "run")
			if test.invalid {
				if err == nil {
					t.Fatal("invalid logs accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want, err := DecodeWorkflowExecutionLogs(test.value)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(status.Logs, want) {
				t.Fatalf("logs=%+v want=%+v", status.Logs, want)
			}
			if _, ok := status.Raw["logs"]; ok {
				t.Fatal("logs duplicated into raw result")
			}
		})
	}
}
