package plugins_test

import (
	"slices"
	"testing"

	"github.com/addp/common/authorization"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/resourcetree"
)

func TestBusinessCatalogNodeScopes(t *testing.T) {
	for _, tc := range []struct {
		engine     string
		node, item resourcetree.ResourceType
		path       []string
	}{
		{"gaussdb", "schema", "table", []string{"a.b"}},
		{"ftp", "directory", "file", []string{"reports", "2026"}},
		{"rustfs", "bucket", "object", []string{"reports"}},
		{"rustfs", "prefix", "object", []string{"reports", "2026"}},
	} {
		t.Run(tc.engine+"/"+string(tc.node), func(t *testing.T) {
			p, err := plugin.Get(tc.engine)
			if err != nil {
				t.Fatal(err)
			}
			model := p.(plugin.EngineCatalogModelProvider).EngineCatalogModel()
			pathFor := func(id uint, kind resourcetree.ResourceType, parts []string) plugin.EngineCatalogPath {
				t.Helper()
				path, err := resourcetree.EngineCatalogPathFromLocator(model, &resourcetree.ResourceLocator{EngineID: id, Type: kind, Path: parts})
				if err != nil {
					t.Fatal(err)
				}
				return path
			}
			node := authorization.SourceNodeScope{Target: pathFor(31, tc.node, tc.path)}
			root := authorization.SourceNodeScope{Target: plugin.EngineCatalogRootPath(model, 31), Recursive: true}
			if !authorizationfacts.NodeIdentityMatchesEngine(tc.engine, node.Target) || !authorizationfacts.NodeIdentityMatchesEngine(tc.engine, root.Target) {
				t.Fatal("declared node rejected for engine")
			}
			if !root.CoversNode(node) {
				t.Fatal("root must cover branch even with no items")
			}
			for _, name := range []string{"current", "new"} {
				member := append(slices.Clone(tc.path), name)
				item := pathFor(31, tc.item, member)
				if !node.Covers(item) || !root.Covers(item) {
					t.Fatal("declared branch must cover a direct item")
				}
				if root.Covers(pathFor(32, tc.item, member)) {
					t.Fatal("cross-engine match")
				}
				member[len(tc.path)-1] += "0"
				if node.Covers(pathFor(31, tc.item, member)) {
					t.Fatal("sibling with shared name prefix matched")
				}
			}
		})
	}
}

func TestNodeGrantMatchesRegisteredStorageModel(t *testing.T) {
	if authorizationfacts.NodeIdentityMatchesEngine("nfs", plugin.ObjectRootPath(99)) {
		t.Fatal("an object-service root cannot be signed for an NFS engine with the same ID")
	}
	for _, path := range []plugin.EngineCatalogPath{plugin.ObjectRootPath(9), plugin.ObjectDirectoryPath(9, "bucket", ""), plugin.ObjectDirectoryPath(9, "bucket", "a/ exact.prefix ")} {
		if !authorizationfacts.NodeIdentityMatchesEngine("minio", path) || authorizationfacts.NodeIdentityMatchesEngine("nfs", path) {
			t.Fatal("wrong MinIO profile", path)
		}
	}
	for _, path := range []plugin.EngineCatalogPath{plugin.ObjectItemPath(9, "bucket", "a/file.ifc"), plugin.FileDirectoryPath(9, "bucket/a")} {
		if authorizationfacts.NodeIdentityMatchesEngine("minio", path) {
			t.Fatal("non-node or wrong profile", path)
		}
	}
}
