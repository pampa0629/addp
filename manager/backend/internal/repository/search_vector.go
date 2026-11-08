package repository

import (
	"context"
	"fmt"

	"github.com/addp/manager/internal/models"
	"github.com/lib/pq"
)

const SearchVectorBatchSize = 200

type SearchVectorQuery struct {
	TenantID       uint
	EngineID       *uint
	Vector         []float32
	ModelProfileID string
	ProfileVersion int64
	Dimension      int
	MaxDistance    float64
}
type SearchVectorPosition struct {
	Distance    float64
	Fingerprint string
}

// SearchReadyVectorBatch traverses the exact ordered candidate set. Permission
// filtering follows this bounded read, before assigning effective RRF ranks.
func (r *EmbeddingRepository) SearchReadyVectorBatch(ctx context.Context, q SearchVectorQuery, after *SearchVectorPosition) ([]EmbeddingSimilarityResult, error) {
	return r.readSearchVectors(ctx, q, after, nil, SearchVectorBatchSize)
}

// SearchVectorIdentities reads only the bounded current input/output identities;
// this is never the user's complete authorized fingerprint set.
func (r *EmbeddingRepository) SearchVectorIdentities(ctx context.Context, q SearchVectorQuery, ids []string) ([]EmbeddingSimilarityResult, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	return r.readSearchVectors(ctx, q, nil, ids, len(ids))
}

func (r *EmbeddingRepository) readSearchVectors(ctx context.Context, q SearchVectorQuery, after *SearchVectorPosition, ids []string, limit int) ([]EmbeddingSimilarityResult, error) {
	vector := vectorToString(q.Vector)
	distance := "embedding OPERATOR(manager.<=>) ?::manager.vector"
	where := "tenant_id=? AND status='ready' AND model_profile_id=? AND profile_version=? AND dimension=? AND embedding IS NOT NULL"
	args := []any{vector, q.TenantID, q.ModelProfileID, q.ProfileVersion, q.Dimension}
	if q.EngineID != nil {
		where += " AND engine_id=?"
		args = append(args, *q.EngineID)
	}
	if q.MaxDistance > 0 {
		where += " AND " + distance + "<=?"
		args = append(args, vector, q.MaxDistance)
	}
	if after != nil {
		where += " AND (" + distance + `,item_fingerprint COLLATE "C")>(?,?::text COLLATE "C")`
		args = append(args, vector, after.Distance, after.Fingerprint)
	}
	if ids != nil {
		where += " AND item_fingerprint=ANY(?::text[])"
		args = append(args, pq.Array(ids))
	}
	args = append(args, vector, limit)
	query := fmt.Sprintf(`SELECT id,tenant_id,item_fingerprint,item_id,engine_id,locator,source_version,model_profile_id,profile_version,deployment_id,dimension,status,%s AS distance
 FROM manager.embeddings WHERE %s ORDER BY %s,item_fingerprint COLLATE "C" LIMIT ?`, distance, where, distance)
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]EmbeddingSimilarityResult, 0, limit)
	for rows.Next() {
		var emb models.Embedding
		var d float64
		if err := rows.Scan(&emb.ID, &emb.TenantID, &emb.ItemFingerprint, &emb.ItemID, &emb.EngineID, &emb.Locator, &emb.SourceVersion, &emb.ModelProfileID, &emb.ProfileVersion, &emb.DeploymentID, &emb.Dimension, &emb.Status, &d); err != nil {
			return nil, err
		}
		result = append(result, EmbeddingSimilarityResult{Embedding: &emb, Distance: d})
	}
	return result, rows.Err()
}
