package engineaccess

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"time"

	commonapi "github.com/addp/common/api"
	shared "github.com/addp/common/authorization"
	"github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/authorization"
	"github.com/addp/system/internal/iam"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type UploadRuntimeActor struct {
	Actor
	ClientID string
}
type UploadReceiptReader interface {
	ReadUploadReceipt(context.Context, uint, string) (*client.UploadReceipt, error)
}
type UploadIdentificationReader interface {
	ReadUploadIdentification(context.Context, uint, string) (*client.MetaUploadIdentification, error)
}

func (s *Service) WithUploadReaders(receipts UploadReceiptReader, identified UploadIdentificationReader) *Service {
	s.uploadReceipts = receipts
	s.uploadIdentification = identified
	return s
}

type uploadGrantOutcome struct {
	TenantID           int64     `gorm:"primaryKey"`
	UploadID           uuid.UUID `gorm:"type:uuid;primaryKey"`
	ItemFingerprint    string    `gorm:"primaryKey"`
	Outcome            string
	RecordedAt         time.Time
	GrantRequestID     *uuid.UUID `gorm:"type:uuid"`
	DenialCode         *string
	IssuerPrincipalID  int64
	IssuerMembershipID int64
}

func (uploadGrantOutcome) TableName() string { return "system.engine_access_upload_grant_outcomes" }
func (r *Repository) uploadOutcome(ctx context.Context, tenant int64, id uuid.UUID, fp string) (*uploadGrantOutcome, error) {
	var row uploadGrantOutcome
	err := r.db.WithContext(ctx).Where("tenant_id=? AND upload_id=? AND item_fingerprint=?", tenant, id, fp).Take(&row).Error
	return &row, err
}
func (r *Repository) uploadResult(ctx context.Context, row *uploadGrantOutcome) (*shared.UploadInitialResult, error) {
	result := &shared.UploadInitialResult{TenantID: row.TenantID, UploadID: row.UploadID.String(), ItemFingerprint: row.ItemFingerprint, Outcome: row.Outcome}
	if row.Outcome == "issued" {
		if row.GrantRequestID == nil {
			return nil, commonapi.ErrConflict
		}
		g, err := r.findSourceGrant(ctx, *row.GrantRequestID)
		if err != nil {
			return nil, err
		}
		if g.ApprovalMode != shared.UploadInitialSource || g.TenantID != row.TenantID || g.UploadID == nil || *g.UploadID != row.UploadID || g.ItemFingerprint != row.ItemFingerprint || g.IssuerPrincipalID == nil || *g.IssuerPrincipalID != row.IssuerPrincipalID || g.IssuerMembershipID == nil || *g.IssuerMembershipID != row.IssuerMembershipID {
			return nil, commonapi.ErrConflict
		}
		result.RequestID = &g.RequestID
		result.GrantedAt = &g.GrantedAt
		result.Action = g.Action
	} else if row.DenialCode != nil {
		result.Reason = *row.DenialCode
	}
	return result, result.Validate()
}
func (s *Service) withUploadRuntime(ctx context.Context, actor UploadRuntimeActor, op func(*Repository, func() error) error, humans ...int64) error {
	if actor.ClientID != "addp-manager" {
		return commonapi.ErrForbidden
	}
	return s.withSourceRuntime(ctx, actor.Actor, actor.ClientID, authorization.PermissionSystemEngineAccessInitialGrantExecute, op, humans...)
}

