package duckdb

import (
	"testing"

	"github.com/addp/common/engine/plugin"
)

func TestPluginDeclaresFederatedQueryRuntime(t *testing.T) {
	p := &Plugin{}
	if err := plugin.ValidatePluginCapabilities(p); err != nil {
		t.Fatalf("ValidatePluginCapabilities() error = %v", err)
	}
	caps := p.Capabilities()
	if caps.Compute == nil || caps.Compute.Query == nil || caps.Compute.Query.Federation == nil ||
		!caps.Compute.Query.Federation.Supported || caps.Compute.Query.Federation.RuntimeAPI != RuntimeAPI {
		t.Fatalf("unexpected capabilities: %#v", caps.Compute)
	}
}

func TestResolveObjectTableReferences(t *testing.T) {
	p := &Plugin{}
	got := p.ResolveObjectTableReferences(`
		SELECT *
		FROM lake.public.sales s
		JOIN object_store.customers c ON c.id = s.customer_id
		JOIN lake.public.sales duplicate ON duplicate.id = s.id
	`, []plugin.FederatedQuerySource{
		{ID: 1, Name: "lake", LifecycleState: "active"},
		{ID: 2, Name: "object_store", LifecycleState: "active"},
	})
	if len(got) != 2 {
		t.Fatalf("references = %#v", got)
	}
	if got[0].SourceName != "lake" || got[0].TableName != "public.sales" ||
		got[1].SourceName != "object_store" || got[1].TableName != "customers" {
		t.Fatalf("references = %#v", got)
	}
}
