package api

import (
	"encoding/json"
	"github.com/addp/ontology/internal/models"
	"github.com/addp/ontology/internal/semantic"
	"time"
)

// DefinitionInput intentionally excludes Scope: identity belongs to the route
// and authenticated Tenant, not to an editable semantic document.
type DefinitionInput struct {
	Classes    []semantic.Class    `json:"classes"`
	Properties []semantic.Property `json:"properties"`
	Relations  []semantic.Relation `json:"relations"`
	Rules      []semantic.Rule     `json:"rules"`
}

func (d DefinitionInput) definition(scope semantic.Scope) semantic.Definition {
	return semantic.Definition{Scope: scope, Classes: d.Classes, Properties: d.Properties, Relations: d.Relations, Rules: d.Rules}
}

type CreateRequest struct {
	Revision   uint64          `json:"revision"`
	Definition DefinitionInput `json:"definition"`
}
type SaveRequest struct {
	Version    uint64          `json:"version"`
	Definition DefinitionInput `json:"definition"`
}
type VersionRequest struct {
	Version uint64 `json:"version"`
}
type RebuildRequest struct {
	Version           uint64 `json:"version"`
	FailedGeneration  string `json:"failed_generation"`
	ActivationVersion uint64 `json:"activation_version"`
}
type ErrorResponse struct {
	Error     string            `json:"error"`
	ErrorCode string            `json:"error_code"`
	Intent    *AcceptedResponse `json:"intent,omitempty"`
}
type AcceptedResponse struct {
	OntologyID  string `json:"ontology_id"`
	Revision    uint64 `json:"revision"`
	Version     uint64 `json:"version"`
	Generation  string `json:"generation"`
	ExecutionID string `json:"execution_id"`
}
type HeadResponse struct {
	OntologyID        string  `json:"ontology_id"`
	LastRevision      uint64  `json:"last_revision"`
	ActivationVersion uint64  `json:"activation_version"`
	ActiveRevision    *uint64 `json:"active_revision"`
	ActiveGeneration  *string `json:"active_generation"`
}

func headResponse(r *models.Ontology) HeadResponse {
	return HeadResponse{r.OntologyID, r.LastRevision, r.ActivationVersion, r.ActiveRevision, r.ActiveGeneration}
}

type RevisionResponse struct {
	OntologyID         string          `json:"ontology_id"`
	Revision           uint64          `json:"revision"`
	Version            uint64          `json:"version"`
	Status             string          `json:"status"`
	Snapshot           json.RawMessage `json:"snapshot" swaggertype:"object"`
	Digest             string          `json:"digest"`
	InitialGeneration  *string         `json:"initial_generation"`
	InitialExecutionID *string         `json:"initial_execution_id"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
	PublishedAt        *time.Time      `json:"published_at"`
}

func revisionResponse(r *models.Revision) RevisionResponse {
	return RevisionResponse{r.OntologyID, r.Revision, r.Version, r.Status, json.RawMessage(r.Payload), r.Digest, r.Generation, r.BuildExecutionID, r.CreatedAt, r.UpdatedAt, r.PublishedAt}
}

type ProjectionResponse struct {
	OntologyID            string  `json:"ontology_id"`
	Revision              uint64  `json:"revision"`
	Generation            string  `json:"generation"`
	PredecessorGeneration *string `json:"predecessor_generation"`
	ExecutionID           string  `json:"execution_id"`
	Digest                string  `json:"digest"`
	Status                string  `json:"status"`
	BaselineVersion       uint64  `json:"baseline_version"`
}

func projectionResponse(r *models.Projection) ProjectionResponse {
	return ProjectionResponse{r.OntologyID, r.Revision, r.Generation, r.PredecessorGeneration, r.ExecutionID, r.Digest, r.Status, r.BaselineVersion}
}
