package scanprocessor

import (
	"context"

	commonClient "github.com/addp/common/client"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
)

func (p Processor) cleanComponentIndexes(ctx context.Context, input input, removed []models.MetaItem) error {
	if len(removed) == 0 || p.indexer == nil {
		return nil
	}
	progress := scanflow.FileProgressFrom(ctx)
	progress.Operation("cleaning_index", input.PhysicalPath)
	progress.Update(func(s *models.ScanProgress) { s.IndexCleanupTotal += int64(len(removed)) })
	for start := 0; start < len(removed); start += commonClient.ManagerContentBatchLimit {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := min(len(removed), start+commonClient.ManagerContentBatchLimit)
		ids := make([]string, 0, end-start)
		for _, item := range removed[start:end] {
			ids = append(ids, item.Fingerprint)
		}
		err := p.indexer.DeleteItemsFromIndex(ctx, input.TenantID, input.EngineID, ids)
		progress.Update(func(s *models.ScanProgress) {
			if err == nil {
				s.IndexCleanupCompleted += int64(len(ids))
			} else {
				s.IndexCleanupFailed += int64(len(ids))
			}
		})
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	return nil
}
