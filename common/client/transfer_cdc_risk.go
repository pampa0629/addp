package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/addp/common/httptrace"
)

const TransferCDCRiskSchema = "transfer.cdc_risk_observations/v3"
const TransferCDCRiskBatchSize = 100

type CDCRiskObservation struct {
	State     string     `json:"state"`
	Reason    string     `json:"reason,omitempty"`
	SampledAt *time.Time `json:"sampled_at,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type CDCCaptureObservation struct {
	State       string     `json:"state"`
	Reason      string     `json:"reason,omitempty"`
	FailedSince *time.Time `json:"failed_since"`
	SampledAt   *time.Time `json:"sampled_at,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

type CDCBufferSample struct {
	CommittedOffset string    `json:"committed_offset"`
	LagOffsets      string    `json:"lag_offsets"`
	SampledAt       time.Time `json:"sampled_at"`
	ExpiresAt       time.Time `json:"expires_at"`
}
type CDCDelaySample struct {
	DelaySeconds        float64   `json:"delay_seconds"`
	EventCount          string    `json:"event_count"`
	LastEventAgeSeconds float64   `json:"last_event_age_seconds"`
	SampledAt           time.Time `json:"sampled_at"`
	ExpiresAt           time.Time `json:"expires_at"`
}
type TransferCDCRiskItem struct {
	TaskName                 string           `json:"task_name"`
	SourceType               string           `json:"source_type"`
	InitialSnapshotCompleted *bool            `json:"initial_snapshot_completed"`
	ConsumptionState         string           `json:"consumption_state"`
	BufferSample             *CDCBufferSample `json:"buffer_sample"`
	DelaySample              *CDCDelaySample  `json:"delay_sample"`

	Capture     CDCCaptureObservation `json:"capture"`
	TenantID    uint                  `json:"tenant_id"`
	TaskID      uint                  `json:"task_id"`
	Generation  uint64                `json:"generation,string" swaggertype:"string"`
	ExecutionID string                `json:"execution_id"`
	State       string                `json:"state"`
	Source      CDCRiskObservation    `json:"source"`
	Buffer      CDCRiskObservation    `json:"buffer"`
}

type TransferCDCRisks struct {
	SchemaVersion string                `json:"schema_version"`
	ObservedAt    time.Time             `json:"observed_at"`
	Items         []TransferCDCRiskItem `json:"items"`
	NextAfterID   uint                  `json:"next_after_id"`
	HasMore       bool                  `json:"has_more"`
}

// ReadCDCRisks reads the bounded current Owner inventory with the Monitor platform identity.
func (c *TransferClient) ReadCDCRisks(ctx context.Context, afterID uint) (*TransferCDCRisks, error) {
	if c == nil || c.tokenSource == nil {
		return nil, errors.New("CDC risks require Monitor service identity")
	}
	platform, ok := c.tokenSource.(PlatformServiceTokenProvider)
	if !ok {
		return nil, errors.New("CDC risks require platform service token")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/transfer/runtime/cdc-risk-observations?after_id="+strconv.FormatUint(uint64(afterID), 10), nil)
	if err != nil {
		return nil, err
	}
	token, err := platform.PlatformToken(ctx)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	response, err := httptrace.Do(c.httpClient, req, c.baseURL)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("CDC risk owner returned HTTP %d", response.StatusCode)
	}
	var result TransferCDCRisks
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&result); err != nil {
		return nil, errors.New("invalid CDC risk response")
	}
	if result.SchemaVersion != TransferCDCRiskSchema || result.ObservedAt.IsZero() || len(result.Items) > TransferCDCRiskBatchSize {
		return nil, errors.New("CDC risk response scope mismatch")
	}
	last := afterID
	for _, item := range result.Items {
		if item.TaskID <= last || item.TenantID == 0 {
			return nil, errors.New("CDC risk task scope mismatch")
		}
		last = item.TaskID
		if item.State == "closed" {
			continue
		}
		if item.State != "observing" && item.State != "unknown" {
			return nil, errors.New("invalid CDC task state")
		}
		if item.State == "observing" && (item.Generation == 0 || !nonzeroExecutionUUID(item.ExecutionID)) {
			return nil, errors.New("invalid CDC generation identity")
		}
		if item.ConsumptionState != "running" && item.ConsumptionState != "paused" && item.ConsumptionState != "blocked" && item.ConsumptionState != "stopped" && item.ConsumptionState != "unknown" {
			return nil, errors.New("invalid CDC consumption state")
		}
		for _, risk := range []CDCRiskObservation{item.Source, item.Buffer} {
			if risk.State != "active" && risk.State != "clear" && risk.State != "unknown" {
				return nil, errors.New("invalid CDC risk state")
			}
			if risk.State != "unknown" && (risk.SampledAt == nil || risk.SampledAt.IsZero() || risk.ExpiresAt == nil || !risk.ExpiresAt.After(*risk.SampledAt)) {
				return nil, errors.New("invalid CDC risk sample")
			}
		}
		capture := item.Capture
		if capture.State != "healthy" && capture.State != "failed" && capture.State != "unknown" {
			return nil, errors.New("invalid CDC capture state")
		}
		if capture.State != "unknown" && (capture.SampledAt == nil || capture.SampledAt.IsZero() || capture.ExpiresAt == nil || !capture.ExpiresAt.After(*capture.SampledAt)) {
			return nil, errors.New("invalid CDC capture sample")
		}
		if capture.State == "failed" && (capture.FailedSince == nil || capture.FailedSince.IsZero() || capture.FailedSince.After(*capture.SampledAt) || (capture.Reason != "connector_not_running" && capture.Reason != "task_not_running" && capture.Reason != "source_disconnected")) {
			return nil, errors.New("invalid CDC capture failure")
		}
		if capture.State != "failed" && (capture.FailedSince != nil || capture.Reason != "") {
			return nil, errors.New("invalid CDC capture failure scope")
		}
		if b := item.BufferSample; b != nil {
			committed, e1 := strconv.ParseInt(b.CommittedOffset, 10, 64)
			lag, e2 := strconv.ParseInt(b.LagOffsets, 10, 64)
			if e1 != nil || e2 != nil || committed < 0 || lag < 0 || strconv.FormatInt(committed, 10) != b.CommittedOffset || strconv.FormatInt(lag, 10) != b.LagOffsets || b.SampledAt.IsZero() || !b.ExpiresAt.After(b.SampledAt) {
				return nil, errors.New("invalid CDC buffer sample")
			}
		}
		if d := item.DelaySample; d != nil {
			count, e := strconv.ParseInt(d.EventCount, 10, 64)
			if e != nil || count < 0 || strconv.FormatInt(count, 10) != d.EventCount || math.IsNaN(d.DelaySeconds) || math.IsInf(d.DelaySeconds, 0) || math.IsNaN(d.LastEventAgeSeconds) || math.IsInf(d.LastEventAgeSeconds, 0) || d.LastEventAgeSeconds < 0 || d.SampledAt.IsZero() || !d.ExpiresAt.After(d.SampledAt) {
				return nil, errors.New("invalid CDC activity sample")
			}
		}
		if item.Source.State == "active" && item.Source.Reason != "slot_missing" && item.Source.Reason != "wal_lost" || item.Buffer.State == "active" && item.Buffer.Reason != "buffer_position_lost" {
			return nil, errors.New("invalid CDC risk reason")
		}
	}
	if result.NextAfterID != last || result.HasMore && len(result.Items) != TransferCDCRiskBatchSize {
		return nil, errors.New("invalid CDC risk pagination")
	}
	return &result, nil
}
