package authorization

import (
	"errors"
	"time"
)

const (
	SharingExpiryAtTime       = "at_time"
	SharingExpiryUntilRevoked = "until_revoked"
)

var ErrInvalidSharingExpiry = errors.New("invalid ordinary read-sharing expiry")

// NormalizeSharingExpiry never interprets an omitted mode as indefinite. This
// value contract covers ordinary read sharing and explicit source read Deny;
// it does not cover temporary onboarding, management delegations, tokens or
// sensitive raw-value exemptions. Callers enforce their own eligibility.
func NormalizeSharingExpiry(mode string, expiresAt *time.Time) (*time.Time, error) {
	switch mode {
	case SharingExpiryUntilRevoked:
		if expiresAt != nil {
			return nil, ErrInvalidSharingExpiry
		}
		return nil, nil
	case SharingExpiryAtTime:
		if expiresAt == nil || expiresAt.IsZero() {
			return nil, ErrInvalidSharingExpiry
		}
		value := expiresAt.UTC().Truncate(time.Microsecond)
		if value.IsZero() || value.Year() < 1 || value.Year() > 9999 {
			return nil, ErrInvalidSharingExpiry
		}
		return &value, nil
	default:
		return nil, ErrInvalidSharingExpiry
	}
}

// A historical expired value can be normalized and compared for recovery;
// only NEW confirmation/acceptance checks future validity after lock waiting.
func SharingExpiryFuture(mode string, expiresAt *time.Time, now time.Time) bool {
	value, err := NormalizeSharingExpiry(mode, expiresAt)
	return err == nil && (value == nil || value.After(now))
}

func EqualSharingExpiry(leftMode string, left *time.Time, rightMode string, right *time.Time) bool {
	if leftMode != rightMode {
		return false
	}
	a, err := NormalizeSharingExpiry(leftMode, left)
	if err != nil {
		return false
	}
	b, err := NormalizeSharingExpiry(rightMode, right)
	return err == nil && ((a == nil && b == nil) || (a != nil && b != nil && a.Equal(*b)))
}
