package api

import (
	commonExecution "github.com/addp/common/execution"
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

type qualityPlanResponse models.QualityPlan
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
	Status string `json:"status" binding:"required" enums:"resolved,ignored" example:"resolved"`
	Note   string `json:"note" binding:"required" example:"已修复源数据"`
}
