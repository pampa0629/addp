package repository

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/addp/common/infra/database"
	"github.com/addp/transfer/internal/models"
)

func TestInstalledTransferStorage(t *testing.T) {
	if os.Getenv("ADDP_TEST_TRANSFER_DEPLOYMENT") != "1" {
		t.Skip("requires isolated Transfer deployment")
	}
	db, err := OpenDatabase(database.Config{Host: os.Getenv("POSTGRES_HOST"), Port: os.Getenv("POSTGRES_PORT"), DBName: os.Getenv("POSTGRES_DB"), Schema: "transfer"})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	ctx := context.Background()
	checkStoredDeadLetter(t, db)
	bounded, _, err := NewTaskRepository(db).ClaimNextBoundedExecution(ctx, "fixture-worker", time.Minute)
	if err != nil || bounded != nil {
		t.Fatalf("empty bounded queue: %+v %v", bounded, err)
	}
	claim, err := NewRuntimeLeaseRepository(db, ContinuousRecoveryPolicy{}).ClaimNext(ctx, "fixture-continuous", time.Minute)
	if err != nil || claim != nil {
		t.Fatalf("empty continuous queue: %+v %v", claim, err)
	}
	t.Run("CDC approval and scan origin share stored time", func(t *testing.T) {
		now := time.Now().UTC()
		_, parent, _ := seedPendingSchemaChangeRuntime(t, db, 7, now, now.Add(time.Minute))
		var request models.SchemaChangeRequest
		if err := db.Where("execution_id = ?", parent.ExecutionID).First(&request).Error; err != nil {
			t.Fatal(err)
		}
		assertAppliedScanOriginClaimRoundTrip(t, db, request)
	})
	t.Log("dead letter repeat and collision verified; bounded and continuous worker polling succeeded")
}
