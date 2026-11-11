package dataitem_test

import (
	"strings"
	"testing"

	"github.com/addp/common/dataitem"
)

func TestTIFFContentVersionsRequireTheExactCommittedSet(t *testing.T) {
	versions := []dataitem.ContentVersion{
		{Path: "images/a.tif", SizeBytes: 12, SHA256: strings.Repeat("a", 64)},
		{Path: "images/a.tif.aux.xml", SizeBytes: 0, SHA256: strings.Repeat("b", 64)},
	}
	attrs := map[string]interface{}{
		"item": map[string]interface{}{"format": "tiff", "layout": "multi", "refs": []interface{}{
			map[string]interface{}{"path": "images/a.tif", "primary": true},
			map[string]interface{}{"path": "images/a.tif.aux.xml", "role": "auxiliary_metadata"},
		}},
		"storage": map[string]interface{}{"physical_path": "images/a.tif", "content_versions": versions},
	}
	if _, err := dataitem.TIFFContentVersionsFromAttributes(attrs); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range [][]dataitem.ContentVersion{nil, versions[:1], {versions[1], versions[0]},
		{versions[0], {Path: "images/other.xml", SHA256: strings.Repeat("b", 64)}},
		{versions[0], {Path: versions[1].Path, SizeBytes: -1, SHA256: versions[1].SHA256}},
		{versions[0], {Path: versions[1].Path, SHA256: strings.Repeat("B", 64)}},
	} {
		attrs["storage"].(map[string]interface{})["content_versions"] = invalid
		if _, err := dataitem.TIFFContentVersionsFromAttributes(attrs); err == nil {
			t.Fatalf("accepted incomplete or changed version set: %#v", invalid)
		}
	}
}
