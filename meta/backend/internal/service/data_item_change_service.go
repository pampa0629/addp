package service

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	metaErrors "github.com/addp/meta/internal/errors"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

const (
	defaultDataItemChangeLimit = 200
	maxDataItemChangeLimit     = 500
)

type DataItemChangeService struct {
	db *gorm.DB
}

type dataItemChangeRow struct {
	ID             int64
	Operation      string
	SourceIdentity string
	Snapshot       models.JSONMap
	ObservedAt     time.Time
}

func NewDataItemChangeService(db *gorm.DB) *DataItemChangeService {
	return &DataItemChangeService{db: db}
}

func (s *DataItemChangeService) List(tenantID uint, afterCursor string, limit int) (*models.DataItemChangesResponse, error) {
	if tenantID == 0 {
		return nil, metaErrors.ErrInvalidTenantID
	}
	afterID, err := DecodeDataItemChangeCursor(afterCursor)
	if err != nil {
		return nil, err
	}
	if limit == 0 {
		limit = defaultDataItemChangeLimit
	}
	if limit < 1 || limit > maxDataItemChangeLimit {
		return nil, fmt.Errorf("%w: limit must be between 1 and %d", metaErrors.ErrInvalidChangeRequest, maxDataItemChangeLimit)
	}

	var rows []dataItemChangeRow
	if err := s.db.Raw(`
		SELECT id, operation, source_identity, snapshot, observed_at
		FROM meta.data_item_changes
		WHERE tenant_id = ? AND id > ?
		ORDER BY id ASC
		LIMIT ?
	`, tenantID, afterID, limit+1).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list data item changes: %w", err)
	}

	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	changes := make([]models.DataItemChange, 0, len(rows))
	nextID := afterID
	for _, row := range rows {
		changeID := EncodeDataItemChangeCursor(row.ID)
		changes = append(changes, models.DataItemChange{
			ChangeID:       changeID,
			Operation:      row.Operation,
			SourceIdentity: row.SourceIdentity,
			SourceVersion:  fmt.Sprintf("%020d", row.ID),
			ObservedAt:     row.ObservedAt,
			Snapshot:       row.Snapshot,
		})
		nextID = row.ID
	}

	return &models.DataItemChangesResponse{
		SchemaVersion: models.DataItemChangesSchemaVersion,
		Changes:       changes,
		NextCursor:    EncodeDataItemChangeCursor(nextID),
		HasMore:       hasMore,
	}, nil
}

func EncodeDataItemChangeCursor(id int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(id, 10)))
}

func DecodeDataItemChangeCursor(cursor string) (int64, error) {
	cursor = strings.TrimSpace(cursor)
	if cursor == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, fmt.Errorf("%w: malformed after_cursor", metaErrors.ErrInvalidChangeCursor)
	}
	id, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil || id < 0 || strconv.FormatInt(id, 10) != string(raw) {
		return 0, fmt.Errorf("%w: malformed after_cursor", metaErrors.ErrInvalidChangeCursor)
	}
	return id, nil
}
