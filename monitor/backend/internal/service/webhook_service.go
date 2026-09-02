package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	commonModels "github.com/addp/common/models"
	secretcipher "github.com/addp/common/secretcipher"
	monitorModels "github.com/addp/monitor/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrWebhookDestinationInvalid   = errors.New("invalid webhook destination")
	ErrWebhookDestinationNotFound  = errors.New("webhook destination not found")
	ErrWebhookDestinationConflict  = errors.New("webhook destination name already exists")
	ErrWebhookDeliveryInvalid      = errors.New("invalid webhook delivery")
	ErrWebhookDeliveryNotFound     = errors.New("webhook delivery not found")
	ErrWebhookDeliveryNotRetryable = errors.New("webhook delivery is not retryable")
	ErrWebhookTestFailed           = errors.New("webhook test delivery failed")
)

var alertEventOrder = []string{
	monitorModels.AlertEventOpened,
	monitorModels.AlertEventEscalated,
	monitorModels.AlertEventResolved,
}

type WebhookService struct {
	db            *gorm.DB
	encryptionKey []byte
	allowPrivate  bool
	consoleURL    string
	sender        WebhookSender
}

func NewWebhookService(db *gorm.DB, encryptionKey []byte, allowPrivate bool, consoleURL string, sender WebhookSender) *WebhookService {
	return &WebhookService{
		db:            db,
		encryptionKey: encryptionKey,
		allowPrivate:  allowPrivate,
		consoleURL:    strings.TrimRight(consoleURL, "/"),
		sender:        sender,
	}
}

type CreateWebhookDestinationInput struct {
	TenantID   int
	Name       string
	URL        string
	Secret     string
	Enabled    bool
	EventTypes []string
}

type UpdateWebhookDestinationInput struct {
	TenantID   int
	ID         uint
	Name       *string
	URL        *string
	Secret     *string
	Enabled    *bool
	EventTypes *[]string
}

type ListWebhookDeliveriesRequest struct {
	TenantID      int
	DestinationID uint
	Status        string
	EventType     string
	Page          int
	PageSize      int
}

type ListWebhookDeliveriesResponse struct {
	Data       []monitorModels.WebhookDelivery `json:"data"`
	Total      int64                           `json:"total"`
	Page       int                             `json:"page"`
	PageSize   int                             `json:"page_size"`
	TotalPages int                             `json:"total_pages"`
}

type WebhookTestResult struct {
	DeliveryID string `json:"delivery_id"`
	HTTPStatus int    `json:"http_status"`
}

func (s *WebhookService) ListDestinations(ctx context.Context, tenantID int) ([]monitorModels.WebhookDestination, error) {
	var destinations []monitorModels.WebhookDestination
	if err := s.db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Order("created_at DESC, id DESC").
		Find(&destinations).Error; err != nil {
		return nil, err
	}
	markWebhookSecretsConfigured(destinations)
	return destinations, nil
}

func (s *WebhookService) CreateDestination(ctx context.Context, input CreateWebhookDestinationInput) (*monitorModels.WebhookDestination, error) {
	name, targetURL, events, err := s.validateDestination(ctx, input.Name, input.URL, input.Secret, input.EventTypes, true)
	if err != nil {
		return nil, err
	}
	ciphertext, err := secretcipher.Encrypt(input.Secret, s.encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("encrypt webhook secret: %w", err)
	}
	destination := monitorModels.WebhookDestination{
		TenantID:         input.TenantID,
		Name:             name,
		URL:              targetURL,
		SecretCiphertext: ciphertext,
		Enabled:          input.Enabled,
		EventTypes:       monitorModels.StringList(events),
	}
	result := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "tenant_id"}, {Name: "name"}},
		DoNothing: true,
	}).Create(&destination)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, ErrWebhookDestinationConflict
	}
	destination.SecretConfigured = true
	return &destination, nil
}

