package inference_runtime

import (
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
