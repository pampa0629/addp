package models

import "time"

// AppStatus 表示应用的运行状态
type AppStatus string

const (
	AppStatusRegistered AppStatus = "registered"
	AppStatusStarting   AppStatus = "starting"
	AppStatusRunning    AppStatus = "running"
	AppStatusFailed     AppStatus = "failed"
)

// ModificationRecord 记录应用的一次修改历史
type ModificationRecord struct {
	Timestamp time.Time `json:"timestamp"`
	Prompt    string    `json:"prompt"`
	TaskID    string    `json:"task_id"`
	Success   bool      `json:"success"`
}

// App 表示一个可以管理/运行的应用入口
type App struct {
	ID                   string                `json:"id"`
	Name                 string                `json:"name"`
	Slug                 string                `json:"slug"`
	Description          string                `json:"description"`
	Category             string                `json:"category,omitempty"`
	Icon                 string                `json:"icon,omitempty"`
	Cover                string                `json:"cover,omitempty"`
	Tags                 []string              `json:"tags,omitempty"`
	EntryURL             string                `json:"entry_url"`
	TaskID               string                `json:"task_id,omitempty"`
	WorkspacePath        string                `json:"workspace_path,omitempty"`
	StartCommand         []string              `json:"start_command,omitempty"`
	Status               AppStatus             `json:"status"`
	LastStartedAt        *time.Time            `json:"last_started_at,omitempty"`
	LastStartResult      string                `json:"last_start_result,omitempty"`
	RunningPID           int                   `json:"running_pid,omitempty"`
	ModificationHistory  []ModificationRecord  `json:"modification_history,omitempty"`
	CreatedAt            time.Time             `json:"created_at"`
	UpdatedAt            time.Time             `json:"updated_at"`
}

// CreateAppRequest 是创建应用的请求体
type CreateAppRequest struct {
	Name          string   `json:"name" binding:"required"`
	Description   string   `json:"description"`
	Category      string   `json:"category"`
	Icon          string   `json:"icon"`
	Cover         string   `json:"cover"`
	EntryURL      string   `json:"entry_url" binding:"required"`
	TaskID        string   `json:"task_id"`
	WorkspacePath string   `json:"workspace_path"`
	StartCommand  []string `json:"start_command"`
	Tags          []string `json:"tags"`
}

// ModifyAppRequest 修改已有应用的请求体
type ModifyAppRequest struct {
	Prompt string `json:"prompt" binding:"required"`
}

// LaunchResponse 返回启动应用的结果
type LaunchResponse struct {
	App       *App   `json:"app"`
	Message   string `json:"message"`
	ProcessID int    `json:"process_id,omitempty"`
}