func (s *WebhookService) UpdateDestination(ctx context.Context, input UpdateWebhookDestinationInput) (*monitorModels.WebhookDestination, error) {
	var destination monitorModels.WebhookDestination
	if err := s.db.WithContext(ctx).Where("id = ? AND tenant_id = ?", input.ID, input.TenantID).First(&destination).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrWebhookDestinationNotFound
		}
		return nil, err
	}
	if input.Name == nil && input.URL == nil && input.Secret == nil && input.Enabled == nil && input.EventTypes == nil {
		return nil, ErrWebhookDestinationInvalid
	}

	name := destination.Name
	if input.Name != nil {
		name = strings.TrimSpace(*input.Name)
	}
	targetURL := destination.URL
	if input.URL != nil {
		targetURL = strings.TrimSpace(*input.URL)
	}
	events := []string(destination.EventTypes)
	if input.EventTypes != nil {
		events = *input.EventTypes
	}
	secretForValidation := strings.Repeat("x", 16)
	if input.Secret != nil {
		secretForValidation = *input.Secret
	}
	name, targetURL, events, err := s.validateDestination(ctx, name, targetURL, secretForValidation, events, input.URL != nil)
	if err != nil {
		return nil, err
	}
	var duplicateCount int64
	if err := s.db.WithContext(ctx).Model(&monitorModels.WebhookDestination{}).
		Where("tenant_id = ? AND name = ? AND id <> ?", input.TenantID, name, input.ID).
		Count(&duplicateCount).Error; err != nil {
		return nil, err
	}
	if duplicateCount > 0 {
		return nil, ErrWebhookDestinationConflict
	}

	updates := map[string]interface{}{
		"name": name, "url": targetURL, "event_types": monitorModels.StringList(events), "updated_at": time.Now(),
	}
	if input.Enabled != nil {
		updates["enabled"] = *input.Enabled
	}
	if input.Secret != nil {
		ciphertext, encryptErr := secretcipher.Encrypt(*input.Secret, s.encryptionKey)
		if encryptErr != nil {
			return nil, fmt.Errorf("encrypt webhook secret: %w", encryptErr)
		}
		updates["secret_ciphertext"] = ciphertext
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&monitorModels.WebhookDestination{}).
			Where("id = ? AND tenant_id = ?", input.ID, input.TenantID).
			Updates(updates).Error; err != nil {
			return err
		}
		if input.Enabled != nil && !*input.Enabled {
			return tx.Model(&monitorModels.WebhookDelivery{}).
				Where("tenant_id = ? AND destination_id = ? AND status = ?", input.TenantID, input.ID, monitorModels.WebhookDeliveryPending).
				Updates(map[string]interface{}{
					"status": monitorModels.WebhookDeliveryCancelled, "secret_ciphertext": "", "next_attempt_at": nil, "updated_at": time.Now(),
				}).Error
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Where("id = ? AND tenant_id = ?", input.ID, input.TenantID).First(&destination).Error; err != nil {
		return nil, err
	}
	destination.SecretConfigured = destination.SecretCiphertext != ""
	return &destination, nil
}

func (s *WebhookService) DeleteDestination(ctx context.Context, tenantID int, id uint, now time.Time) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var destination monitorModels.WebhookDestination
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND tenant_id = ?", id, tenantID).First(&destination).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrWebhookDestinationNotFound
			}
			return err
		}
		if err := tx.Model(&monitorModels.WebhookDelivery{}).
			Where("tenant_id = ? AND destination_id = ? AND status = ?", tenantID, id, monitorModels.WebhookDeliveryPending).
			Updates(map[string]interface{}{
				"status": monitorModels.WebhookDeliveryCancelled, "secret_ciphertext": "",
				"next_attempt_at": nil, "updated_at": now,
			}).Error; err != nil {
			return err
		}
		if err := tx.Where("tenant_id = ? AND channel = ? AND destination_id = ?", tenantID, monitorModels.NotificationChannelWebhook, id).
			Delete(&monitorModels.NotificationRoute{}).Error; err != nil {
			return err
		}
		return tx.Delete(&destination).Error
	})
}

