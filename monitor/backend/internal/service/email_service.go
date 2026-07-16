package service

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/mail"
	"sort"
	"strings"
	"time"

	monitorModels "github.com/addp/monitor/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrEmailDestinationInvalid   = errors.New("invalid email destination")
	ErrEmailDestinationNotFound  = errors.New("email destination not found")
	ErrEmailDestinationConflict  = errors.New("email destination name already exists")
	ErrEmailDeliveryInvalid      = errors.New("invalid email delivery")
	ErrEmailDeliveryNotFound     = errors.New("email delivery not found")
	ErrEmailDeliveryNotRetryable = errors.New("email delivery is not retryable")
	ErrEmailSenderUnavailable    = errors.New("email sender is unavailable")
	ErrEmailTestFailed           = errors.New("email test delivery failed")
)

type EmailService struct {
	db         *gorm.DB
	consoleURL string
	sender     EmailSender
}

func NewEmailService(db *gorm.DB, consoleURL string, sender EmailSender) *EmailService {
	return &EmailService{db: db, consoleURL: strings.TrimRight(consoleURL, "/"), sender: sender}
}

type CreateEmailDestinationInput struct {
	TenantID   int
	Name       string
	Recipients []string
	Enabled    bool
	EventTypes []string
}

type UpdateEmailDestinationInput struct {
	TenantID   int
	ID         uint
	Name       *string
	Recipients *[]string
	Enabled    *bool
	EventTypes *[]string
}

type ListEmailDeliveriesRequest struct {
	TenantID      int
	DestinationID uint
	Status        string
	EventType     string
	Page          int
	PageSize      int
}

type ListEmailDeliveriesResponse struct {
	Data       []monitorModels.EmailDelivery `json:"data"`
	Total      int64                         `json:"total"`
	Page       int                           `json:"page"`
	PageSize   int                           `json:"page_size"`
	TotalPages int                           `json:"total_pages"`
}

type EmailTestResult struct {
	DeliveryID string `json:"delivery_id"`
	Recipients int    `json:"recipients"`
}

func (s *EmailService) ListDestinations(ctx context.Context, tenantID int) ([]monitorModels.EmailDestination, error) {
	var destinations []monitorModels.EmailDestination
	err := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).
		Order("created_at DESC, id DESC").Find(&destinations).Error
	return destinations, err
}

func (s *EmailService) CreateDestination(ctx context.Context, input CreateEmailDestinationInput) (*monitorModels.EmailDestination, error) {
	name, recipients, events, err := validateEmailDestination(input.Name, input.Recipients, input.EventTypes)
	if err != nil {
		return nil, err
	}
	destination := monitorModels.EmailDestination{
		TenantID: input.TenantID, Name: name, Recipients: monitorModels.StringList(recipients),
		Enabled: input.Enabled, EventTypes: monitorModels.StringList(events),
	}
	result := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "tenant_id"}, {Name: "name"}}, DoNothing: true,
	}).Create(&destination)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, ErrEmailDestinationConflict
	}
	return &destination, nil
}

