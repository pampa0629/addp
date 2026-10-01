package authorization

import (
	"testing"
	"time"
)

func TestSharingExpiryRequiresExplicitConsistentMode(t *testing.T) {
	now := time.Now().UTC()
	future, zero := now.AddDate(8, 0, 0), time.Time{}
	invalidUTCYear := time.Date(9999, 12, 31, 23, 59, 59, 0, time.FixedZone("west", -3600))
	for _, value := range []struct {
		mode string
		date *time.Time
	}{
		{"", nil}, {"", &future}, {"unknown", nil}, {SharingExpiryAtTime, nil},
		{SharingExpiryAtTime, &zero}, {SharingExpiryUntilRevoked, &future}, {SharingExpiryUntilRevoked, &zero},
		{SharingExpiryAtTime, &invalidUTCYear},
	} {
		if _, err := NormalizeSharingExpiry(value.mode, value.date); err == nil || SharingExpiryFuture(value.mode, value.date, now) {
			t.Fatalf("invalid mode/date accepted: %+v", value)
		}
	}
	canonical, err := NormalizeSharingExpiry(SharingExpiryAtTime, &future)
	if err != nil || canonical.Nanosecond()%1000 != 0 || canonical.After(future) || !SharingExpiryFuture(SharingExpiryAtTime, &future, now) {
		t.Fatalf("finite expiry=%v err=%v", canonical, err)
	}
	if !SharingExpiryFuture(SharingExpiryUntilRevoked, nil, now.AddDate(50, 0, 0)) ||
		!EqualSharingExpiry(SharingExpiryUntilRevoked, nil, SharingExpiryUntilRevoked, nil) ||
		EqualSharingExpiry(SharingExpiryUntilRevoked, nil, SharingExpiryAtTime, &future) ||
		EqualSharingExpiry("", nil, "", nil) {
		t.Fatal("mode omitted or changed in equality/validity")
	}
	past := now.Add(-time.Second)
	if !EqualSharingExpiry(SharingExpiryAtTime, &past, SharingExpiryAtTime, &past) || SharingExpiryFuture(SharingExpiryAtTime, &past, now) {
		t.Fatal("expired historical recovery and new validity must differ")
	}
}
