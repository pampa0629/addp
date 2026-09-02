package dataprotection

import (
	"errors"
	"strings"
)

const (
	ProjectionChangesSchemaV1 = "security.protection_projection_changes/v1"
	ChangeOperationUpsert     = "upsert"
	ChangeOperationRelease    = "release"
)

type ProjectionRelease struct {
	ProjectionID string            `json:"projection_id"`
	Revision     string            `json:"revision"`
	Target       ResourceReference `json:"target"`
}

type ProjectionChange struct {
	ChangeID   string             `json:"change_id"`
	Operation  string             `json:"operation"`
	Projection *Projection        `json:"projection,omitempty"`
	Release    *ProjectionRelease `json:"release,omitempty"`
}

type ProjectionChangesResponse struct {
	SchemaVersion string             `json:"schema_version"`
	Changes       []ProjectionChange `json:"changes"`
	NextCursor    string             `json:"next_cursor"`
	HasMore       bool               `json:"has_more"`
}

type ProjectionAcknowledgementRequest struct {
	AppliedCursor string `json:"applied_cursor" binding:"required"`
}

func (r ProjectionRelease) Validate() error {
	if strings.TrimSpace(r.ProjectionID) == "" || !revisionPattern.MatchString(r.Revision) {
		return errors.New("invalid protection projection release identity")
	}
	return r.Target.Validate()
}
