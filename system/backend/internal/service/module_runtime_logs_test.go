package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/runtimelog"
)

var logTestKey = []byte("runtime-log-test-key")
var logTestTime = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

func logQuery() RuntimeLogQuery {
	return RuntimeLogQuery{From: logTestTime.Add(-time.Hour), To: logTestTime.Add(time.Hour), Limit: 2, UserID: 1}
}
func testLog(seq uint64, stamp time.Time) runtimelog.Entry {
	return runtimelog.Entry{InstanceID: "instance", Module: "manager", Timestamp: stamp.Format(time.RFC3339Nano), ID: fmt.Sprintf("instance:%d", seq), Message: fmt.Sprint(seq), Level: "unknown"}
}

// Storage intentionally orders equal timestamps in reverse of the API's
// sequence order, so incomplete timestamp candidates cannot appear correct.
func fakeLogStorage(t *testing.T, entries *[]runtimelog.Entry, inspect func(*http.Request)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if inspect != nil {
			inspect(r)
		}
		from, _ := strconv.ParseInt(r.URL.Query().Get("start"), 10, 64)
		to, _ := strconv.ParseInt(r.URL.Query().Get("end"), 10, 64)
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		selected := []runtimelog.Entry{}
		for _, entry := range *entries {
			stamp, _ := time.Parse(time.RFC3339Nano, entry.Timestamp)
			if stamp.UnixNano() >= from && stamp.UnixNano() < to {
				selected = append(selected, entry)
			}
		}
		sort.SliceStable(selected, func(i, j int) bool {
			a, _ := time.Parse(time.RFC3339Nano, selected[i].Timestamp)
			b, _ := time.Parse(time.RFC3339Nano, selected[j].Timestamp)
			if a.Equal(b) {
				return selected[i].ID < selected[j].ID
			}
			return a.After(b)
		})
		if len(selected) > limit {
			selected = selected[:limit]
		}
		values := [][]any{}
		for _, entry := range selected {
			stamp, _ := time.Parse(time.RFC3339Nano, entry.Timestamp)
			body, _ := json.Marshal(entry)
			values = append(values, []any{strconv.FormatInt(stamp.UnixNano(), 10), string(body)})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"resultType": "streams", "result": []any{map[string]any{"values": values}}}})
	}))
}
func TestRuntimeLogsLiteralQueryIdentityAndPaging(t *testing.T) {
	q := logQuery()
	q.Keyword = `"} |~ ".*`
	q.Level = "unknown"
	entries := []runtimelog.Entry{testLog(9, logTestTime), testLog(10, logTestTime), testLog(10, logTestTime), testLog(11, logTestTime)}
	server := fakeLogStorage(t, &entries, func(r *http.Request) {
		query := r.URL.Query()
		if r.Header.Get("Authorization") != "Bearer read-token" || query.Get("direction") != "backward" || !strings.Contains(query.Get("query"), `|= "\"} |~ \".*"`) {
			t.Error("unsafe query or missing authentication")
		}
		if q.Cursor == "" && query.Get("start") == strconv.FormatInt(q.From.UnixNano(), 10) && query.Get("end") != strconv.FormatInt(q.To.UnixNano(), 10) {
			t.Error("exclusive upper bound changed")
		}
		if size, _ := strconv.Atoi(query.Get("limit")); size < 1 || size > runtimeLogStorageLimit {
			t.Error("unexpected storage budget")
		}
	})
	defer server.Close()
	service := NewRuntimeLogService(server.URL, "read-token", logTestKey)
	first, err := service.Query(context.Background(), "manager", "instance", q)
	if err != nil {
		t.Fatal(err)
	}
	if !first.HasMore || first.NextCursor == "" || first.Returned != 2 || first.Entries[0].ID != "instance:10" || first.Entries[1].ID != "instance:11" {
		t.Fatalf("bad first page: %+v", first)
	}
	q.Cursor = first.NextCursor
	last, err := service.Query(context.Background(), "manager", "instance", q)
	if err != nil {
		t.Fatal(err)
	}
	if last.HasMore || last.NextCursor != "" || last.Returned != 1 || last.Entries[0].ID != "instance:9" {
		t.Fatalf("boundary entry lost: %+v", last)
	}
}
func TestRuntimeLogFailuresNotEmptySuccess(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{{"denied", "", 401}, {"malformed", "not-json", 200}, {"wrong-instance", `{"status":"success","data":{"resultType":"streams","result":[{"values":[["1","{\"instance_id\":\"other\",\"module_name\":\"manager\",\"entry_id\":\"a\"}"]]}]}}`, 200}} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			result, err := NewRuntimeLogService(server.URL, "", logTestKey).Query(context.Background(), "manager", "instance", logQuery())
			if err == nil || result != nil {
				t.Fatal("failure became empty success")
			}
		})
	}
	if _, err := NewRuntimeLogService("", "", logTestKey).Query(context.Background(), "manager", "instance", logQuery()); !errors.Is(err, ErrRuntimeLogsDisabled) {
		t.Fatal(err)
	}
	for _, mutate := range []func(*RuntimeLogQuery){func(q *RuntimeLogQuery) { q.To = q.From.Add(8 * 24 * time.Hour) }, func(q *RuntimeLogQuery) { q.Level = "stderr" }, func(q *RuntimeLogQuery) { q.UserID = 0 }, func(q *RuntimeLogQuery) { q.To = time.Date(2500, 1, 1, 0, 0, 0, 0, time.UTC) }} {
		q := logQuery()
		mutate(&q)
		if q.Validate() == nil {
			t.Fatal("invalid query accepted")
		}
	}
}
func TestRuntimeLogsBoundariesPrecisionAndByteBudget(t *testing.T) {
	q := logQuery()
	q.Limit = 1000
	entries := []runtimelog.Entry{testLog(1, q.From.Add(-time.Nanosecond)), testLog(2, q.From), testLog(3, logTestTime), testLog(4, logTestTime.Add(time.Nanosecond)), testLog(9007199254740992, q.To.Add(-time.Nanosecond)), testLog(9007199254740993, q.To.Add(-time.Nanosecond)), testLog(^uint64(0), q.To.Add(-time.Nanosecond)), testLog(5, q.To)}
	server := fakeLogStorage(t, &entries, nil)
	defer server.Close()
	service := NewRuntimeLogService(server.URL, "", logTestKey)
	result, err := service.Query(context.Background(), "manager", "instance", q)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"instance:2", "instance:3", "instance:4", "instance:9007199254740992", "instance:9007199254740993", "instance:18446744073709551615"}
	for i, id := range want {
		if result.Entries[i].ID != id {
			t.Fatalf("boundary or uint64 ordering: %+v", result.Entries)
		}
	}
	entries = nil
	for i := 1; i <= 80; i++ {
		entry := testLog(uint64(i), logTestTime.Add(time.Duration(i)*time.Nanosecond))
		entry.Message = strings.Repeat("x", 64000)
		entries = append(entries, entry)
	}
	seen := map[string]bool{}
	for {
		result, err = service.Query(context.Background(), "manager", "instance", q)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := json.Marshal(result)
		if len(body) > 2<<20 || result.Returned == 0 {
			t.Fatal("body budget or progress violated")
		}
		for _, entry := range result.Entries {
			if seen[entry.ID] {
				t.Fatal("duplicate across batches")
			}
			seen[entry.ID] = true
		}
		if !result.HasMore {
			break
		}
		q.Cursor = result.NextCursor
	}
	if len(seen) != 80 {
		t.Fatalf("byte cut lost entries: %d", len(seen))
	}
}
func TestRuntimeLogsCompleteSaturatedTimestampBeforeCursor(t *testing.T) {
	entries := []runtimelog.Entry{}
	for i := 1; i <= 1000; i++ {
		entries = append(entries, testLog(uint64(i), logTestTime))
	}
	entries = append(entries, testLog(1001, logTestTime.Add(-time.Nanosecond)), testLog(1002, logTestTime.Add(-2*time.Nanosecond)))
	server := fakeLogStorage(t, &entries, nil)
	defer server.Close()
	service := NewRuntimeLogService(server.URL, "", logTestKey)
	q := logQuery()
	q.Limit = 500
	seen := map[string]bool{}
	for {
		result, err := service.Query(context.Background(), "manager", "instance", q)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range result.Entries {
			if seen[entry.ID] {
				t.Fatal("repeated boundary")
			}
			seen[entry.ID] = true
		}
		if !result.HasMore {
			break
		}
		q.Cursor = result.NextCursor
	}
	if len(seen) != 1002 {
		t.Fatalf("lost %d entries", 1002-len(seen))
	}
	entries = append(entries, testLog(1003, logTestTime))
	q.Cursor = ""
	if result, err := service.Query(context.Background(), "manager", "instance", q); result != nil || !errors.Is(err, ErrRuntimeLogsBoundary) {
		t.Fatalf("dense boundary silently skipped: %v", err)
	}
}
func TestRuntimeLogsCursorBindingsExpiryAndLateArrival(t *testing.T) {
	entries := []runtimelog.Entry{testLog(1, logTestTime), testLog(2, logTestTime), testLog(3, logTestTime)}
	calls := 0
	server := fakeLogStorage(t, &entries, func(*http.Request) { calls++ })
	defer server.Close()
	service := NewRuntimeLogService(server.URL, "", logTestKey)
	now := logTestTime.Add(time.Hour)
	service.now = func() time.Time { return now }
	q := logQuery()
	result, err := service.Query(context.Background(), "manager", "instance", q)
	if err != nil {
		t.Fatal(err)
	}
	q.Cursor = result.NextCursor
	for _, tc := range []struct {
		name       string
		change     func(*RuntimeLogQuery)
		module, id string
	}{{"tamper", func(q *RuntimeLogQuery) { q.Cursor = "x" + q.Cursor }, "manager", "instance"}, {"user", func(q *RuntimeLogQuery) { q.UserID = 2 }, "manager", "instance"}, {"filter", func(q *RuntimeLogQuery) { q.Keyword = "new" }, "manager", "instance"}, {"node", func(q *RuntimeLogQuery) { q.Node = "foreign" }, "manager", "instance"}, {"limit", func(q *RuntimeLogQuery) { q.Limit = 3 }, "manager", "instance"}, {"window", func(q *RuntimeLogQuery) { q.From = q.From.Add(time.Nanosecond) }, "manager", "instance"}, {"module", func(*RuntimeLogQuery) {}, "meta", "instance"}, {"instance", func(*RuntimeLogQuery) {}, "manager", "other"}} {
		t.Run(tc.name, func(t *testing.T) {
			changed := q
			tc.change(&changed)
			before := calls
			result, err := service.Query(context.Background(), tc.module, tc.id, changed)
			if result != nil || !errors.Is(err, ErrRuntimeLogsInvalid) || calls != before {
				t.Fatal("cursor mismatch reached storage")
			}
		})
	}
	entries = append(entries, testLog(4, logTestTime))
	last, err := service.Query(context.Background(), "manager", "instance", q)
	if err != nil {
		t.Fatal(err)
	}
	if last.Returned != 1 || last.Entries[0].ID != "instance:1" {
		t.Fatal("late arrival changed previous boundary")
	}
	latest := q
	latest.Cursor = ""
	first, err := service.Query(context.Background(), "manager", "instance", latest)
	if err != nil || first.Entries[1].ID != "instance:4" {
		t.Fatal("refresh did not show late arrival")
	}
	now = now.Add(runtimeLogCursorTTL)
	before := calls
	if result, err := service.Query(context.Background(), "manager", "instance", q); result != nil || !errors.Is(err, ErrRuntimeLogsInvalid) || calls != before {
		t.Fatal("expired cursor accepted")
	}
}
func TestRuntimeLogsRejectConflictingIdentity(t *testing.T) {
	entries := []runtimelog.Entry{testLog(1, logTestTime), testLog(1, logTestTime)}
	entries[1].Message = "conflict"
	server := fakeLogStorage(t, &entries, nil)
	defer server.Close()
	if result, err := NewRuntimeLogService(server.URL, "", logTestKey).Query(context.Background(), "manager", "instance", logQuery()); result != nil || !errors.Is(err, ErrRuntimeLogsUpstream) {
		t.Fatal("conflicting duplicate accepted")
	}
}
func TestRuntimeLogsConfigurationAndRedirectFailClosed(t *testing.T) {
	t.Setenv("LOKI_RETENTION_HOURS", "invalid")
	if _, err := NewRuntimeLogService("http://storage", "token", logTestKey).Query(context.Background(), "manager", "instance", logQuery()); !errors.Is(err, ErrRuntimeLogsUpstream) {
		t.Fatal(err)
	}
	t.Setenv("LOKI_RETENTION_HOURS", "168")
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("redirect forwarded credentials") }))
	defer destination.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, http.StatusFound) }))
	defer origin.Close()
	if _, err := NewRuntimeLogService(origin.URL, "token", logTestKey).Query(context.Background(), "manager", "instance", logQuery()); !errors.Is(err, ErrRuntimeLogsUpstream) {
		t.Fatal(err)
	}
}
