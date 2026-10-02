package service

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"strings"
	"time"

	"github.com/addp/common/secretcipher"
	"github.com/addp/monitor/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type PlatformLogNotifications struct {
	db                            *gorm.DB
	key                           []byte
	allowPrivate                  bool
	webhook                       WebhookTransport
	email                         EmailTransport
	maxAttempts                   int
	lease, retryInitial, retryMax time.Duration
}

func NewPlatformLogNotifications(db *gorm.DB, key []byte, allowPrivate bool, webhook WebhookTransport, email EmailTransport, maxAttempts int, lease, retryInitial, retryMax time.Duration) *PlatformLogNotifications {
	return &PlatformLogNotifications{db: db, key: key, allowPrivate: allowPrivate, webhook: webhook, email: email, maxAttempts: maxAttempts, lease: lease, retryInitial: retryInitial, retryMax: retryMax}
}

type LogDestinationInput struct {
	Version    uint64   `json:"version"`
	Name       string   `json:"name" binding:"required"`
	Channel    string   `json:"channel" binding:"required"`
	URL        string   `json:"url"`
	Recipients []string `json:"recipients"`
	EventTypes []string `json:"event_types" binding:"required"`
	Enabled    bool     `json:"enabled"`
}

func (n *PlatformLogNotifications) validate(ctx context.Context, input LogDestinationInput) (models.PlatformLogDestination, error) {
	d := models.PlatformLogDestination{Name: strings.TrimSpace(input.Name), Channel: input.Channel, URL: input.URL, Recipients: models.StringList{}, Enabled: input.Enabled, Version: 1}
	if d.Name == "" || len(d.Name) > 100 {
		return d, ErrLogInvalid
	}
	events, err := normalizeAlertEventTypes(input.EventTypes)
	if err != nil {
		return d, ErrLogInvalid
	}
	d.EventTypes = events
	switch d.Channel {
	case "webhook":
		if len(input.Recipients) != 0 || len(d.URL) > 2048 || ValidateWebhookURL(ctx, d.URL, n.allowPrivate) != nil {
			return d, ErrLogInvalid
		}
	case "email":
		if d.URL != "" {
			return d, ErrLogInvalid
		}
		_, recipients, _, err := validateEmailDestination(d.Name, input.Recipients, events)
		if err != nil {
			return d, ErrLogInvalid
		}
		d.Recipients = recipients
	default:
		return d, ErrLogInvalid
	}
	return d, nil
}
func (n *PlatformLogNotifications) List(ctx context.Context) ([]models.PlatformLogDestination, error) {
	d := []models.PlatformLogDestination{}
	err := n.db.WithContext(ctx).Order("id DESC").Find(&d).Error
	for i := range d {
		d[i].SecretConfigured = d[i].SecretCiphertext != ""
	}
	return d, err
}
func (n *PlatformLogNotifications) Save(ctx context.Context, id uint, input LogDestinationInput) (models.PlatformLogDestination, error) {
	d, err := n.validate(ctx, input)
	if err != nil {
		return d, err
	}
	if id == 0 {
		if input.Version != 0 || d.Enabled && d.Channel == "webhook" {
			return d, ErrLogInvalid
		}
		var count int64
		if err = n.db.WithContext(ctx).Model(&d).Count(&count).Error; err != nil {
			return d, err
		}
		if count >= 50 {
			return d, ErrLogInvalid
		}
		result := n.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&d)
		if result.Error != nil {
			return d, result.Error
		}
		if result.RowsAffected != 1 {
			return d, ErrLogConflict
		}
		return d, nil
	}
	err = n.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var old models.PlatformLogDestination
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&old, id).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrLogNotFound
		} else if err != nil {
			return err
		}
		if input.Version == 0 || input.Version != old.Version {
			return ErrLogConflict
		}
		if old.Channel != d.Channel {
			return ErrLogInvalid
		}
		if d.Enabled && d.Channel == "webhook" && old.SecretCiphertext == "" {
			return ErrLogInvalid
		}
		d.ID = id
		d.Version = old.Version + 1
		d.SecretCiphertext = old.SecretCiphertext
		d.CreatedAt = old.CreatedAt
		if err := tx.Save(&d).Error; err != nil {
			return err
		}
		if !d.Enabled {
			return tx.Model(&models.PlatformLogDelivery{}).Where("destination_id=? AND status='pending'", id).Updates(map[string]any{"status": "cancelled", "secret_ciphertext": "", "next_attempt_at": nil}).Error
		}
		return nil
	})
	d.SecretConfigured = d.SecretCiphertext != ""
	return d, err
}
func (n *PlatformLogNotifications) SetSecret(ctx context.Context, id uint, version uint64, secret string) (models.PlatformLogDestination, error) {
	var d models.PlatformLogDestination
	if len(secret) < 16 || len(secret) > 256 || version == 0 {
		return d, ErrLogInvalid
	}
	cipher, err := secretcipher.Encrypt(secret, n.key)
	if err != nil {
		return d, err
	}
	err = n.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND channel='webhook'", id).Take(&d).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrLogNotFound
		} else if err != nil {
			return err
		}
		if d.Version != version {
			return ErrLogConflict
		}
		d.Version++
		d.SecretCiphertext = cipher
		return tx.Save(&d).Error
	})
	d.SecretConfigured = d.SecretCiphertext != ""
	return d, err
}
func (n *PlatformLogNotifications) Delete(ctx context.Context, id uint, version uint64) error {
	if version == 0 {
		return ErrLogInvalid
	}
	return n.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var d models.PlatformLogDestination
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&d, id).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrLogNotFound
		} else if err != nil {
			return err
		}
		if d.Version != version {
			return ErrLogConflict
		}
		if err := tx.Model(&models.PlatformLogDelivery{}).Where("destination_id=? AND status='pending'", id).Updates(map[string]any{"status": "cancelled", "secret_ciphertext": "", "next_attempt_at": nil}).Error; err != nil {
			return err
		}
		return tx.Delete(&d).Error
	})
}
func (n *PlatformLogNotifications) Status(ctx context.Context) (string, error) {
	d, err := n.List(ctx)
	if err != nil {
		return "unknown", err
	}
	usable := false
	for _, dest := range d {
		if !dest.Enabled {
			continue
		}
		if dest.Channel == "email" && n.email == nil {
			return "smtp_unconfigured", nil
		}
		if dest.Channel == "webhook" && (!dest.SecretConfigured || n.webhook == nil) {
			return "unconfigured", nil
		}
		usable = true
	}
	var failed int64
	if err = n.db.WithContext(ctx).Model(&models.PlatformLogDelivery{}).Where("status='dead'").Count(&failed).Error; err != nil {
		return "unknown", err
	}
	if failed > 0 {
		return "delivery_failed", nil
	}
	if usable {
		return "configured", nil
	}
	return "unconfigured", nil
}
func (n *PlatformLogNotifications) RecordTx(tx *gorm.DB, event models.PlatformLogEvent, incident models.PlatformLogIncident, now time.Time) error {
	var destinations []models.PlatformLogDestination
	if err := tx.Where("enabled=true").Order("id").Find(&destinations).Error; err != nil {
		return err
	}
	payload := map[string]any{"schema": "addp.platform-log-alert/v1", "event_id": event.ID, "event_type": event.Type, "incident_id": incident.ID, "node": incident.Node, "signal": logSignalName(incident.Signal), "instance_id": incident.InstanceID, "severity": incident.Severity, "occurred_at": now}
	body, _ := json.Marshal(payload)
	for _, d := range destinations {
		if !stringListContains(d.EventTypes, event.Type) {
			continue
		}
		status := "pending"
		if incident.SuppressedUntil != nil && incident.SuppressedUntil.After(now) {
			status = "suppressed"
		}
		delivery := models.PlatformLogDelivery{ID: uuid.NewString(), EventID: event.ID, DestinationID: d.ID, Channel: d.Channel, URL: d.URL, SecretCiphertext: d.SecretCiphertext, Recipients: d.Recipients, Payload: string(body), Status: status, NextAttemptAt: &now, CreatedAt: now}
		if status != "pending" {
			delivery.SecretCiphertext = ""
			delivery.NextAttemptAt = nil
		}
		if err := tx.Create(&delivery).Error; err != nil {
			return err
		}
	}
	return nil
}
func stringListContains(v []string, s string) bool {
	for _, x := range v {
		if x == s {
			return true
		}
	}
	return false
}
func (n *PlatformLogNotifications) send(ctx context.Context, d models.PlatformLogDelivery, now time.Time) error {
	if d.Channel == "webhook" {
		if n.webhook == nil {
			return ErrLogInvalid
		}
		secret, err := DecryptWebhookSecret(d.SecretCiphertext, n.key)
		if err != nil {
			return err
		}
		var payload any
		if err = json.Unmarshal([]byte(d.Payload), &payload); err != nil {
			return err
		}
		_, err = n.webhook.SendMessage(ctx, WebhookMessage{DeliveryID: d.ID, RequestURL: d.URL, Payload: payload}, secret, now)
		return err
	}
	if n.email == nil {
		return ErrEmailSenderUnavailable
	}
	return n.email.SendMessage(ctx, EmailMessage{DeliveryID: d.ID, Recipients: d.Recipients, Subject: "ADDP platform log pipeline alert", TextBody: d.Payload, HTMLBody: "<pre>" + html.EscapeString(d.Payload) + "</pre>"}, now)
}
func (n *PlatformLogNotifications) Test(ctx context.Context, id uint, now time.Time) error {
	var d models.PlatformLogDestination
	if err := n.db.WithContext(ctx).First(&d, id).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrLogNotFound
	} else if err != nil {
		return err
	}
	return n.send(ctx, models.PlatformLogDelivery{ID: uuid.NewString(), Channel: d.Channel, URL: d.URL, SecretCiphertext: d.SecretCiphertext, Recipients: d.Recipients, Payload: `{"schema":"addp.platform-log-notification-test/v1","test":true}`}, now)
}
func (n *PlatformLogNotifications) DispatchOnce(ctx context.Context, now time.Time) (bool, error) {
	claim := uuid.NewString()
	var deliveries []models.PlatformLogDelivery
	err := n.db.WithContext(ctx).Raw(`WITH candidate AS (
 SELECT id FROM monitor.platform_log_deliveries WHERE ((status='pending' AND next_attempt_at<=?) OR (status='delivering' AND lease_expires_at<=?)) AND (channel='webhook' OR ?)
 ORDER BY next_attempt_at NULLS FIRST,id LIMIT 1 FOR UPDATE SKIP LOCKED)
 UPDATE monitor.platform_log_deliveries d SET status='delivering',attempt_count=d.attempt_count+1,claim_id=?,lease_expires_at=? FROM candidate WHERE d.id=candidate.id RETURNING d.*`, now, now, n.email != nil, claim, now.Add(n.lease)).Scan(&deliveries).Error
	if err != nil || len(deliveries) == 0 {
		return false, err
	}
	d := deliveries[0]
	if d.AttemptCount > n.maxAttempts {
		result := n.db.WithContext(ctx).Model(&models.PlatformLogDelivery{}).Where("id=? AND status='delivering' AND claim_id=?", d.ID, claim).Updates(map[string]any{"status": "dead", "last_error": "notification_attempt_limit", "secret_ciphertext": "", "claim_id": "", "lease_expires_at": nil, "next_attempt_at": nil})
		return true, result.Error
	}
	// Re-read suppression and destination while the claim is current. A request already sent may complete.
	var event models.PlatformLogEvent
	if err = n.db.WithContext(ctx).First(&event, "id=?", d.EventID).Error; err != nil {
		return true, err
	}
	var i models.PlatformLogIncident
	if err = n.db.WithContext(ctx).First(&i, event.IncidentID).Error; err != nil {
		return true, err
	}
	var dest models.PlatformLogDestination
	targetErr := n.db.WithContext(ctx).First(&dest, d.DestinationID).Error
	updates := map[string]any{"claim_id": "", "lease_expires_at": nil, "next_attempt_at": nil}
	if i.SuppressedUntil != nil && i.SuppressedUntil.After(now) {
		updates["status"] = "suppressed"
		updates["secret_ciphertext"] = ""
	} else if errors.Is(targetErr, gorm.ErrRecordNotFound) || targetErr == nil && !dest.Enabled {
		updates["status"] = "cancelled"
		updates["secret_ciphertext"] = ""
	} else if targetErr != nil {
		return true, targetErr
	} else {
		sendErr := n.send(ctx, d, now)
		if sendErr == nil {
			updates["status"] = "delivered"
			updates["delivered_at"] = now
			updates["last_error"] = ""
			updates["secret_ciphertext"] = ""
		} else {
			updates["last_error"] = "notification_send_failed"
			if d.AttemptCount >= n.maxAttempts {
				updates["status"] = "dead"
				updates["secret_ciphertext"] = ""
			} else {
				updates["status"] = "pending"
				updates["next_attempt_at"] = now.Add(notificationBackoff(n.retryInitial, n.retryMax, d.AttemptCount))
			}
		}
	}
	result := n.db.WithContext(ctx).Model(&models.PlatformLogDelivery{}).Where("id=? AND status='delivering' AND claim_id=?", d.ID, claim).Updates(updates)
	if result.Error != nil {
		return true, result.Error
	}
	if result.RowsAffected != 1 {
		return true, ErrLogConflict
	}
	return true, nil
}
func (n *PlatformLogNotifications) Run(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		for i := 0; i < 50; i++ {
			processed, err := n.DispatchOnce(ctx, time.Now().UTC())
			if err != nil || !processed {
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

type LogDeliveryList struct {
	Data       []models.PlatformLogDelivery `json:"data"`
	Total      int64                        `json:"total"`
	Page       int                          `json:"page"`
	PageSize   int                          `json:"page_size"`
	TotalPages int                          `json:"total_pages"`
}

func (n *PlatformLogNotifications) Deliveries(ctx context.Context, page, size int) (LogDeliveryList, error) {
	r := LogDeliveryList{Data: []models.PlatformLogDelivery{}, Page: page, PageSize: size}
	if page < 1 || page > 10000 || size < 1 || size > 100 {
		return r, ErrLogInvalid
	}
	q := n.db.WithContext(ctx).Model(&models.PlatformLogDelivery{})
	if err := q.Count(&r.Total).Error; err != nil {
		return r, err
	}
	r.TotalPages = int((r.Total + int64(size) - 1) / int64(size))
	err := q.Order("created_at DESC,id DESC").Offset((page - 1) * size).Limit(size).Find(&r.Data).Error
	return r, err
}
