package buildinfo

import "time"

var (
	BuildID           = "unknown"
	GitCommit         = "unknown"
	SourceFingerprint = "unknown"
	BuiltAt           = "unknown"
	startedAt         = time.Now().UTC().Format(time.RFC3339Nano)
)

type HealthResponse struct {
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
		Status:            "ok",
		Module:            module,
		BuildID:           BuildID,
		GitCommit:         GitCommit,
		SourceFingerprint: SourceFingerprint,
		BuiltAt:           BuiltAt,
		StartedAt:         startedAt,
	}
}
