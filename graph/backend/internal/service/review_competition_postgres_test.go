package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/client"
	"github.com/addp/graph/internal/models"
	"github.com/addp/graph/internal/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// The winner uses the production admission/operation/receipt transactions;
// competing requests use the actual approve/reject/modify Service methods.
// Native graph effects are represented by the durable operation protocol.
func testGraphReviewCommandCompetition(t *testing.T) {
	for _, winnerAction := range []string{"approve", "reject", "modify"} {
		for _, loserAction := range []string{"approve", "reject", "modify"} {
			t.Run(winnerAction+"_before_"+loserAction, func(t *testing.T) {
				f := newReviewPGFixture(t)
				ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
				defer cancel()
				var nativeEntryCalls atomic.Int64
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					nativeEntryCalls.Add(1)
					http.Error(w, "losing review must not resolve a graph target", http.StatusServiceUnavailable)
				}))
				defer server.Close()
				losingService := func(db *gorm.DB) *BuildService {
					return &BuildService{
						buildRepo: repository.NewBuildRepository(db),
						graphDataSvc: NewGraphDataService(repository.NewKnowledgeGraphRepository(db), repository.NewOntologyRepository(db),
							client.NewSystemServiceClient(server.URL, graphServiceToken{}, server.Client()), nil),
					}
				}
				compete := func(svc *BuildService) error {
					switch loserAction {
					case "approve":
						return svc.ApproveReviewItem(ctx, f.task.GraphID, f.item.ID, f.task.TenantID, 8)
					case "reject":
						return svc.RejectReviewItem(ctx, f.task.GraphID, f.item.ID, f.task.TenantID, 8)
					default:
						return svc.ModifyAndApproveReviewItem(ctx, f.task.GraphID, f.item.ID, f.task.TenantID, 8, map[string]interface{}{"name": "loser-content"})
					}
				}
				tx := f.db.WithContext(ctx).Begin()
				if tx.Error != nil {
					t.Fatal(tx.Error)
				}
				defer tx.Rollback()
				var blocker int
				if err := tx.Raw("SELECT pg_backend_pid()").Scan(&blocker).Error; err != nil {
					t.Fatal(err)
				}
				winner, err := repository.NewBuildRepository(tx).BeginReview(ctx, f.task.GraphID, f.item.ID, f.task.TenantID, 7)
				if err != nil || winner.ActiveCommandID == "" {
					t.Fatalf("winner admission: %v", err)
				}
				var admitted models.ReviewItem
				if err := tx.First(&admitted, f.item.ID).Error; err != nil {
					t.Fatal(err)
				}
				pids, result := make(chan int, 1), make(chan error, 1)
				go func() {
					result <- f.db.WithContext(ctx).Connection(func(conn *gorm.DB) error {
						var pid int
						if err := conn.Raw("SELECT pg_backend_pid()").Scan(&pid).Error; err != nil {
							return err
						}
						pids <- pid
						return compete(losingService(conn))
					})
				}()
				var waiter int
				select {
				case waiter = <-pids:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				// The first read saw the old committed pending row. The real PG
				// blocker proves that the loser reached the competing transaction.
				waitReviewCleanupPG(t, ctx, f.db, "SELECT ? = ANY(pg_blocking_pids(?))", blocker, waiter)
				if err := tx.Commit().Error; err != nil {
					t.Fatal(err)
				}
				select {
				case err = <-result:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				if !errors.Is(err, commonapi.ErrConflict) {
					t.Fatalf("competing service admitted another owner: %v", err)
				}
				assertReviewCompetitionRow(t, f.db, admitted)
				assertReviewCompetitionOperations(t, f, 0)
				if nativeEntryCalls.Load() != 0 {
					t.Fatal("loser reached the external graph entry")
				}

				repo := repository.NewBuildRepository(f.db)
				status, elementID := models.ReviewStatusRejected, ""
				var finalContent []byte
				var operation models.GraphOperation
				var expectedOperations int64
				if winnerAction != "reject" {
					operation = reviewCompetitionOperation(t, f, winner.ActiveCommandID)
					if err := repo.BeginCommandOperation(ctx, &operation); err != nil {
						t.Fatal(err)
					}
					expectedOperations = 1
					status, elementID = models.ReviewStatusApproved, "winner-native-id"
					if winnerAction == "modify" {
						status, finalContent = models.ReviewStatusModified, []byte(`{"name":"winner-content"}`)
					}
				}
				// Possessing a stale row or the right command ID with the wrong
				// actor must not authorize the loser's receipt.
				for _, identity := range []string{"actor", "command"} {
					foreign := *winner
					if identity == "actor" {
						foreign.CommandActor = 8
					} else {
						foreign.ActiveCommandID = uuid.NewString()
					}
					if err := repo.EndReview(ctx, &foreign, models.ReviewStatusModified, "loser-native-id", []byte(`{"name":"loser-content"}`), nil); !errors.Is(err, commonapi.ErrConflict) {
						t.Fatalf("foreign %s overwrote winner: %v", identity, err)
					}
					assertReviewCompetitionRow(t, f.db, admitted)
				}
				if winnerAction != "reject" {
					if err := repo.SettleCommandOperation(ctx, operation, strings.Repeat("b", 64), true); err != nil {
						t.Fatal(err)
					}
				}
				if err := repo.EndReview(ctx, winner, status, elementID, finalContent, nil); err != nil {
					t.Fatal(err)
				}
				var completed models.ReviewItem
				if err := f.db.First(&completed, f.item.ID).Error; err != nil {
					t.Fatal(err)
				}
				if completed.Status != status || completed.ActiveCommandID != "" || completed.CommandActor != 0 || completed.ReviewedBy == nil || *completed.ReviewedBy != 7 || completed.ReviewedAt == nil || completed.ElementID != elementID || string(completed.Content) != string(admitted.Content) {
					t.Fatal("winner lost its status, content or ownership")
				}
				if winnerAction == "modify" {
					var content map[string]interface{}
					if err := json.Unmarshal(completed.FinalContent, &content); err != nil || !reflect.DeepEqual(content, map[string]interface{}{"name": "winner-content"}) {
						t.Fatalf("modified winner content changed: %v", err)
					}
				} else if len(completed.FinalContent) != 0 {
					t.Fatal("approve/reject inherited losing modifications")
				}
				// Repeated service requests and a stale operation registration
				// after finalization cannot create another command or effect.
				if err := compete(losingService(f.db)); !errors.Is(err, commonapi.ErrConflict) {
					t.Fatalf("terminal item accepted another action: %v", err)
				}
				stale := reviewCompetitionOperation(t, f, winner.ActiveCommandID)
				if err := repo.BeginCommandOperation(ctx, &stale); !errors.Is(err, gorm.ErrRecordNotFound) {
					t.Fatalf("terminal command registered another operation: %v", err)
				}
				assertReviewCompetitionRow(t, f.db, completed)
				assertReviewCompetitionOperations(t, f, expectedOperations)
				if nativeEntryCalls.Load() != 0 {
					t.Fatal("losing or repeated request reached external graph access")
				}
				if expectedOperations != 0 {
					var stored models.GraphOperation
					if err := f.db.First(&stored, "id = ?", operation.ID).Error; err != nil || stored.CommandID != winner.ActiveCommandID || stored.State != models.GraphOperationKnown || stored.ReviewItemID != f.item.ID {
						t.Fatalf("winner operation changed: %v", err)
					}
				}
				f.assertBuildUnchanged(t)
			})
		}
	}
}

