package authorizationfacts

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
)

func TestMetadataBoundaryCannotAuthorizeContent(t *testing.T) {
	paths := []plugin.EngineCatalogPath{plugin.FileItemPath(9, "outputs/nodes.gpkg"), plugin.TabularItemPath(9, "database", "a.b", "c")}
	object, _ := plugin.ObjectItemPathFromRefPath(9, "bucket/a/nodes.gpkg")
	paths = append(paths, object, plugin.FileDirectoryPath(9, "outputs/scene"), plugin.ObjectDirectoryPath(9, "bucket", "scene"), plugin.ObjectDirectoryPath(9, "bucket", ""))
	for _, term := range []string{"view", "collection", "graph", "index", "keyspace", "topic"} {
		root := "server"
		if term == "topic" || term == "index" {
			root = "service"
		}
		pathTerm := term
		if term == "view" {
			pathTerm = "table"
		}
		paths = append(paths, plugin.EngineCatalogPath{Version: plugin.EngineCatalogPathVersion, EngineID: 9, Segments: []plugin.EngineCatalogSegment{{Term: root, Kind: root, Name: ""}, {Term: pathTerm, Kind: term, Name: "a.b / 空格"}}})
	}
	for _, p := range paths {
		t.Run(p.Segments[len(p.Segments)-1].Term, func(t *testing.T) {
			kind, name, err := MetadataTargetIdentity(p)
			if err != nil {
				t.Fatal(err)
			}
			layout := "single"
			if ValidMetadataScopeRoot(p) {
				layout = "whole"
			}
			f := AuthorizationFacts{SchemaVersion: AuthorizationFactsSchemaV1, EngineID: p.EngineID, ItemType: kind, FullName: name, ItemFingerprint: models.GenerateItemFingerprint(p.EngineID, name), BasisState: AuthorizationBasisComplete, ObservedAt: time.Now(), Boundary: &AuthorizationBoundary{Layout: layout, MetadataTarget: &MetadataBoundary{Target: p, Layout: layout, DataType: "container", Format: "geopackage", CompositionDigest: strings.Repeat("a", 64)}}}
			hash, err := f.ComputeBoundaryHash()
			if err != nil {
				t.Fatal(err)
			}
			f.BoundaryHash = &hash
			if f.Validate() != nil {
				t.Fatal("metadata identity rejected")
			}
			b, err := f.ProveMetadataTarget(p)
			if err != nil {
				t.Fatal(err)
			}
			for _, read := range []func() (Binding, error){f.ApprovedBinding, func() (Binding, error) { return f.ProvePrimaryTarget(p) }, func() (Binding, error) { return f.ProveNativeTarget(p) }} {
				if _, err := read(); err == nil {
					t.Fatal("metadata-only boundary became content proof")
				}
			}
			raw, err := json.Marshal(f)
			if err != nil {
				t.Fatal(err)
			}
			var wire AuthorizationFacts
			if err := json.Unmarshal(raw, &wire); err != nil {
				t.Fatal(err)
			}
			raw, err = json.Marshal(b)
			if err != nil {
				t.Fatal(err)
			}
			var copied Binding
			if err := json.Unmarshal(raw, &copied); err != nil || copied.Validate() != nil {
				t.Fatalf("binding wire: %v", err)
			}
			b.MetadataTarget.Target.EngineID++
			if b.Validate() == nil {
				t.Fatal("cross-engine identity accepted")
			}
			f.Boundary.MetadataTarget.Layout = "multi"
			if f.Validate() == nil {
				t.Fatal("changed layout reused old boundary")
			}
		})
	}
}

func TestMetadataBoundaryStrictUnion(t *testing.T) {
	m := MetadataBoundary{Target: plugin.FileItemPath(9, "a.gpkg"), Layout: "single", CompositionDigest: strings.Repeat("a", 64)}
	raw, _ := json.Marshal(m)
	for _, bad := range []string{string(raw) + "{}", strings.Replace(string(raw), `"name":""`, `"name":null`, 1), strings.Replace(string(raw), `"data_type":""`, `"data_type":null`, 1), strings.Replace(string(raw), `"format":""`, `"format":null`, 1), strings.Replace(string(raw), `"layout":"single"`, `"layout":"single","layout":"single"`, 1), strings.Replace(string(raw), `"format":""`, `"format":"","credential":"private"`, 1), strings.Replace(string(raw), `"composition_digest":"`+strings.Repeat("a", 64)+`"`, `"composition_digest":null`, 1)} {
		var got MetadataBoundary
		if json.Unmarshal([]byte(bad), &got) == nil {
			t.Fatal("invalid metadata wire accepted")
		}
	}
	b := AuthorizationBoundary{Layout: "single", MetadataTarget: &m, NativeTarget: plugin.FileItemPath(9, "b.gpkg")}
	if _, err := json.Marshal(b); err == nil {
		t.Fatal("ambiguous boundary accepted")
	}
}