func (s *WebhookService) TestDestination(ctx context.Context, tenantID int, id uint, now time.Time) (*WebhookTestResult, error) {
	var destination monitorModels.WebhookDestination
	if err := s.db.WithContext(ctx).Where("id = ? AND tenant_id = ?", id, tenantID).First(&destination).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrWebhookDestinationNotFound
		}
		return nil, err
	}
	secret, err := DecryptWebhookSecret(destination.SecretCiphertext, s.encryptionKey)
	if err != nil {
		return nil, err
	}
	if s.sender == nil {
		return nil, errors.New("webhook sender is unavailable")
	}
	deliveryID := uuid.NewString()
	delivery := monitorModels.WebhookDelivery{
		DeliveryID: deliveryID,
		RequestURL: destination.URL,
		Payload: commonModels.JSONMap{
			"schema_version": "monitor.webhook.test/v1",
			"delivery_id":    deliveryID,
			"sent_at":        now,
			"destination":    map[string]interface{}{"id": destination.ID, "name": destination.Name},
		},
	}
	result, err := s.sender.Send(ctx, delivery, secret, now)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrWebhookTestFailed, err)
	}
	return &WebhookTestResult{DeliveryID: deliveryID, HTTPStatus: result.HTTPStatus}, nil
}

func (s *WebhookService) RetryDelivery(ctx context.Context, tenantID int, deliveryID string, now time.Time) (*monitorModels.WebhookDelivery, error) {
	deliveryID = strings.TrimSpace(deliveryID)
	if _, err := uuid.Parse(deliveryID); err != nil {
		return nil, ErrWebhookDeliveryInvalid
	}
	var updated monitorModels.WebhookDelivery
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var delivery monitorModels.WebhookDelivery
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("delivery_id = ? AND tenant_id = ?", deliveryID, tenantID).First(&delivery).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrWebhookDeliveryNotFound
			}
			return err
		}
		if delivery.Status != monitorModels.WebhookDeliveryDead {
			return ErrWebhookDeliveryNotRetryable
		}
		var destination monitorModels.WebhookDestination
		if err := tx.Where("id = ? AND tenant_id = ? AND enabled = ?", delivery.DestinationID, tenantID, true).
			First(&destination).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrWebhookDeliveryNotRetryable
			}
			return err
		}
		updates := map[string]interface{}{
			"destination_name": destination.Name, "request_url": destination.URL,
			"secret_ciphertext": destination.SecretCiphertext, "status": monitorModels.WebhookDeliveryPending,
			"retry_base_attempt_count": delivery.AttemptCount,
			"manual_retry_count":       gorm.Expr("manual_retry_count + 1"),
			"next_attempt_at":          now, "claimed_by": "", "lease_expires_at": nil,
			"last_http_status": nil, "last_error": "", "delivered_at": nil, "updated_at": now,
		}
		if err := tx.Model(&monitorModels.WebhookDelivery{}).Where("id = ?", delivery.ID).Updates(updates).Error; err != nil {
			return err
		}
		return tx.First(&updated, delivery.ID).Error
	})
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

