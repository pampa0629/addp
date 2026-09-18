package service

import (
	"context"
	"errors"
	"net"
	"os"
	"testing"
	"time"

	"github.com/addp/common/execution"
	"github.com/addp/ontology/internal/falkor"
	"github.com/addp/ontology/internal/models"
	"github.com/addp/ontology/internal/repository"
	"github.com/addp/ontology/internal/semantic"
	"github.com/google/uuid"
)

// Runs only in the owned PG/Falkor gate; the disposable volume owns all graphs.
// System's live authorization endpoint has its own IAM gate, not this fixture.
func TestPostgresProjectionRuntime(t *testing.T) {
	if os.Getenv("ADDP_ONTOLOGY_FALKOR_INTEGRATION") != "1" {
		t.Skip("requires owner disposable FalkorDB gate")
	}
	address, password := os.Getenv("ONTOLOGY_FALKOR_TEST_ADDRESS"), os.Getenv("ONTOLOGY_FALKOR_TEST_PASSWORD")
	host, port, err := net.SplitHostPort(address)
	if err != nil || host != "127.0.0.1" || port == "6379" || port == "16379" || port == "16479" || len(password) != 32 {
		t.Fatal("invalid disposable endpoint")
	}
	if _, err := uuid.Parse(password); err != nil {
		t.Fatal("invalid owner run identity")
	}
	graph, err := falkor.New(falkor.Config{Address: address, Password: password})
	if err != nil {
		t.Fatal(err)
	}
	db := postgresFixture(t)
	repo := repository.NewRevisionRepository(db)
	s := NewRevisionService(repo)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	actor := testActor(101)
	d := testDefinition("outdoor_beijing_runtime")
	old := publishRuntimeFixture(t, s, actor, d)
	run := func(executionID string, g ProjectionGraph) error {
		t.Helper()
		lease, err := repo.ClaimProjection(ctx, "real-graph-fixture", 30*time.Second)
		if err != nil || lease == nil || lease.ExecutionID != executionID {
			t.Fatalf("claim: %v %v", lease, err)
		}
		e, err := NewProjectionExecutor(repo, g, testProjectionAuthorizer(testProjectionReceipt))
		if err != nil {
			t.Fatal(err)
		}
		return e.Execute(ctx, *lease)
	}
	if err := run(*old.BuildExecutionID, graph); err != nil {
		t.Fatal(err)
	}
	snapshot, err := semantic.Restore([]byte(old.Payload), old.Digest)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := falkor.Plan(snapshot, *old.Generation)
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.Verify(ctx, plan); err != nil {
		t.Fatal(err)
	}
	d.Scope.Revision++
	next := publishRuntimeFixture(t, s, actor, d)
	if err := run(*next.BuildExecutionID, rejectVerifiedProjection{graph}); err == nil {
		t.Fatal("failed verification activated")
	}
	var head models.Ontology
	if err := db.Where("tenant_id=? AND ontology_id=?", old.TenantID, old.OntologyID).First(&head).Error; err != nil {
		t.Fatal(err)
	}
	if head.ActiveRevision == nil || *head.ActiveRevision != old.Revision || head.ActiveGeneration == nil || *head.ActiveGeneration != *old.Generation {
		t.Fatal("failed replacement changed active pointer")
	}
	for _, item := range []struct {
		r                     *models.Revision
		projection, execution string
	}{{old, "ready", execution.ExecutionStatusSuccess}, {next, "failed", execution.ExecutionStatusFailed}} {
		var p models.Projection
		var job execution.TaskExecution
		if err := db.Where("generation=?", item.r.Generation).First(&p).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Where("execution_id=?", item.r.BuildExecutionID).First(&job).Error; err != nil {
			t.Fatal(err)
		}
		if p.Status != item.projection || job.Status != item.execution {
			t.Fatalf("inconsistent terminal states: %s %s", p.Status, job.Status)
		}
	}
	rebuilt, err := s.RebuildProjection(ctx, actor, d.Scope, next.Version, *next.Generation, head.ActivationVersion)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AdmitProjection(ctx, actor, d.Scope, next.Version, rebuilt.Generation, "addp_at_rebuild", runtimeFixtureIssuer(actor)); err != nil {
		t.Fatal(err)
	}
	if err := run(rebuilt.ExecutionID, graph); err != nil {
		t.Fatal(err)
	}
	rebuiltSnapshot, err := semantic.Restore([]byte(next.Payload), next.Digest)
	if err != nil {
		t.Fatal(err)
	}
	rebuiltPlan, err := falkor.Plan(rebuiltSnapshot, rebuilt.Generation)
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.Verify(ctx, rebuiltPlan); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("tenant_id=? AND ontology_id=?", next.TenantID, next.OntologyID).First(&head).Error; err != nil {
		t.Fatal(err)
	}
	if head.ActiveGeneration == nil || *head.ActiveGeneration != rebuilt.Generation || head.ActiveRevision == nil || *head.ActiveRevision != next.Revision {
		t.Fatal("verified rebuilt graph not activated")
	}
	var failed models.Projection
	if err := db.Where("generation=?", next.Generation).First(&failed).Error; err != nil || failed.Status != "failed" {
		t.Fatal("rebuild overwrote failed predecessor")
	}
}

type rejectVerifiedProjection struct{ *falkor.Client }

func (g rejectVerifiedProjection) Verify(ctx context.Context, p *falkor.Projection) error {
	if err := g.Client.Verify(ctx, p); err != nil {
		return err
	}
	return errors.New("injected post-build verification failure")
}
