package service

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/addp/common/client"
)

type fileAdmissionAuthority interface {
	CheckDevelopFileAdmission(context.Context, client.IssuedDevelopFileAuthorization, client.DevelopFileAdmissionRequest) (*client.DevelopFileAdmissionObservation, error)
}

// This is only renewal of an original bound pending record. The current machine
// credential comes from the existing System SDK; no User token is retained.
// Unknown HTTP outcomes leave the original row closed and never cause signing.
func (s *ExecutionService) refreshPrivateFileAdmissions(ctx context.Context) error {
	f := s.privateFiles
	if f.admission == nil {
		return nil
	}
	// One observation round must not indefinitely delay the same supervisor's
	// existing recovery work. This HTTP budget never starts a business timeout.
	round, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	candidates, err := f.repository.PendingAdmissions(round, 100)
	if err != nil {
		return err
	}
	var observations []error
	for _, candidate := range candidates {
		if round.Err() != nil {
			return round.Err()
		}
		if candidate.Issued.Scope.Target != nil {
			continue
		} // Saved publication is closed.
		facts := candidate.Facts
		tenant, _ := strconv.ParseUint(facts.TenantID, 10, 64)
		observation, err := f.admission(uint(tenant)).CheckDevelopFileAdmission(round, candidate.Issued, client.DevelopFileAdmissionRequest{ExecutionID: facts.ExecutionID, ExpectedAttempt: facts.Intent.ExpectedAttempt, ScopeDigest: facts.ScopeDigest})
		if err != nil {
			observations = append(observations, errors.New("Develop file admission observation unavailable"))
			continue
		}
		if observation == nil {
			observations = append(observations, errors.New("Develop file admission observation unavailable"))
			continue
		}
		if err := f.repository.ApplyAdmission(round, uint(tenant), facts.ExecutionID, *observation); err != nil {
			observations = append(observations, errors.New("Develop file admission observation no longer matches original pending execution"))
		}
	}
	return errors.Join(observations...)
}
