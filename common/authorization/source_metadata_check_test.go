package authorization

import (
	"testing"

	"github.com/addp/common/engine/plugin"
)

func TestMetadataObservationCandidatesAreExplicitAndBounded(t *testing.T) {
	path := plugin.TabularItemPath(2, "schema", "outdoor", "persons")
	for _, test := range []struct {
		name  string
		paths []plugin.EngineCatalogPath
		valid bool
	}{
		{"one precise item", []plugin.EngineCatalogPath{path}, true},
		{"empty", nil, false},
		{"duplicate", []plugin.EngineCatalogPath{path, path}, false},
		{"invalid", []plugin.EngineCatalogPath{{}}, false},
		{"unbounded", make([]plugin.EngineCatalogPath, 201), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if (SourceMetadataCheckRequest{Targets: test.paths}).Valid() != test.valid {
				t.Fatal("incorrect candidate validation")
			}
		})
	}
}
