package engineaccess

import (
	"context"
	"encoding/json"
	"time"

	shared "github.com/addp/common/authorization"
	"github.com/google/uuid"
)

// A denied result has source and issuer identity even though it has no Grant.
// An issued result is built from the same Grant by the owning service.
type initialSourceOutcome interface {
	sourceResultFields() (sourceGrant, *time.Time, *string)
}

func (o *initialProductionOutcome) sourceResultFields() (sourceGrant, *time.Time, *string) {
	return sourceGrant{
		ApprovalMode: productionApprovalMode, TenantID: o.TenantID, ProducerOwner: &o.ProducerOwner,
		ProductionExecutionID: &o.ExecutionID, ProductionResultID: &o.ResultID,
		IssuerPrincipalID: &o.IssuerPrincipalID, IssuerMembershipID: &o.IssuerMembershipID,
	}, &o.RecordedAt, o.DenialCode
}

func (o *uploadGrantOutcome) sourceResultFields() (sourceGrant, *time.Time, *string) {
	return sourceGrant{
		ApprovalMode: shared.UploadInitialSource, TenantID: o.TenantID, UploadID: &o.UploadID,
		ItemFingerprint: o.ItemFingerprint, IssuerPrincipalID: &o.IssuerPrincipalID, IssuerMembershipID: &o.IssuerMembershipID,
	}, &o.RecordedAt, o.DenialCode
}

// SQL NULL means this kind of target has no corresponding JSON document.
// Send present JSON as text so the MySQL driver does not bind binary strings.
func mysqlGrantJSON(document json.RawMessage) any {
	if len(document) == 0 {
		return nil
	}
	return string(document)
}

func (r *Repository) persistMySQLSourceGrant(ctx context.Context, grant *sourceGrant, outcome initialSourceOutcome) error {
	var fields sourceGrant
	var recordedAt *time.Time
	var denial *string
	if outcome != nil {
		fields, recordedAt, denial = outcome.sourceResultFields()
	}
	var id *uuid.UUID
	if grant != nil {
		fields = *grant
		id = &grant.RequestID
	}
	err := r.db.WithContext(ctx).Exec(`CALL system.persist_source_grant(
		?,?,?,?,?, ?,?,?,?, ?,?,?, ?,?,?, ?,?, ?,?,?,?, ?,?,?, ?,?,?, ?,?)`,
		fields.ApprovalMode, id, fields.TenantID, fields.EngineID, mysqlGrantJSON(fields.CatalogPath),
		fields.CatalogRequestID, fields.NodeRecursive, fields.ItemFingerprint, fields.BoundaryHash,
		mysqlGrantJSON(fields.FileComposition), mysqlGrantJSON(fields.ScopeBoundary), mysqlGrantJSON(fields.MetadataTarget),
		fields.RecipientType, fields.RecipientID, fields.Action, fields.ExpiryMode, fields.ExpiresAt,
		fields.OperatorPrincipalID, fields.OperatorMembershipID, fields.OperatorAuthorizationVersion, fields.Reason,
		fields.ProducerOwner, fields.ProductionExecutionID, fields.ProductionResultID,
		fields.UploadID, fields.IssuerPrincipalID, fields.IssuerMembershipID, recordedAt, denial,
	).Error
	if err != nil || grant == nil {
		return err
	}
	// PG RETURNING includes database time and facts supplied by the Catalog
	// receipt trigger. Read the same row on this transaction's connection.
	stored, err := r.findSourceGrant(ctx, grant.RequestID)
	if err == nil {
		*grant = *stored
	}
	return err
}
