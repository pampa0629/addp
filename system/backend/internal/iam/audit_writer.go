package iam

import (
	"context"
	"encoding/json"
	"fmt"

	commonapi "github.com/addp/common/api"
)

type AuditMetadata struct {
	PrincipalID   *int64
	PrincipalType *PrincipalType
	ContextType   *ContextType
	TenantID      *int64
	HTTPMethod    *string
	ResourcePath  *string
	HTTPStatus    *int
	RequestID     *string
	IPAddress     *string
	UserAgent     *string
}

type AuditEvent struct {
	Metadata   AuditMetadata
	EventName  string
	Result     AuditResult
	RiskLevel  AuditRiskLevel
	ModuleName string
	EntityType string
	EntityID   string
	Details    map[string]any
}

type AuditWriter struct {
	repository *Repository
}

func NewAuditWriter(repository *Repository) *AuditWriter {
	return &AuditWriter{repository: repository}
}

func (w *AuditWriter) Write(ctx context.Context, event AuditEvent) error {
	if w == nil || w.repository == nil {
		return fmt.Errorf("%w: audit repository is required", commonapi.ErrBadRequest)
	}
	if event.EventName == "" || event.ModuleName == "" || event.EntityType == "" || event.EntityID == "" {
		return fmt.Errorf("%w: audit event identity is incomplete", commonapi.ErrBadRequest)
	}
	details := event.Details
	if details == nil {
		details = map[string]any{}
	}
	encodedDetails, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("%w: encode audit details: %v", commonapi.ErrBadRequest, err)
	}

	entityType := event.EntityType
	entityID := event.EntityID
	return w.repository.CreateAuditLog(ctx, &AuditLog{
		PrincipalID:   event.Metadata.PrincipalID,
		PrincipalType: event.Metadata.PrincipalType,
		ContextType:   event.Metadata.ContextType,
		TenantID:      event.Metadata.TenantID,
		EventName:     event.EventName,
		Result:        event.Result,
		RiskLevel:     event.RiskLevel,
		ModuleName:    event.ModuleName,
		HTTPMethod:    event.Metadata.HTTPMethod,
		ResourcePath:  event.Metadata.ResourcePath,
		HTTPStatus:    event.Metadata.HTTPStatus,
		RequestID:     event.Metadata.RequestID,
		IPAddress:     event.Metadata.IPAddress,
		UserAgent:     event.Metadata.UserAgent,
		EntityType:    &entityType,
		EntityID:      &entityID,
		Details:       encodedDetails,
	})
}
