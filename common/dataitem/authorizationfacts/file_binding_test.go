package authorizationfacts

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
)

func TestFileApprovedBindingRetainsWholeCompositionAndPrimaryProof(t *testing.T) {
	facts := fileCompositionFacts(t)
	primary := plugin.FileItemPath(33, "models/scene.3DS")
	binding, err := facts.ApprovedBinding()
	if err != nil || binding.Validate() != nil || binding.FileComposition == nil || len(binding.FileComposition.Refs) != 2 {
		t.Fatal("complete approved composition lost", err)
	}
	actual, err := binding.PrimaryTarget()
	if err != nil || !reflect.DeepEqual(actual, primary) {
		t.Fatal("primary changed", err)
	}
	if _, err := facts.ProvePrimaryTarget(primary); err != nil {
		t.Fatal(err)
	}
	if _, err := facts.ProvePrimaryTarget(plugin.FileItemPath(33, "models/TEST.PNG")); err == nil {
		t.Fatal("texture treated as independent approved data item")
	}
	if _, err := facts.ProveNativeTarget(primary); err == nil {
		t.Fatal("native proof dropped composition")
	}
	fingerprint, err := PrimaryCandidateFingerprint(primary)
	if err != nil || fingerprint != binding.ItemFingerprint {
		t.Fatal("primary candidate identity changed", err)
	}
	wire, err := json.Marshal(binding)
	if err != nil || strings.Contains(string(wire), "native_target") || strings.Contains(string(wire), "observed_at") {
		t.Fatal("ambiguous file binding wire", err)
	}
	var decoded Binding
	if json.Unmarshal(wire, &decoded) != nil || !reflect.DeepEqual(binding, decoded) {
		t.Fatal("whole approved snapshot did not round trip")
	}
	facts.Boundary.FileComposition.Refs[0].Role = "changed"
	if binding.Validate() != nil {
		t.Fatal("binding aliases mutable Meta facts")
	}
	for _, mutate := range []func(*Binding){
		func(b *Binding) { b.FileComposition.Refs[0].Role = "changed" },
		func(b *Binding) { b.BoundaryHash = strings.Repeat("f", 64) },
		func(b *Binding) { b.NativeTarget = primary },
	} {
		var bad Binding
		if json.Unmarshal(wire, &bad) != nil {
			t.Fatal("fixture")
		}
		mutate(&bad)
		if bad.Validate() == nil {
			t.Fatal("invalid approval snapshot accepted")
		}
	}
	for _, bad := range []string{
		strings.Replace(string(wire), `"item_fingerprint":`, `"allow":true,"item_fingerprint":`, 1),
		strings.Replace(string(wire), `"file_composition":`, `"native_target":null,"file_composition":`, 1),
		strings.Replace(string(wire), `"file_composition":`, `"file_composition":null,"file_composition":`, 1),
	} {
		if json.Unmarshal([]byte(bad), &decoded) == nil {
			t.Fatal("ambiguous binding accepted")
		}
	}
}

func TestFileBindingNearCompositionBudgetRoundTripsAndRejectsOversize(t *testing.T) {
	refs := modelFileRefs()[:1]
	for i := 0; ; i++ {
		refs = append(refs, FileCompositionRef{Target: plugin.FileItemPath(33, "models/"+fmt.Sprintf("texture_%03d_", i)+strings.Repeat("x", 1000)), Role: "texture", Required: true})
		composition, err := NewFileCompositionBoundary("3ds", refs)
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(composition)
		if len(encoded) < 63000 {
			continue
		}
		last := len(refs) - 1
		refs[last].Target.Segments[2].Name += strings.Repeat("p", 65450-len(encoded))
		composition, err = NewFileCompositionBoundary("3ds", refs)
		if err != nil {
			t.Fatal(err)
		}
		facts := fileCompositionFacts(t)
		facts.Boundary.FileComposition = composition
		hash, err := facts.ComputeBoundaryHash()
		if err != nil {
			t.Fatal(err)
		}
		facts.BoundaryHash = &hash
		factsWire, err := json.Marshal(facts)
		var decodedFacts AuthorizationFacts
		if err != nil || len(factsWire) <= 64<<10 || json.Unmarshal(factsWire, &decodedFacts) != nil {
			t.Fatal("valid composition identity envelope did not round trip", err)
		}
		binding, err := facts.ApprovedBinding()
		if err != nil {
			t.Fatal(err)
		}
		wire, err := json.Marshal(binding)
		if err != nil || len(wire) <= 64<<10 {
			t.Fatal("fixture must cross old envelope budget", err)
		}
		var decoded Binding
		if json.Unmarshal(wire, &decoded) != nil || !reflect.DeepEqual(binding, decoded) {
			t.Fatal("valid near-limit approval did not round trip")
		}
		composition.Refs[len(composition.Refs)-1].Target.Segments[2].Name += strings.Repeat("z", 200)
		if composition.Validate(33) == nil {
			t.Fatal("oversize composition accepted")
		}
		break
	}
}
