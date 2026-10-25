package service

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/addp/common/client"
	commoni18n "github.com/addp/common/middleware/i18n"
	_ "github.com/addp/monitor/i18n"
	"github.com/addp/monitor/internal/metricsdiscovery"
	"github.com/addp/monitor/internal/resourcequery"

	"github.com/addp/monitor/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func validPlatformAlertCategory(category string) bool {
	return category == models.PlatformAlertCategoryLog || category == models.PlatformAlertCategoryResource || category == models.PlatformAlertCategoryHTTP || category == models.PlatformAlertCategoryProcess
}

type PlatformAlertView struct {
	models.PlatformAlertIncident
	Name string `json:"name"`
}

type PlatformAlertList struct {
	Data       []PlatformAlertView `json:"data"`
	Total      int64               `json:"total"`
	Page       int                 `json:"page"`
	PageSize   int                 `json:"page_size"`
	TotalPages int                 `json:"total_pages"`
}

type PlatformAlertFilter struct {
	Page, PageSize                        int
	Category, Status, SubjectID, Severity string
	Search, HostName, Language            string
	TimeFrom, TimeTo                      *time.Time
}

func (s *PlatformAlertService) Incidents(ctx context.Context, filter PlatformAlertFilter, userToken string) (PlatformAlertList, error) {
	if !validAlertSearch(filter.Search) || !validAlertSearch(filter.HostName) {
		return PlatformAlertList{}, ErrLogInvalid
	}
	filter.Search = strings.TrimSpace(filter.Search)
	filter.HostName = strings.TrimSpace(filter.HostName)
	result := PlatformAlertList{Data: []PlatformAlertView{}, Page: filter.Page, PageSize: filter.PageSize}
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
	var hostIDs []string
	if filter.HostName != "" {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if s.hosts == nil {
			return result, metricsdiscovery.ErrIdentityUnavailable
		}
		hosts, err := s.hosts.HostNodesForUser(ctx, userToken)
		if errors.Is(err, client.ErrHostNodeScopeBudget) {
			return result, resourcequery.ErrBudget
		}
		if err != nil {
			return result, errors.Join(metricsdiscovery.ErrIdentityUnavailable, err)
		}
		for _, host := range hosts {
			if strings.Contains(strings.ToLower(host.DisplayName), strings.ToLower(filter.HostName)) {
				hostIDs = append(hostIDs, host.NodeID)
			}
		}
		if len(hostIDs) == 0 {
			return result, nil
		}
	}
	q := s.db.WithContext(ctx).Model(&models.PlatformAlertIncident{})
	if filter.HostName != "" {
		q = q.Where("category=? AND subject_kind=? AND subject_id IN ?", models.PlatformAlertCategoryResource, models.PlatformAlertSubjectHostNode, hostIDs)
	}
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
	if filter.Search != "" {
		pattern := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(filter.Search) + "%"
		const ruleNameSQL = `(CASE category WHEN 'resource' THEN resource_evidence->>'rule_name' WHEN 'http_quality' THEN http_evidence->>'rule_name' WHEN 'process_resource' THEN process_evidence->>'rule_name' END) ILIKE ? ESCAPE '\'`
		logSignals := []string{}
		for _, signal := range platformLogNameSignals {
			if strings.Contains(strings.ToLower(platformLogAlertName(signal, filter.Language)), strings.ToLower(filter.Search)) {
				logSignals = append(logSignals, signal)
			}
		}
		if len(logSignals) == 0 {
			q = q.Where(ruleNameSQL, pattern)
		} else {
			q = q.Where("("+ruleNameSQL+" OR (category=? AND split_part(signal, ':', 1) IN ?))", pattern, models.PlatformAlertCategoryLog, logSignals)
		}
	}
	if err := q.Count(&result.Total).Error; err != nil {
		return result, err
	}
	result.TotalPages = int((result.Total + int64(filter.PageSize) - 1) / int64(filter.PageSize))
	var rows []models.PlatformAlertIncident
	err := q.Order("CASE WHEN severity='critical' AND status IN ('open','acknowledged') THEN 0 ELSE 1 END").Order("opened_at DESC,id DESC").Offset((filter.Page - 1) * filter.PageSize).Limit(filter.PageSize).Find(&rows).Error
	if err != nil {
		return result, err
	}
	for _, row := range rows {
		result.Data = append(result.Data, PlatformAlertView{PlatformAlertIncident: row, Name: platformAlertName(row, filter.Language)})
	}
	return result, nil
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

func validAlertSearch(value string) bool {
	if !utf8.ValidString(value) || utf8.RuneCountInString(strings.TrimSpace(value)) > 100 {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

var platformLogNameSignals = []string{"observation_missing", "log_api", "delivery_probe", "delivery_delay", "collector_metrics", "collector_dropped", "collector_retries", "source_observation", "housekeeping", "source_quota", "source_capacity", "receiver_missing", "receiver_loss", "source_early_cleaning"}

func platformLogAlertName(signal, lang string) string {
	signal = logSignalName(signal)
	for _, known := range platformLogNameSignals {
		if signal == known {
			return commoni18n.ForLanguage(lang, "monitor.platform_alert.name."+signal)
		}
	}
	return ""
}

func platformAlertName(row models.PlatformAlertIncident, lang string) string {
	switch row.Category {
	case models.PlatformAlertCategoryLog:
		return platformLogAlertName(row.Signal, lang)
	case models.PlatformAlertCategoryResource:
		if row.ResourceEvidence != nil {
			return row.ResourceEvidence.RuleName
		}
	case models.PlatformAlertCategoryHTTP:
		if row.HTTPEvidence != nil {
			return row.HTTPEvidence.RuleName
		}
	case models.PlatformAlertCategoryProcess:
		if row.ProcessEvidence != nil {
			return row.ProcessEvidence.RuleName
		}
	}
	return ""
}
