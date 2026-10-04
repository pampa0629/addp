package engineaccess

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	shared "github.com/addp/common/authorization"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestGrantRevocationRejectsInvalidCommandBeforeDatabase(t *testing.T) {
	service := NewService(NewRepository(nil), nil)
	for _, input := range []RevokeGrantInput{
		{Reason: "missing Grant identity"},
		{RequestID: uuid.New(), Reason: " "},
		{RequestID: uuid.New(), Reason: strings.Repeat("字", 2001)},
		{RequestID: uuid.New(), Reason: "no authenticated actor"},
	} {
		if result, err := service.RevokeGrant(context.Background(), input); result != nil || !errors.Is(err, commonapi.ErrBadRequest) {
			t.Fatalf("invalid input reached database: %+v %v", result, err)
		}
	}
}

func TestGrantRevocationUsesNaturalExpiryNotAcceptanceWindow(t *testing.T) {
	expires := time.Date(2026, 10, 3, 0, 10, 0, 0, time.UTC)
	window := expires.Add(-5 * time.Minute)
	outcome := fulfillmentOutcome{ExpiryMode: shared.SharingExpiryAtTime, GrantExpiresAt: &expires, Deadline: &window}
	for _, tc := range []struct {
		name string
		now  time.Time
		want error
	}{
		{"after acceptance window", expires.Add(-time.Minute), nil},
		{"just before natural expiry", expires.Add(-time.Nanosecond), nil},
		{"at natural expiry", expires, ErrGrantRevocationExpired},
		{"after natural expiry", expires.Add(time.Nanosecond), ErrGrantRevocationExpired},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := checkGrantRevocationExpiry(outcome, tc.now); !errors.Is(err, tc.want) {
				t.Fatalf("expiry=%v want=%v", err, tc.want)
			}
		})
	}
	outcome.ExpiryMode, outcome.GrantExpiresAt = shared.SharingExpiryUntilRevoked, nil
	if err := checkGrantRevocationExpiry(outcome, expires.Add(100*365*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []fulfillmentOutcome{
		{}, {ExpiryMode: shared.SharingExpiryAtTime},
		{ExpiryMode: shared.SharingExpiryUntilRevoked, GrantExpiresAt: &expires},
	} {
		if err := checkGrantRevocationExpiry(invalid, expires); err == nil || errors.Is(err, ErrGrantRevocationExpired) {
			t.Fatalf("invalid immutable expiry was treated as a valid Grant: %v", err)
		}
	}
}

func TestGrantRevocationExpiryConstraintMapping(t *testing.T) {
	constraint := &pgconn.PgError{Code: "23514", ConstraintName: "engine_access_grant_revocation_expiry"}
	if err := mapError(constraint); !errors.Is(err, ErrGrantRevocationExpired) || !errors.Is(err, commonapi.ErrConflict) {
		t.Fatal(err)
	}
	unknown := &pgconn.PgError{Code: "23514", ConstraintName: "other_constraint"}
	if err := mapError(unknown); err != unknown {
		t.Fatal("unrelated storage error disguised as expiry")
	}
}