func (s *EmailService) UpdateDestination(ctx context.Context, input UpdateEmailDestinationInput) (*monitorModels.EmailDestination, error) {
	var destination monitorModels.EmailDestination
	if err := s.db.WithContext(ctx).Where("id = ? AND tenant_id = ?", input.ID, input.TenantID).First(&destination).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrEmailDestinationNotFound
		}
		return nil, err
	}
	if input.Name == nil && input.Recipients == nil && input.Enabled == nil && input.EventTypes == nil {
		return nil, ErrEmailDestinationInvalid
	}
	name := destination.Name
	if input.Name != nil {
		name = *input.Name
	}
	recipients := []string(destination.Recipients)
	if input.Recipients != nil {
		recipients = *input.Recipients
	}
	events := []string(destination.EventTypes)
	if input.EventTypes != nil {
		events = *input.EventTypes
	}
	name, recipients, events, err := validateEmailDestination(name, recipients, events)
	if err != nil {
		return nil, err
	}
	var duplicateCount int64
	if err := s.db.WithContext(ctx).Model(&monitorModels.EmailDestination{}).
		Where("tenant_id = ? AND name = ? AND id <> ?", input.TenantID, name, input.ID).
		Count(&duplicateCount).Error; err != nil {
		return nil, err
	}
	if duplicateCount > 0 {
		return nil, ErrEmailDestinationConflict
	}
	updates := map[string]interface{}{
		"name": name, "recipients": monitorModels.StringList(recipients),
		"event_types": monitorModels.StringList(events), "updated_at": time.Now(),
	}
	if input.Enabled != nil {
		updates["enabled"] = *input.Enabled
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&monitorModels.EmailDestination{}).
			Where("id = ? AND tenant_id = ?", input.ID, input.TenantID).Updates(updates).Error; err != nil {
			return err
		}
		if input.Enabled != nil && !*input.Enabled {
			return tx.Model(&monitorModels.EmailDelivery{}).
				Where("tenant_id = ? AND destination_id = ? AND status = ?", input.TenantID, input.ID, monitorModels.EmailDeliveryPending).
				Updates(map[string]interface{}{
					"status": monitorModels.EmailDeliveryCancelled, "next_attempt_at": nil, "updated_at": time.Now(),
				}).Error
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &destination, s.db.WithContext(ctx).Where("id = ? AND tenant_id = ?", input.ID, input.TenantID).First(&destination).Error
}

func (s *EmailService) DeleteDestination(ctx context.Context, tenantID int, id uint, now time.Time) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var destination monitorModels.EmailDestination
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND tenant_id = ?", id, tenantID).First(&destination).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrEmailDestinationNotFound
			}
			return err
		}
		if err := tx.Model(&monitorModels.EmailDelivery{}).
			Where("tenant_id = ? AND destination_id = ? AND status = ?", tenantID, id, monitorModels.EmailDeliveryPending).
			Updates(map[string]interface{}{
				"status": monitorModels.EmailDeliveryCancelled, "next_attempt_at": nil, "updated_at": now,
			}).Error; err != nil {
			return err
		}
		if err := tx.Where("tenant_id = ? AND channel = ? AND destination_id = ?", tenantID, monitorModels.NotificationChannelEmail, id).
			Delete(&monitorModels.NotificationRoute{}).Error; err != nil {
			return err
		}
		return tx.Delete(&destination).Error
	})
}

func (s *EmailService) TestDestination(ctx context.Context, tenantID int, id uint, now time.Time) (*EmailTestResult, error) {
	var destination monitorModels.EmailDestination
	if err := s.db.WithContext(ctx).Where("id = ? AND tenant_id = ?", id, tenantID).First(&destination).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrEmailDestinationNotFound
		}
		return nil, err
	}
	if s.sender == nil {
		return nil, ErrEmailSenderUnavailable
	}
	deliveryID := uuid.NewString()
	delivery := monitorModels.EmailDelivery{
		DeliveryID: deliveryID, Recipients: destination.Recipients,
		Subject:  "[ADDP] 邮件通知测试 / Email notification test",
		TextBody: "这是一封 ADDP Monitor 测试邮件。\nThis is an ADDP Monitor test email.\n\nDelivery ID: " + deliveryID,
		HTMLBody: "<p>这是一封 ADDP Monitor 测试邮件。</p><p>This is an ADDP Monitor test email.</p><p>Delivery ID: <code>" + html.EscapeString(deliveryID) + "</code></p>",
	}
	if err := s.sender.Send(ctx, delivery, now); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEmailTestFailed, err)
	}
	return &EmailTestResult{DeliveryID: deliveryID, Recipients: len(destination.Recipients)}, nil
}

