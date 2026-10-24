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

type PlatformAlertFilter struct {
	Page, PageSize                        int
	Category, Status, SubjectID, Severity string
	TimeFrom, TimeTo                      *time.Time
}

func (s *PlatformAlertService) Incidents(ctx context.Context, filter PlatformAlertFilter) (PlatformAlertList, error) {
	result := PlatformAlertList{Data: []models.PlatformAlertIncident{}, Page: filter.Page, PageSize: filter.PageSize}
	if filter.Page < 1 || filter.Page > 10000 || filter.PageSize < 1 || filter.PageSize > 100 || (filter.Category != "" && !validPlatformAlertCategory(filter.Category)) || (filter.Severity != "" && filter.Severity != "critical" && filter.Severity != "warning") || len(filter.SubjectID) > 100 || strings.ContainsAny(filter.SubjectID, "\r\n\x00") || (filter.TimeFrom != nil && filter.TimeTo != nil && !filter.TimeFrom.Before(*filter.TimeTo)) {
		return result, ErrLogInvalid
	}
	for _, bound := range []*time.Time{filter.TimeFrom, filter.TimeTo} {
		if bound != nil && (bound.UTC().Year() < 1 || bound.UTC().Year() > 9999) {
			return result, ErrLogInvalid
		}
	}
	switch filter.Status {
	case "", "active", "open", "acknowledged", "resolved":
	default:
		return result, ErrLogInvalid
	}
	if s == nil || s.db == nil {
		return result, ErrLogUnconfigured
	}
	q := s.db.WithContext(ctx).Model(&models.PlatformAlertIncident{})
	if filter.Category != "" {
		q = q.Where("category=?", filter.Category)
	}
	if filter.Severity != "" {
		q = q.Where("severity=?", filter.Severity)
	}
	if filter.SubjectID != "" {
		q = q.Where("subject_id=?", filter.SubjectID)
	}
	if filter.Status == "active" {
		q = q.Where("status IN ?", []string{"open", "acknowledged"})
	} else if filter.Status != "" {
		q = q.Where("status=?", filter.Status)
	}
	if filter.TimeFrom != nil {
		q = q.Where("opened_at >= ?", *filter.TimeFrom)
	}
	if filter.TimeTo != nil {
		q = q.Where("opened_at < ?", *filter.TimeTo)
	}
	if err := q.Count(&result.Total).Error; err != nil {
		return result, err
	}
	result.TotalPages = int((result.Total + int64(filter.PageSize) - 1) / int64(filter.PageSize))
	err := q.Order("opened_at DESC,id DESC").Offset((filter.Page - 1) * filter.PageSize).Limit(filter.PageSize).Find(&result.Data).Error
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
