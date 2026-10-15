package scantask

import (
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"testing"
	"time"
)

type captureProgressUpdater struct{ fields []map[string]interface{} }

func (u *captureProgressUpdater) UpdateExecutionProgress(_ string, _ int, fields map[string]interface{}) {
	u.fields = append(u.fields, fields)
}
func TestFileProgressPersistsDuringWorkAndCannotBeReplacedByTargetCompletion(t *testing.T) {
	updater := &captureProgressUpdater{}
	reporter := NewExecProgressReporter(updater, "execution", 1)
	defer reporter.Close()
	snapshot := models.ScanProgress{Phase: "processing", Operation: "hashing", ItemsPlanned: 10, ItemsTotalKnown: true, CurrentPath: "3d/model.obj", CurrentBytesTotal: 1000, LastProgressAt: time.Now().UTC()}
	reporter.Report(snapshot)
	for i := 1; i <= 10; i++ {
		snapshot.CurrentBytesRead = int64(i)
		reporter.Report(snapshot)
	}
	if len(updater.fields) != 1 {
		t.Fatalf("unbounded byte writes: %d", len(updater.fields))
	}
	reporter.lastFlushTime = time.Now().Add(-time.Second)
	snapshot.CurrentBytesRead = 50
	reporter.Report(snapshot)
	if len(updater.fields) != 2 {
		t.Fatal("byte change did not persist while item still running")
	}
	p := models.ObserveScanProgress(updater.fields[1]["metadata"].(commonModels.JSONMap))
	if p == nil || p.CurrentBytesRead != 50 || p.ItemsCompleted != 0 {
		t.Fatalf("read telemetry: %+v", p)
	}
	reporter.Advance("3d", 1, 1, map[string]interface{}{"items": 10})
	reporter.Message("outer target complete")
	if len(updater.fields) != 2 {
		t.Fatal("outer target overwrote inner file progress")
	}
	snapshot.Phase = "finalizing"
	snapshot.ItemsCompleted = 10
	snapshot.ItemsSucceeded = 10
	reporter.Report(snapshot)
	if len(updater.fields) != 3 || updater.fields[2]["progress"] != 99 {
		t.Fatalf("final snapshot did not flush: %+v", updater.fields)
	}
}

type progressChannelUpdater struct{ updates chan map[string]interface{} }

func (u progressChannelUpdater) UpdateExecutionProgress(_ string, _ int, fields map[string]interface{}) {
	u.updates <- fields
}
func TestBlockedReadStillPublishesPendingOperationWithoutFakeHeartbeats(t *testing.T) {
	updates := make(chan map[string]interface{}, 10)
	reporter := NewExecProgressReporter(progressChannelUpdater{updates}, "execution", 1)
	defer reporter.Close()
	at := time.Now().UTC()
	snapshot := models.ScanProgress{Phase: "processing", Operation: "enriching", CurrentPath: "previous.obj", LastProgressAt: at}
	reporter.Report(snapshot)
	<-updates
	snapshot.Operation = "hashing"
	snapshot.CurrentPath = "large.obj"
	snapshot.CurrentBytesTotal = 1 << 30
	snapshot.LastProgressAt = at.Add(time.Millisecond)
	reporter.Report(snapshot)
	// No further Report call: the underlying OpenContent/Read is blocked.
	select {
	case fields := <-updates:
		p := models.ObserveScanProgress(fields["metadata"].(commonModels.JSONMap))
		if p == nil || p.Operation != "hashing" || p.CurrentPath != "large.obj" || !p.LastProgressAt.Equal(snapshot.LastProgressAt) {
			t.Fatalf("pending operation lost: %+v", p)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("pending operation was never flushed")
	}
	select {
	case <-updates:
		t.Fatal("unchanged work emitted a fake heartbeat")
	case <-time.After(1100 * time.Millisecond):
	}
}
