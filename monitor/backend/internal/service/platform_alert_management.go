package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/addp/monitor/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func validPlatformAlertCategory(category string) bool {
	return category == models.PlatformAlertCategoryLog || category == models.PlatformAlertCategoryResource || category == models.PlatformAlertCategoryHTTP || category == models.PlatformAlertCategoryProcess
}

type PlatformAlertList struct {
	Data       []models.PlatformAlertIncident `json:"data"`
	Total      int64                          `json:"total"`
	Page       int                            `json:"page"`
	PageSize   int                            `json:"page_size"`
	TotalPages int                            `json:"total_pages"`
}

func (s *PlatformAlertService) Incidents(ctx context.Context, page, size int, category, status, subject string) (PlatformAlertList, error) {
	result := PlatformAlertList{Data: []models.PlatformAlertIncident{}, Page: page, PageSize: size}
	if page < 1 || page > 10000 || size < 1 || size > 100 || (category != "" && !validPlatformAlertCategory(category)) || len(subject) > 100 || strings.ContainsAny(subject, "\r\n\x00") {
		return result, ErrLogInvalid
	}
	switch status {
	case "", "active", "open", "acknowledged", "resolved":
	default:
		return result, ErrLogInvalid
	}
	if s == nil || s.db == nil {
		return result, ErrLogUnconfigured
	}
	q := s.db.WithContext(ctx).Model(&models.PlatformAlertIncident{})
	if category != "" {
		q = q.Where("category=?", category)
	}
	if subject != "" {
		q = q.Where("subject_id=?", subject)
	}
	if status == "active" {
		q = q.Where("status IN ?", []string{"open", "acknowledged"})
	} else if status != "" {
		q = q.Where("status=?", status)
	}
	if err := q.Count(&result.Total).Error; err != nil {
		return result, err
	}
	result.TotalPages = int((result.Total + int64(size) - 1) / int64(size))
	err := q.Order("opened_at DESC,id DESC").Offset((page - 1) * size).Limit(size).Find(&result.Data).Error
	return result, err
}
func (s *PlatformAlertService) ManageIncident(ctx context.Context, id uint, version uint64, action string, until *time.Time, principal int64, now time.Time) (models.PlatformAlertIncident, error) {
	var i models.PlatformAlertIncident
	if version == 0 || principal <= 0 || (action != "acknowledge" && action != "suppress") || (action == "suppress" && (until == nil || !until.After(now) || until.After(now.Add(24*time.Hour)))) {
		return i, ErrLogInvalid
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=?", id).Take(&i).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrLogNotFound
		} else if err != nil {
			return err
		}
		if i.Version != version {
			return ErrLogConflict
		}
		if i.Status == "resolved" {
			return ErrLogConflict
		}
		i.Version++
		if action == "acknowledge" {
			i.Status = "acknowledged"
			i.AcknowledgedAt = &now
			i.AcknowledgedBy = &principal
		} else {
			i.SuppressedUntil = until
		}
		return tx.Save(&i).Error
	})
	return i, err
}
