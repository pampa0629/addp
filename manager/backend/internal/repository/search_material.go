package repository

import (
	"context"

	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
)

type SearchMaterialRevision struct {
	Epoch             string
	SearchRevision    int64
	NextSearchOrdinal int64
	Generation        string
	VectorRevision    int64
}

// ReserveSearchOrdinals is indexing metadata under the existing outlet lock.
// Existing IDs retain their ordinal; deletion never makes an ordinal reusable.
func (r *ContentIndexDeliveryRepository) ReserveSearchOrdinals(ctx context.Context, tx *gorm.DB, epoch string, ids []string) (map[string]int64, error) {
	state, err := r.lock(tx.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	if state.Epoch != epoch || !state.Configured || state.Isolated || state.CleanupHoldID != "" {
		return nil, ErrContentIndexIsolated
	}
	var rows []models.SearchDocumentOrdinal
	if err := tx.Where("index_name=? AND document_id IN ?", r.index, ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make(map[string]int64, len(ids))
	for _, row := range rows {
		result[row.DocumentID] = row.SearchOrdinal
	}
	for _, id := range ids {
		if result[id] > 0 {
			continue
		}
		state.NextSearchOrdinal++
		row := models.SearchDocumentOrdinal{IndexName: r.index, DocumentID: id, SearchOrdinal: state.NextSearchOrdinal}
		if err := tx.Create(&row).Error; err != nil {
			return nil, err
		}
		result[id] = row.SearchOrdinal
	}
	if err := tx.Model(state).Update("next_search_ordinal", state.NextSearchOrdinal).Error; err != nil {
		return nil, err
	}
	return result, nil
}

func (r *ContentIndexDeliveryRepository) SearchMaterial(ctx context.Context, epoch string, tenant uint) (SearchMaterialRevision, error) {
	var result SearchMaterialRevision
	err := r.db.WithContext(ctx).Raw(`SELECT o.epoch,o.search_revision,o.next_search_ordinal,CAST(g.generation AS text) AS generation,COALESCE(v.version,0) AS vector_revision
 FROM manager.content_index_outlets o CROSS JOIN manager.search_material_generation g
 LEFT JOIN manager.search_vector_revisions v ON v.tenant_id=?
 WHERE o.index_name=? AND o.epoch=? AND o.configured AND NOT o.isolated AND o.cleanup_hold_id='' AND g.singleton
 AND NOT EXISTS(SELECT 1 FROM manager.content_index_deliveries d WHERE d.index_name=o.index_name AND d.status IN ('queued','submitting','submitted','unknown'))`, tenant, r.index, epoch).Scan(&result).Error
	if err != nil {
		return result, err
	}
	if result.Epoch == "" || result.Generation == "" {
		return result, ErrContentIndexIsolated
	}
	return result, nil
}

func (r *ContentIndexDeliveryRepository) WithTransaction(ctx context.Context, apply func(*gorm.DB) error) error {
	return r.db.WithContext(ctx).Transaction(apply)
}
