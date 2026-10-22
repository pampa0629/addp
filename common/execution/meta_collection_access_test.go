package execution

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestMetaCollectionObservationIsStrictAndNotAnAuthorityEnvelope(t *testing.T) {
	value := MetaCollectionAccessObservation{ExecutionID: uuid.NewString(), ExecutionPlanDigest: strings.Repeat("a", 64), QualificationID: uuid.NewString(), ObservedAt: time.Now().UTC()}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var current MetaCollectionAccessObservation
	if err := json.Unmarshal(raw, &current); err != nil || current != value {
		t.Fatal("valid current observation rejected", err)
	}
	for _, invalid := range []string{
		`{}`, `null`, string(raw) + `{}`,
		strings.Replace(string(raw), `"observed_at":`, `"allow":true,"observed_at":`, 1),
		strings.Replace(string(raw), `"observed_at":`, `"execution_id":"`+value.ExecutionID+`","observed_at":`, 1),
		strings.Replace(string(raw), value.ExecutionID, uuid.Nil.String(), 1),
		strings.Replace(string(raw), value.QualificationID, "0", 1),
		strings.Replace(string(raw), value.ExecutionPlanDigest, "bad", 1),
		strings.Replace(string(raw), `"observed_at":"`+value.ObservedAt.Format(time.RFC3339Nano)+`"`, `"observed_at":null`, 1),
	} {
		original := value
		if err := json.Unmarshal([]byte(invalid), &original); err == nil || original != value {
			t.Fatal("ambiguous authority accepted or receiver changed", err)
		}
	}
}

func TestMetaCollectionRequestRequiresCurrentLeaseIdentity(t *testing.T) {
	valid := MetaCollectionAccessRequest{ExecutionID: uuid.NewString(), Attempt: 1, LeaseToken: uuid.NewString(), ExecutionPlanDigest: strings.Repeat("a", 64)}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*MetaCollectionAccessRequest){
		func(r *MetaCollectionAccessRequest) { r.Attempt = 0 },
		func(r *MetaCollectionAccessRequest) { r.LeaseToken = uuid.Nil.String() },
		func(r *MetaCollectionAccessRequest) { r.ExecutionID = "23DD4F40-95B5-4CC1-A294-DED9F78D10A2" },
		func(r *MetaCollectionAccessRequest) { r.ExecutionPlanDigest = "" },
	} {
		invalid := valid
		mutate(&invalid)
		if invalid.Validate() == nil {
			t.Fatal("incomplete execution binding accepted")
		}
	}
}
