package client

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/addp/common/models"
)

const ImageSegmentationAccessEndpoint = "/api/v1/system/execution-authorizations/manager-image-segmentation/access"

type ImageSegmentationAccessRequest struct {
	ExecutionID string `json:"execution_id"`
	Attempt     int    `json:"attempt"`
	LeaseToken  string `json:"lease_token"`
}

type ImageSegmentationAccessObservation struct {
	ObservedAt   time.Time      `json:"observed_at"`
	SourceEngine *models.Engine `json:"source_engine"`
	TargetEngine *models.Engine `json:"target_engine"`
}

func (c *SystemServiceClient) CheckImageSegmentationAccess(ctx context.Context, request ImageSegmentationAccessRequest) (*ImageSegmentationAccessObservation, error) {
	if c == nil || c.tenantTokens == nil || c.tenantID == nil || *c.tenantID == 0 ||
		!canonicalExecutionUUID(request.ExecutionID) || !canonicalExecutionUUID(request.LeaseToken) || request.Attempt != 1 {
		return nil, errors.New("invalid Manager image segmentation access request")
	}
	token, err := c.tenantTokens.Token(ctx, *c.tenantID)
	if err != nil || !validExecutionBearer(token) {
		return nil, errors.New("Manager tenant service credential unavailable")
	}
	var observation ImageSegmentationAccessObservation
	if err := c.executionAuthorizationJSON(ctx, ImageSegmentationAccessEndpoint, token, request, &observation, http.StatusOK); err != nil {
		return nil, err
	}
	if observation.ObservedAt.IsZero() || observation.SourceEngine == nil || observation.TargetEngine == nil {
		return nil, errors.New("incomplete Manager image segmentation access observation")
	}
	return &observation, nil
}
