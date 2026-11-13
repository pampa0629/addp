package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/addp/common/httptrace"
)

const TransferCDCRiskSchema = "transfer.cdc_risk_observations/v1"
const TransferCDCRiskBatchSize = 100

type CDCRiskObservation struct {
	State     string     `json:"state"`
	Reason    string     `json:"reason,omitempty"`
	SampledAt *time.Time `json:"sampled_at,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type TransferCDCRiskItem struct {
	TenantID    uint               `json:"tenant_id"`
	TaskID      uint               `json:"task_id"`
	Generation  uint64             `json:"generation,string" swaggertype:"string"`
	ExecutionID string             `json:"execution_id"`
	State       string             `json:"state"`
	Source      CDCRiskObservation `json:"source"`
	Buffer      CDCRiskObservation `json:"buffer"`
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
		for _, risk := range []CDCRiskObservation{item.Source, item.Buffer} {
			if risk.State != "active" && risk.State != "clear" && risk.State != "unknown" {
				return nil, errors.New("invalid CDC risk state")
			}
			if risk.State != "unknown" && (risk.SampledAt == nil || risk.SampledAt.IsZero() || risk.ExpiresAt == nil || !risk.ExpiresAt.After(*risk.SampledAt)) {
				return nil, errors.New("invalid CDC risk sample")
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
