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
		&monitorModels.LogPipelinePolicy{}, &monitorModels.LogPipelineNode{}, &monitorModels.LogObserverBoot{},
		&monitorModels.PlatformLogIncident{}, &monitorModels.PlatformLogEvent{}, &monitorModels.PlatformLogDestination{}, &monitorModels.PlatformLogDelivery{},
	); err != nil {
		return err
	}
	if err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uq_monitor_platform_log_active
 ON monitor.platform_log_incidents(node, signal) WHERE status IN ('open','acknowledged')`).Error; err != nil {
		return err
	}
	if err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_monitor_platform_log_due
 ON monitor.platform_log_deliveries(next_attempt_at, id) WHERE status IN ('pending','delivering')`).Error; err != nil {
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
	if err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_monitor_email_delivery_expired_lease
		ON monitor.email_deliveries (lease_expires_at, id)
		WHERE status = 'delivering'`).Error; err != nil {
		return err
	}
	// Converge platform subscriptions and pending deliveries atomically. Historical
	// events and completed deliveries remain immutable; tenant alerts are separate.
	return db.Exec(`WITH changed AS (
 UPDATE monitor.platform_log_destinations
 SET event_types = event_types - 'escalated',
     enabled = enabled AND jsonb_array_length(event_types - 'escalated') > 0,
     version = version + 1, updated_at = clock_timestamp()
 WHERE event_types @> '["escalated"]'::jsonb
 RETURNING id, enabled
)
UPDATE monitor.platform_log_deliveries
 SET status = 'cancelled', secret_ciphertext = '', next_attempt_at = NULL
 WHERE status = 'pending' AND (
   destination_id IN (SELECT id FROM changed WHERE NOT enabled)
   OR event_id IN (SELECT id FROM monitor.platform_log_events WHERE type = 'escalated')
 )`).Error
}
