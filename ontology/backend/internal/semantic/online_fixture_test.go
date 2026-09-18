package semantic

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

func TestBeijingOnlineFixtureCompilesAndKeepsUnknownFacts(t *testing.T) {
	data, err := os.ReadFile("testdata/beijing_outdoor_online.json")
	if err != nil {
		t.Fatal(err)
	}
	var definition Definition
	if err := json.Unmarshal(data, &definition); err != nil {
		t.Fatal(err)
	}
	definition.Scope = Scope{TenantID: 42, OntologyID: "beijing_outdoor_online", Revision: 1}
	snapshot, err := Freeze(definition)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		fact Fact
		want Outcome
	}{
		{Fact{Known, "北京市"}, Matched},
		{Fact{Known, "其他"}, NotMatched},
		{Fact{Unknown, nil}, Undetermined},
	} {
		result := snapshot.Evaluate(context.Background(), definition.Scope, "beijing_activity", map[string]Fact{"city": tc.fact})
		if result.Outcome != tc.want {
			t.Fatalf("outcome = %s, want %s", result.Outcome, tc.want)
		}
	}
}
