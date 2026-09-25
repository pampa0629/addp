package all_test

import (
	"testing"

	"github.com/addp/common/engine/plugin"
	_ "github.com/addp/common/engine/plugins/builtin/all"
	"github.com/addp/common/query/sqlcompile/conformance"
)

// Every registered analytical compiler must pass the same connection-free
// contract. A new built-in provider joins this gate through the plugin registry.
func TestRegisteredAnalyticalCompilers(t *testing.T) {
	count := 0
	for _, engineType := range plugin.List() {
		engine, err := plugin.Get(engineType)
		if err != nil {
			t.Fatal(err)
		}
		provider, ok := engine.(plugin.AnalyticalCompilerProvider)
		if !ok || provider.AnalyticalCompiler() == nil {
			continue
		}
		count++
		t.Run(engineType, func(t *testing.T) {
			compiler, err := plugin.ResolveAnalyticalCompiler(engineType)
			if err != nil {
				t.Fatal(err)
			}
			conformance.CompilerContract(t, compiler)
		})
	}
	if count == 0 {
		t.Fatal("no registered analytical compiler was checked")
	}
}