func (s *WebhookService) ListDeliveries(ctx context.Context, request ListWebhookDeliveriesRequest) (*ListWebhookDeliveriesResponse, error) {
	if request.Page <= 0 {
		request.Page = 1
	}
	if request.PageSize <= 0 || request.PageSize > 100 {
		request.PageSize = 20
	}
	query := s.db.WithContext(ctx).Model(&monitorModels.WebhookDelivery{}).Where("tenant_id = ?", request.TenantID)
	if request.DestinationID != 0 {
		query = query.Where("destination_id = ?", request.DestinationID)
	}
	if request.Status != "" {
		query = query.Where("status = ?", request.Status)
	}
	if request.EventType != "" {
		query = query.Where("event_type = ?", request.EventType)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	var deliveries []monitorModels.WebhookDelivery
	if err := query.Order("created_at DESC, id DESC").
		Offset((request.Page - 1) * request.PageSize).
		Limit(request.PageSize).
		Find(&deliveries).Error; err != nil {
		return nil, err
	}
	return &ListWebhookDeliveriesResponse{
		Data: deliveries, Total: total, Page: request.Page, PageSize: request.PageSize,
		TotalPages: int((total + int64(request.PageSize) - 1) / int64(request.PageSize)),
	}, nil
}

func (s *WebhookService) CreateDeliveriesTx(tx *gorm.DB, event monitorModels.AlertEvent, incident monitorModels.AlertIncident, now time.Time) error {
	var destinations []monitorModels.WebhookDestination
	query := tx.Where("tenant_id = ? AND enabled = ?", incident.TenantID, true)
	if incident.AlertRuleID != nil {
		query = query.Where(`id IN (
			SELECT destination_id FROM monitor.notification_routes
			WHERE tenant_id = ? AND alert_rule_id = ? AND channel = ?
		)`, incident.TenantID, *incident.AlertRuleID, monitorModels.NotificationChannelWebhook)
	}
	if err := query.Find(&destinations).Error; err != nil {
		return err
	}
	for _, destination := range destinations {
		if !containsString(destination.EventTypes, event.EventType) {
			continue
		}
		deliveryID := uuid.NewString()
		status := monitorModels.WebhookDeliveryPending
		secretCiphertext := destination.SecretCiphertext
		var nextAttemptAt *time.Time
		if incident.SuppressedUntil != nil && incident.SuppressedUntil.After(now) {
			status = monitorModels.WebhookDeliverySuppressed
			secretCiphertext = ""
		} else {
			next := now
			nextAttemptAt = &next
		}
		delivery := monitorModels.WebhookDelivery{
			DeliveryID: deliveryID, TenantID: incident.TenantID, DestinationID: destination.ID,
			DestinationName: destination.Name, AlertEventID: event.ID, IncidentID: incident.ID,
			EventType: event.EventType, RequestURL: destination.URL, SecretCiphertext: secretCiphertext,
			Payload: s.webhookPayload(deliveryID, event, incident), Status: status, NextAttemptAt: nextAttemptAt,
		}
		if err := tx.Create(&delivery).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *WebhookService) validateDestination(ctx context.Context, name, targetURL, secret string, eventTypes []string, validateTarget bool) (string, string, []string, error) {
	name = strings.TrimSpace(name)
	targetURL = strings.TrimSpace(targetURL)
	if name == "" || len(name) > 100 || len(targetURL) > 2048 || len(secret) < 16 || len(secret) > 256 {
		return "", "", nil, ErrWebhookDestinationInvalid
	}
	events, err := normalizeAlertEventTypes(eventTypes)
	if err != nil {
		return "", "", nil, err
	}
	if validateTarget {
		validationCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := ValidateWebhookURL(validationCtx, targetURL, s.allowPrivate); err != nil {
			return "", "", nil, ErrWebhookDestinationInvalid
		}
	}
	return name, targetURL, events, nil
}

func (s *WebhookService) webhookPayload(deliveryID string, event monitorModels.AlertEvent, incident monitorModels.AlertIncident) commonModels.JSONMap {
	return commonModels.JSONMap{
		"schema_version": "monitor.alert.webhook/v1",
		"delivery_id":    deliveryID,
		"event": map[string]interface{}{
			"event_id": event.EventID, "type": event.EventType, "occurred_at": event.OccurredAt,
			"from_severity": event.FromSeverity, "to_severity": event.ToSeverity,
		},
		"incident": map[string]interface{}{
			"id": incident.ID, "module": incident.Module, "task_type": incident.TaskType,
			"source_task_id": incident.SourceTaskID, "execution_id": incident.ExecutionID,
			"signal_code": incident.SignalCode, "severity": incident.Severity, "status": incident.Status,
			"opened_at": incident.OpenedAt, "resolved_at": incident.ResolvedAt,
		},
		"console_url": s.consoleURL + "/monitor/alerts",
	}
}

func normalizeAlertEventTypes(values []string) ([]string, error) {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if !containsString(alertEventOrder, value) {
			return nil, ErrWebhookDestinationInvalid
		}
		seen[value] = struct{}{}
	}
	if len(seen) == 0 {
		return nil, ErrWebhookDestinationInvalid
	}
	result := make([]string, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool {
		return indexOfString(alertEventOrder, result[i]) < indexOfString(alertEventOrder, result[j])
	})
	return result, nil
}

func containsString(values []string, target string) bool {
	return indexOfString(values, target) >= 0
}

func indexOfString(values []string, target string) int {
	for index, value := range values {
		if value == target {
			return index
		}
	}
	return -1
}

func markWebhookSecretsConfigured(destinations []monitorModels.WebhookDestination) {
	for index := range destinations {
		destinations[index].SecretConfigured = destinations[index].SecretCiphertext != ""
	}
}
