package engineaccess

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/system/internal/iam"
)

func developGeoPackageFacts(t *testing.T, path plugin.EngineCatalogPath) *authorizationfacts.AuthorizationFacts {
	t.Helper()
	composition, err := authorizationfacts.NewFileCompositionBoundary("geopackage", []authorizationfacts.FileCompositionRef{{Target: path, Role: "container", Required: true, Primary: true}})
	if err != nil {
		t.Fatal(err)
	}
	facts := &authorizationfacts.AuthorizationFacts{SchemaVersion: authorizationfacts.AuthorizationFactsSchemaV1, EngineID: path.EngineID, ItemFingerprint: commonmodels.GenerateItemFingerprint(path.EngineID, path.StringPath()), ItemType: "file", FullName: path.StringPath(), BasisState: authorizationfacts.AuthorizationBasisComplete, ObservedAt: time.Now(), Boundary: &authorizationfacts.AuthorizationBoundary{Layout: "single", FileComposition: composition}}
	hash, err := facts.ComputeBoundaryHash()
	if err != nil {
		t.Fatal(err)
	}
	facts.BoundaryHash = &hash
	if facts.Validate() != nil {
		t.Fatal("invalid test-only Meta contract")
	}
	return facts
}

func TestDevelopFileReadRequiresCompleteExactGeoPackageComposition(t *testing.T) {
	p := plugin.FileItemPath(11, "sources/A.gpkg")
	facts := developGeoPackageFacts(t, p)
	binding, err := facts.ApprovedBinding()
	if err != nil {
		t.Fatal(err)
	}
	if !developSingleGeoPackage(binding, p) {
		t.Fatal("complete contract rejected")
	}
	for _, mutate := range []func(*authorizationfacts.Binding){
		func(b *authorizationfacts.Binding) { b.FileComposition = nil },
		func(b *authorizationfacts.Binding) { b.BoundaryHash = strings.Repeat("f", 64) },
		func(b *authorizationfacts.Binding) { b.FileComposition.Format = "ifc" },
		func(b *authorizationfacts.Binding) { b.FileComposition.Refs[0].Role = "model" },
		func(b *authorizationfacts.Binding) { b.FileComposition.Refs[0].Required = false },
		func(b *authorizationfacts.Binding) {
			b.FileComposition.Refs = append(b.FileComposition.Refs, authorizationfacts.FileCompositionRef{Target: plugin.FileItemPath(11, "sources/A.gpkg-wal"), Role: "journal", Required: true})
		},
	} {
		copy := binding
		copy.FileComposition = binding.FileComposition.Clone()
		mutate(&copy)
		if developSingleGeoPackage(copy, p) {
			t.Fatal("incomplete or unrelated content accepted")
		}
	}
	if developSingleGeoPackage(binding, plugin.FileItemPath(11, "sources/neighbor.gpkg")) {
		t.Fatal("neighboring path accepted")
	}
}

type developMetadataOnlyReader struct {
	facts *authorizationfacts.AuthorizationFacts
	calls int
}

func (r *developMetadataOnlyReader) ReadAuthorizationFacts(_ context.Context, _ uint, fps []string) (*authorizationfacts.AuthorizationFactsBatch, error) {
	r.calls++
	entry := authorizationfacts.AuthorizationFactsResult{ItemFingerprint: fps[0], Facts: r.facts}
	return &authorizationfacts.AuthorizationFactsBatch{Data: []authorizationfacts.AuthorizationFactsResult{entry}}, nil
}

func TestDevelopFileReadNeverPromotesMetadataBoundaryToContent(t *testing.T) {
	p := plugin.FileItemPath(11, "sources/A.gpkg")
	facts := developGeoPackageFacts(t, p)
	facts.Boundary = &authorizationfacts.AuthorizationBoundary{Layout: "single", MetadataTarget: &authorizationfacts.MetadataBoundary{Target: p, Layout: "single", DataType: "container", Format: "geopackage", CompositionDigest: strings.Repeat("a", 64)}}
	hash, err := facts.ComputeBoundaryHash()
	if err != nil {
		t.Fatal(err)
	}
	facts.BoundaryHash = &hash
	reader := &developMetadataOnlyReader{facts: facts}
	service := NewService(NewRepository(nil), nil).WithAuthorizationFactsReader(reader)
	intent, err := execution.NewDevelopFileIntent([]byte(`{"purpose":"test-only"}`), 1, 45, []execution.DevelopFileSourceIntent{{NodeID: "load", Target: p}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	value, err := service.PrepareDevelopFileRead(t.Context(), &iam.ExecutionAuthorization{TenantID: 3, ActorPrincipalID: 31, TenantMembershipID: 41, IssuedAuthorizationVersion: 5, SourceType: "user", Audience: "develop"}, *intent)
	if value != nil || !errors.Is(err, ErrAuthorizationBoundaryChanged) || reader.calls != 1 {
		t.Fatal("metadata became a content connection", value, err, reader.calls)
	}
}
