package service

import (
	"time"

	monitorModels "github.com/addp/monitor/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type NotificationService struct {
	webhookService *WebhookService
	emailService   *EmailService
}

func NewNotificationService(webhookService *WebhookService, emailService *EmailService) *NotificationService {
	return &NotificationService{webhookService: webhookService, emailService: emailService}
}

func (s *NotificationService) RecordAlertEventTx(
	tx *gorm.DB,
	incident monitorModels.AlertIncident,
	eventType string,
	fromSeverity string,
	now time.Time,
) error {
	event := monitorModels.AlertEvent{
		EventID: uuid.NewString(), TenantID: incident.TenantID, IncidentID: incident.ID,
		EventType: eventType, FromSeverity: fromSeverity, ToSeverity: incident.Severity, OccurredAt: now,
	}
	if err := tx.Create(&event).Error; err != nil {
		return err
	}
	if s.webhookService != nil {
		if err := s.webhookService.CreateDeliveriesTx(tx, event, incident, now); err != nil {
			return err
		}
	}
	if s.emailService != nil {
		if err := s.emailService.CreateDeliveriesTx(tx, event, incident, now); err != nil {
			return err
		}
	}
	return nil
}
