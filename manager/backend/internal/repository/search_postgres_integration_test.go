package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/addp/manager/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationPostgresManagerSearchSchemaAndVectorBatches(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("requires standard Manager PostgreSQL gate")
	}
	db, err := gorm.Open(postgres.Open(managerTileCacheRepositoryIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	// The existing standard gate serializes Manager packages. Transaction rollback
	// restores the previous owner schema and extension, including on failure.
	if err := tx.Exec("DROP SCHEMA IF EXISTS manager CASCADE; CREATE SCHEMA manager").Error; err != nil {
		t.Fatal(err)
	}
	if err := ensureEmbeddingArtifactStateSchema(tx, models.EmbeddingVectorDimension); err != nil {
		t.Fatal(err)
	}
	if err := MigrateContentIndexDeliverySchema(tx); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := MigrateSearchSchema(tx); err != nil {
			t.Fatal(err)
		}
	}
	// Startup's vector type check must remain repeatable after revision triggers.
	if err := ensureEmbeddingArtifactStateSchema(tx, models.EmbeddingVectorDimension); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	repo := NewContentIndexDeliveryRepository(tx, "search-test")
	epoch := openActiveContentIndex(t, repo)
	first, err := repo.ReserveSearchOrdinals(ctx, tx, epoch, []string{"first", "second"})
	if err != nil {
		t.Fatal(err)
	}
	again, err := repo.ReserveSearchOrdinals(ctx, tx, epoch, []string{"second", "third"})
	if err != nil || first["second"] != again["second"] || again["third"] <= first["second"] {
		t.Fatal(first, again, err)
	}
	rollback := errors.New("rollback")
	err = tx.Transaction(func(probe *gorm.DB) error {
		_, err := repo.ReserveSearchOrdinals(ctx, probe, epoch, []string{"not-committed"})
		if err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	next, err := repo.ReserveSearchOrdinals(ctx, tx, epoch, []string{"fourth"})
	if err != nil || next["fourth"] != again["third"]+1 {
		t.Fatal("rollback leaked ordinal", next, err)
	}
	profile := uuid.NewString()
	deployment := uuid.NewString()
	vector := "[1" + strings.Repeat(",0", models.EmbeddingVectorDimension-1) + "]"
	for i := 1; i <= 403; i++ {
		engine := 11
		if i == 1 {
			engine = 12
		}
		if err := tx.Exec(`INSERT INTO manager.embeddings(tenant_id,item_fingerprint,item_id,engine_id,locator,source_version,embedding,model_profile_id,profile_version,deployment_id,dimension,status,created_at,updated_at)
   VALUES(7,?,?,?,'fixture','v1',?::manager.vector,?,1,?,2560,'ready',now(),now())`, fmt.Sprintf("%064x", i), i, engine, vector, profile, deployment).Error; err != nil {
			t.Fatal(err)
		}
	}
	engine := uint(11)
	q := SearchVectorQuery{TenantID: 7, EngineID: &engine, Vector: make([]float32, 2560), ModelProfileID: profile, ProfileVersion: 1, Dimension: 2560, MaxDistance: 0.1}
	q.Vector[0] = 1
	vectors := NewEmbeddingRepository(tx)
	all := []string{}
	var after *SearchVectorPosition
	for batches := 0; batches < 4; batches++ {
		rows, err := vectors.SearchReadyVectorBatch(ctx, q, after)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			all = append(all, row.Embedding.ItemFingerprint)
			if row.Embedding.EngineID != 11 || row.Distance != 0 {
				t.Fatal("scope/order mismatch", row)
			}
		}
		if len(rows) < SearchVectorBatchSize {
			break
		}
		last := rows[len(rows)-1]
		after = &SearchVectorPosition{Distance: last.Distance, Fingerprint: last.Embedding.ItemFingerprint}
	}
	if len(all) != 402 {
		t.Fatalf("keyset lost equal-distance tail: %d", len(all))
	}
	for i, id := range all {
		if id != fmt.Sprintf("%064x", i+2) {
			t.Fatal("unstable tie", i, id)
		}
	}
	current, err := repo.SearchMaterial(ctx, epoch, 7)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Exec("UPDATE manager.embeddings SET updated_at=now() WHERE tenant_id=7").Error; err != nil {
		t.Fatal(err)
	}
	unchanged, err := repo.SearchMaterial(ctx, epoch, 7)
	if err != nil || unchanged.VectorRevision != current.VectorRevision {
		t.Fatal("statistics-only update changed ranking basis", current, unchanged, err)
	}
	if err := tx.Exec("UPDATE manager.embeddings SET status='failed' WHERE item_fingerprint=?", all[0]).Error; err != nil {
		t.Fatal(err)
	}
	changed, err := repo.SearchMaterial(ctx, epoch, 7)
	if err != nil || changed.VectorRevision != current.VectorRevision+1 {
		t.Fatal("vector membership did not change basis", current, changed, err)
	}
}
