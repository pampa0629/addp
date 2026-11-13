package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	monitorModels "github.com/addp/monitor/internal/models"
	"log/slog"
	"strconv"
	"time"
)

var cdcRiskCodes = []string{"cdc_source_recovery_lost", "cdc_buffer_recovery_lost"}

type cdcRiskReader interface {
	ReadCDCRisks(context.Context, uint) (*commonClient.TransferCDCRisks, error)
}

func (s *AlertService) SetCDCRiskReader(reader cdcRiskReader) { s.cdcRiskReader = reader }

// evaluateCDCRisks never treats an unavailable or incomplete Owner inventory as recovery.
func (s *AlertService) evaluateCDCRisks(ctx context.Context, now time.Time) ([]activeAlertSignal, []string, error) {
	var existing []monitorModels.AlertIncident
	if err := s.db.WithContext(ctx).Where("signal_code IN ? AND status IN ?", cdcRiskCodes, []string{monitorModels.AlertStatusOpen, monitorModels.AlertStatusAcknowledged}).Find(&existing).Error; err != nil {
		return nil, nil, err
	}
	all := make([]string, 0, len(existing))
	for _, incident := range existing {
		all = append(all, incident.Fingerprint)
	}
	if s.cdcRiskReader == nil {
		return nil, all, nil
	}
	probe, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	rows := []commonClient.TransferCDCRiskItem{}
	var after uint
	for {
		page, err := s.cdcRiskReader.ReadCDCRisks(probe, after)
		if err != nil {
			slog.Warn("CDC task risk inventory unavailable; existing incidents retained")
			return nil, all, nil
		}
		rows = append(rows, page.Items...)
		if !page.HasMore {
			break
		}
		after = page.NextAfterID
	}
	signals := make([]activeAlertSignal, 0)
	preserve := []string{}
	for _, row := range rows {
		if row.State == "closed" {
			continue
		}
		risks := []commonClient.CDCRiskObservation{row.Source, row.Buffer}
		for i, risk := range risks {
			code := cdcRiskCodes[i]
			known := row.State == "observing" && risk.State != "unknown" && risk.SampledAt != nil && risk.ExpiresAt != nil && !risk.SampledAt.After(now) && risk.ExpiresAt.After(now)
			if !known {
				for _, incident := range existing {
					if incident.TenantID == int(row.TenantID) && incident.SourceTaskID == strconv.FormatUint(uint64(row.TaskID), 10) && incident.SignalCode == code {
						preserve = append(preserve, incident.Fingerprint)
					}
				}
				continue
			}
			if risk.State != "active" {
				continue
			}
			digest := sha256.Sum256([]byte(fmt.Sprintf("cdc-risk|%d|%d|%d|%s", row.TenantID, row.TaskID, row.Generation, code)))
			signals = append(signals, activeAlertSignal{TenantID: int(row.TenantID), Module: "transfer", TaskType: "sync", SourceTaskID: strconv.FormatUint(uint64(row.TaskID), 10), ExecutionID: row.ExecutionID, Code: code, Severity: monitorModels.AlertSeverityCritical, Fingerprint: hex.EncodeToString(digest[:]), Details: commonModels.JSONMap{"generation": strconv.FormatUint(row.Generation, 10), "reason": risk.Reason, "sampled_at": risk.SampledAt.UTC().Format(time.RFC3339Nano)}})
		}
	}
	return signals, preserve, nil
}
