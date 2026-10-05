package projectionstore

import (
	"context"
	"errors"
	"time"

	"github.com/addp/common/dataprotection"
	"gorm.io/gorm"
)

var ErrVersionChanged = errors.New("protection projection version changed; execute again")

// GateReader exposes only protection facts, not projection mutation or grants.
type GateReader interface {
	Gate(int64, dataprotection.ResourceReference, time.Time) GateResult
}

// Version is an opaque, tenant- and owner-bound durable checkpoint observation.
// It is not a source version, an authorization token, or a serializable grant.
type Version struct {
	tenantID int64
	schema   string
	owner    string
	cursor   string
}

// CaptureVersion reads the checkpoint and protection facts under the same lock
// used by ApplyBatch. Long-running owner work starts only after this returns.
func (s *Store) CaptureVersion(ctx context.Context, tenantID int64, observe func(GateReader) error) (Version, error) {
	var version Version
	if s == nil || tenantID <= 0 || observe == nil {
		return version, errors.New("protection version observation requires store, tenant and callback")
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		checkpoint, err := s.lockCheckpoint(tx, tenantID)
		if err != nil {
			return err
		}
		gate, err := s.transactionGate(tx, tenantID)
		if err != nil {
			return err
		}
		if err := observe(gate); err != nil {
			return err
		}
		version = Version{tenantID: tenantID, schema: s.schema, owner: s.consumerOwner, cursor: checkpoint.Cursor}
		return nil
	})
	if err != nil {
		return Version{}, err
	}
	return version, nil
}

// CommitVersion serializes an owner database write with projection installation
// and cleanup. The callback must use tx, revalidate current time-dependent
// protection, and perform database work only; external I/O is not atomic here.
func (s *Store) CommitVersion(ctx context.Context, tenantID int64, version Version, commit func(*gorm.DB, GateReader) error) error {
	if s == nil || tenantID <= 0 || version.tenantID != tenantID || version.schema != s.schema || version.owner != s.consumerOwner || commit == nil {
		return errors.New("protection version commit requires matching store, tenant, version and callback")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		checkpoint, err := s.lockCheckpoint(tx, tenantID)
		if err != nil {
			return err
		}
		if checkpoint.Cursor != version.cursor {
			return ErrVersionChanged
		}
		gate, err := s.transactionGate(tx, tenantID)
		if err != nil {
			return err
		}
		return commit(tx, gate)
	})
}

func (s *Store) transactionGate(tx *gorm.DB, tenantID int64) (GateReader, error) {
	var rows []projectionRow
	if err := tx.Table(s.entriesTable).Where("tenant_id = ?", tenantID).Find(&rows).Error; err != nil {
		return nil, err
	}
	gate := &Store{byResource: make(map[resourceKey][]dataprotection.Projection)}
	if err := gate.replaceRows(rows, 0); err != nil {
		return nil, err
	}
	return gate, nil
}
