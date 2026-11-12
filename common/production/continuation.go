package production

import (
	"time"

	"github.com/google/uuid"
)

// DeliveryState contains no permission, expiry or duplicate ACL. The native
// owner records it atomically with its original committed-result observation.
type DeliveryState struct {
	SchemaVersion   string `json:"schema_version"`
	Status          string `json:"status"`
	Code            string `json:"code,omitempty"`
	Attempts        int    `json:"attempts"`
	NextAttemptUnix int64  `json:"next_attempt_unix"`
	GrantRequestID  string `json:"grant_request_id,omitempty"`
}

func (s DeliveryState) Validate(schema string) error {
	if s.SchemaVersion != schema || s.Attempts < 0 || s.NextAttemptUnix < 0 {
		return ErrEvidence
	}
	switch s.Status {
	case "pending", "denied":
		if s.GrantRequestID != "" {
			return ErrEvidence
		}
	case "issued":
		id, err := uuid.Parse(s.GrantRequestID)
		if err != nil || id == uuid.Nil || id.String() != s.GrantRequestID {
			return ErrEvidence
		}
	default:
		return ErrEvidence
	}
	return nil
}

func (s *DeliveryState) Claim(now time.Time) {
	s.Attempts++
	delay := 30 * time.Second
	for i := 1; i < s.Attempts && delay < 5*time.Minute; i++ {
		delay *= 2
	}
	if delay > 5*time.Minute {
		delay = 5 * time.Minute
	}
	s.NextAttemptUnix = now.Add(delay).Unix()
}
