package models

import "time"

type LineageGraphRequest struct {
	SubjectKind string
	ItemID      *uint
	ServiceID   *uint
	Revision    string
	Direction   string
	Depth       int
	Limit       int
	AsOf        *time.Time
}

type LineageServiceDependencyInput struct {
	SourceItemID     uint                   `json:"source_item_id"`
	DependencyKind   string                 `json:"dependency_kind"`
	Granularity      string                 `json:"granularity,omitempty"`
	DependencyFields map[string]interface{} `json:"dependency_fields,omitempty"`
}

type RecordServicePublicationRequest struct {
	ServiceID         uint                            `json:"service_id"`
	PublishedRevision string                          `json:"published_revision"`
	DependencyHash    string                          `json:"dependency_hash,omitempty"`
	Dependencies      []LineageServiceDependencyInput `json:"dependencies"`
}

type LineageGraphResponse struct {
	Subject   LineageNode   `json:"subject"`
	Nodes     []LineageNode `json:"nodes"`
	Edges     []LineageEdge `json:"edges"`
	Truncated bool          `json:"truncated"`
	AsOf      *time.Time    `json:"as_of,omitempty"`
}

type LineageErrorResponse struct {
	Error     string `json:"error"`
	ErrorCode string `json:"error_code,omitempty"`
}

type LineageNode struct {
	Kind              string `json:"kind"`
	ItemID            *uint  `json:"item_id,omitempty"`
	ItemFingerprint   string `json:"item_fingerprint,omitempty"`
	EngineID          *uint  `json:"engine_id,omitempty"`
	EngineName        string `json:"engine_name,omitempty"`
	ItemType          string `json:"item_type,omitempty"`
	Name              string `json:"name,omitempty"`
	FullName          string `json:"full_name,omitempty"`
	ServiceID         *uint  `json:"service_id,omitempty"`
	PublishedRevision string `json:"published_revision,omitempty"`
}

type LineageEdge struct {
	Source         LineageNode            `json:"source"`
	Target         LineageNode            `json:"target"`
	RelationKind   string                 `json:"relation_kind"`
	Granularity    string                 `json:"granularity"`
	Evidence       map[string]interface{} `json:"evidence,omitempty"`
	Status         string                 `json:"status"`
	LastObservedAt time.Time              `json:"last_observed_at"`
}
