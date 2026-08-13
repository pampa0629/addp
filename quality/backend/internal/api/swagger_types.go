package api

import (
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/quality/internal/models"
	"github.com/addp/quality/internal/service"
)

type qualityRuleApplicationListResponse struct {
	Data       []service.RuleApplicationListItem `json:"data"`
	Total      int64                             `json:"total"`
	Page       int                               `json:"page"`
	PageSize   int                               `json:"page_size"`
	TotalPages int                               `json:"total_pages"`
}

type qualityElementCandidateListResponse struct {
	Data       []service.RuleApplicationElementCandidate `json:"data"`
	Total      int64                                     `json:"total"`
	Page       int                                       `json:"page"`
	PageSize   int                                       `json:"page_size"`
	TotalPages int                                       `json:"total_pages"`
}

type qualityCheckTaskListResponse struct {
	Data       []models.CheckTask `json:"data"`
	Total      int64              `json:"total"`
	Page       int                `json:"page"`
	PageSize   int                `json:"page_size"`
	TotalPages int                `json:"total_pages"`
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

type qualityRuleApplicationResponse models.RuleApplication
type qualityCheckTaskResponse models.CheckTask
type qualityIssueResponse models.Issue
type qualityExecutionResponse commonExecution.TaskExecution

type issueStatusRequest struct {
	Status string `json:"status" binding:"required" enums:"resolved,ignored" example:"resolved"`
	Note   string `json:"note" binding:"required" example:"已修复源数据"`
}
