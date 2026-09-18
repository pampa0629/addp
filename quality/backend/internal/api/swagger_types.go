package api

import (
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/taskprovider"
	"github.com/addp/quality/internal/models"
	"github.com/addp/quality/internal/service"
)

type qualityElementCandidateListResponse struct {
	Data       []service.PlanElementCandidate `json:"data"`
	Total      int64                          `json:"total"`
	Page       int                            `json:"page"`
	PageSize   int                            `json:"page_size"`
	TotalPages int                            `json:"total_pages"`
}

type qualityIssueListResponse struct {
	Data       []models.Issue `json:"data"`
	Total      int64          `json:"total"`
	Page       int            `json:"page"`
	PageSize   int            `json:"page_size"`
	TotalPages int            `json:"total_pages"`
}

type qualityExecutionListResponse struct {
	Data       []commonExecution.TaskExecution `json:"data"`
	Total      int64                           `json:"total"`
	Page       int                             `json:"page"`
	PageSize   int                             `json:"page_size"`
	TotalPages int                             `json:"total_pages"`
}

type qualityPlanResponse struct {
	models.QualityPlan
	ExecutionContract taskprovider.ExecutionContract `json:"execution_contract"`
}
type qualityRuleResponse models.QualityRule
type qualityRuleListResponse struct {
	Data       []models.QualityRule `json:"data"`
	Total      int64                `json:"total"`
	Page       int                  `json:"page"`
	PageSize   int                  `json:"page_size"`
	TotalPages int                  `json:"total_pages"`
}

type qualityPlanListResponse struct {
	Data       []models.QualityPlan `json:"data"`
	Total      int64                `json:"total"`
	Page       int                  `json:"page"`
	PageSize   int                  `json:"page_size"`
	TotalPages int                  `json:"total_pages"`
}
type qualityIssueResponse models.Issue
type qualityExecutionResponse commonExecution.TaskExecution

type issueStatusRequest struct {
	Version int64  `json:"version" binding:"required,min=1" example:"1"`
	Status  string `json:"status" binding:"required,oneof=resolved accepted" enums:"resolved,accepted" example:"accepted"`
	Note    string `json:"note" binding:"required" example:"已修复源数据"`
}
