package engineaccess

import (
	"errors"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestGrantWindowUsesOriginalStrictBoundary(t *testing.T) {
	accepted := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	deadline := accepted.Add(5 * time.Minute)
	row := fulfillmentOutcome{Outcome: "accepted", RecordedAt: accepted, Deadline: &deadline}
	for _, tc := range []struct {
		name string
		now  time.Time
		want error
	}{
		{"before receipt", accepted.Add(-time.Nanosecond), errFulfillmentExpired},
		{"receipt time", accepted, nil},
		{"inside original window", deadline.Add(-time.Nanosecond), nil},
		{"at deadline", deadline, errFulfillmentExpired},
		{"after deadline", deadline.Add(time.Nanosecond), errFulfillmentExpired},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := grantWindow(&row, tc.now); !errors.Is(err, tc.want) {
				t.Fatalf("window=%v want=%v", err, tc.want)
			}
		})
	}
	row.Outcome, row.Deadline = "closed", nil
	if err := grantWindow(&row, accepted); !errors.Is(err, errFulfillmentClosed) {
		t.Fatal(err)
	}
	row.Outcome = "accepted"
	if err := grantWindow(&row, accepted); !errors.Is(err, errFulfillmentClosed) {
		t.Fatal("missing deadline opened window")
	}
}

func TestGrantErrorPreservesAuthorizationAndDatabaseErrors(t *testing.T) {
	window := &pgconn.PgError{Code: "23514", ConstraintName: "engine_access_grant_acceptance_window"}
	if err := grantError(window); !errors.Is(err, commonapi.ErrConflict) || !errors.Is(err, errFulfillmentExpired) {
		t.Fatal(err)
	}
	for _, original := range []error{nil, commonapi.ErrForbidden, commonapi.ErrUnauthorized, errors.New("audit storage failed")} {
		if err := grantError(original); !errors.Is(err, original) {
			t.Fatalf("changed error=%v want=%v", err, original)
		}
	}
}
