package models

import (
	"math"
	"time"
)

// ListPage is bounded management pagination, not a business-data cursor.
type ListPage struct {
	Page     int
	PageSize int
}

func (p ListPage) Valid() bool {
	return p.Page > 0 && p.PageSize > 0 && p.PageSize <= 100 && (p.Page-1) <= math.MaxInt32/p.PageSize
}

func (p ListPage) Offset() int { return (p.Page - 1) * p.PageSize }

// RevisionSummary deliberately excludes the potentially large definition and
// all execution authorization / lease facts. It is not an executable snapshot.
type RevisionSummary struct {
	OntologyID         string     `json:"ontology_id"`
	Revision           uint64     `json:"revision"`
	Version            uint64     `json:"version"`
	Status             string     `json:"status"`
	Digest             string     `json:"digest"`
	InitialGeneration  *string    `json:"initial_generation" gorm:"column:generation"`
	InitialExecutionID *string    `json:"initial_execution_id" gorm:"column:build_execution_id"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	PublishedAt        *time.Time `json:"published_at"`
}
