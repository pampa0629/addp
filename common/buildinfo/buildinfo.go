package buildinfo

import (
	"github.com/google/uuid"
	"os"
	"sync"
	"time"
)

var (
	BuildID           = "unknown"
	GitCommit         = "unknown"
	SourceFingerprint = "unknown"
	BuiltAt           = "unknown"
	startedAt         = time.Now().UTC()
	identityOnce      sync.Once
	processID         string
)

type HealthResponse struct {
	InstanceID        string `json:"instance_id"`
	Status            string `json:"status"`
	Module            string `json:"module"`
	BuildID           string `json:"build_id"`
	GitCommit         string `json:"git_commit"`
	SourceFingerprint string `json:"source_fingerprint"`
	BuiltAt           string `json:"built_at"`
	StartedAt         string `json:"started_at"`
}

func Health(module string) HealthResponse {
	return HealthResponse{
		InstanceID:        ProcessInstanceID(),
		Status:            "ok",
		Module:            module,
		BuildID:           BuildID,
		GitCommit:         GitCommit,
		SourceFingerprint: SourceFingerprint,
		BuiltAt:           BuiltAt,
		StartedAt:         startedAt.Format(time.RFC3339Nano),
	}
}

func ProcessStartedAt() time.Time { return startedAt }

// ProcessInstanceID is the single process identity shared by health, registration
// and logs. Standard launchers supply it before executing the application.
func ProcessInstanceID() string {
	identityOnce.Do(func() {
		processID = os.Getenv("ADDP_PROCESS_INSTANCE_ID")
		if processID == "" {
			processID = uuid.NewString()
		}
	})
	return processID
}
