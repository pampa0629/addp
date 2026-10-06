package execution

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
)

func TestManagerProfileScopeBindsEntireRawConfigWithoutFloatRoundTrip(t *testing.T) {
	set, _ := plugin.NewQueryReadSet(plugin.TabularItemPath(12, "schema", "public", "C"))
	left, err := NewManagerProfileReadScope(json.RawMessage(`{"engine_id":12,"condition":9007199254740993,"timeout":120000}`), set)
	if err != nil {
		t.Fatal(err)
	}
	reordered, _ := NewManagerProfileReadScope(json.RawMessage(`{ "timeout":120000, "condition":9007199254740993, "engine_id":12 }`), set)
	if reordered.ConfigDigest != left.ConfigDigest {
		t.Fatal("key order or whitespace changed the digest")
	}
	for _, raw := range []string{
		`{"engine_id":12,"condition":9007199254740992,"timeout":120000}`,
		`{"engine_id":12,"condition":9007199254740993,"timeout":120001}`,
		`{"engine_id":12,"condition":9007199254740993,"timeout":120000,"budget":{"rows":1}}`,
	} {
		right, err := NewManagerProfileReadScope(json.RawMessage(raw), set)
		if err != nil || right.ConfigDigest == left.ConfigDigest {
			t.Fatalf("full configuration change lost: %v", err)
		}
	}
	set.Paths[0].Segments[2].Name = "changed"
	if left.ReadSet.Paths[0].Segments[2].Name != "C" {
		t.Fatal("scope aliases provider sources")
	}
}

func TestManagerProfileScopeRejectsInvalidRawConfig(t *testing.T) {
	set, _ := plugin.NewQueryReadSet(plugin.TabularItemPath(12, "schema", "public", "C"))
	for _, raw := range []string{"", "null", "[]", "1", "{", "{} {}", `{"x":NaN}`, strings.Repeat(" ", 512<<10) + "{}"} {
		if scope, err := NewManagerProfileReadScope(json.RawMessage(raw), set); err == nil || scope != nil {
			t.Fatalf("invalid raw configuration accepted, length=%d", len(raw))
		}
	}
	if _, err := NewManagerProfileReadScope(json.RawMessage(`{}`), nil); err == nil {
		t.Fatal("missing actual read set accepted")
	}
}

func TestManagerProfileReadScopeSingleOwnerValidation(t *testing.T) {
	set, err := plugin.NewQueryReadSet(plugin.TabularItemPath(12, "schema", "public", "C"))
	if err != nil {
		t.Fatal(err)
	}
	scope := &ManagerProfileReadScope{ConfigDigest: strings.Repeat("a", 64), ReadSet: *set}
	if scope.Validate() != nil || (*ManagerProfileReadScope)(nil).Validate() == nil {
		t.Fatal("invalid scope validation")
	}
	clone := scope.Clone()
	clone.ReadSet.Paths[0].Segments[2].Name = "D"
	if scope.ReadSet.Paths[0].Segments[2].Name != "C" {
		t.Fatal("aliased scope")
	}
	for _, mutate := range []func(*ManagerProfileReadScope){
		func(s *ManagerProfileReadScope) { s.ConfigDigest = strings.Repeat("A", 64) },
		func(s *ManagerProfileReadScope) { s.ReadSet.Paths = nil },
		func(s *ManagerProfileReadScope) { s.ReadSet.Paths = append(s.ReadSet.Paths, s.ReadSet.Paths[0]) },
		func(s *ManagerProfileReadScope) {
			s.ReadSet.Paths = append(s.ReadSet.Paths, plugin.TabularItemPath(13, "schema", "public", "D"))
		},
		func(s *ManagerProfileReadScope) {
			s.ReadSet.Paths[0] = plugin.TabularNamespacePath(12, "schema", "public")
		},
	} {
		invalid := scope.Clone()
		mutate(invalid)
		if invalid.Validate() == nil {
			t.Fatal("invalid source accepted")
		}
	}
}