func (s *EmailService) RetryDelivery(ctx context.Context, tenantID int, deliveryID string, now time.Time) (*monitorModels.EmailDelivery, error) {
	deliveryID = strings.TrimSpace(deliveryID)
	if _, err := uuid.Parse(deliveryID); err != nil {
		return nil, ErrEmailDeliveryInvalid
	}
	var updated monitorModels.EmailDelivery
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var delivery monitorModels.EmailDelivery
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("delivery_id = ? AND tenant_id = ?", deliveryID, tenantID).First(&delivery).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrEmailDeliveryNotFound
			}
			return err
		}
		if delivery.Status != monitorModels.EmailDeliveryDead {
			return ErrEmailDeliveryNotRetryable
		}
		var destination monitorModels.EmailDestination
		if err := tx.Where("id = ? AND tenant_id = ? AND enabled = ?", delivery.DestinationID, tenantID, true).
			First(&destination).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrEmailDeliveryNotRetryable
			}
			return err
		}
		updates := map[string]interface{}{
			"destination_name": destination.Name, "recipients": destination.Recipients,
			"status":                   monitorModels.EmailDeliveryPending,
			"retry_base_attempt_count": delivery.AttemptCount,
			"manual_retry_count":       gorm.Expr("manual_retry_count + 1"),
			"next_attempt_at":          now, "claimed_by": "", "lease_expires_at": nil,
			"last_error": "", "delivered_at": nil, "updated_at": now,
		}
		if err := tx.Model(&monitorModels.EmailDelivery{}).Where("id = ?", delivery.ID).Updates(updates).Error; err != nil {
			return err
		}
		return tx.First(&updated, delivery.ID).Error
	})
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

func (s *EmailService) ListDeliveries(ctx context.Context, request ListEmailDeliveriesRequest) (*ListEmailDeliveriesResponse, error) {
	if request.Page <= 0 {
		request.Page = 1
	}
	if request.PageSize <= 0 || request.PageSize > 100 {
		request.PageSize = 20
	}
	query := s.db.WithContext(ctx).Model(&monitorModels.EmailDelivery{}).Where("tenant_id = ?", request.TenantID)
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
	var deliveries []monitorModels.EmailDelivery
	if err := query.Order("created_at DESC, id DESC").Offset((request.Page - 1) * request.PageSize).
		Limit(request.PageSize).Find(&deliveries).Error; err != nil {
		return nil, err
	}
	return &ListEmailDeliveriesResponse{
		Data: deliveries, Total: total, Page: request.Page, PageSize: request.PageSize,
		TotalPages: int((total + int64(request.PageSize) - 1) / int64(request.PageSize)),
	}, nil
}

