package protection

import (
	"context"
	"fmt"

	"github.com/addp/common/dataprotection"
)

const QueryHistoryMetadataKey = "protected_query_history"

type queryCaptureKey struct{}
type interactiveQuerySubjectKey struct{}

// WithInteractiveQuerySubject is used only after verifying the Notebook
// session's current-user control-plane authorization.
func WithInteractiveQuerySubject(ctx context.Context, subject dataprotection.SubjectReference) context.Context {
	return context.WithValue(ctx, interactiveQuerySubjectKey{}, subject)
}

// QueryCapture exists only during an execution whose actor was verified by
// its Execution Authorization. It never accepts an actor from query JSON.
type QueryCapture struct {
	Subject dataprotection.SubjectReference
	History *dataprotection.HistoricalQueryResult
}

func WithQueryCapture(ctx context.Context, subject dataprotection.SubjectReference) (context.Context, *QueryCapture) {
	capture := &QueryCapture{Subject: subject}
	return context.WithValue(ctx, queryCaptureKey{}, capture), capture
}

func (g *Gate) ReadQueryHistory(ctx context.Context, tenantID int64, history *dataprotection.HistoricalQueryResult, subject dataprotection.SubjectReference) (*dataprotection.HistoricalQueryReadResult, error) {
	if g == nil || g.store == nil {
		return nil, ErrRequired
	}
	end, err := g.begin(uint(tenantID))
	if err != nil {
		return nil, err
	}
	defer end()
	result, err := g.store.ReadHistoricalQueryResult(ctx, tenantID, history, subject, "query")
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrResultRequired, err)
	}
	return result, nil
}
