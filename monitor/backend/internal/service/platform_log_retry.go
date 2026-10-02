package service

import (
	"context"
	"errors"
	"time"

	"github.com/addp/monitor/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrLogRetryUnavailable = errors.New("platform log delivery cannot currently be retried")

type LogDeliveryRetryInput struct {
	ExpectedManualRetryCount *int   `json:"expected_manual_retry_count" binding:"required"`
	DestinationVersion       uint64 `json:"destination_version" binding:"required"`
}

// LogDeliveryView joins only Monitor-owned safe lifecycle and current target facts.
type LogDeliveryView struct {
	models.PlatformLogDelivery `gorm:"embedded"`
	CycleAttemptCount          int        `json:"cycle_attempt_count"`
	EventType                  string     `json:"event_type"`
	OccurredAt                 time.Time  `json:"occurred_at"`
	IncidentID                 uint       `json:"incident_id"`
	IncidentStatus             string     `json:"incident_status"`
	SuppressedUntil            *time.Time `json:"suppressed_until,omitempty"`
	DestinationName            string     `json:"destination_name"`
	DestinationVersion         uint64     `json:"destination_version"`
}

func logDeliveryQuery(db *gorm.DB) *gorm.DB {
	return db.Table("monitor.platform_log_deliveries AS d").
		Select(`d.*, d.attempt_count-d.retry_base_attempt_count AS cycle_attempt_count,
 e.type AS event_type,e.occurred_at,e.incident_id,i.status AS incident_status,i.suppressed_until,
 t.name AS destination_name,t.version AS destination_version`).
		Joins("JOIN monitor.platform_log_events e ON e.id=d.event_id").
		Joins("JOIN monitor.platform_log_incidents i ON i.id=e.incident_id").
		Joins("LEFT JOIN monitor.platform_log_destinations t ON t.id=d.destination_id")
}

func (n *PlatformLogNotifications) Retry(ctx context.Context, id string, input LogDeliveryRetryInput, now time.Time) (LogDeliveryView, error) {
	var result LogDeliveryView
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.String() != id || input.ExpectedManualRetryCount == nil || *input.ExpectedManualRetryCount < 0 || input.DestinationVersion == 0 {
		return result, ErrLogInvalid
	}
	var seed models.PlatformLogDelivery
	if err = n.db.WithContext(ctx).Select("id,event_id,destination_id").First(&seed, "id=?", id).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return result, ErrLogNotFound
	} else if err != nil {
		return result, err
	}
	err = n.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Target -> incident -> delivery: target changes and suppression cannot cross the enqueue decision.
		var target models.PlatformLogDestination
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&target, seed.DestinationID).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrLogRetryUnavailable
		} else if err != nil {
			return err
		}
		if target.Version != input.DestinationVersion {
			return ErrLogConflict
		}
		var event models.PlatformLogEvent
		if err := tx.First(&event, "id=?", seed.EventID).Error; err != nil {
			return err
		}
		var incident models.PlatformLogIncident
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&incident, event.IncidentID).Error; err != nil {
			return err
		}
		var delivery models.PlatformLogDelivery
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&delivery, "id=?", id).Error; err != nil {
			return err
		}
		if delivery.ManualRetryCount != *input.ExpectedManualRetryCount || delivery.Status != "dead" {
			return ErrLogConflict
		}
		if !target.Enabled || target.Channel != delivery.Channel || !stringListContains(target.EventTypes, event.Type) || incident.SuppressedUntil != nil && incident.SuppressedUntil.After(now) {
			return ErrLogRetryUnavailable
		}
		switch target.Channel {
		case "webhook":
			if n.webhook == nil {
				return ErrLogRetryUnavailable
			}
			if _, err := DecryptWebhookSecret(target.SecretCiphertext, n.key); err != nil {
				return ErrLogRetryUnavailable
			}
		case "email":
			if n.email == nil {
				return ErrLogRetryUnavailable
			}
		default:
			return ErrLogRetryUnavailable
		}
		updates := map[string]any{
			"status": "pending", "manual_retry_count": delivery.ManualRetryCount + 1,
			"retry_base_attempt_count": delivery.AttemptCount, "next_attempt_at": now,
			"url": target.URL, "recipients": target.Recipients, "secret_ciphertext": target.SecretCiphertext,
			"claim_id": "", "lease_expires_at": nil, "delivered_at": nil, "last_error": "",
		}
		if err := tx.Model(&delivery).Updates(updates).Error; err != nil {
			return err
		}
		return logDeliveryQuery(tx).Where("d.id=?", id).Scan(&result).Error
	})
	return result, err
}
