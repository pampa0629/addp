package engineaccess

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	shared "github.com/addp/common/authorization"
	"github.com/google/uuid"
)

func TestDenyReleaseExpiryAndIdentity(t *testing.T) {
	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name string
		mode string
		date *time.Time
		err  error
	}{
		{"long-term", shared.SharingExpiryUntilRevoked, nil, nil},
		{"long-term with date", shared.SharingExpiryUntilRevoked, &now, commonapi.ErrBadRequest},
		{"missing date", shared.SharingExpiryAtTime, nil, commonapi.ErrBadRequest},
		{"unknown", "permanent", nil, commonapi.ErrBadRequest},
		{"at expiry", shared.SharingExpiryAtTime, &now, ErrDenyReleaseExpired},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := checkDenyReleaseExpiry(SourceDeny{ExpiryMode: tc.mode, ExpiresAt: tc.date}, now)
			if tc.err == commonapi.ErrBadRequest {
				if err == nil || errors.Is(err, ErrDenyReleaseExpired) {
					t.Fatalf("malformed history misclassified: %v", err)
				}
			} else if !errors.Is(err, tc.err) {
				t.Fatalf("expiry=%v, want %v", err, tc.err)
			}
		})
	}
	deny := SourceDeny{ExpiryMode: shared.SharingExpiryAtTime, ExpiresAt: &now}
	if err := checkDenyReleaseExpiry(deny, now.Add(-time.Nanosecond)); err != nil {
		t.Fatalf("before boundary=%v", err)
	}
	if err := checkDenyReleaseExpiry(deny, now.Add(time.Nanosecond)); !errors.Is(err, ErrDenyReleaseExpired) {
		t.Fatalf("after boundary=%v", err)
	}
	input := ReleaseDenyInput{DenyID: uuid.New(), Actor: Actor{PrincipalID: 9007199254740993, MembershipID: 9007199254740995}, Reason: "  Remove restriction\n"}
	row := &DenyRelease{DenyID: input.DenyID, ReleasedByPrincipalID: input.Actor.PrincipalID, ReleasedByMembershipID: input.Actor.MembershipID, Reason: "Remove restriction", ReleasedAt: now}
	if !sameDenyRelease(row, input) {
		t.Fatal("normalized retry rejected")
	}
	for name, mutate := range map[string]func(*ReleaseDenyInput){
		"id":         func(v *ReleaseDenyInput) { v.DenyID = uuid.New() },
		"actor":      func(v *ReleaseDenyInput) { v.Actor.PrincipalID++ },
		"membership": func(v *ReleaseDenyInput) { v.Actor.MembershipID++ },
		"reason":     func(v *ReleaseDenyInput) { v.Reason = "different" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := input
			mutate(&changed)
			if sameDenyRelease(row, changed) {
				t.Fatal("changed release matched history")
			}
		})
	}
	encoded, err := json.Marshal(row)
	var fields map[string]any
	if err != nil || json.Unmarshal(encoded, &fields) != nil || fields["released_by_principal_id"] != "9007199254740993" || fields["released_by_membership_id"] != "9007199254740995" {
		t.Fatalf("lost precision: %s %v", encoded, err)
	}
	if _, exists := fields["allowed"]; exists {
		t.Fatal("history claimed access")
	}
}