func assertReviewCompetitionRow(t *testing.T, db *gorm.DB, expected models.ReviewItem) {
	t.Helper()
	var actual models.ReviewItem
	if err := db.First(&actual, expected.ID).Error; err != nil || !reflect.DeepEqual(actual, expected) {
		t.Fatalf("loser changed review row, including timestamps: %v", err)
	}
}

func assertReviewCompetitionOperations(t *testing.T, f *reviewCleanupPGFixture, expected int64) {
	t.Helper()
	var count int64
	if err := f.db.Model(&models.GraphOperation{}).Where("tenant_id = ? AND graph_id = ?", f.task.TenantID, f.task.GraphID).Count(&count).Error; err != nil || count != expected {
		t.Fatalf("extra durable operations: count=%d expected=%d err=%v", count, expected, err)
	}
}

func reviewCompetitionOperation(t *testing.T, f *reviewCleanupPGFixture, commandID string) models.GraphOperation {
	t.Helper()
	descriptor, err := json.Marshal(models.GraphOperationTarget{EngineID: f.graph.EngineID, EngineVersion: 1, CatalogPath: f.graph.CatalogPath, IndexManagement: f.graph.IndexManagement})
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(descriptor)
	return models.GraphOperation{ID: uuid.NewString(), TenantID: f.task.TenantID, GraphID: f.task.GraphID, ReviewItemID: f.item.ID, CommandID: commandID, RunnerID: "review-winner", Kind: "graph", TargetDescriptor: string(descriptor), TargetDigest: hex.EncodeToString(hash[:]), RequestDigest: strings.Repeat("a", 64)}
}
