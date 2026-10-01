package engineaccess

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

func TestDelegationInputRejectsInvalidIdentityReasonAndMissingExpiry(t *testing.T) {
	s := NewService(nil, nil) // Invalid input must fail before any DB call.
	actor := Actor{TenantID: 1, PrincipalID: 2, MembershipID: 3, AuthorizationVersion: 4, TokenExpiresAt: time.Now().Add(time.Hour)}
	for _, reason := range []string{"", " \t\n", strings.Repeat("责", 2001)} {
		_, err := s.Create(context.Background(), CreateInput{Actor: actor, EngineID: 1, TenantMembershipID: 2, Reason: reason})
		if !errors.Is(err, commonapi.ErrBadRequest) {
			t.Fatalf("invalid reason error=%v", err)
		}
	}
	if !validReason(strings.Repeat("责", 2000)) {
		t.Fatal("reason limit must count characters, not bytes")
	}
	for _, change := range []func(*Actor){
		func(a *Actor) { a.TenantID = 0 }, func(a *Actor) { a.PrincipalID = 0 },
		func(a *Actor) { a.MembershipID = 0 }, func(a *Actor) { a.AuthorizationVersion = 0 },
		func(a *Actor) { a.TokenExpiresAt = time.Time{} },
	} {
		invalid := actor
		change(&invalid)
		_, err := s.Create(context.Background(), CreateInput{Actor: invalid, EngineID: 1, TenantMembershipID: 2, Reason: "explicit"})
		if !errors.Is(err, commonapi.ErrBadRequest) {
			t.Fatalf("invalid actor error=%v", err)
		}
	}
	if _, err := s.Create(context.Background(), CreateInput{Actor: actor, EngineID: 1, TenantMembershipID: 2, Reason: "explicit"}); !errors.Is(err, ErrExpiry) {
		t.Fatalf("missing expiry error=%v", err)
	}
	if _, err := s.Revoke(context.Background(), RevokeInput{Actor: actor, EngineID: 1, ID: 2, Reason: "explicit"}); !errors.Is(err, commonapi.ErrBadRequest) {
		t.Fatalf("missing version error=%v", err)
	}
}

func TestDelegationRepositoryMapsOnlyKnownConflicts(t *testing.T) {
	if !errors.Is(mapError(gorm.ErrRecordNotFound), commonapi.ErrNotFound) {
		t.Fatal("not found mapping")
	}
	if !errors.Is(mapError(&pgconn.PgError{Code: "23505", ConstraintName: "engine_access_delegations_overlap"}), ErrOverlap) {
		t.Fatal("overlap mapping")
	}
	if !errors.Is(mapError(&pgconn.PgError{Code: "23514", ConstraintName: "engine_access_delegations_expiry"}), ErrExpiry) ||
		!errors.Is(mapError(&pgconn.PgError{Code: "23514", ConstraintName: "engine_access_delegations_expired_history"}), ErrExpired) {
		t.Fatal("expiry after a lock wait must return a domain error")
	}
	unknown := &pgconn.PgError{Code: "23514", ConstraintName: "other_constraint"}
	if mapError(unknown) != unknown {
		t.Fatal("unknown constraint must not masquerade as a permitted domain conflict")
	}
}
