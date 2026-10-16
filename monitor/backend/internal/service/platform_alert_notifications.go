package service

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"sort"
	"strings"
	"time"

	"github.com/addp/common/secretcipher"
	"github.com/addp/monitor/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type PlatformAlertService struct {
	db                            *gorm.DB
	key                           []byte
	allowPrivate                  bool
	webhook                       WebhookTransport
	wecom                         WeComTransport
	email                         EmailTransport
	maxAttempts                   int
	lease, retryInitial, retryMax time.Duration
}

func NewPlatformAlertService(db *gorm.DB, key []byte, allowPrivate bool, webhook WebhookTransport, email EmailTransport, maxAttempts int, lease, retryInitial, retryMax time.Duration) *PlatformAlertService {
	wecom, _ := webhook.(WeComTransport)
	return &PlatformAlertService{wecom: wecom, db: db, key: key, allowPrivate: allowPrivate, webhook: webhook, email: email, maxAttempts: maxAttempts, lease: lease, retryInitial: retryInitial, retryMax: retryMax}
}

type PlatformAlertDestinationInput struct {
	Categories []string `json:"categories" binding:"required" enums:"log_pipeline,resource"`
	Version    uint64   `json:"version"`
	Name       string   `json:"name" binding:"required"`
	Channel    string   `json:"channel" binding:"required" enums:"webhook,email,wecom"`
	URL        string   `json:"url"`
	Recipients []string `json:"recipients"`
	EventTypes []string `json:"event_types" binding:"required" enums:"opened,resolved"`
	Enabled    bool     `json:"enabled"`
}

