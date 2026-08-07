package service

import (
	monitorModels "github.com/addp/monitor/internal/models"
	"gorm.io/gorm"
)

func EnsureMonitorStore(db *gorm.DB) error {
	if err := db.Exec("CREATE SCHEMA IF NOT EXISTS monitor").Error; err != nil {
		return err
	}
	if err := db.AutoMigrate(
		&monitorModels.AlertIncident{},
		&monitorModels.AlertEvent{},
		&monitorModels.AlertRule{},
		&monitorModels.NotificationRoute{},
		&monitorModels.WebhookDestination{},
		&monitorModels.WebhookDelivery{},
		&monitorModels.EmailDestination{},
		&monitorModels.EmailDelivery{},
		&monitorModels.RuntimePolicy{},
		&monitorModels.SMTPRelay{},
	); err != nil {
		return err
	}
	if err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uq_monitor_active_alert_fingerprint
		ON monitor.alert_incidents (fingerprint)
		WHERE status IN ('open', 'acknowledged')`).Error; err != nil {
		return err
	}
	if err := db.Exec("DROP INDEX IF EXISTS monitor.idx_monitor_webhook_delivery_due").Error; err != nil {
		return err
	}
	if err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_monitor_webhook_delivery_pending
		ON monitor.webhook_deliveries (next_attempt_at, id)
		WHERE status = 'pending'`).Error; err != nil {
		return err
	}
	if err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_monitor_webhook_delivery_expired_lease
		ON monitor.webhook_deliveries (lease_expires_at, id)
		WHERE status = 'delivering'`).Error; err != nil {
		return err
	}
	if err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_monitor_email_delivery_pending
		ON monitor.email_deliveries (next_attempt_at, id)
		WHERE status = 'pending'`).Error; err != nil {
		return err
	}
	return db.Exec(`CREATE INDEX IF NOT EXISTS idx_monitor_email_delivery_expired_lease
		ON monitor.email_deliveries (lease_expires_at, id)
		WHERE status = 'delivering'`).Error
}
