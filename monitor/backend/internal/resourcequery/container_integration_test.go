package resourcequery

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"github.com/addp/common/models"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestIntegrationMetricsCgroupSources(t *testing.T) {
	if os.Getenv("ADDP_METRICS_QUERY_INTEGRATION") != "1" || os.Getenv("ADDP_PROCESS_T2_RECORDS") == "" {
		t.Skip("requires standard cgroup-source T2 lifecycle")
	}
	read := func(path string) []byte {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return b
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
		ID         string                 `json:"id"`
		Module     string                 `json:"module_name"`
		InstanceID string                 `json:"instance_id"`
		Role       string                 `json:"role"`
		Started    time.Time              `json:"started_at"`
		Address    string                 `json:"instance"`
		Cgroup     *models.CgroupIdentity `json:"cgroup"`
	}
	if json.Unmarshal(read(os.Getenv("ADDP_PROCESS_T2_RECORDS")), &records) != nil || len(records) != 3 {
		t.Fatal("invalid source records")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 110*time.Second)
	defer cancel()
	deadline := time.Now().Add(100 * time.Second)
	for {
		ready := true
		for _, r := range records {
			id, err := strconv.ParseUint(r.ID, 10, 64)
			if err != nil || r.Cgroup == nil || r.Cgroup.Validate() != nil {
				t.Fatal("source missing exact cgroup declaration")
			}
			scope := ProcessScope{ID: uint(id), ModuleName: r.Module, InstanceID: r.InstanceID, Role: r.Role, Instance: r.Address, StartedAt: r.Started, Cgroup: r.Cgroup}
			now := time.Now().UTC().Truncate(time.Second)
			result, err := c.ContainerObservation(ctx, scope, now, DefaultBudget())
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range result.Series[:2] {
				ready = ready && row.Points[0].DataState == "valid" && row.Points[0].Value != nil
			}
			for _, row := range result.Series[2:] {
				if row.Points[0].Value != nil {
					t.Fatal("shared source inferred a container quota")
				}
			}
			if ready {
				different := *r.Cgroup
				different.CgroupID = "1"
				scope.Cgroup = &different
				denied, err := c.ContainerObservation(ctx, scope, now, DefaultBudget())
				if err != nil {
					t.Fatal(err)
				}
				for _, row := range denied.Series {
					if row.Points[0].Value != nil {
						t.Fatal("foreign cgroup accepted")
					}
				}
			}
		}
		if ready {
			t.Log("real SDK/mTLS/Prometheus cgroup CPU complete minute and memory, foreign identity rejection, no shared-container quota inference")
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("cgroup CPU complete window did not become available")
		}
		time.Sleep(time.Second)
	}
}
