package inference_runtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/addp/common/engine/plugin"
)

func TestInferenceRuntimeCapabilitiesMatchProvider(t *testing.T) {
	p := &Plugin{}
	if err := plugin.ValidatePluginCapabilities(p); err != nil {
		t.Fatal(err)
	}
	caps := p.Capabilities()
	if caps.Compute == nil || caps.Compute.Inference == nil || caps.Compute.Inference.RuntimeAPI != "addp.inference/v1" {
		t.Fatalf("unexpected inference capability: %#v", caps.Compute)
	}
}

func TestConnectionUsesReadyHealthEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/health/ready" {
			t.Fatalf("unexpected health request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}
	connInfo := plugin.ConnectionInfo{
		"protocol": parsed.Scheme,
		"host":     parsed.Hostname(),
		"port":     port,
	}
	if err := (&Plugin{}).TestConnection(context.Background(), connInfo); err != nil {
		t.Fatal(err)
	}
}
