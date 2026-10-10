package models

import "testing"

func TestProcessMetricsDeclarationCanonicalEndpoints(t *testing.T) {
	if (*ProcessMetricsDeclaration)(nil).Validate() != nil {
		t.Fatal("absent declaration is valid")
	}
	for _, endpoint := range []string{"https://127.0.0.1:18100/metrics", "https://host.docker.internal:18100/metrics", "https://[::1]:18100/metrics"} {
		if (&ProcessMetricsDeclaration{SchemaVersion: ProcessMetricsSchema, Endpoint: endpoint}).Validate() != nil {
			t.Fatalf("valid: %s", endpoint)
		}
	}
	for _, endpoint := range []string{"http://127.0.0.1:18100/metrics", "https://127.0.0.1/metrics", "https://127.0.0.1:0/metrics", "https://127.0.0.1:018100/metrics", "https://localhost:18100/metrics?", "https://localhost:18100/metrics?token=secret", "https://localhost:18100/metrics#fragment", "https://localhost:18100/%6detrics", "https://user@localhost:18100/metrics", "https://0.0.0.0:18100/metrics", "https://[::]:18100/metrics", "https://LOCALHOST:18100/metrics", "https://localhost.:18100/metrics", "https://999.0.0.1:18100/metrics", "https://本机:18100/metrics"} {
		if (&ProcessMetricsDeclaration{SchemaVersion: ProcessMetricsSchema, Endpoint: endpoint}).Validate() == nil {
			t.Fatalf("invalid accepted: %s", endpoint)
		}
	}
	if (&ProcessMetricsDeclaration{SchemaVersion: "other", Endpoint: "https://localhost:18100/metrics"}).Validate() == nil {
		t.Fatal("unknown schema accepted")
	}
}
