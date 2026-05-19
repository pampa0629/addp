package metaitem

import (
	"context"
	"testing"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/engine/plugin"
)

func TestUnclaimedFileEntriesFiltersClaimedPaths(t *testing.T) {
	files := []plugin.FileEntry{
		{Name: "roads.shp", Path: "/shp/roads.shp"},
		{Name: "roads.shx", Path: "/shp/roads.shx"},
		{Name: "readme.pdf", Path: "/shp/readme.pdf"},
	}

	got := unclaimedFileEntries(files, ResourceClaimSet{
		"/shp/roads.shp": true,
		"/shp/roads.shx": true,
	})

	if len(got) != 1 || got[0].Path != "/shp/readme.pdf" {
		t.Fatalf("unclaimedFileEntries() = %#v, want only readme.pdf", got)
	}
}

func TestResolveMetaItemsPassesOnlyUnclaimedFilesToNextResolver(t *testing.T) {
	first := &testScopeResolver{
		priority: 20,
		result: &DetectionResult{
			Items: []*DetectedItem{detectedItemForTest(dataitem.ResolvedItem{
				Layout:    dataitem.LayoutMulti,
				EntryPath: "/shp/roads.shp",
			})},
			Claims: ResourceClaimSet{"/shp/roads.shp": true},
		},
	}
	second := &testScopeResolver{priority: 10, result: &DetectionResult{Claims: ResourceClaimSet{}}}
	old := itemResolvers
	itemResolvers = []ItemResolver{first, second}
	defer func() { itemResolvers = old }()

	_, err := ResolveItems(context.Background(), DirectoryResolveInput{
		Files: []plugin.FileEntry{
			{Name: "roads.shp", Path: "/shp/roads.shp"},
			{Name: "readme.pdf", Path: "/shp/readme.pdf"},
		},
	})
	if err != nil {
		t.Fatalf("ResolveItems() error = %v", err)
	}
	if len(second.seenFiles) != 1 || second.seenFiles[0].Path != "/shp/readme.pdf" {
		t.Fatalf("second resolver saw %#v, want only readme.pdf", second.seenFiles)
	}
}

type testScopeResolver struct {
	priority  int
	result    *DetectionResult
	seenFiles []plugin.FileEntry
}

func (d *testScopeResolver) Priority() int { return d.priority }

func (d *testScopeResolver) ResolveItems(ctx context.Context, input DirectoryResolveInput) (*DetectionResult, error) {
	d.seenFiles = input.Files
	return d.result, nil
}
