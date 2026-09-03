package models

// SensitiveDiscoveryQualityMetrics contains review-backed quality evidence for
// one scope. Rates are omitted when no human-reviewed sample exists.
type SensitiveDiscoveryQualityMetrics struct {
	CurrentFindingCount       int64    `json:"current_finding_count"`
	AwaitingReviewCount       int64    `json:"awaiting_review_count"`
	ReviewedSampleCount       int64    `json:"reviewed_sample_count"`
	ConfirmedCount            int64    `json:"confirmed_count"`
	AdjustedCount             int64    `json:"adjusted_count"`
	RejectedCount             int64    `json:"rejected_count"`
	SensitiveConfirmationRate *float64 `json:"sensitive_confirmation_rate"`
}

type SensitiveDiscoveryCapabilityQuality struct {
	DetectorCode        string `json:"detector_code"`
	CapabilityKey       string `json:"capability_key"`
	SensitiveDataTypeID int64  `json:"sensitive_data_type_id,string"`
	SensitiveDiscoveryQualityMetrics
}

// SensitiveDiscoveryQualitySummary is an on-demand, value-free aggregation of
// existing Finding, review, and current Assessment facts. It is not persisted.
type SensitiveDiscoveryQualitySummary struct {
	SensitiveDataTypeID          *int64                                `json:"sensitive_data_type_id,omitempty,string"`
	CurrentFindingCount          int64                                 `json:"current_finding_count"`
	AwaitingReviewCount          int64                                 `json:"awaiting_review_count"`
	ReviewedSampleCount          int64                                 `json:"reviewed_sample_count"`
	ConfirmedCount               int64                                 `json:"confirmed_count"`
	AdjustedCount                int64                                 `json:"adjusted_count"`
	RejectedCount                int64                                 `json:"rejected_count"`
	SensitiveConfirmationRate    *float64                              `json:"sensitive_confirmation_rate"`
	ActiveManualAssessmentCount  int64                                 `json:"active_manual_assessment_count"`
	RevokedManualAssessmentCount int64                                 `json:"revoked_manual_assessment_count"`
	Capabilities                 []SensitiveDiscoveryCapabilityQuality `json:"capabilities"`
}
