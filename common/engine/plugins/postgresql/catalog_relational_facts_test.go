package postgresql

import (
	"github.com/addp/common/engine/plugin"
	"testing"
)

func TestProtocolRelationalFactsRequireOwnerDeclaration(t *testing.T) {
	for _, tc := range []struct {
		name     string
		identity *ProtocolIdentity
		want     bool
	}{
		{name: "native", want: true},
		{name: "unverified_protocol", identity: &ProtocolIdentity{EngineType: "fixture"}},
		{name: "certified_protocol", identity: &ProtocolIdentity{EngineType: "fixture", CatalogFacts: &plugin.EngineCatalogFactsCapability{Indexes: true, Constraints: true, Partitioning: true}}, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &PostgreSQLPlugin{identity: tc.identity}
			cb := p.tabularCatalogCallbacks()
			if (cb.ListIndexes != nil) != tc.want || (cb.ListConstraints != nil) != tc.want || (cb.DescribePartitioning != nil) != tc.want {
				t.Fatal("relational callback certification mismatch")
			}
			facts := p.Capabilities().Storage.Facts
			if facts.Indexes != tc.want || facts.Constraints != tc.want || facts.Partitioning != tc.want {
				t.Fatal("capability/callback mismatch")
			}
		})
	}
	facts := &plugin.EngineCatalogFactsCapability{Indexes: true}
	p := NewProtocolCompatiblePlugin(ProtocolIdentity{EngineType: "fixture", CatalogFacts: facts})
	facts.Indexes = false
	cb := p.tabularCatalogCallbacks()
	if cb.ListIndexes == nil || cb.ListConstraints != nil || cb.DescribePartitioning != nil {
		t.Fatal("owner declaration was mutated or expanded")
	}
}
