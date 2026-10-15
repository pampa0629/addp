package scanprocessor

import (
	"context"
	"errors"
	"fmt"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
	"testing"
)

type cleanupIndexer struct {
	batches   [][]string
	failBatch int
	onDelete  func()
}

func (i *cleanupIndexer) DeleteItemsFromIndex(ctx context.Context, tenant, engine uint, ids []string) error {
	if tenant != 7 || engine != 14 {
		return errors.New("wrong scope")
	}
	i.batches = append(i.batches, append([]string{}, ids...))
	if i.onDelete != nil {
		i.onDelete()
	}
	if len(i.batches) == i.failBatch {
		return errors.New("delivery unresolved")
	}
	return nil
}
func (*cleanupIndexer) IndexTechnicalMetadata(context.Context, *commonModels.Engine, uint, *models.MetaNode, ...*models.MetaItem) bool {
	return true
}
func (*cleanupIndexer) IndexCatalogContent(context.Context, *commonModels.Engine, uint, *models.MetaItem, string, bool) bool {
	return true
}
func TestComponentIndexCleanupBatchesAndReportsConfirmedOutcomes(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelled), func(t *testing.T) {
			recorder := &hashProgressRecorder{}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ctx, p := scanflow.WithFileProgress(ctx, recorder)
			p.Update(func(s *models.ScanProgress) { s.Phase = "processing" })
			indexer := &cleanupIndexer{failBatch: 2}
			if cancelled {
				indexer.onDelete = cancel
			}
			removed := make([]models.MetaItem, 205)
			for i := range removed {
				removed[i].Fingerprint = fmt.Sprintf("old-%03d", i)
			}
			err := (Processor{indexer: indexer}).cleanComponentIndexes(ctx, input{TenantID: 7, EngineID: 14, PhysicalPath: "3d/whole"}, removed)
			if cancelled {
				if !errors.Is(err, context.Canceled) || len(indexer.batches) != 1 {
					t.Fatalf("cancel error=%v batches=%d", err, len(indexer.batches))
				}
				return
			}
			if err != nil || len(indexer.batches) != 3 || len(indexer.batches[0]) != 100 || len(indexer.batches[2]) != 5 {
				t.Fatalf("not bounded: %v %+v", err, indexer.batches)
			}
			snapshot := recorder.snapshots[len(recorder.snapshots)-1]
			if snapshot.Operation != "cleaning_index" || snapshot.IndexCleanupTotal != 205 || snapshot.IndexCleanupCompleted != 105 || snapshot.IndexCleanupFailed != 100 {
				t.Fatalf("false success: %+v", snapshot)
			}
			sawFirstBatch := false
			for _, s := range recorder.snapshots {
				if s.IndexCleanupCompleted == 100 && s.IndexCleanupFailed == 0 {
					sawFirstBatch = true
				}
			}
			if !sawFirstBatch {
				t.Fatal("no intermediate cleanup progress")
			}
		})
	}
}
