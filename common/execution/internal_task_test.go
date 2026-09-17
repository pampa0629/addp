package execution

import (
	"strings"
	"testing"
)

func TestInternalTaskScopeStrictBoundary(t *testing.T) {
	scope := InternalTaskScope{TaskType: "semantic_projection", ResourceID: "beijing_outdoor", Revision: "1", Digest: strings.Repeat("a", 64), Generation: "12345678-1234-1234-1234-123456789abc"}
	if err := scope.Validate("ontology"); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*InternalTaskScope){
		func(s *InternalTaskScope) { s.TaskType = "read" }, func(s *InternalTaskScope) { s.ResourceID = "../other" },
		func(s *InternalTaskScope) { s.Revision = "01" }, func(s *InternalTaskScope) { s.Revision = "9223372036854775808" },
		func(s *InternalTaskScope) { s.Digest = strings.Repeat("A", 64) }, func(s *InternalTaskScope) { s.Generation = "00000000-0000-0000-0000-000000000000" },
	} {
		copy := scope
		mutate(&copy)
		if copy.Validate("ontology") == nil {
			t.Fatalf("invalid scope accepted: %+v", copy)
		}
	}
	if scope.Validate("develop") == nil {
		t.Fatal("wrong audience accepted")
	}
}
