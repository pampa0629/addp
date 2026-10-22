package execution

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/jsonmap"
	"github.com/google/uuid"
)

type MetaCollectionAccessRequest struct {
	ExecutionID         string `json:"execution_id"`
	Attempt             int    `json:"attempt"`
	LeaseToken          string `json:"lease_token"`
	ExecutionPlanDigest string `json:"execution_plan_digest"`
}

func (r MetaCollectionAccessRequest) Validate() error {
	id, err := uuid.Parse(r.ExecutionID)
	token, tokenErr := uuid.Parse(r.LeaseToken)
	if err != nil || id == uuid.Nil || id.String() != r.ExecutionID || tokenErr != nil || token == uuid.Nil || token.String() != r.LeaseToken || r.Attempt < 1 || !authorizationfacts.ValidItemFingerprint(r.ExecutionPlanDigest) {
		return errors.New("invalid Meta collection check")
	}
	return nil
}

type MetaCollectionAccessObservation struct {
	ExecutionID         string    `json:"execution_id"`
	ExecutionPlanDigest string    `json:"execution_plan_digest"`
	QualificationID     string    `json:"qualification_id"`
	ObservedAt          time.Time `json:"observed_at"`
}

func (o *MetaCollectionAccessObservation) UnmarshalJSON(raw []byte) error {
	invalid := errors.New("invalid Meta collection observation")
	object, err := jsonmap.DecodeObjectStrict(raw, 8192, 8)
	if err != nil || !metaScanClosedObject(object, []string{"execution_id", "execution_plan_digest", "qualification_id", "observed_at"}, nil, nil) {
		return invalid
	}
	type plain MetaCollectionAccessObservation
	var result plain
	if json.Unmarshal(raw, &result) != nil {
		return invalid
	}
	execID, err := uuid.Parse(result.ExecutionID)
	qualID, qualErr := uuid.Parse(result.QualificationID)
	if err != nil || execID == uuid.Nil || execID.String() != result.ExecutionID || qualErr != nil || qualID == uuid.Nil || qualID.String() != result.QualificationID || !authorizationfacts.ValidItemFingerprint(result.ExecutionPlanDigest) || result.ObservedAt.IsZero() {
		return invalid
	}
	*o = MetaCollectionAccessObservation(result)
	return nil
}