func (n *PlatformAlertService) validate(ctx context.Context, input PlatformAlertDestinationInput) (models.PlatformAlertDestination, error) {
	d := models.PlatformAlertDestination{Categories: models.StringList{}, Name: strings.TrimSpace(input.Name), Channel: input.Channel, URL: input.URL, Recipients: models.StringList{}, Enabled: input.Enabled, Version: 1}
	if d.Name == "" || len(d.Name) > 100 {
		return d, ErrLogInvalid
	}
	if len(input.Categories) < 1 || len(input.Categories) > 2 {
		return d, ErrLogInvalid
	}
	for _, category := range input.Categories {
		if !validPlatformAlertCategory(category) || stringListContains(d.Categories, category) {
			return d, ErrLogInvalid
		}
		d.Categories = append(d.Categories, category)
	}
	sort.Strings(d.Categories)
	events, err := normalizeAlertEventTypes(input.EventTypes)
	if err != nil {
		return d, ErrLogInvalid
	}
	for _, event := range events {
		if event != "opened" && event != "resolved" {
			return d, ErrLogInvalid
		}
	}
	d.EventTypes = events
	switch d.Channel {
	case "webhook":
		if len(input.Recipients) != 0 || len(d.URL) > 2048 || ValidateWebhookURL(ctx, d.URL, n.allowPrivate) != nil {
			return d, ErrLogInvalid
		}
	case "wecom":
		if d.URL != "" || len(input.Recipients) != 0 {
			return d, ErrLogInvalid
		}
		d.URL = wecomEndpoint
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
func (n *PlatformAlertService) Destinations(ctx context.Context) ([]models.PlatformAlertDestination, error) {
	d := []models.PlatformAlertDestination{}
	err := n.db.WithContext(ctx).Order("id DESC").Find(&d).Error
	for i := range d {
		d[i].SecretConfigured = d[i].SecretCiphertext != ""
	}
	return d, err
}
func (n *PlatformAlertService) Save(ctx context.Context, id uint, input PlatformAlertDestinationInput) (models.PlatformAlertDestination, error) {
	d, err := n.validate(ctx, input)
	if err != nil {
		return d, err
	}
	if id == 0 {
		if input.Version != 0 || d.Enabled && (d.Channel == "webhook" || d.Channel == "wecom") {
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
		var old models.PlatformAlertDestination
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
		if d.Enabled && (d.Channel == "webhook" || d.Channel == "wecom") && old.SecretCiphertext == "" {
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
			return tx.Model(&models.PlatformAlertDelivery{}).Where("destination_id=? AND status='pending'", id).Updates(map[string]any{"status": "cancelled", "secret_ciphertext": "", "next_attempt_at": nil}).Error
		}
		return nil
	})
	d.SecretConfigured = d.SecretCiphertext != ""
	return d, err
}
func (n *PlatformAlertService) SetSecret(ctx context.Context, id uint, version uint64, secret string) (models.PlatformAlertDestination, error) {
	var d models.PlatformAlertDestination
	if len(secret) < 16 || len(secret) > 256 || version == 0 {
		return d, ErrLogInvalid
	}
	err := n.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND channel IN ?", id, []string{"webhook", "wecom"}).Take(&d).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrLogNotFound
		} else if err != nil {
			return err
		}
		if d.Version != version {
			return ErrLogConflict
		}
		credential := secret
		if d.Channel == "wecom" {
			var err error
			credential, err = wecomKey(secret)
			if err != nil {
				return err
			}
		}
		cipher, err := secretcipher.Encrypt(credential, n.key)
		if err != nil {
			return err
		}
		d.Version++
		d.SecretCiphertext = cipher
		return tx.Save(&d).Error
	})
	d.SecretConfigured = d.SecretCiphertext != ""
	return d, err
}
func (n *PlatformAlertService) Delete(ctx context.Context, id uint, version uint64) error {
	if version == 0 {
		return ErrLogInvalid
	}
	return n.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var d models.PlatformAlertDestination
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&d, id).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrLogNotFound
		} else if err != nil {
			return err
		}
		if d.Version != version {
			return ErrLogConflict
		}
		if err := tx.Model(&models.PlatformAlertDelivery{}).Where("destination_id=? AND status='pending'", id).Updates(map[string]any{"status": "cancelled", "secret_ciphertext": "", "next_attempt_at": nil}).Error; err != nil {
			return err
		}
		return tx.Delete(&d).Error
	})
}
func (n *PlatformAlertService) Status(ctx context.Context) (string, error) {
	d, err := n.Destinations(ctx)
	if err != nil {
		return "unknown", err
	}
	usable := false
	for _, dest := range d {
		if !dest.Enabled || !stringListContains(dest.Categories, models.PlatformAlertCategoryLog) {
			continue
		}
		if dest.Channel == "email" && n.email == nil {
			return "smtp_unconfigured", nil
		}
		if dest.Channel == "webhook" && (!dest.SecretConfigured || n.webhook == nil) {
			return "unconfigured", nil
		}
		if dest.Channel == "wecom" && (!dest.SecretConfigured || n.wecom == nil) {
			return "unconfigured", nil
		}
		usable = true
	}
	var failed int64
	if err = platformAlertDeliveryQuery(n.db.WithContext(ctx)).Where("i.category=? AND d.status='dead'", models.PlatformAlertCategoryLog).Count(&failed).Error; err != nil {
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
func (n *PlatformAlertService) RecordTx(tx *gorm.DB, event models.PlatformAlertEvent, incident models.PlatformAlertIncident, now time.Time) error {
	if !(PlatformAlertEvaluation{Category: incident.Category, SubjectKind: incident.SubjectKind, SubjectID: incident.SubjectID, Signal: incident.Signal, InstanceID: incident.InstanceID, Severity: incident.Severity, Verdict: PlatformAlertRisk}).valid() || event.IncidentID != incident.ID || (event.Type != "opened" && event.Type != "resolved") {
		return ErrLogInvalid
	}
	if incident.Category == models.PlatformAlertCategoryResource && (incident.Severity != "warning" || (incident.Signal != "node.cpu.busy_percent" && incident.Signal != "node.memory.used_percent")) {
		return ErrLogInvalid
	}
	var destinations []models.PlatformAlertDestination
	categoryJSON, _ := json.Marshal([]string{incident.Category})
	if err := tx.Where("enabled=true AND categories @> ?::jsonb", string(categoryJSON)).Order("id").Find(&destinations).Error; err != nil {
		return err
	}
	payload := map[string]any{"schema": "addp.platform-log-alert/v1", "event_id": event.ID, "event_type": event.Type, "incident_id": incident.ID, "node": incident.SubjectID, "signal": logSignalName(incident.Signal), "instance_id": incident.InstanceID, "severity": incident.Severity, "occurred_at": now}
	if incident.Category == models.PlatformAlertCategoryResource {
		payload = map[string]any{"schema": "addp.platform-resource-alert/v1", "event_id": event.ID, "event_type": event.Type, "incident_id": incident.ID, "category": incident.Category, "subject_kind": incident.SubjectKind, "subject_id": incident.SubjectID, "signal": incident.Signal, "severity": incident.Severity, "occurred_at": now}
	}
	body, _ := json.Marshal(payload)
	for _, d := range destinations {
		if !stringListContains(d.EventTypes, event.Type) {
			continue
		}
		status := "pending"
		if incident.SuppressedUntil != nil && incident.SuppressedUntil.After(now) {
			status = "suppressed"
		}
		delivery := models.PlatformAlertDelivery{ID: uuid.NewString(), EventID: event.ID, DestinationID: d.ID, Channel: d.Channel, URL: d.URL, SecretCiphertext: d.SecretCiphertext, Recipients: d.Recipients, Payload: string(body), Status: status, NextAttemptAt: &now, CreatedAt: now}
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
func (n *PlatformAlertService) send(ctx context.Context, d models.PlatformAlertDelivery, now time.Time) error {
	if d.Channel == "wecom" {
		if n.wecom == nil {
			return ErrLogInvalid
		}
		key, err := DecryptWebhookSecret(d.SecretCiphertext, n.key)
		if err != nil {
			return ErrLogInvalid
		}
		payload, err := wecomPlatformAlertPayload(d.Payload, d.ID)
		if err != nil {
			return err
		}
		_, err = n.wecom.SendWeComMessage(ctx, WebhookMessage{DeliveryID: d.ID, RequestURL: d.URL, Payload: payload}, key, now)
		return err
	}
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
	if d.Channel != "email" {
		return ErrLogInvalid
	}
	if n.email == nil {
		return ErrEmailSenderUnavailable
	}
	subject := "ADDP platform log pipeline alert"
	var message struct {
		Schema string `json:"schema"`
	}
	if json.Unmarshal([]byte(d.Payload), &message) != nil {
		return ErrLogInvalid
	}
	if message.Schema == "addp.platform-alert-notification-test/v1" {
		subject = "ADDP platform alert notification test"
	}
	if message.Schema == "addp.platform-resource-alert/v1" {
		subject = "ADDP platform resource alert"
	}
	return n.email.SendMessage(ctx, EmailMessage{DeliveryID: d.ID, Recipients: d.Recipients, Subject: subject, TextBody: d.Payload, HTMLBody: "<pre>" + html.EscapeString(d.Payload) + "</pre>"}, now)
}
func (n *PlatformAlertService) Test(ctx context.Context, id uint, now time.Time) error {
	var d models.PlatformAlertDestination
	if err := n.db.WithContext(ctx).First(&d, id).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrLogNotFound
	} else if err != nil {
		return err
	}
	return n.send(ctx, models.PlatformAlertDelivery{ID: uuid.NewString(), Channel: d.Channel, URL: d.URL, SecretCiphertext: d.SecretCiphertext, Recipients: d.Recipients, Payload: `{"schema":"addp.platform-alert-notification-test/v1","test":true}`}, now)
}
func (n *PlatformAlertService) DispatchOnce(ctx context.Context, now time.Time) (bool, error) {
	claim := uuid.NewString()
	var deliveries []models.PlatformAlertDelivery
	err := n.db.WithContext(ctx).Raw(`WITH candidate AS (
 SELECT id FROM monitor.platform_alert_deliveries WHERE ((status='pending' AND next_attempt_at<=?) OR (status='delivering' AND lease_expires_at<=?)) AND (channel='webhook' OR (channel='wecom' AND ?) OR (channel='email' AND ?))
 ORDER BY next_attempt_at NULLS FIRST,id LIMIT 1 FOR UPDATE SKIP LOCKED)
 UPDATE monitor.platform_alert_deliveries d SET status='delivering',attempt_count=d.attempt_count+1,claim_id=?,lease_expires_at=? FROM candidate WHERE d.id=candidate.id RETURNING d.*`, now, now, n.wecom != nil, n.email != nil, claim, now.Add(n.lease)).Scan(&deliveries).Error
	if err != nil || len(deliveries) == 0 {
		return false, err
	}
	d := deliveries[0]
	cycleAttempt := d.AttemptCount - d.RetryBaseAttemptCount
	if cycleAttempt > n.maxAttempts {
		result := n.db.WithContext(ctx).Model(&models.PlatformAlertDelivery{}).Where("id=? AND status='delivering' AND claim_id=?", d.ID, claim).Updates(map[string]any{"status": "dead", "last_error": "notification_attempt_limit", "secret_ciphertext": "", "claim_id": "", "lease_expires_at": nil, "next_attempt_at": nil})
		return true, result.Error
	}
	// Re-read suppression and destination while the claim is current. A request already sent may complete.
	var event models.PlatformAlertEvent
	if err = n.db.WithContext(ctx).First(&event, "id=?", d.EventID).Error; err != nil {
		return true, err
	}
	var i models.PlatformAlertIncident
	if err = n.db.WithContext(ctx).First(&i, event.IncidentID).Error; err != nil {
		return true, err
	}
	var dest models.PlatformAlertDestination
	targetErr := n.db.WithContext(ctx).First(&dest, d.DestinationID).Error
	updates := map[string]any{"claim_id": "", "lease_expires_at": nil, "next_attempt_at": nil}
	if i.SuppressedUntil != nil && i.SuppressedUntil.After(now) {
		updates["status"] = "suppressed"
		updates["secret_ciphertext"] = ""
	} else if errors.Is(targetErr, gorm.ErrRecordNotFound) || targetErr == nil && (!dest.Enabled || !stringListContains(dest.Categories, i.Category)) {
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
			updates["last_error"] = platformNotificationError(sendErr)
			if cycleAttempt >= n.maxAttempts {
				updates["status"] = "dead"
				updates["secret_ciphertext"] = ""
			} else {
				updates["status"] = "pending"
				updates["next_attempt_at"] = now.Add(notificationBackoff(n.retryInitial, n.retryMax, cycleAttempt))
			}
		}
	}
	result := n.db.WithContext(ctx).Model(&models.PlatformAlertDelivery{}).Where("id=? AND status='delivering' AND claim_id=?", d.ID, claim).Updates(updates)
	if result.Error != nil {
		return true, result.Error
	}
	if result.RowsAffected != 1 {
		return true, ErrLogConflict
	}
	return true, nil
}
func (n *PlatformAlertService) Run(ctx context.Context) {
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

type PlatformAlertDeliveryList struct {
	Data       []PlatformAlertDeliveryView `json:"data"`
	Total      int64                       `json:"total"`
	Page       int                         `json:"page"`
	PageSize   int                         `json:"page_size"`
	TotalPages int                         `json:"total_pages"`
}

func (n *PlatformAlertService) Deliveries(ctx context.Context, page, size int) (PlatformAlertDeliveryList, error) {
	r := PlatformAlertDeliveryList{Data: []PlatformAlertDeliveryView{}, Page: page, PageSize: size}
	if page < 1 || page > 10000 || size < 1 || size > 100 {
		return r, ErrLogInvalid
	}
	q := platformAlertDeliveryQuery(n.db.WithContext(ctx))
	if err := q.Count(&r.Total).Error; err != nil {
		return r, err
	}
	r.TotalPages = int((r.Total + int64(size) - 1) / int64(size))
	err := platformAlertDeliveryQuery(n.db.WithContext(ctx)).Order("d.created_at DESC,d.id DESC").Offset((page - 1) * size).Limit(size).Scan(&r.Data).Error
	return r, err
}

func platformNotificationError(err error) string {
	for _, entry := range []struct {
		err  error
		code string
	}{
		{ErrWeComRateLimited, "wecom_rate_limited"}, {ErrWeComRejected, "wecom_rejected"},
		{ErrWeComResponse, "wecom_invalid_response"}, {ErrWeComNetwork, "wecom_network_failed"}, {ErrWeComHTTP, "wecom_http_failed"},
	} {
		if errors.Is(err, entry.err) {
			return entry.code
		}
	}
	return "notification_send_failed"
}
