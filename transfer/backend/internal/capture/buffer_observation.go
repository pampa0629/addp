package capture

import (
	"context"
	"strconv"
	"time"

	"github.com/addp/transfer/internal/models"
)

func (s *Supervisor) observeBuffer(ctx context.Context, resource *models.CaptureResource) error {
	probeCtx, cancel := context.WithTimeout(ctx, s.config.SourceProbeTimeout)
	defer cancel()
	now := time.Now().UTC()
	observation := models.CaptureBufferObservation{Status: "unavailable", SampledAt: now, ExpiresAt: now.Add(2 * s.config.MonitorInterval), RecoveryState: "unknown"}
	// Read the target-committed position first, so concurrent progress cannot put
	// this observation ahead of the subsequently sampled broker end.
	committed, snapshotCompleted, err := s.store.BufferCommittedPosition(probeCtx, resource)
	if err == nil {
		observation.InitialSnapshotCompleted = &snapshotCompleted
		earliest, latest, readErr := s.topics.BufferOffsets(probeCtx, resource.TopicName)
		err = readErr
		if err == nil && earliest >= 0 && latest >= earliest && (committed == nil || *committed <= latest) {
			observation.Status = "available"
			start, end := strconv.FormatInt(earliest, 10), strconv.FormatInt(latest, 10)
			observation.EarliestOffset, observation.LatestOffset = &start, &end
			recoveryStart := int64(0)
			observation.RecoveryState = "not_started"
			if committed != nil {
				recoveryStart = *committed
				next, lag := strconv.FormatInt(*committed, 10), strconv.FormatInt(latest-*committed, 10)
				observation.CommittedOffset, observation.LagOffsets = &next, &lag
				observation.RecoveryState = "available"
			}
			if recoveryStart < earliest {
				observation.RecoveryState = "lost"
			}
		}
	}
	// Missing/invalid samples replace the previous values. Publishing uses the
	// same lifecycle CAS as other capture facts, including concurrent Stop.
	value := sourceObservationMap(&observation)
	return s.advance(ctx, resource, map[string]interface{}{"buffer_observation": value})
}