func (s *EmailService) CreateDeliveriesTx(tx *gorm.DB, event monitorModels.AlertEvent, incident monitorModels.AlertIncident, now time.Time) error {
	var destinations []monitorModels.EmailDestination
	query := tx.Where("tenant_id = ? AND enabled = ?", incident.TenantID, true)
	if incident.AlertRuleID != nil {
		query = query.Where(`id IN (
			SELECT destination_id FROM monitor.notification_routes
			WHERE tenant_id = ? AND alert_rule_id = ? AND channel = ?
		)`, incident.TenantID, *incident.AlertRuleID, monitorModels.NotificationChannelEmail)
	}
	if err := query.Find(&destinations).Error; err != nil {
		return err
	}
	for _, destination := range destinations {
		if !containsString(destination.EventTypes, event.EventType) {
			continue
		}
		deliveryID := uuid.NewString()
		subject, textBody, htmlBody := s.alertEmailContent(deliveryID, event, incident)
		status := monitorModels.EmailDeliveryPending
		var nextAttemptAt *time.Time
		if incident.SuppressedUntil != nil && incident.SuppressedUntil.After(now) {
			status = monitorModels.EmailDeliverySuppressed
		} else {
			next := now
			nextAttemptAt = &next
		}
		delivery := monitorModels.EmailDelivery{
			DeliveryID: deliveryID, TenantID: incident.TenantID, DestinationID: destination.ID,
			DestinationName: destination.Name, AlertEventID: event.ID, IncidentID: incident.ID,
			EventType: event.EventType, Recipients: destination.Recipients, Subject: subject,
			TextBody: textBody, HTMLBody: htmlBody, Status: status, NextAttemptAt: nextAttemptAt,
		}
		if err := tx.Create(&delivery).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *EmailService) alertEmailContent(deliveryID string, event monitorModels.AlertEvent, incident monitorModels.AlertIncident) (string, string, string) {
	consoleURL := s.consoleURL + "/monitor/alerts"
	subject := fmt.Sprintf("[ADDP][%s] %s: %s", strings.ToUpper(incident.Severity), event.EventType, incident.SignalCode)
	textBody := fmt.Sprintf(`ADDP Monitor 告警通知 / Alert notification

Event: %s
Severity: %s
Signal: %s
Module: %s
Task type: %s
Task ID: %s
Execution ID: %s
Occurred at: %s
Delivery ID: %s

Console: %s
`, event.EventType, incident.Severity, incident.SignalCode, incident.Module, incident.TaskType,
		incident.SourceTaskID, incident.ExecutionID, event.OccurredAt.UTC().Format(time.RFC3339), deliveryID, consoleURL)
	htmlBody := fmt.Sprintf(`<h2>ADDP Monitor 告警通知 / Alert notification</h2>
<table><tbody>
<tr><th align="left">Event</th><td>%s</td></tr>
<tr><th align="left">Severity</th><td>%s</td></tr>
<tr><th align="left">Signal</th><td>%s</td></tr>
<tr><th align="left">Module</th><td>%s</td></tr>
<tr><th align="left">Task type</th><td>%s</td></tr>
<tr><th align="left">Task ID</th><td>%s</td></tr>
<tr><th align="left">Execution ID</th><td>%s</td></tr>
<tr><th align="left">Occurred at</th><td>%s</td></tr>
<tr><th align="left">Delivery ID</th><td><code>%s</code></td></tr>
</tbody></table>
<p><a href="%s">Open ADDP Console</a></p>`, html.EscapeString(event.EventType), html.EscapeString(incident.Severity),
		html.EscapeString(incident.SignalCode), html.EscapeString(incident.Module), html.EscapeString(incident.TaskType),
		html.EscapeString(incident.SourceTaskID), html.EscapeString(incident.ExecutionID),
		html.EscapeString(event.OccurredAt.UTC().Format(time.RFC3339)), html.EscapeString(deliveryID), html.EscapeString(consoleURL))
	return subject, textBody, htmlBody
}

func validateEmailDestination(name string, recipients, eventTypes []string) (string, []string, []string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 {
		return "", nil, nil, ErrEmailDestinationInvalid
	}
	normalizedRecipients, err := normalizeEmailRecipients(recipients)
	if err != nil {
		return "", nil, nil, err
	}
	events, err := normalizeAlertEventTypes(eventTypes)
	if err != nil {
		return "", nil, nil, ErrEmailDestinationInvalid
	}
	return name, normalizedRecipients, events, nil
}

func normalizeEmailRecipients(values []string) ([]string, error) {
	seen := make(map[string]string, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		address, err := mail.ParseAddress(value)
		if err != nil || address.Name != "" || address.Address != value {
			return nil, ErrEmailDestinationInvalid
		}
		key := strings.ToLower(address.Address)
		seen[key] = address.Address
	}
	if len(seen) == 0 || len(seen) > 50 {
		return nil, ErrEmailDestinationInvalid
	}
	result := make([]string, 0, len(seen))
	for _, value := range seen {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return strings.ToLower(result[i]) < strings.ToLower(result[j]) })
	return result, nil
}
