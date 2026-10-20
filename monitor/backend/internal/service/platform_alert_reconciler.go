package service

import (
	"errors"
	"strings"
	"time"

	"github.com/addp/common/logpipeline"
	"github.com/addp/monitor/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type PlatformAlertVerdict string

const (
	PlatformAlertRisk    PlatformAlertVerdict = "risk"
	PlatformAlertNormal  PlatformAlertVerdict = "normal"
	PlatformAlertUnknown PlatformAlertVerdict = "unknown"
	PlatformAlertPaused  PlatformAlertVerdict = "paused"
)

// PlatformAlertEvaluation covers exactly one source-owned signal. Missing
// evaluations are never inferred to mean normal for any other source or subject.
type PlatformAlertEvaluation struct {
	HTTPEvidence                                                   *models.HTTPAlertEvidence
	ResourceEvidence                                               *models.ResourceAlertEvidence
	Category, SubjectKind, SubjectID, Signal, InstanceID, Severity string
	Verdict                                                        PlatformAlertVerdict
}

func (e PlatformAlertEvaluation) valid() bool {
	if e.SubjectID == "" || len(e.SubjectID) > 100 || e.Signal == "" || len(e.Signal) > 160 {
		return false
	}
	switch e.Category {
	case models.PlatformAlertCategoryLog:
		if e.SubjectKind != models.PlatformAlertSubjectLogObserver || !logpipeline.Identity.MatchString(e.SubjectID) {
			return false
		}
	case models.PlatformAlertCategoryResource:
		id, err := uuid.Parse(e.SubjectID)
		if e.SubjectKind != models.PlatformAlertSubjectHostNode || err != nil || id.String() != e.SubjectID || e.InstanceID != "" {
			return false
		}
	case models.PlatformAlertCategoryHTTP:
		if e.SubjectKind != models.PlatformAlertSubjectModule || e.InstanceID != "" || len(e.SubjectID) > 50 || strings.TrimSpace(e.SubjectID) != e.SubjectID || strings.ContainsAny(e.SubjectID, "\r\n\x00") || !resourceRuleUUID(e.Signal) {
			return false
		}
	default:
		return false
	}
	switch e.Verdict {
	case PlatformAlertUnknown, PlatformAlertPaused, PlatformAlertNormal:
		return true
	case PlatformAlertRisk:
		return e.Severity == "warning" || e.Severity == "critical"
	default:
		return false
	}
}

// The caller supplies the transaction that owns the source evaluation state.
// The advisory lock also protects a previously absent incident across competing
// evaluators; the partial unique index remains the durable invariant.
func reconcilePlatformAlert(tx *gorm.DB, notifications *PlatformAlertService, e PlatformAlertEvaluation, now time.Time) error {
	if !e.valid() || now.IsZero() {
		return ErrLogInvalid
	}
	if e.Verdict == PlatformAlertUnknown || e.Verdict == PlatformAlertPaused {
		return nil
	}
	key := "monitor.platform-alert:" + e.Category + ":" + e.SubjectKind + ":" + e.SubjectID + ":" + e.Signal
	if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", key).Error; err != nil {
		return err
	}
	var incident models.PlatformAlertIncident
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
		"category=? AND subject_kind=? AND subject_id=? AND signal=? AND status IN ?",
		e.Category, e.SubjectKind, e.SubjectID, e.Signal, []string{"open", "acknowledged"},
	).Take(&incident).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	eventType := ""
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if e.Verdict == PlatformAlertNormal {
			return nil
		}
		incident = models.PlatformAlertIncident{
			Version: 1, Category: e.Category, SubjectKind: e.SubjectKind, SubjectID: e.SubjectID,
			Signal: e.Signal, InstanceID: e.InstanceID, Severity: e.Severity,
			Status: "open", OpenedAt: now, LastObservedAt: now, ResourceEvidence: e.ResourceEvidence, HTTPEvidence: e.HTTPEvidence,
		}
		if err := tx.Create(&incident).Error; err != nil {
			return err
		}
		eventType = "opened"
	} else {
		if now.Before(incident.LastObservedAt) || (e.Severity != "" && e.Severity != incident.Severity) || e.InstanceID != incident.InstanceID {
			return ErrLogConflict
		}
		incident.Version++
		incident.LastObservedAt = now
		if e.Verdict == PlatformAlertNormal {
			incident.Status, incident.ResolvedAt = "resolved", &now
			eventType = "resolved"
		}
		if err := tx.Save(&incident).Error; err != nil {
			return err
		}
	}
	if eventType == "" {
		return nil
	}
	event := models.PlatformAlertEvent{ID: uuid.NewString(), IncidentID: incident.ID, Type: eventType, Severity: incident.Severity, OccurredAt: now, ResourceEvidence: e.ResourceEvidence, HTTPEvidence: e.HTTPEvidence}
	if err := tx.Create(&event).Error; err != nil {
		return err
	}
	if notifications != nil {
		return notifications.RecordTx(tx, event, incident, now)
	}
	return nil
}
