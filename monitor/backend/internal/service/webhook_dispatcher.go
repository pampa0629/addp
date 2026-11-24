package service

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync/atomic"
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
	active atomic.Int64
}

func (d *WebhookDispatcher) ActiveCount() int { return int(d.active.Load()) }

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
	d.active.Add(1)
	defer d.active.Add(-1)
	secret, decryptErr := DecryptWebhookSecret(delivery.SecretCiphertext, d.config.EncryptionKey)
	if decryptErr != nil {
		return true, d.finishAttempt(ctx, *delivery, WebhookSendResult{}, decryptErr, now)
	}
	result, sendErr := d.sender.Send(ctx, *delivery, secret, now)
	return true, d.finishAttempt(ctx, *delivery, result, sendErr, now)
}

func (d *WebhookDispatcher) claimDue(ctx context.Context, now time.Time) (*monitorModels.WebhookDelivery, error) {
	return claimNotificationDelivery[monitorModels.WebhookDelivery](ctx, d.db, now, now.Add(d.config.LeaseDuration),
		map[string]any{"claimed_by": d.config.WorkerID, "updated_at": now}, nil)
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
	return notificationBackoff(d.config.RetryInitial, d.config.RetryMax, attempt)
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
