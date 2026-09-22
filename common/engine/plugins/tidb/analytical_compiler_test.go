package tidb

import (
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/sqlcompile/conformance"
)

func TestNativeAnalyticalCompiler(t *testing.T) {
	p := &Plugin{}
	c, err := plugin.ResolveAnalyticalCompiler(p.Type())
	if err != nil {
		t.Fatal(err)
	}
	if c.Identity() != p.AnalyticalCompiler().Identity() {
		t.Fatal("registry identity mismatch")
	}
	conformance.CompilerContract(t, c)
	if a := p.Capabilities().Compute.Query.Analytical; a != nil && a.Supported {
		t.Fatal("static template advertises uncertified instance")
	}
}
