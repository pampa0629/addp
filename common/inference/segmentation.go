package inference

const OperationSegmentation = "segmentation"

type SegmentPrompt struct {
	Points [][]int `json:"points,omitempty"`
	Labels []int   `json:"labels,omitempty"`
	Text   string  `json:"text,omitempty"`
}
type SegmentRequest struct {
	SchemaVersion  string         `json:"schema_version"`
	TenantID       uint           `json:"tenant_id"`
	ModelProfileID string         `json:"model_profile_id"`
	Scope          string         `json:"scope"` // Caller-derived digest of user, source and analysis window.
	Action         string         `json:"action"`
	SessionID      string         `json:"session_id,omitempty"`
	Image          string         `json:"image,omitempty"`
	Prompt         *SegmentPrompt `json:"prompt,omitempty"`
	CallerToken    string         `json:"-"`
}
type InstanceMask struct {
	X      int     `json:"x"`
	Y      int     `json:"y"`
	Width  int     `json:"width"`
	Height int     `json:"height"`
	PNG    string  `json:"png"`
	Score  float64 `json:"score"`
}
type SegmentResponse struct {
	SessionID      string         `json:"session_id"`
	Width          int            `json:"width,omitempty"`
	Height         int            `json:"height,omitempty"`
	Masks          []InstanceMask `json:"masks,omitempty"`
	DeploymentID   string         `json:"deployment_id"`
	ProfileVersion int64          `json:"profile_version"`
}
