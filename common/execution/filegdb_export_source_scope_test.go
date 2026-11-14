package execution

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
)

func TestFileGDBExportScopeBindsExactSourceAndEntireConfig(t *testing.T) {
	for namespace, engineType := range map[string]string{"schema": "postgresql", "database": "mysql"} {
		t.Run(engineType, func(t *testing.T) {
			target := plugin.TabularItemPath(4, namespace, "business", "roads")
			facts := authorizationfacts.AuthorizationFacts{
				SchemaVersion: authorizationfacts.AuthorizationFactsSchemaV1, EngineID: 4, ItemType: "table", FullName: "business.roads",
				ItemFingerprint: models.GenerateItemFingerprint(4, "business.roads"), BasisState: authorizationfacts.AuthorizationBasisComplete,
				ObservedAt: time.Now().UTC(), Boundary: &authorizationfacts.AuthorizationBoundary{Layout: "single", NativeTarget: target},
			}
			hash, err := facts.ComputeBoundaryHash()
			if err != nil {
				t.Fatal(err)
			}
			facts.BoundaryHash = &hash
			binding, err := facts.ProvePrimaryTarget(target)
			if err != nil {
				t.Fatal(err)
			}
			source := map[string]any{"locator": "addp://engine/4/path/business/roads?type=table", "data_type": "table", "representation": "native"}
			output := map[string]any{"parent_locator": "addp-infra://object/manager/tenant_7/export/manager/session?type=prefix", "name": "roads.gdb.zip",
				"data_type": "table", "representation": "encoded", "format": "filegdb"}
			config := map[string]any{
				"runtime": map[string]any{"boundary": "bounded"}, "load": map[string]any{"mode": "snapshot"},
				"source": source, "target": output, "filegdb_source": FileGDBExportSource{EngineVersion: "8", Binding: binding},
			}
			derive := func() (*FileGDBExportReadScope, error) {
				raw, err := json.Marshal(config)
				if err != nil {
					t.Fatal(err)
				}
				return NewFileGDBExportReadScope(raw)
			}
			scope, err := derive()
			if err != nil || scope.FileGDBSource.EngineType() != engineType {
				t.Fatalf("canonical source rejected: %#v, %v", scope, err)
			}
			output["name"] = "renamed.gdb.zip"
			modified, err := derive()
			if err != nil || modified.ConfigDigest == scope.ConfigDigest {
				t.Fatal("authorization digest did not bind archive identity", err)
			}
			source["locator"] = "addp://engine/4/path/business/sibling?type=table"
			if _, err := derive(); err == nil {
				t.Fatal("sibling table reused the frozen authorization source")
			}
			source["locator"] = "addp://engine/4/path/business/roads?type=table"
			source["query"] = map[string]any{"statement": "SELECT * FROM sibling"}
			if _, err := derive(); err == nil {
				t.Fatal("native table scope accepted custom SQL")
			}
			delete(source, "query")
			output["parent_locator"] = "addp://engine/4/path/exports?type=prefix"
			if _, err := derive(); err == nil {
				t.Fatal("source scope accepted a business storage target")
			}
		})
	}
}
