package service

import (
	"time"

	"github.com/addp/monitor/internal/models"
)

// LogIncidentView is a projection for the log diagnostic API. Its node denotes
// a log observer, never a System host UUID or a generic platform alert subject.
type LogIncidentView struct {
	ID              uint       `json:"id"`
	Version         uint64     `json:"version"`
	Node            string     `json:"node"`
	Signal          string     `json:"signal"`
	InstanceID      string     `json:"instance_id,omitempty"`
	Severity        string     `json:"severity"`
	Status          string     `json:"status"`
	OpenedAt        time.Time  `json:"opened_at"`
	LastObservedAt  time.Time  `json:"last_observed_at"`
	ResolvedAt      *time.Time `json:"resolved_at,omitempty"`
	AcknowledgedBy  *int64     `json:"acknowledged_by,omitempty"`
	AcknowledgedAt  *time.Time `json:"acknowledged_at,omitempty"`
	SuppressedUntil *time.Time `json:"suppressed_until,omitempty"`
}

const logIncidentColumns = "id,version,subject_id AS node,signal,instance_id,severity,status,opened_at,last_observed_at,resolved_at,acknowledged_by,acknowledged_at,suppressed_until"

func logIncidentView(i models.PlatformAlertIncident) LogIncidentView {
	return LogIncidentView{
		ID: i.ID, Version: i.Version, Node: i.SubjectID, Signal: i.Signal, InstanceID: i.InstanceID,
		Severity: i.Severity, Status: i.Status, OpenedAt: i.OpenedAt, LastObservedAt: i.LastObservedAt,
		ResolvedAt: i.ResolvedAt, AcknowledgedBy: i.AcknowledgedBy, AcknowledgedAt: i.AcknowledgedAt, SuppressedUntil: i.SuppressedUntil,
	}
}
