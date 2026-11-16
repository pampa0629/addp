package authorizationfacts

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
)

func TestNativeObjectBindingIsOneExactObject(t *testing.T) {
	target := plugin.ObjectItemPath(39, "addp-demo", "tables/订单.csv")
	candidate, err := NativeCandidateBinding(target)
	if err != nil {
		t.Fatal(err)
	}
	f := AuthorizationFacts{SchemaVersion: AuthorizationFactsSchemaV1, EngineID: 39, ItemType: "object", FullName: target.StringPath(), ItemFingerprint: models.GenerateItemFingerprint(39, target.StringPath()), BasisState: AuthorizationBasisComplete, ObservedAt: time.Now(), Boundary: &AuthorizationBoundary{Layout: "single", NativeTarget: target}, BoundaryHash: &candidate.BoundaryHash}
	b, err := f.ProvePrimaryTarget(target)
	if err != nil || b.Validate() != nil {
		t.Fatal(b, err)
	}
	raw, err := json.Marshal(b)
	var decoded Binding
	if err != nil || json.Unmarshal(raw, &decoded) != nil || !reflect.DeepEqual(b, decoded) {
		t.Fatal("native object wire binding", err, string(raw))
	}
	for _, other := range []plugin.EngineCatalogPath{plugin.ObjectItemPath(39, "addp-demo", "tables/客户.csv"), plugin.ObjectItemPath(39, "other", "tables/订单.csv"), plugin.ObjectItemPath(40, "addp-demo", "tables/订单.csv"), plugin.FileItemPath(39, target.StringPath()), plugin.ObjectDirectoryPath(39, "addp-demo", "")} {
		if _, err := f.ProvePrimaryTarget(other); err == nil {
			t.Fatal("precise Grant widened", other)
		}
	}
	for _, mutate := range []func(*plugin.EngineCatalogPath){func(p *plugin.EngineCatalogPath) { p.Segments[2].Name = ".." }, func(p *plugin.EngineCatalogPath) { p.Segments[2].Kind = "directory" }, func(p *plugin.EngineCatalogPath) { p.Segments[0].Term = "root" }, func(p *plugin.EngineCatalogPath) { p.Segments[3].Name = "next/订单.csv" }} {
		p := target
		p.Segments = append([]plugin.EngineCatalogSegment(nil), target.Segments...)
		mutate(&p)
		if _, err := NativeCandidateBinding(p); err == nil {
			t.Fatal("malformed object accepted", p)
		}
	}
	f.Boundary.Layout = "multi"
	if _, err := f.ApprovedBinding(); err == nil {
		t.Fatal("multi object became native single")
	}
}
