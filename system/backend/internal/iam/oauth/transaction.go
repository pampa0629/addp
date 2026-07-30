package oauth

import (
	"context"
	"errors"
	"sync"

	"github.com/addp/system/internal/iam"
	fositestorage "github.com/ory/fosite/storage"
	"gorm.io/gorm"
)

type transactionContextKey struct{}
type transactionAuditContextKey struct{}

type transactionAudit struct {
	mu        sync.Mutex
	event     iam.AuditEvent
	committed bool
}

type transactionState struct {
	db    *gorm.DB
	audit *transactionAudit
	mu    sync.Mutex
	done  bool
}

// WithTransactionAudit attaches a success event to the next Fosite-managed
// Storage transaction. The event is written immediately before commit.
func WithTransactionAudit(ctx context.Context, event iam.AuditEvent) context.Context {
	return context.WithValue(ctx, transactionAuditContextKey{}, &transactionAudit{event: event})
}

func (s *Storage) BeginTX(ctx context.Context) (context.Context, error) {
	if ctx.Value(transactionContextKey{}) != nil {
		return nil, errors.New("OAuth Storage 不支持嵌套事务")
	}
	tx := s.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return nil, tx.Error
	}
	audit, _ := ctx.Value(transactionAuditContextKey{}).(*transactionAudit)
	return context.WithValue(ctx, transactionContextKey{}, &transactionState{db: tx, audit: audit}), nil
}

func (s *Storage) Commit(ctx context.Context) error {
	state, err := transactionFromContext(ctx)
	if err != nil {
		return err
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.done {
		return errors.New("OAuth Storage 事务已经结束")
	}
	if state.audit != nil {
		state.audit.mu.Lock()
		event := state.audit.event
		state.audit.mu.Unlock()
		if err := s.writeAudit(ctx, event); err != nil {
			return err
		}
	}
	if err := state.db.Commit().Error; err != nil {
		state.done = true
		return err
	}
	state.done = true
	if state.audit != nil {
		state.audit.mu.Lock()
		state.audit.committed = true
		state.audit.mu.Unlock()
	}
	return nil
}

func (*Storage) Rollback(ctx context.Context) error {
	state, err := transactionFromContext(ctx)
	if err != nil {
		return err
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.done {
		return errors.New("OAuth Storage 事务已经结束")
	}
	state.done = true
	return state.db.Rollback().Error
}

func (s *Storage) dbFromContext(ctx context.Context) *gorm.DB {
	state, ok := ctx.Value(transactionContextKey{}).(*transactionState)
	if ok && state != nil {
		return state.db.WithContext(ctx)
	}
	return s.db.WithContext(ctx)
}

func transactionFromContext(ctx context.Context) (*transactionState, error) {
	state, ok := ctx.Value(transactionContextKey{}).(*transactionState)
	if !ok || state == nil || state.db == nil {
		return nil, errors.New("OAuth Storage 事务上下文不存在")
	}
	return state, nil
}

func (s *Storage) WriteAudit(ctx context.Context, event iam.AuditEvent) error {
	return s.writeAudit(ctx, event)
}

func (s *Storage) writeAudit(ctx context.Context, event iam.AuditEvent) error {
	return iam.NewAuditWriter(iam.NewRepository(s.dbFromContext(ctx))).Write(ctx, event)
}

func updateTransactionAudit(ctx context.Context, update func(*iam.AuditEvent)) {
	audit, _ := ctx.Value(transactionAuditContextKey{}).(*transactionAudit)
	if audit == nil {
		return
	}
	audit.mu.Lock()
	defer audit.mu.Unlock()
	update(&audit.event)
}

// SetTransactionAuditClientID records the client identity only after Fosite
// has authenticated and resolved it. This keeps confidential-client audits
// accurate without trusting the unverified HTTP Basic username.
func SetTransactionAuditClientID(ctx context.Context, clientID string) {
	updateTransactionAudit(ctx, func(event *iam.AuditEvent) {
		if event.Details == nil {
			event.Details = make(map[string]any, 1)
		}
		event.Details["client_id"] = clientID
	})
}

// TransactionAuditCommitted reports whether a Fosite-managed transaction
// committed the attached audit event and returns a detached event copy.
func TransactionAuditCommitted(ctx context.Context) (iam.AuditEvent, bool) {
	audit, _ := ctx.Value(transactionAuditContextKey{}).(*transactionAudit)
	if audit == nil {
		return iam.AuditEvent{}, false
	}
	audit.mu.Lock()
	defer audit.mu.Unlock()
	event := audit.event
	if event.Details != nil {
		event.Details = make(map[string]any, len(audit.event.Details))
		for key, value := range audit.event.Details {
			event.Details[key] = value
		}
	}
	return event, audit.committed
}

var _ fositestorage.Transactional = (*Storage)(nil)
