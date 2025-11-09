package models

import "time"

// TaskStatus represents the status of an ABS task
type TaskStatus string

const (
	StatusPending    TaskStatus = "pending"
	StatusProcessing TaskStatus = "processing"
	StatusCompiling  TaskStatus = "compiling"
	StatusDeploying  TaskStatus = "deploying"
	StatusCompleted  TaskStatus = "completed"
	StatusFailed     TaskStatus = "failed"
)

// Task represents an AI bootstrapping task
type Task struct {
	ID          string     `json:"id"`
	Prompt      string     `json:"prompt"`
	Status      TaskStatus `json:"status"`
	Code        string     `json:"code,omitempty"`
	Error       string     `json:"error,omitempty"`
	Logs        []string   `json:"logs"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// CreateTaskRequest represents a request to create a new task
type CreateTaskRequest struct {
	Prompt string `json:"prompt" binding:"required"`
}

// TaskResponse represents a task response
type TaskResponse struct {
	Task    *Task  `json:"task"`
	Message string `json:"message,omitempty"`
}

// ProgressUpdate represents a real-time progress update
type ProgressUpdate struct {
	TaskID  string     `json:"task_id"`
	Status  TaskStatus `json:"status"`
	Message string     `json:"message"`
	Log     string     `json:"log,omitempty"`
	Error   string     `json:"error,omitempty"` // Error message for failed tasks
}
