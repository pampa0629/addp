package capture

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestConnectDelayUsesExactTaskIdentityAndRejectsUnmatchedResponses(t *testing.T) {
	mode := "valid"
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || r.URL.Path != "/jolokia/" || r.Header.Get("Authorization") != "" {
			t.Error("unexpected metrics request")
		}
		var request struct {
			Type      string   `json:"type"`
			MBean     string   `json:"mbean"`
			Attribute []string `json:"attribute"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			t.Fatal("invalid body")
		}
		if request.MBean != "debezium.postgres:context=streaming,server=addp-cdc-task-a-g1,type=connector-metrics" || len(request.Attribute) != 4 || request.Attribute[2] != "TotalNumberOfEventsSeen" || request.Attribute[3] != "MilliSecondsSinceLastEvent" || request.Attribute[0] != "Connected" || request.Attribute[1] != "MilliSecondsBehindSource" {
			t.Error("not a fixed exact-task query")
		}
		if mode == "foreign" {
			request.MBean = "debezium.postgres:context=streaming,server=other,type=connector-metrics"
		}
		status := 200
		var value any = map[string]any{"Connected": true, "MilliSecondsBehindSource": 125, "TotalNumberOfEventsSeen": 9007199254740993, "MilliSecondsSinceLastEvent": 250}
		if mode == "negative" {
			value = map[string]any{"Connected": true, "MilliSecondsBehindSource": -2}
		}
		if mode == "sentinel" {
			value = map[string]any{"Connected": true, "MilliSecondsBehindSource": -1}
		}
		if mode == "disconnected" {
			value = map[string]any{"Connected": false, "MilliSecondsBehindSource": 1000}
		}
		errorType := ""
		if mode == "absent" {
			status = 404
			value = nil
			errorType = "javax.management.InstanceNotFoundException"
		}
		if mode == "redirect" {
			w.Header().Set("Location", "http://other.invalid")
			w.WriteHeader(302)
			return
		}
		if mode == "failed" {
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(map[string]any{"status": status, "value": value, "request": map[string]any{"mbean": request.MBean}, "error_type": errorType})
	}))
	defer server.Close()
	client, err := NewConnectClient(server.URL, server.URL, "rest-user", "rest-secret", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range []struct {
		mode    string
		status  string
		seconds *float64
	}{{"valid", "available", delayFloat(.125)}, {"negative", "available", delayFloat(-.002)}, {"sentinel", "available", nil}, {"disconnected", "available", nil}, {"absent", "no_sample", nil}} {
		mode = entry.mode
		value, e := client.Delay(context.Background(), "addp-cdc-task-a-g1")
		if entry.mode == "valid" && (value.EventCount == nil || *value.EventCount != "9007199254740993" || value.LastEventAgeSeconds == nil || *value.LastEventAgeSeconds != .25) {
			t.Fatal("activity facts lost precision", value)
		}
		if e != nil || value.Status != entry.status {
			t.Fatalf("%s: %v %+v", mode, e, value)
		}
		if entry.seconds == nil && value.Seconds != nil || entry.seconds != nil && (value.Seconds == nil || *value.Seconds != *entry.seconds) {
			t.Fatalf("%s: stale/incorrect delay %+v", mode, value)
		}
	}
	for _, name := range []string{"foreign", "redirect", "failed"} {
		mode = name
		before := calls
		if _, e := client.Delay(context.Background(), "addp-cdc-task-a-g1"); e == nil || calls != before+1 {
			t.Fatalf("%s accepted/retried", mode)
		}
	}
	before := calls
	if _, e := client.Delay(context.Background(), "*"); e == nil || calls != before {
		t.Fatal("wildcard metric request allowed")
	}
}
func delayFloat(value float64) *float64 { return &value }
