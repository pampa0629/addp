package resourcequery

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestIntegrationMetricsProcessSources(t *testing.T) {
	if os.Getenv("ADDP_METRICS_QUERY_INTEGRATION") != "1" || os.Getenv("ADDP_PROCESS_T2_RECORDS") == "" {
		t.Skip("requires standard metrics T2 native sources")
	}
	read := func(path string) []byte {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	cert, err := tls.X509KeyPair(read(os.Getenv("MONITOR_PROMETHEUS_CLIENT_CERT_FILE")), read(os.Getenv("MONITOR_PROMETHEUS_CLIENT_KEY_FILE")))
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewClient(os.Getenv("MONITOR_PROMETHEUS_URL"), read(os.Getenv("MONITOR_PROMETHEUS_CA_FILE")), cert)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var records []struct {
		ID         string    `json:"id"`
		Module     string    `json:"module_name"`
		InstanceID string    `json:"instance_id"`
		Role       string    `json:"role"`
		Started    time.Time `json:"started_at"`
		Address    string    `json:"instance"`
	}
	if err := json.Unmarshal(read(os.Getenv("ADDP_PROCESS_T2_RECORDS")), &records); err != nil || len(records) != 2 {
		t.Fatal("native process records", err)
	}
	scopes := []ProcessScope{}
	for _, record := range records {
		id, err := strconv.ParseUint(record.ID, 10, 63)
		if err != nil {
			t.Fatal(err)
		}
		scopes = append(scopes, ProcessScope{ID: uint(id), ModuleName: record.Module, InstanceID: record.InstanceID, Role: record.Role, StartedAt: record.Started, Instance: record.Address})
	}
	stopped := os.Getenv("ADDP_PROCESS_T2_STOPPED") == "1"
	deadline := time.Now().Add(100 * time.Second)
	for {
		values, err := c.ProcessSummaries(context.Background(), scopes, time.Now().UTC().Truncate(time.Second), DefaultBudget())
		ready := err == nil && len(values) == 2
		if ready {
			for _, scope := range scopes {
				value := values[scope.ID]
				if stopped && scope.ID == 1 {
					ready = ready && value.Collection.State == "failed"
					for _, series := range value.Series {
						ready = ready && series.Points[0].Value == nil && series.Points[0].DataState == "no_data"
					}
					continue
				}
				ready = ready && value.Collection.State == "collecting" && len(value.Series) == 3
				for _, series := range value.Series {
					point := series.Points[0]
					ready = ready && point.DataState == "valid" && point.Value != nil
					if point.Value != nil {
						ready = ready && *point.Value > 0
					}
				}
			}
		}
		if ready {
			t.Log("genuine Linux CPU/RSS/uptime and stopped-source exclusion passed")
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("native process observation not ready: %+v %v", values, err)
		}
		time.Sleep(time.Second)
	}
}
