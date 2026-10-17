package service

import (
	"context"
	"strings"

	commonClient "github.com/addp/common/client"
)

// Display observations never change an immutable decision or its recipients.
type SharingRecipientDisplay struct {
	RecipientName        string `json:"recipient_name,omitempty"`
	RecipientCode        string `json:"recipient_code,omitempty"`
	RecipientLabelStatus string `json:"recipient_label_status" enums:"resolved,not_found,unavailable"`
}

func (s *EntryService) resolveSharingRecipientDisplays(ctx context.Context, tenantID int64, refs []commonClient.SystemCatalogReference) []SharingRecipientDisplay {
	labels := make([]SharingRecipientDisplay, len(refs))
	for i := range labels {
		labels[i].RecipientLabelStatus = "unavailable"
	}
	if s.system == nil {
		return labels
	}
	// The history page can contain 100 rows; resolve only those exact identities.
	if len(refs) == 0 {
		return labels
	}
	resolved, err := s.system.ResolveSystemReferences(ctx, tenantID, refs)
	if err != nil || len(resolved) != len(refs) {
		return labels
	}
	for i, ref := range resolved {
		if ref.SubjectType != refs[i].SubjectType || ref.ID != refs[i].ID || (ref.Found && strings.TrimSpace(ref.Name) == "") {
			return labels
		}
	}
	for i, ref := range resolved {
		labels[i].RecipientLabelStatus = "not_found"
		if ref.Found {
			labels[i] = SharingRecipientDisplay{RecipientLabelStatus: "resolved", RecipientName: ref.Name, RecipientCode: ref.Code}
		}
	}
	return labels
}
