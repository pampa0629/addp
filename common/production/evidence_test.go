package production

import (
	"context"
	"testing"
	"time"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/dataitem/authorizationfacts/factstest"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
	"github.com/google/uuid"
)

func committedReceipt(target plugin.EngineCatalogPath) Receipt {
	id := uuid.NewString()
	return Receipt{Reference: Reference{Owner: "develop", ExecutionID: id, ResultID: uuid.NewString()},
		TenantID: 7, TargetEngineVersion: 1, Attempt: 1, OperationID: uuid.NewString(), CommittedAt: time.Now().UTC(),
		Origin:  &UserOrigin{PrincipalID: 31, MembershipID: 41, AuthorizationVersion: 5},
		Primary: target, Objects: []CommittedObject{{Target: target, Created: true}},
		Sources: SourceEvidence{Kind: SourcesUnknown}}
}

func TestCommittedBoundaryProofDoesNotDependOnOwnerOrTaskStatus(t *testing.T) {
	path := plugin.TabularItemPath(2, "schema", "public", "result")
	for _, owner := range []string{"transfer", "develop", "manager", "future_module"} {
		r := committedReceipt(path)
		r.Reference.Owner = owner
		if err := r.ProveNewBoundary(factstest.Binding(path)); err != nil {
			t.Fatal(owner, err)
		}
		// Proven creation is independent of human eligibility: service-only
		// facts can be identified but must never imply a personal recipient.
		r.Origin = nil
		if err := r.Validate(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestProductionBoundaryRejectsIncompleteAndExistingMembers(t *testing.T) {
	path := plugin.TabularItemPath(2, "schema", "public", "result")
	for _, scenario := range []string{"existing", "other_target", "extra_object", "duplicate", "cross_engine", "missing_commit", "invalid_operation", "partial_user"} {
		t.Run(scenario, func(t *testing.T) {
			r := committedReceipt(path)
			switch scenario {
			case "existing":
				r.Objects[0].Created = false
			case "other_target":
				r.Primary = plugin.TabularItemPath(2, "schema", "public", "other")
			case "extra_object":
				r.Objects = append(r.Objects, CommittedObject{Target: plugin.TabularItemPath(2, "schema", "public", "other"), Created: true})
			case "duplicate":
				r.Objects = append(r.Objects, r.Objects[0])
			case "cross_engine":
				r.Objects[0].Target.EngineID = 3
			case "missing_commit":
				r.CommittedAt = time.Time{}
			case "invalid_operation":
				r.OperationID = "caller-choice"
			case "partial_user":
				r.Origin.MembershipID = 0
			}
			if r.ProveNewBoundary(factstest.Binding(path)) == nil {
				t.Fatal("unproven boundary accepted")
			}
		})
	}
}

func TestSourceFreeEvidenceCannotBeInferredFromAnEmptyReadSet(t *testing.T) {
	for _, kind := range []string{SourcesUnknown, SourcesNone} {
		if (SourceEvidence{Kind: kind}).Validate() != nil {
			t.Fatal(kind)
		}
		if (SourceEvidence{Kind: kind, ReadSet: &plugin.QueryReadSet{}}).Validate() == nil {
			t.Fatal("ambiguous source proof", kind)
		}
	}
	for _, set := range []*plugin.QueryReadSet{nil, {}} {
		if (SourceEvidence{Kind: SourcesDataItems, ReadSet: set}).Validate() == nil {
			t.Fatal("empty reads became source-free")
		}
	}
	for _, path := range []plugin.EngineCatalogPath{plugin.TabularItemPath(2, "schema", "public", "source"), plugin.FileItemPath(3, "source/input.gpkg"), plugin.ObjectItemPath(4, "bucket", "source/input.geojson")} {
		set, err := plugin.NewQueryReadSet(path)
		if err != nil {
			t.Fatal(err)
		}
		if (SourceEvidence{Kind: SourcesDataItems, ReadSet: set, EngineVersions: []EngineVersion{{EngineID: int64(path.EngineID), Version: 2}}}).Validate() != nil {
			t.Fatal("non-PG source rejected")
		}
	}
}

func TestSourceVersionsRequireExactCanonicalSessionCoverage(t *testing.T) {
	set, _ := plugin.NewQueryReadSet(plugin.FileItemPath(2, "a.gpkg"), plugin.FileItemPath(3, "b.gpkg"))
	for _, versions := range [][]EngineVersion{nil, {{2, 1}}, {{2, 1}, {3, 0}}, {{2, 1}, {2, 1}}, {{3, 1}, {2, 1}}, {{2, 1}, {4, 1}}} {
		if ValidateEngineVersions(set, versions) == nil {
			t.Fatal("incomplete or inconsistent source versions accepted", versions)
		}
	}
	if ValidateEngineVersions(set, []EngineVersion{{2, 1}, {3, 2}}) != nil {
		t.Fatal("complete actual session versions rejected")
	}
}

func TestFileBoundaryRequiresTheWholeMetaComposition(t *testing.T) {
	primary := plugin.FileItemPath(2, "out/result.gpkg")
	composition, err := authorizationfacts.NewFileCompositionBoundary("geopackage", []authorizationfacts.FileCompositionRef{{Target: primary, Role: "container", Required: true, Primary: true}})
	if err != nil {
		t.Fatal(err)
	}
	facts := authorizationfacts.AuthorizationFacts{SchemaVersion: authorizationfacts.AuthorizationFactsSchemaV1, EngineID: 2,
		BasisState: authorizationfacts.AuthorizationBasisComplete, ObservedAt: time.Now().UTC(),
		ItemFingerprint: models.GenerateItemFingerprint(2, "out/result.gpkg"), ItemType: "file", FullName: "out/result.gpkg", Boundary: &authorizationfacts.AuthorizationBoundary{Layout: "single", FileComposition: composition}}
	hash, err := facts.ComputeBoundaryHash()
	if err != nil {
		t.Fatal(err)
	}
	facts.BoundaryHash = &hash
	binding, err := facts.ApprovedBinding()
	if err != nil {
		t.Fatal(err)
	}
	r := committedReceipt(primary)
	if err := r.ProveNewBoundary(binding); err != nil {
		t.Fatal(err)
	}
	r.Objects = append(r.Objects, CommittedObject{Target: plugin.FileItemPath(2, "out/unrelated.gpkg"), Created: true})
	if r.ProveNewBoundary(binding) == nil {
		t.Fatal("scan expanded beyond complete Meta item")
	}

}

func TestRegistryIsExplicitAndIndependentOfTaskType(t *testing.T) {
	owners := []Owner{{Module: "transfer", ClientID: "addp-transfer", ReceiptPermission: "transfer.production_receipt.read", ReadReceipt: func(context.Context, uint, Reference) (*Receipt, error) { return nil, nil }, ExecutionPermission: "transfer.task.execute"},
		{Module: "develop", ClientID: "addp-develop", ReceiptPermission: "develop.production_receipt.read", ReadReceipt: func(context.Context, uint, Reference) (*Receipt, error) { return nil, nil }, ExecutionPermission: "develop.task.execute"}}
	r, err := NewRegistry(owners...)
	if err != nil {
		t.Fatal(err)
	}
	owners[0].ClientID = "addp-attacker"
	if _, ok := r.Resolve("transfer", "addp-transfer"); !ok {
		t.Fatal("registry aliases input")
	}
	for _, pair := range [][2]string{{"develop", "addp-transfer"}, {"future_module", "addp-future_module"}, {"transfer", ""}} {
		if _, ok := r.Resolve(pair[0], pair[1]); ok {
			t.Fatal("unregistered or cross-owner service accepted")
		}
	}
	if _, err := NewRegistry(owners[1], owners[1]); err == nil {
		t.Fatal("duplicate owner accepted")
	}
}

func TestMultiFileResultCannotClaimAnExistingOrMissingMember(t *testing.T) {
	primary := plugin.FileItemPath(2, "out/scene.gltf")
	buffer := plugin.FileItemPath(2, "out/scene.bin")
	composition, err := authorizationfacts.NewFileCompositionBoundary("gltf", []authorizationfacts.FileCompositionRef{
		{Target: primary, Role: "manifest", Required: true, Primary: true},
		{Target: buffer, Role: "buffer", Required: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	facts := authorizationfacts.AuthorizationFacts{SchemaVersion: authorizationfacts.AuthorizationFactsSchemaV1, EngineID: 2,
		BasisState: authorizationfacts.AuthorizationBasisComplete, ObservedAt: time.Now().UTC(),
		ItemFingerprint: models.GenerateItemFingerprint(2, "out/scene.gltf"), ItemType: "file", FullName: "out/scene.gltf",
		Boundary: &authorizationfacts.AuthorizationBoundary{Layout: "multi", FileComposition: composition}}
	hash, err := facts.ComputeBoundaryHash()
	if err != nil {
		t.Fatal(err)
	}
	facts.BoundaryHash = &hash
	binding, err := facts.ApprovedBinding()
	if err != nil {
		t.Fatal(err)
	}
	r := committedReceipt(primary)
	if r.ProveNewBoundary(binding) == nil {
		t.Fatal("primary alone claimed complete multi-file result")
	}
	r.Objects = append(r.Objects, CommittedObject{Target: buffer, Created: false})
	if r.ProveNewBoundary(binding) == nil {
		t.Fatal("new primary claimed an existing buffer")
	}
	r.Objects[1].Created = true
	if err := r.ProveNewBoundary(binding); err != nil {
		t.Fatal("complete newly committed multi-file result rejected", err)
	}
}
