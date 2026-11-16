package engineaccess

import (
	"context"
	"testing"
	"time"

	shared "github.com/addp/common/authorization"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/google/uuid"
)

func exactNativeObjectFacts(t *testing.T, target plugin.EngineCatalogPath) authorizationfacts.AuthorizationFacts {
	t.Helper()
	binding, err := authorizationfacts.NativeCandidateBinding(target)
	if err != nil {
		t.Fatal(err)
	}
	return authorizationfacts.AuthorizationFacts{SchemaVersion: authorizationfacts.AuthorizationFactsSchemaV1, EngineID: target.EngineID, ItemFingerprint: binding.ItemFingerprint, ItemType: "object", FullName: target.StringPath(), BasisState: authorizationfacts.AuthorizationBasisComplete, ObservedAt: time.Now(), Boundary: &authorizationfacts.AuthorizationBoundary{Layout: "single", NativeTarget: target}, BoundaryHash: &binding.BoundaryHash}
}

func exerciseRustFSExactSourceRead(t *testing.T, s *Service, ctx context.Context, actor Actor, reader userProvenance, credential string, target plugin.EngineCatalogPath, current *authorizationfacts.AuthorizationFacts) {
	t.Helper()
	read := func(p plugin.EngineCatalogPath, want bool) {
		t.Helper()
		r, err := s.ObserveCurrentSourceRead(authenticatedManagerReadRequest(ctx, s, credential), credential, SourceReadCheckRequest{Targets: []plugin.EngineCatalogPath{p}})
		if want != (err == nil && r != nil) {
			t.Fatalf("exact source read want=%v result=%+v error=%v", want, r, err)
		}
	}
	read(target, false)
	input := CreateIndependentGrantInput{Actor: actor, EngineID: int64(target.EngineID), RequestID: uuid.New(), CatalogPath: target, ItemFingerprint: current.ItemFingerprint, ExpectedBoundaryHash: *current.BoundaryHash, RecipientType: "user", RecipientID: reader.PrincipalID, Action: shared.SourceAccessRead, ExpiryMode: shared.SharingExpiryUntilRevoked, Reason: "Exact RustFS object preview"}
	grant, err := s.CreateIndependentGrant(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	read(target, true)
	approved := *current
	for _, p := range []plugin.EngineCatalogPath{plugin.ObjectItemPath(target.EngineID, "models", "future.csv"), plugin.ObjectItemPath(target.EngineID, "other", "scene.csv")} {
		*current = exactNativeObjectFacts(t, p)
		read(p, false)
	}
	*current = approved
	read(target, true)
	if _, err := s.RevokeGrant(ctx, RevokeGrantInput{Actor: actor, EngineID: int64(target.EngineID), RequestID: grant.RequestID, Reason: "Withdraw exact object"}); err != nil {
		t.Fatal(err)
	}
	read(target, false)
	if current.Boundary.FileComposition != nil && execution.ManagerFileSourceComposition("rustfs", current.Boundary.FileComposition) {
		t.Fatal("synchronous preview expanded background file task admission")
	}
	t.Log("RustFS exact read: metadata-only, explicit content, revoke, expire, sibling/bucket isolation; background file admission remains closed")
}
