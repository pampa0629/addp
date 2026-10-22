// Package domainreference defines the read-only, evidence-preserving page contract
// shared by professional owners. Callers own their SQL and authorization.
package domainreference

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"gorm.io/gorm"
)

var ErrInvalidQuery = errors.New("invalid domain standard reference query")

type Evidence struct {
	OwnerModule        string `json:"owner_module"`
	ResourceType       string `json:"resource_type"`
	ResourceID         string `json:"resource_id"`
	ResourceName       string `json:"resource_name"`
	Position           string `json:"position"`
	RevisionID         string `json:"revision_id,omitempty"`
	ResourceRevisionID string `json:"resource_revision_id,omitempty"`
	State              string `json:"state"`
	RelationKind       string `json:"relation_kind"`
}
type Reference struct {
	SourceType        string     `json:"source_type"`
	SourceIdentity    string     `json:"source_identity"`
	EvidenceTruncated bool       `json:"evidence_truncated"`
	Evidence          []Evidence `gorm:"type:jsonb;serializer:json" json:"evidence"`
}
type Page struct {
	Data       []Reference `json:"data"`
	Total      int64       `json:"total"`
	Page       int         `json:"page"`
	PageSize   int         `json:"page_size"`
	TotalPages int         `json:"total_pages"`
}

// ReadPage groups a caller-authorized local query by stable target. Evidence is
// aggregated only for this page; no unbounded application-side enumeration.
func ReadPage(ctx context.Context, db *gorm.DB, evidenceQuery *gorm.DB, page, pageSize int) (*Page, error) {
	if page < 1 || pageSize < 1 || pageSize > 100 || page > 1000000 {
		return nil, ErrInvalidQuery
	}
	result := &Page{Data: []Reference{}, Page: page, PageSize: pageSize}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Raw(`WITH evidence AS (?) SELECT COUNT(*) FROM (SELECT source_type,source_identity FROM evidence GROUP BY source_type,source_identity) targets`, evidenceQuery).Scan(&result.Total).Error; err != nil {
			return err
		}
		if result.Total > 0 {
			result.TotalPages = int((result.Total + int64(pageSize) - 1) / int64(pageSize))
		}
		return tx.Raw(`WITH evidence AS (?), targets AS (
   SELECT source_type,source_identity FROM evidence GROUP BY source_type,source_identity ORDER BY source_type,source_identity LIMIT ? OFFSET ?),
   selected AS (SELECT DISTINCT e.* FROM targets JOIN evidence e USING(source_type,source_identity)),
   ranked AS (SELECT selected.*,ROW_NUMBER() OVER (PARTITION BY source_type,source_identity ORDER BY owner_module,resource_type,resource_id,position,revision_id,resource_revision_id) AS evidence_rank FROM selected)
   SELECT source_type,source_identity,MAX(evidence_rank)>100 AS evidence_truncated,
    jsonb_agg(jsonb_build_object('owner_module',owner_module,'resource_type',resource_type,'resource_id',resource_id,
     'resource_name',resource_name,'position',position,'revision_id',revision_id,'resource_revision_id',resource_revision_id,'state',state,'relation_kind',relation_kind) ORDER BY evidence_rank) FILTER (WHERE evidence_rank<=100) AS evidence
   FROM ranked GROUP BY source_type,source_identity ORDER BY source_type,source_identity`, evidenceQuery, pageSize, (page-1)*pageSize).Scan(&result.Data).Error
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("read domain standard reference page: %w", err)
	}
	return result, nil
}