// Only the producer's real committed writes and Meta's independently observed
// first logical boundary can seed the canonical Grant. No caller supplies ACLs.
func (s *Service) IssueInitialUploadGrant(ctx context.Context, actor UploadRuntimeActor, id, fp string) (*shared.UploadInitialResult, error) {
	uploadID, err := uuid.Parse(id)
	if err != nil || uploadID==uuid.Nil || uploadID.String()!=id || !authorizationfacts.ValidItemFingerprint(fp) {
		return nil, commonapi.ErrBadRequest
	}
	var result *shared.UploadInitialResult
	err = s.withUploadRuntime(ctx, actor, func(tx *Repository, check func() error) error {
		row, e := tx.uploadOutcome(ctx, actor.TenantID, uploadID, fp)
		if errors.Is(e, gorm.ErrRecordNotFound) {
			return check()
		}
		if e != nil {
			return e
		}
		result, e = tx.uploadResult(ctx, row)
		if e != nil {
			return e
		}
		return check()
	})
	if err != nil || result != nil {
		return result, err
	}
	if s.uploadReceipts == nil || s.uploadIdentification == nil {
		return nil, ErrAuthorizationFactsUnavailable
	}
	receipt, err := s.uploadReceipts.ReadUploadReceipt(ctx, uint(actor.TenantID), id)
	if err != nil {
		return nil, err
	}
	if receipt == nil || receipt.Validate() != nil || receipt.TenantID != actor.TenantID || receipt.UploadID != id {
		return nil, commonapi.ErrForbidden
	}
	requested := false
	created := false
	for _, write := range receipt.Writes {
		candidate, e := authorizationfacts.PrimaryCandidateFingerprint(write.Target)
		if e == nil && candidate == fp {
			requested = true
			created = write.Disposition == "created"
		}
	}
	if !requested {
		return nil, commonapi.ErrForbidden
	}
	denial := ""
	var binding authorizationfacts.Binding
	if !created {
		denial = "not_new"
	} else {
		identified, e := s.uploadIdentification.ReadUploadIdentification(ctx, uint(actor.TenantID), id)
		if e != nil {
			return nil, e
		}
		if identified == nil || identified.Validate() != nil || identified.UploadID != id {
			return nil, ErrAuthorizationFactsUnavailable
		}
		if identified.Status != "success" {
			if identified.Status == "failed" || identified.Status == "cancelled" || identified.Status == "timeout" {
				denial = "boundary_ineligible"
			} else {
				return nil, ErrAuthorizationFactsUnavailable
			}
		}
		if denial == "" {
			for _, item := range identified.NewItems {
				if item.ItemFingerprint == fp {
					binding = item
					break
				}
			}
			if binding.Validate() != nil {
				denial = "not_new"
			}
		}
	}
	if denial == "" {
		primary, e := binding.PrimaryTarget()
		if e != nil || primary.EngineID != receipt.EngineID || binding.Scope != nil {
			return nil, commonapi.ErrConflict
		}
		// Read only exact current owner facts. Metadata-only facts never become read.
		current, e := s.resolveMetadataBindings(ctx, actor.TenantID, []plugin.EngineCatalogPath{primary})
		if e != nil {
			return nil, e
		}
		if len(current) != 1 || !reflect.DeepEqual(current[0], binding) {
			denial = "boundary_ineligible"
		}
		writes := map[string]bool{}
		for _, write := range receipt.Writes {
			writes[write.Target.StringPath()] = write.Disposition == "created"
		}
		if !writes[primary.StringPath()] {
			denial = "not_new"
		}
		if binding.FileComposition != nil {
			for _, ref := range binding.FileComposition.Refs {
				if !writes[ref.Target.StringPath()] {
					denial = "not_new"
				}
			}
		}
	}
	source := userProvenance{receipt.Uploader.PrincipalID, receipt.Uploader.MembershipID, receipt.Uploader.AuthorizationVersion}
	err = s.withUploadRuntime(ctx, actor, func(tx *Repository, runtimeCheck func() error) error {
		human, e := tx.lockUserProvenance(ctx, actor.TenantID, source)
		if e != nil {
			return e
		}
		var engine struct {
			ID, Version                int64
			EngineType, LifecycleState string
		}
		if e := tx.db.WithContext(ctx).Table("system.engines").Select("id,version,engine_type,lifecycle_state").Where("tenant_id=? AND id=?", actor.TenantID, receipt.EngineID).Clauses(clause.Locking{Strength: "SHARE"}).Take(&engine).Error; e != nil {
			return e
		}
		if e := tx.db.WithContext(ctx).Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,0))", fmt.Sprintf("initial_upload|%d|%s|%s", actor.TenantID, id, fp)).Error; e != nil {
			return e
		}
		if row, e := tx.uploadOutcome(ctx, actor.TenantID, uploadID, fp); e == nil {
			result, e = tx.uploadResult(ctx, row)
			if e != nil {
				return e
			}
			return runtimeCheck()
		} else if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		now, e := tx.wallClock(ctx)
		if e != nil {
			return e
		}
		code := denial
		if human.check(now) != nil {
			code = "uploader_inactive"
		}
		permissions, e := tx.identity().ListEffectiveRoleAssignmentPermissions(ctx, source.PrincipalID, iam.PrincipalTypeUser, iam.ContextTypeTenant, &actor.TenantID, &source.MembershipID, now)
		if e != nil {
			return e
		}
		if !hasCurrentTenantPermission(permissions, actor.TenantID, "manager.data_item.create", now) {
			code = "uploader_inactive"
		}
		if engine.EngineType != "nfs" || engine.LifecycleState != "active" || strconv.FormatInt(engine.Version, 10) != receipt.EngineVersion {
			code = "engine_changed"
		}
		var grant *sourceGrant
		if code == "" {
			primary, _ := binding.PrimaryTarget()
			path, e := shared.EncodeSharingTarget(primary)
			if e != nil {
				return e
			}
			if e := tx.lockFulfillmentTarget(ctx, actor.TenantID, path); e != nil {
				return e
			}
			var history bool
			if e := tx.db.WithContext(ctx).Raw(productionTargetHistorySQL, actor.TenantID, int64(receipt.EngineID), fp, string(path)).Scan(&history).Error; e != nil {
				return e
			}
			if history {
				code = "target_history_exists"
			} else {
				requestID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("addp.initial-upload|%d|%s|%s", actor.TenantID, id, fp)))
				grant = &sourceGrant{RequestID: requestID, ApprovalMode: shared.UploadInitialSource, TenantID: actor.TenantID, EngineID: int64(receipt.EngineID), CatalogPath: path, ItemFingerprint: fp, BoundaryHash: binding.BoundaryHash, RecipientType: "user", RecipientID: source.PrincipalID, Action: "read", ExpiryMode: shared.SharingExpiryUntilRevoked, OperatorPrincipalID: source.PrincipalID, OperatorMembershipID: source.MembershipID, OperatorAuthorizationVersion: source.AuthorizationVersion, UploadID: &uploadID, IssuerPrincipalID: &actor.PrincipalID, IssuerMembershipID: &actor.MembershipID}
				if binding.MetadataTarget != nil {
					grant.Action = "metadata_read"
					grant.MetadataTarget, e = json.Marshal(binding.MetadataTarget)
				} else if binding.FileComposition != nil {
					grant.FileComposition, e = json.Marshal(binding.FileComposition)
				} else {
					return commonapi.ErrConflict
				}
				if e != nil {
					return e
				}
			}
		}
		row := &uploadGrantOutcome{TenantID: actor.TenantID, UploadID: uploadID, ItemFingerprint: fp, Outcome: "denied", RecordedAt: now, IssuerPrincipalID: actor.PrincipalID, IssuerMembershipID: actor.MembershipID, DenialCode: &code}
		if grant != nil {
			row.Outcome = "issued"
			row.GrantRequestID = &grant.RequestID
			row.DenialCode = nil
		}
		if e := tx.db.WithContext(ctx).Create(row).Error; e != nil {
			return e
		}
		if grant != nil {
			if e := tx.insertSourceGrant(ctx, grant); e != nil {
				return e
			}
		}
		p, c := iam.PrincipalTypeServicePrincipal, iam.ContextTypeTenant
		if e := iam.NewAuditWriter(tx.identity()).Write(ctx, iam.AuditEvent{Metadata: iam.AuditMetadata{PrincipalID: &actor.PrincipalID, PrincipalType: &p, TenantID: &actor.TenantID, ContextType: &c}, EventName: "system.engine_access_initial_grant." + row.Outcome, Result: iam.AuditResultSucceeded, RiskLevel: iam.AuditRiskHigh, ModuleName: "system", EntityType: "engine_access_initial_grant", EntityID: id, Details: map[string]any{"source": shared.UploadInitialSource, "upload_id": id, "item_fingerprint": fp, "outcome": row.Outcome, "denial_code": code}}); e != nil {
			return e
		}
		if grant != nil {
			now, e = tx.wallClock(ctx)
			if e != nil {
				return e
			}
			if e = human.check(now); e != nil {
				return e
			}
			if !hasCurrentTenantPermission(permissions, actor.TenantID, "manager.data_item.create", now) {
				return commonapi.ErrForbidden
			}
		}
		if e := runtimeCheck(); e != nil {
			return e
		}
		result, e = tx.uploadResult(ctx, row)
		return e
	}, source.PrincipalID)
	return result, err
}
