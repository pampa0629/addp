package falkor

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/addp/ontology/internal/platform"
	"github.com/google/uuid"
)

func platformFixture(t *testing.T) *platform.Snapshot {
	t.Helper()
	snapshot, err := platform.CompileTransferRelease()
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func maximumPlatformFixture(t *testing.T) *platform.Snapshot {
	t.Helper()
	d, _ := platformFixture(t).Context()
	d.Digest = ""
	d.Concepts = nil
	d.Relations = []platform.Relation{}
	for i := 0; i < 32; i++ {
		names := map[string]string{"en": "Concept", "zh-cn": "概念"}
		for j := 0; j < 14; j++ {
			names[fmt.Sprintf("locale_%d", j)] = "Name"
		}
		d.Concepts = append(d.Concepts, platform.Concept{ID: fmt.Sprintf("concept_%d", i), Name: names})
		for j := 0; j < 2; j++ {
			d.Relations = append(d.Relations, platform.Relation{From: fmt.Sprintf("concept_%d", i), To: "concept_0", Kind: fmt.Sprintf("relation_%d", j)})
		}
	}
	data, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := platform.Compile(data)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestPlatformPlan(t *testing.T) {
	snapshot := platformFixture(t)
	generation := uuid.NewString()
	p, err := PlanPlatform(snapshot, generation)
	if err != nil {
		t.Fatal(err)
	}
	q, err := PlanPlatform(snapshot, generation)
	d, _ := snapshot.Context()
	prefix := fmt.Sprintf("ontology:p:%s:r%d:g", d.Capability, d.Revision)
	if err != nil || !reflect.DeepEqual(p, q) || !strings.HasPrefix(p.key, prefix) || strings.Contains(p.key, "t0:") {
		t.Fatal("unstable or unscoped plan", p, err)
	}
	if len(p.nodes) != len(d.Concepts)*3 || len(p.edges) != len(d.Relations)+len(d.Concepts)*2 {
		t.Fatal("missing concept/name/relation")
	}
	other, _ := PlanPlatform(snapshot, uuid.NewString())
	if other.key == p.key {
		t.Fatal("generation reused")
	}
	for _, bad := range []*platform.Snapshot{nil, {}} {
		if _, err := PlanPlatform(bad, generation); !errors.Is(err, ErrInvalid) {
			t.Fatal(err)
		}
	}
	for _, bad := range []string{"", uuid.Nil.String(), strings.ToUpper(generation)} {
		if _, err := PlanPlatform(snapshot, bad); !errors.Is(err, ErrInvalid) {
			t.Fatal(err)
		}
	}
	if _, err := parameters(map[string]any{"nodes": p.nodes}); err != nil {
		t.Fatal(err)
	}
	if _, err := parameters(map[string]any{"edges": p.edges}); err != nil {
		t.Fatal(err)
	}
	maximum, err := PlanPlatform(maximumPlatformFixture(t), uuid.NewString())
	if err != nil || len(maximum.nodeRows) != 544 || len(maximum.edgeRows) != 576 {
		t.Fatal("platform plan limit", err)
	}
}
