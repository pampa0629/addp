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
		Stopped    bool      `json:"stopped"`
	}
	if err := json.Unmarshal(read(os.Getenv("ADDP_PROCESS_T2_RECORDS")), &records); err != nil || len(records) != 3 {
		t.Fatal("native process records", err)
	}
	scopes := []ProcessScope{}
	stoppedIDs := map[uint]bool{}
	for index, record := range records {
		id, err := strconv.ParseUint(record.ID, 10, 63)
		if err != nil || id != uint64(index+1) {
			t.Fatal("unexpected native process identity", err)
		}
		stoppedIDs[uint(id)] = record.Stopped
		scopes = append(scopes, ProcessScope{ID: uint(id), ModuleName: record.Module, InstanceID: record.InstanceID, Role: record.Role, StartedAt: record.Started, Instance: record.Address})
	}
	stopped := false
	for _, value := range stoppedIDs {
		stopped = stopped || value
	}
	if scopes[0].Role != "backend" || scopes[1].Role != "worker" || scopes[2].Role != "worker" || scopes[1].ModuleName != scopes[2].ModuleName || scopes[1].InstanceID == scopes[2].InstanceID || scopes[1].Instance == scopes[2].Instance || scopes[1].StartedAt.Equal(scopes[2].StartedAt) {
		t.Fatal("requires one Backend and two independently started same-module Workers")
	}
	deadline := time.Now().Add(100 * time.Second)
	if stopped {
		// Leave time to report the last observation inside the runner's
		// existing 30-second deadline instead of losing diagnostics to kill.
		deadline = time.Now().Add(20 * time.Second)
	}
	for {
		values, err := c.ProcessSummaries(context.Background(), scopes, time.Now().UTC().Truncate(time.Second), DefaultBudget())
		ready := err == nil && len(values) == len(scopes)
		if ready {
			for _, scope := range scopes {
				value := values[scope.ID]
				if stoppedIDs[scope.ID] {
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
			{
				at := time.Now().UTC().Truncate(time.Second)
				for _, scope := range scopes {
					if stoppedIDs[scope.ID] {
						continue
					}
					p, err := NewProcessTrendPlan(at.Add(-3*time.Minute), at, at, DefaultBudget())
					if err != nil {
						t.Fatal(err)
					}
					p = LimitTrendStart(p, scope.StartedAt)
					series, err := c.ProcessTrend(context.Background(), p, scope, DefaultBudget())
					if err != nil || len(series) != 2 {
						t.Fatal("native process trend", series, err)
					}
					for _, line := range series {
						valid := false
						for _, point := range line.Points {
							valid = valid || point.DataState == "valid" && point.Value != nil && *point.Value > 0
						}
						if !valid {
							// The last range grid point can precede the instant
							// evaluation by almost one step. Wait for both windows
							// inside the existing deadline; never relax CPU guards.
							ready = false
						}
					}
					foreign := scope
					foreign.InstanceID = "previous-start"
					old, err := c.ProcessTrend(context.Background(), p, foreign, DefaultBudget())
					if err != nil {
						t.Fatal(err)
					}
					for _, line := range old {
						for _, point := range line.Points {
							if point.Value != nil {
								t.Fatal("another instance trend leaked")
							}
						}
					}
				}
			}
			if ready {
				if !stopped {
					// Both real Workers are collecting. Mixing one identity component
					// must not read the other Worker at either endpoint or in history.
					at := time.Now().UTC().Truncate(time.Second)
					for _, pair := range [][2]ProcessScope{{scopes[1], scopes[2]}, {scopes[2], scopes[1]}} {
						own, peer := pair[0], pair[1]
						wrongInstance, wrongEndpoint, wrongStart := own, own, own
						wrongInstance.InstanceID = peer.InstanceID
						wrongEndpoint.Instance = peer.Instance
						wrongStart.StartedAt = peer.StartedAt
						for _, foreign := range []ProcessScope{wrongInstance, wrongEndpoint, wrongStart} {
							values, err := c.ProcessSummaries(context.Background(), []ProcessScope{foreign}, at, DefaultBudget())
							if err != nil || len(values) != 1 || len(values[foreign.ID].Series) != 3 || values[foreign.ID].Collection.State == "collecting" {
								t.Fatal("mixed Worker identity was collecting", err)
							}
							for _, line := range values[foreign.ID].Series {
								if len(line.Points) == 0 {
									t.Fatal("missing mixed Worker response points")
								}
								for _, point := range line.Points {
									if point.Value != nil {
										t.Fatal("mixed Worker summary leaked")
									}
								}
							}
							p, err := NewProcessTrendPlan(at.Add(-3*time.Minute), at, at, DefaultBudget())
							if err != nil {
								t.Fatal(err)
							}
							p = LimitTrendStart(p, foreign.StartedAt)
							lines, err := c.ProcessTrend(context.Background(), p, foreign, DefaultBudget())
							if err != nil || len(lines) != 2 {
								t.Fatal("mixed Worker trend response", err)
							}
							for _, line := range lines {
								if len(line.Points) == 0 {
									t.Fatal("missing mixed Worker response points")
								}
								for _, point := range line.Points {
									if point.Value != nil {
										t.Fatal("mixed Worker trend leaked")
									}
								}
							}
						}
					}
				}
				t.Log("genuine Linux Backend/two-Worker CPU/RSS/uptime, trends, mixed-identity refusal and per-process stopped-source exclusion passed")
				return
			}
		}
		if time.Now().After(deadline) {
			diagnostic, _ := json.Marshal(values)
			t.Fatalf("native process observation not ready: %s %v", diagnostic, err)
		}
		time.Sleep(time.Second)
	}
}
