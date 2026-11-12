package execution

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
)

func developIntentFixture(t *testing.T, write bool) *DevelopFileIntent {
	t.Helper()
	var target *DevelopFileTargetIntent
	if write {
		target = &DevelopFileTargetIntent{NodeID: "save", Target: plugin.FileItemPath(12, "outputs/result.gpkg"), Mode: "fail"}
	}
	intent, err := NewDevelopFileIntent(json.RawMessage(`{"large":9007199254740993,"options":{"mode":"test"}}`), 1, 45,
		[]DevelopFileSourceIntent{{NodeID: "load_b", Target: plugin.FileItemPath(11, "sources/B.gpkg")}, {NodeID: "load_a", Target: plugin.FileItemPath(11, "sources/ A.gpkg ")}}, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	return intent
}

func developReferences() []DevelopFileSourceReference {
	return []DevelopFileSourceReference{
		{Target: plugin.FileItemPath(11, "sources/B.gpkg"), ItemFingerprint: strings.Repeat("b", 64), Format: "geopackage", BoundaryHash: strings.Repeat("c", 64), EngineVersion: "2"},
		{Target: plugin.FileItemPath(11, "sources/ A.gpkg "), ItemFingerprint: strings.Repeat("a", 64), Format: "geopackage", BoundaryHash: strings.Repeat("d", 64), EngineVersion: "2"},
	}
}

func TestDevelopFileIntentFreezesCompleteConfigurationAndNodeBindings(t *testing.T) {
	intent := developIntentFixture(t, true)
	config := json.RawMessage(`{ "options":{"mode":"test"}, "large":9007199254740993 }`)
	sources := intent.Clone().Sources
	target := *intent.Clone().Target
	reordered, err := NewDevelopFileIntent(config, 1, 45, sources, &target, nil)
	if err != nil || intent.ConfigDigest != reordered.ConfigDigest {
		t.Fatal("raw complete configuration was not canonicalized", err)
	}
	want, _ := intent.Digest()
	if got, _ := reordered.Digest(); got != want {
		t.Fatal("equivalent intent changed digest")
	}
	sources[0].Target.Segments[2].Name = "caller-mutated.gpkg"
	target.Target.Segments[2].Name = "caller-output.gpkg"
	if got, _ := reordered.Digest(); got != want {
		t.Fatal("intent aliases caller-owned source or target segments")
	}
	changed, err := NewDevelopFileIntent(json.RawMessage(`{"large":9007199254740992,"options":{"mode":"test"}}`), 1, 45, intent.Clone().Sources, intent.Clone().Target, nil)
	if err != nil || changed.ConfigDigest == intent.ConfigDigest {
		t.Fatal("large JSON number lost precision", err)
	}
	for _, mutate := range []func(*DevelopFileIntent){
		func(s *DevelopFileIntent) { s.Sources[0].NodeID = "load_A" },
		func(s *DevelopFileIntent) { s.TimeoutSeconds++ },
		func(s *DevelopFileIntent) { s.ExpectedAttempt++ },
		func(s *DevelopFileIntent) { s.Target.Target.Segments[2].Name = "neighbor.gpkg" },
	} {
		changed := intent.Clone()
		mutate(changed)
		if got, err := changed.Digest(); err != nil || got == want {
			t.Fatal("execution binding change was omitted from digest", err)
		}
	}
}

func TestDevelopFileIntentRejectsAmbiguousAndNoncanonicalDeclarations(t *testing.T) {
	intent := developIntentFixture(t, true)
	for _, raw := range []string{"", "null", "[]", `{"x":1,"x":2}`, `{"a":{"x":1,"x":2}}`, strings.Repeat(" ", MaxExecutionConfigBytes) + "{}"} {
		if got, err := NewDevelopFileIntent(json.RawMessage(raw), 1, 45, intent.Sources, intent.Target, nil); got != nil || err == nil {
			t.Fatal("accepted ambiguous raw config")
		}
	}
	for _, mutate := range []func(*DevelopFileIntent){
		func(s *DevelopFileIntent) { s.Sources = nil },
		func(s *DevelopFileIntent) { s.ExpectedAttempt = 0 },
		func(s *DevelopFileIntent) { s.TimeoutSeconds = 0 },
		func(s *DevelopFileIntent) { s.SchemaVersion = "unknown" },
		func(s *DevelopFileIntent) { s.ConfigDigest = strings.Repeat("A", 64) },
		func(s *DevelopFileIntent) { s.Sources[0].NodeID = " " },
		func(s *DevelopFileIntent) { s.Sources[1].NodeID = s.Sources[0].NodeID },
		func(s *DevelopFileIntent) { s.Sources[0], s.Sources[1] = s.Sources[1], s.Sources[0] },
		func(s *DevelopFileIntent) { s.Sources[0].Target.EngineID = 0 },
		func(s *DevelopFileIntent) { s.Sources[0].Target.Segments[1].Name = ".." },
		func(s *DevelopFileIntent) { s.Sources[0].Target = plugin.FileDirectoryPath(11, "sources") },
		func(s *DevelopFileIntent) { s.Target.Mode = "replace" },
		func(s *DevelopFileIntent) { s.Target.NodeID = s.Sources[0].NodeID },
	} {
		bad := intent.Clone()
		mutate(bad)
		if bad.Validate() == nil {
			t.Fatal("invalid execution declaration accepted", bad)
		}
	}
	if (*DevelopFileIntent)(nil).Validate() == nil || (*DevelopBoundFileScope)(nil).Validate() == nil {
		t.Fatal("nil boundary accepted")
	}
}

func TestDevelopBoundScopeKeepsNodeMappingAndExactUniqueSourceClosure(t *testing.T) {
	intent := developIntentFixture(t, true)
	// Two actual load nodes can bind the same physical file. Intent keeps both;
	// the sealed physical closure has only one reference for that file.
	intent.Sources = append(intent.Sources, DevelopFileSourceIntent{NodeID: "load_c", Target: plugin.FileItemPath(11, "sources/B.gpkg")})
	refs := developReferences()
	refs = append(refs, refs[0])
	bound, err := NewDevelopBoundFileScope(*intent, refs, "3")
	if err != nil || len(bound.Sources) != 2 || len(intent.Sources) != 3 || bound.ValidateAgainstIntent(*intent) != nil {
		t.Fatal("lost logical binding or physical closure", bound, err)
	}
	want, _ := bound.Digest()
	refs[0].Target.Segments[2].Name = "changed.gpkg"
	refs[0].BoundaryHash = strings.Repeat("e", 64)
	copy := bound.Clone()
	copy.Sources[0].Target.Segments[2].Name = "changed.gpkg"
	copy.Target.Target.Segments[2].Name = "changed-output.gpkg"
	if got, _ := bound.Digest(); got != want {
		t.Fatal("sealed scope aliases caller or clone")
	}
	for _, mutate := range []func(*DevelopBoundFileScope){
		func(s *DevelopBoundFileScope) { s.Sources = s.Sources[:1] },
		func(s *DevelopBoundFileScope) { s.Sources[0].Target.Segments[2].Name = "not-declared.gpkg" },
		func(s *DevelopBoundFileScope) { s.Sources[0].BoundaryHash = "unknown" },
		func(s *DevelopBoundFileScope) { s.Sources[0].EngineVersion = "02" },
		func(s *DevelopBoundFileScope) { s.Sources[0].ItemFingerprint = s.Sources[1].ItemFingerprint },
		func(s *DevelopBoundFileScope) { s.Sources[0], s.Sources[1] = s.Sources[1], s.Sources[0] },
		func(s *DevelopBoundFileScope) { s.IntentDigest = strings.Repeat("e", 64) },
		func(s *DevelopBoundFileScope) { s.ExpectedAttempt++ },
		func(s *DevelopBoundFileScope) { s.TimeoutSeconds++ },
		func(s *DevelopBoundFileScope) { s.Target.Target.Segments[2].Name = "neighbor.gpkg" },
		func(s *DevelopBoundFileScope) { s.Target = nil },
	} {
		bad := bound.Clone()
		mutate(bad)
		if bad.ValidateAgainstIntent(*intent) == nil {
			t.Fatal("sealed scope escaped original declaration", bad)
		}
	}
	conflicting := developReferences()
	other := conflicting[0]
	other.EngineVersion = "3"
	conflicting = append(conflicting, other)
	if _, err := NewDevelopBoundFileScope(*intent, conflicting, "3"); err == nil {
		t.Fatal("conflicting evidence for the same source accepted")
	}
}

func TestDevelopReadOnlyIntentNeverAcquiresPublicationTarget(t *testing.T) {
	intent := developIntentFixture(t, false)
	bound, err := NewDevelopBoundFileScope(*intent, developReferences(), "")
	if err != nil || intent.Target != nil || bound.Target != nil || bound.ValidateAgainstIntent(*intent) != nil {
		t.Fatal("read-only declaration requires a fake write", err)
	}
	if _, err := NewDevelopBoundFileScope(*intent, developReferences(), "3"); err == nil {
		t.Fatal("read-only intent accepted a target engine version")
	}
	bound.Target = &DevelopBoundFileTarget{Target: plugin.FileItemPath(12, "outputs/forged.gpkg"), EngineVersion: "3", Mode: "fail"}
	if bound.ValidateAgainstIntent(*intent) == nil {
		t.Fatal("read-only intent upgraded to create")
	}
}

func TestDevelopFileDeadlineUsesOriginalDatabaseClaimTime(t *testing.T) {
	intent := developIntentFixture(t, false)
	bound, err := NewDevelopBoundFileScope(*intent, developReferences(), "")
	if err != nil {
		t.Fatal(err)
	}
	started := time.Date(2026, 11, 8, 3, 4, 5, 123456000, time.FixedZone("local", 8*3600))
	deadline, err := bound.Deadline(started)
	if err != nil || !deadline.Equal(started.Add(45*time.Second)) || deadline.Location() != time.UTC {
		t.Fatal("deadline changed origin or timeout", deadline, err)
	}
	again, _ := bound.Deadline(started)
	if !reflect.DeepEqual(deadline, again) {
		t.Fatal("re-observation extended the execution budget")
	}
	if _, err := bound.Deadline(time.Time{}); err == nil {
		t.Fatal("missing actual claim time accepted")
	}
}

// Size is bounded by the shared strict protocol budget, not an unrelated
// Manager profile source-count policy.
func TestDevelopFileIntentUsesSharedByteBudgetWithoutNewCountPolicy(t *testing.T) {
	sources := make([]DevelopFileSourceIntent, 201)
	refs := make([]DevelopFileSourceReference, len(sources))
	for i := range sources {
		sources[i] = DevelopFileSourceIntent{NodeID: fmt.Sprintf("load_%03d", i), Target: plugin.FileItemPath(11, fmt.Sprintf("sources/%03d.gpkg", i))}
		refs[i] = DevelopFileSourceReference{Target: sources[i].Target, ItemFingerprint: fmt.Sprintf("%064x", i+1), Format: "geopackage", BoundaryHash: strings.Repeat("a", 64), EngineVersion: "2"}
	}
	intent, err := NewDevelopFileIntent(json.RawMessage(`{"nodes":201}`), 1, 45, sources, nil, nil)
	if err != nil {
		t.Fatal("unrelated source-count policy rejected valid intent", err)
	}
	if _, err := NewDevelopBoundFileScope(*intent, refs, ""); err != nil {
		t.Fatal("unrelated source-count policy rejected bound scope", err)
	}
	oversized := intent.Clone()
	oversized.Sources[len(oversized.Sources)-1].NodeID = strings.Repeat("z", MaxExecutionConfigBytes)
	if oversized.Validate() == nil {
		t.Fatal("unbounded wire intent accepted")
	}
}
