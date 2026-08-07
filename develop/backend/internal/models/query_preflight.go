package models

import "time"

type QueryPreflightRequest struct {
	QueryType     string `json:"query_type" binding:"omitempty,oneof=sql mql cypher"`
	Query         string `json:"query" binding:"required"`
	EngineID      uint   `json:"engine_id" binding:"required"`
	TargetLocator string `json:"target_locator,omitempty"`
}

type QueryPreflightResponse struct {
	Allowed                  bool       `json:"allowed"`
	Effect                   string     `json:"effect"`
	Statement                string     `json:"statement"`
	ClassificationConfidence string     `json:"classification_confidence"`
	TargetObjects            []string   `json:"target_objects,omitempty"`
	TargetLocator            string     `json:"target_locator,omitempty"`
	Warnings                 []string   `json:"warnings,omitempty"`
	Fingerprint              string     `json:"fingerprint"`
	RiskLevel                string     `json:"risk_level"`
	RequiresConfirmation     bool       `json:"requires_confirmation"`
	RequiredPermission       string     `json:"required_permission"`
	ConfirmationToken        string     `json:"confirmation_token,omitempty"`
	ConfirmationExpiresAt    *time.Time `json:"confirmation_expires_at,omitempty"`
}
