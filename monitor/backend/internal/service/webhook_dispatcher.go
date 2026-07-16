package service

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	monitorModels "github.com/addp/monitor/internal/models"
	"gorm.io/gorm"
)

type WebhookDispatcherConfig struct {
	WorkerID         string
	DispatchInterval time.Duration
	LeaseDuration    time.Duration
	MaxAttempts      int
	RetryInitial     time.Duration
	RetryMax         time.Duration
	EncryptionKey    []byte
}

type WebhookDispatcher struct {
	db     *gorm.DB
	sender WebhookSender
	config WebhookDispatcherConfig
}

func NewWebhookDispatcher(db *gorm.DB, sender WebhookSender, config WebhookDispatcherConfig) *WebhookDispatcher {
	return &WebhookDispatcher{db: db, sender: sender, config: config}
}

func (d *WebhookDispatcher) Run(ctx context.Context) {
	ticker := time.NewTicker(d.config.DispatchInterval)
	defer ticker.Stop()
	for {
		for {
			processed, err := d.DispatchOnce(ctx, time.Now())
			if err != nil {
				log.Printf("Webhook delivery failed: %v", err)
				break
			}
			if !processed {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (d *WebhookDispatcher) DispatchOnce(ctx context.Context, now time.Time) (bool, error) {
	delivery, err := d.claimDue(ctx, now)
	if err != nil || delivery == nil {
		return false, err
	}
	secret, decryptErr := DecryptWebhookSecret(delivery.SecretCiphertext, d.config.EncryptionKey)
	if decryptErr != nil {
		return true, d.finishAttempt(ctx, *delivery, WebhookSendResult{}, decryptErr, now)
	}
	result, sendErr := d.sender.Send(ctx, *delivery, secret, now)
	return true, d.finishAttempt(ctx, *delivery, result, sendErr, now)
}

func (d *WebhookDispatcher) claimDue(ctx context.Context, now time.Time) (*monitorModels.WebhookDelivery, error) {
	leaseExpiresAt := now.Add(d.config.LeaseDuration)
	var deliveries []monitorModels.WebhookDelivery
	err := d.db.WithContext(ctx).Raw(`
		WITH candidate AS (
			SELECT id
			FROM monitor.webhook_deliveries
			WHERE (status = ? AND next_attempt_at <= ?)
			   OR (status = ? AND lease_expires_at <= ?)
			ORDER BY next_attempt_at ASC NULLS FIRST, id ASC
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE monitor.webhook_deliveries AS delivery
		SET status = ?, attempt_count = delivery.attempt_count + 1,
			claimed_by = ?, lease_expires_at = ?, updated_at = ?
		FROM candidate
		WHERE delivery.id = candidate.id
		RETURNING delivery.*`,
		monitorModels.WebhookDeliveryPending, now,
		monitorModels.WebhookDeliveryDelivering, now,
		monitorModels.WebhookDeliveryDelivering, d.config.WorkerID, leaseExpiresAt, now,
	).Scan(&deliveries).Error
	if err != nil {
		return nil, err
	}
	if len(deliveries) == 0 {
		return nil, nil
	}
	return &deliveries[0], nil
}

func (d *WebhookDispatcher) finishAttempt(ctx context.Context, delivery monitorModels.WebhookDelivery, result WebhookSendResult, sendErr error, now time.Time) error {
	cycleAttempt := delivery.AttemptCount - delivery.RetryBaseAttemptCount
	if cycleAttempt < 1 {
		cycleAttempt = 1
	}
	updates := map[string]interface{}{
		"claimed_by": "", "lease_expires_at": nil, "last_http_status": nullableHTTPStatus(result.HTTPStatus), "updated_at": now,
	}
	if sendErr == nil {
		updates["status"] = monitorModels.WebhookDeliveryDelivered
		updates["delivered_at"] = now
		updates["last_error"] = ""
		updates["next_attempt_at"] = nil
		updates["secret_ciphertext"] = ""
	} else if cycleAttempt >= d.config.MaxAttempts {
		updates["status"] = monitorModels.WebhookDeliveryDead
		updates["last_error"] = truncateWebhookError(sendErr.Error())
		updates["next_attempt_at"] = nil
		updates["secret_ciphertext"] = ""
	} else {
		updates["status"] = monitorModels.WebhookDeliveryPending
		updates["last_error"] = truncateWebhookError(sendErr.Error())
		updates["next_attempt_at"] = now.Add(d.retryBackoff(cycleAttempt))
	}
	resultDB := d.db.WithContext(ctx).Model(&monitorModels.WebhookDelivery{}).
		Where("id = ? AND status = ? AND claimed_by = ?", delivery.ID, monitorModels.WebhookDeliveryDelivering, d.config.WorkerID).
		Updates(updates)
	if resultDB.Error != nil {
		return resultDB.Error
	}
	if resultDB.RowsAffected != 1 {
		return fmt.Errorf("webhook delivery %s lost claim", delivery.DeliveryID)
	}
	return nil
}

func (d *WebhookDispatcher) retryBackoff(attempt int) time.Duration {
	backoff := d.config.RetryInitial
	for current := 1; current < attempt && backoff < d.config.RetryMax; current++ {
		if backoff > d.config.RetryMax/2 {
			return d.config.RetryMax
		}
		backoff *= 2
	}
	if backoff > d.config.RetryMax {
		return d.config.RetryMax
	}
	return backoff
}

func nullableHTTPStatus(status int) interface{} {
	if status == 0 {
		return nil
	}
	return status
}

func truncateWebhookError(message string) string {
	message = strings.TrimSpace(message)
	if len(message) <= 2000 {
		return message
	}
	return message[:2000]
}
