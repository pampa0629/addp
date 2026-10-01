package service

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/addp/common/runtimelog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func logQuery() RuntimeLogQuery {
	return RuntimeLogQuery{From: time.Now().Add(-time.Hour), To: time.Now(), Limit: 2}
}
func TestRuntimeLogsLiteralQueryIdentityAndLimit(t *testing.T) {
	q := logQuery()
	q.Keyword = `"} |~ ".*`
	q.Level = "unknown"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer read-token" {
			t.Error("missing service authentication")
		}
		query := r.URL.Query()
		if query.Get("end") != strconv.FormatInt(q.To.Add(-time.Nanosecond).UnixNano(), 10) {
			t.Error("exclusive upper time bound was not preserved")
		}
		if query.Get("limit") != "3" || query.Get("direction") != "backward" || !strings.Contains(query.Get("query"), `|= "\"} |~ \".*"`) {
			t.Errorf("unsafe or unbounded query %q", query.Get("query"))
		}
		var values [][]any
		for _, id := range []string{"a", "b", "b", "c"} {
			entry := runtimelog.Entry{InstanceID: "instance", Module: "manager", Timestamp: "2026-10-01T00:00:00Z", ID: id, Message: id, Level: "unknown"}
			body, _ := json.Marshal(entry)
			values = append(values, []any{"1", string(body)})
		}
		json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"resultType": "streams", "result": []any{map[string]any{"values": values}}}})
	}))
	defer server.Close()
	result, e := NewRuntimeLogService(server.URL, "read-token").Query(context.Background(), "manager", "instance", q)
	if e != nil {
		t.Fatal(e)
	}
	if !result.Limited || len(result.Entries) != 2 || result.Entries[0].ID == result.Entries[1].ID || result.CollectionState != "unknown" {
		t.Fatalf("bad bounded result %+v", result)
	}
}
func TestRuntimeLogFailuresNotEmptySuccess(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{{"denied", "", 401}, {"malformed", "not-json", 200}, {"wrong-instance", `{"status":"success","data":{"resultType":"streams","result":[{"values":[["1","{\"instance_id\":\"other\",\"module_name\":\"manager\",\"entry_id\":\"a\"}"]]}]}}`, 200}} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); w.Write([]byte(tc.body)) }))
			defer server.Close()
			result, e := NewRuntimeLogService(server.URL, "").Query(context.Background(), "manager", "instance", logQuery())
			if e == nil || result != nil {
				t.Fatal("failure became empty success")
			}
		})
	}
	if _, e := NewRuntimeLogService("", "").Query(context.Background(), "manager", "instance", logQuery()); !errors.Is(e, ErrRuntimeLogsDisabled) {
		t.Fatal(e)
	}
	q := logQuery()
	q.To = q.From.Add(8 * 24 * time.Hour)
	if q.Validate() == nil {
		t.Fatal("unbounded time range allowed")
	}
	q = logQuery()
	q.Level = "stderr"
	if q.Validate() == nil {
		t.Fatal("invalid level allowed")
	}
}

func TestRuntimeLogsUseParsedTimeAndBoundTotalResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var values [][]any
		for i, timestamp := range []string{"2026-10-01T00:00:00Z", "2026-10-01T00:00:00.1Z"} {
			entry := runtimelog.Entry{InstanceID: "instance", Module: "manager", Timestamp: timestamp, ID: string(rune('a' + i)), Message: timestamp}
			line, _ := json.Marshal(entry)
			values = append(values, []any{"1", string(line)})
		}
		json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"resultType": "streams", "result": []any{map[string]any{"values": values}}}})
	}))
	defer server.Close()
	result, err := NewRuntimeLogService(server.URL, "token").Query(context.Background(), "manager", "instance", logQuery())
	if err != nil {
		t.Fatal(err)
	}
	if result.Entries[0].ID != "a" || result.Entries[1].ID != "b" {
		t.Fatal("fractional seconds sorted lexicographically")
	}
}
func TestRuntimeLogsConfigurationAndRedirectFailClosed(t *testing.T) {
	t.Setenv("LOKI_RETENTION_HOURS", "invalid")
	if _, err := NewRuntimeLogService("http://storage", "token").Query(context.Background(), "manager", "instance", logQuery()); !errors.Is(err, ErrRuntimeLogsUpstream) {
		t.Fatal(err)
	}
	t.Setenv("LOKI_RETENTION_HOURS", "168")
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("redirect forwarded credentials") }))
	defer destination.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, http.StatusFound) }))
	defer origin.Close()
	if _, err := NewRuntimeLogService(origin.URL, "token").Query(context.Background(), "manager", "instance", logQuery()); !errors.Is(err, ErrRuntimeLogsUpstream) {
		t.Fatal(err)
	}
}
