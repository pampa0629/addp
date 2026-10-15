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
	models.PlatformAlertDelivery `gorm:"embedded"`
	CycleAttemptCount            int        `json:"cycle_attempt_count"`
	EventType                    string     `json:"event_type"`
	OccurredAt                   time.Time  `json:"occurred_at"`
	IncidentID                   uint       `json:"incident_id"`
	IncidentStatus               string     `json:"incident_status"`
	SuppressedUntil              *time.Time `json:"suppressed_until,omitempty"`
	DestinationName              string     `json:"destination_name"`
	DestinationVersion           uint64     `json:"destination_version"`
}

func logDeliveryQuery(db *gorm.DB) *gorm.DB {
	return db.Table("monitor.platform_alert_deliveries AS d").
		Select(`d.*, d.attempt_count-d.retry_base_attempt_count AS cycle_attempt_count,
 e.type AS event_type,e.occurred_at,e.incident_id,i.status AS incident_status,i.suppressed_until,
 t.name AS destination_name,t.version AS destination_version`).
		Joins("JOIN monitor.platform_alert_events e ON e.id=d.event_id").
		Joins("JOIN monitor.platform_alert_incidents i ON i.id=e.incident_id").
		Joins("LEFT JOIN monitor.platform_alert_destinations t ON t.id=d.destination_id").
		Where("i.category=? AND i.subject_kind=?", models.PlatformAlertCategoryLog, models.PlatformAlertSubjectLogObserver)
}

func (n *PlatformAlertNotifications) Retry(ctx context.Context, id string, input LogDeliveryRetryInput, now time.Time) (LogDeliveryView, error) {
	var result LogDeliveryView
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.String() != id || input.ExpectedManualRetryCount == nil || *input.ExpectedManualRetryCount < 0 || input.DestinationVersion == 0 {
		return result, ErrLogInvalid
	}
	var seed models.PlatformAlertDelivery
	if err = logDeliveryQuery(n.db.WithContext(ctx)).Select("d.id,d.event_id,d.destination_id").Where("d.id=?", id).Take(&seed).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return result, ErrLogNotFound
	} else if err != nil {
		return result, err
	}
	err = n.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Target -> incident -> delivery: target changes and suppression cannot cross the enqueue decision.
		var target models.PlatformAlertDestination
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("categories = ?::jsonb", `["log_pipeline"]`).First(&target, seed.DestinationID).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrLogRetryUnavailable
		} else if err != nil {
			return err
		}
		if target.Version != input.DestinationVersion {
			return ErrLogConflict
		}
		var event models.PlatformAlertEvent
		if err := tx.First(&event, "id=?", seed.EventID).Error; err != nil {
			return err
		}
		var incident models.PlatformAlertIncident
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("category=? AND subject_kind=?", models.PlatformAlertCategoryLog, models.PlatformAlertSubjectLogObserver).First(&incident, event.IncidentID).Error; err != nil {
			return err
		}
		var delivery models.PlatformAlertDelivery
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
		case "wecom":
			if n.wecom == nil {
				return ErrLogRetryUnavailable
			}
			key, err := DecryptWebhookSecret(target.SecretCiphertext, n.key)
			if err != nil || !wecomKeyPattern.MatchString(key) || target.URL != wecomEndpoint {
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
