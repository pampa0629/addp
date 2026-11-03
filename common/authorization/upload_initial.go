package authorization

import (
	"errors"
	"time"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/google/uuid"
)

const UploadInitialSource = "upload_creation"

// UploadInitialResult is immutable issuance/refusal history. It is not a live
// access verdict, and a replay cannot restore a withdrawn rule.
type UploadInitialResult struct {
	TenantID        int64      `json:"tenant_id,string" swaggertype:"string"`
	UploadID        string     `json:"upload_id"`
	ItemFingerprint string     `json:"item_fingerprint"`
	Outcome         string     `json:"outcome"`
	Action          string     `json:"action,omitempty"`
	RequestID       *uuid.UUID `json:"request_id,omitempty"`
	GrantedAt       *time.Time `json:"granted_at,omitempty"`
	Reason          string     `json:"reason,omitempty"`
}

func (r UploadInitialResult) Validate() error {
	id, err := uuid.Parse(r.UploadID)
	if r.TenantID <= 0 || err != nil || id == uuid.Nil || id.String() != r.UploadID || !authorizationfacts.ValidItemFingerprint(r.ItemFingerprint) {
		return errors.New("invalid upload initial outcome identity")
	}
	if r.Outcome == "issued" {
		if (r.Action != "read" && r.Action != "metadata_read") || r.RequestID == nil || *r.RequestID == uuid.Nil || r.GrantedAt == nil || r.GrantedAt.IsZero() || r.Reason != "" {
			return errors.New("invalid upload Grant history")
		}
		return nil
	}
	if r.Outcome != "denied" || r.RequestID != nil || r.GrantedAt != nil || r.Action != "" {
		return errors.New("invalid upload denial")
	}
	switch r.Reason {
	case "not_new", "boundary_ineligible", "target_history_exists", "uploader_inactive", "engine_changed":
		return nil
	}
	return errors.New("invalid upload denial reason")
}
