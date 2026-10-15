package scanflow

import (
	"context"
	"sync"
	"time"

	"github.com/addp/meta/internal/models"
)

type fileProgressKey struct{}

// FileProgress belongs to one execution and is shared by discovery, recursion and content I/O.
type FileProgress struct {
	mu       sync.Mutex
	reporter ProgressReporter
	snapshot models.ScanProgress
}

func WithFileProgress(ctx context.Context, reporter ProgressReporter) (context.Context, *FileProgress) {
	p := &FileProgress{reporter: reporter}
	return context.WithValue(ctx, fileProgressKey{}, p), p
}

func FileProgressFrom(ctx context.Context) *FileProgress {
	p, _ := ctx.Value(fileProgressKey{}).(*FileProgress)
	return p
}

func (p *FileProgress) Update(change func(*models.ScanProgress)) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	before := p.snapshot
	change(&p.snapshot)
	if before == p.snapshot {
		return
	}
	p.snapshot.LastProgressAt = time.Now().UTC()
	if p.reporter != nil {
		p.reporter.Report(p.snapshot)
	}
}

func (p *FileProgress) Operation(operation, path string) {
	p.Update(func(s *models.ScanProgress) {
		if s.Operation != operation || s.CurrentPath != path {
			s.CurrentBytesRead = 0
			s.CurrentBytesTotal = 0
			s.CurrentRangeBytesRead = 0
			s.CurrentRangesRead = 0
		}
		s.Operation, s.CurrentPath = operation, path
	})
}

func (p *FileProgress) ItemDone(success bool) {
	p.Update(func(s *models.ScanProgress) {
		s.ItemsCompleted++
		if success {
			s.ItemsSucceeded++
		} else {
			s.ItemsFailed++
		}
	})
}
