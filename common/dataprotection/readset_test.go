package dataprotection

import (
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
)

func TestDataItemTargetsFromQueryReadSetUsesStableFingerprints(t *testing.T) {
	model := plugin.DynamicSchemaCatalogModel()
	readSet, err := plugin.NewQueryReadSet(
		plugin.EngineCatalogBranchLeafPath(model, 11, plugin.EngineCatalogTermDatabase, "Outdoor", plugin.EngineCatalogTermCollection, plugin.EngineCatalogKindCollection, "Persons"),
		plugin.EngineCatalogBranchLeafPath(model, 11, plugin.EngineCatalogTermDatabase, "Outdoor", plugin.EngineCatalogTermCollection, plugin.EngineCatalogKindCollection, "Groups"),
	)
	if err != nil {
		t.Fatal(err)
	}
	targets, err := DataItemTargetsFromQueryReadSet(model, readSet)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 {
		t.Fatalf("targets = %#v", targets)
	}
	want := []string{
		models.GenerateItemFingerprint(11, "Outdoor.Groups"),
		models.GenerateItemFingerprint(11, "Outdoor.Persons"),
	}
	for index, target := range targets {
		if target.OwnerModule != "meta" || target.ResourceType != "data_item" || target.ResourceIdentity != want[index] {
			t.Fatalf("target[%d] = %#v", index, target)
		}
	}
}

func TestDataItemTargetFromCatalogPathUsesSameIdentityAsReadSet(t *testing.T) {
	model := plugin.DynamicSchemaCatalogModel()
	path := plugin.EngineCatalogPath{Version: plugin.EngineCatalogPathVersion, EngineID: 11, Segments: []plugin.EngineCatalogSegment{
		{Term: plugin.EngineCatalogTermServer, Kind: plugin.EngineCatalogTermServer, Name: "11"},
		{Term: plugin.EngineCatalogTermDatabase, Kind: plugin.EngineCatalogTermDatabase, Name: "Outdoor"},
		{Term: plugin.EngineCatalogTermCollection, Kind: plugin.EngineCatalogTermCollection, Name: "Persons"},
	}}
	target, err := DataItemTargetFromCatalogPath(model, path)
	if err != nil {
		t.Fatal(err)
	}
	if target.OwnerModule != "meta" || target.ResourceType != "data_item" || target.ResourceIdentity != models.GenerateItemFingerprint(11, "Outdoor.Persons") {
		t.Fatalf("target = %#v", target)
	}
}
