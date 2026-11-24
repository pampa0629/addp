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

type EmailDispatcherConfig struct {
	WorkerID         string
	DispatchInterval time.Duration
	LeaseDuration    time.Duration
	MaxAttempts      int
	RetryInitial     time.Duration
	RetryMax         time.Duration
}

type EmailDispatcher struct {
	db     *gorm.DB
	sender EmailSender
	config EmailDispatcherConfig
	active atomic.Int64
}

func (d *EmailDispatcher) ActiveCount() int { return int(d.active.Load()) }

func NewEmailDispatcher(db *gorm.DB, sender EmailSender, config EmailDispatcherConfig) *EmailDispatcher {
	return &EmailDispatcher{db: db, sender: sender, config: config}
}

func (d *EmailDispatcher) Run(ctx context.Context) {
	ticker := time.NewTicker(d.config.DispatchInterval)
	defer ticker.Stop()
	for {
		for {
			processed, err := d.DispatchOnce(ctx, time.Now())
			if err != nil {
				log.Printf("Email delivery failed: %v", err)
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

func (d *EmailDispatcher) DispatchOnce(ctx context.Context, now time.Time) (bool, error) {
	delivery, err := d.claimDue(ctx, now)
	if err != nil || delivery == nil {
		return false, err
	}
	d.active.Add(1)
	defer d.active.Add(-1)
	sendErr := d.sender.Send(ctx, *delivery, now)
	return true, d.finishAttempt(ctx, *delivery, sendErr, now)
}

func (d *EmailDispatcher) claimDue(ctx context.Context, now time.Time) (*monitorModels.EmailDelivery, error) {
	return claimNotificationDelivery[monitorModels.EmailDelivery](ctx, d.db, now, now.Add(d.config.LeaseDuration),
		map[string]any{"claimed_by": d.config.WorkerID, "updated_at": now}, nil)
}

func (d *EmailDispatcher) finishAttempt(ctx context.Context, delivery monitorModels.EmailDelivery, sendErr error, now time.Time) error {
	cycleAttempt := delivery.AttemptCount - delivery.RetryBaseAttemptCount
	if cycleAttempt < 1 {
		cycleAttempt = 1
	}
	updates := map[string]interface{}{
		"claimed_by": "", "lease_expires_at": nil, "updated_at": now,
	}
	if sendErr == nil {
		updates["status"] = monitorModels.EmailDeliveryDelivered
		updates["delivered_at"] = now
		updates["last_error"] = ""
		updates["next_attempt_at"] = nil
	} else if cycleAttempt >= d.config.MaxAttempts {
		updates["status"] = monitorModels.EmailDeliveryDead
		updates["last_error"] = truncateEmailError(sendErr.Error())
		updates["next_attempt_at"] = nil
	} else {
		updates["status"] = monitorModels.EmailDeliveryPending
		updates["last_error"] = truncateEmailError(sendErr.Error())
		updates["next_attempt_at"] = now.Add(d.retryBackoff(cycleAttempt))
	}
	result := d.db.WithContext(ctx).Model(&monitorModels.EmailDelivery{}).
		Where("id = ? AND status = ? AND claimed_by = ?", delivery.ID, monitorModels.EmailDeliveryDelivering, d.config.WorkerID).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("email delivery %s lost claim", delivery.DeliveryID)
	}
	return nil
}

func (d *EmailDispatcher) retryBackoff(attempt int) time.Duration {
	return notificationBackoff(d.config.RetryInitial, d.config.RetryMax, attempt)
}

func truncateEmailError(message string) string {
	message = strings.TrimSpace(message)
	if len(message) <= 2000 {
		return message
	}
	return message[:2000]
}
