package postgresql

import (
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/sqlcompile/conformance"
	"testing"
)

func TestNativeAnalyticalCompiler(t *testing.T) {
	p := &PostgreSQLPlugin{}
	c, err := plugin.ResolveAnalyticalCompiler(p.Type())
	if err != nil {
		t.Fatal(err)
	}
	if c.Identity() != p.AnalyticalCompiler().Identity() {
		t.Fatal("registry identity mismatch")
	}
	conformance.CompilerContract(t, c)
	conformance.CompilerSourceDiagnostics(t, c, p.EngineCatalogModel(), "integer")
	if a := p.Capabilities().Compute.Query.Analytical; a != nil && a.Supported {
		t.Fatal("static template advertises uncertified instance")
	}

	protocol := NewProtocolCompatiblePlugin(ProtocolIdentity{EngineType: "unregistered_protocol"})
	if protocol.AnalyticalCompiler() != nil {
		t.Fatal("protocol identity inherited certification")
	}

}
