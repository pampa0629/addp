package main

import (
	"github.com/addp/common/engine/plugin"
	"testing"
)

// Verify the actual server's registration, without test-only concrete imports.
func TestGraphServerRegistersBothPropertyGraphProviders(t *testing.T) {
	for _, kind := range []string{"neo4j", "falkordb"} {
		registered, err := plugin.Get(kind)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := registered.(plugin.PropertyGraphProvider); !ok {
			t.Fatalf("%s has no domain graph operations", kind)
		}
	}
}
