package metaitem

import (
	"context"
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/meta/internal/dataitem"
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

func TestResolveMetaItemsPassesOnlyUnclaimedFilesToNextDetector(t *testing.T) {
	first := &testScopeDetector{
		priority: 20,
		result: &DetectionResult{
			Items:  []*DetectedItem{{Organization: dataitem.OrganizationMulti, EntryPath: "/shp/roads.shp"}},
			Claims: ResourceClaimSet{"/shp/roads.shp": true},
		},
	}
	second := &testScopeDetector{priority: 10, result: &DetectionResult{Claims: ResourceClaimSet{}}}
	old := metaItemDetectors
	metaItemDetectors = []CompositeItemDetector{first, second}
	defer func() { metaItemDetectors = old }()

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
		t.Fatalf("second detector saw %#v, want only readme.pdf", second.seenFiles)
	}
}

type testScopeDetector struct {
	priority  int
	result    *DetectionResult
	seenFiles []plugin.FileEntry
}

func (d *testScopeDetector) Detect(ctx context.Context, files []plugin.FileEntry, subdirs []plugin.DirEntry) bool {
	return false
}

func (d *testScopeDetector) ExtractItemInfo(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	dirPath string,
	files []plugin.FileEntry,
) (*CompositeItemInfo, error) {
	return nil, nil
}

func (d *testScopeDetector) Priority() int { return d.priority }

func (d *testScopeDetector) ResolveItems(ctx context.Context, input DirectoryResolveInput) (*DetectionResult, error) {
	d.seenFiles = input.Files
	return d.result, nil
}
