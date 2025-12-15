package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewScheduler(t *testing.T) {
	opts := Options{
		Name: "test-scheduler",
	}

	sched, err := NewScheduler(opts)
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}

	if sched == nil {
		t.Fatal("NewScheduler() returned nil")
	}

	if !sched.IsRunning() {
		t.Log("Scheduler is not running (expected before Start)")
	}
}

func TestScheduler_StartStop(t *testing.T) {
	opts := Options{
		Name: "test-scheduler",
	}

	sched, err := NewScheduler(opts)
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}

	ctx := context.Background()

	// Start scheduler
	if err := sched.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if !sched.IsRunning() {
		t.Error("Scheduler should be running after Start()")
	}

	// Stop scheduler
	if err := sched.Stop(ctx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if sched.IsRunning() {
		t.Error("Scheduler should not be running after Stop()")
	}
}

func TestScheduler_Schedule(t *testing.T) {
	opts := Options{
		Name: "test-scheduler",
	}

	sched, err := NewScheduler(opts)
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}

	ctx := context.Background()
	if err := sched.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer sched.Stop(ctx)

	// Schedule a task
	var executed atomic.Bool
	handler := func(ctx context.Context, taskID string) error {
		executed.Store(true)
		return nil
	}

	// 每秒执行一次（用于测试）
	expr := "* * * * * *" // 需要秒级精度

	// 由于默认不启用秒级精度，这里应该会失败
	err = sched.Schedule(ctx, "test-task", expr, handler)
	if err == nil {
		t.Log("Note: 6-field expression requires EnableSeconds=true")
	}

	// 使用标准 5 字段表达式（每分钟）
	expr = "* * * * *"
	err = sched.Schedule(ctx, "test-task", expr, handler)
	if err != nil {
		t.Fatalf("Schedule() error = %v", err)
	}

	// 验证任务信息
	info, err := sched.GetTaskInfo("test-task")
	if err != nil {
		t.Fatalf("GetTaskInfo() error = %v", err)
	}

	if info.ID != "test-task" {
		t.Errorf("TaskInfo.ID = %v, want %v", info.ID, "test-task")
	}

	if info.CronExpr != expr {
		t.Errorf("TaskInfo.CronExpr = %v, want %v", info.CronExpr, expr)
	}
}

func TestScheduler_Unschedule(t *testing.T) {
	opts := Options{
		Name: "test-scheduler",
	}

	sched, err := NewScheduler(opts)
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}

	ctx := context.Background()
	if err := sched.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer sched.Stop(ctx)

	// Schedule a task
	handler := func(ctx context.Context, taskID string) error {
		return nil
	}

	expr := "* * * * *"
	if err := sched.Schedule(ctx, "test-task", expr, handler); err != nil {
		t.Fatalf("Schedule() error = %v", err)
	}

	// Unschedule the task
	if err := sched.Unschedule(ctx, "test-task"); err != nil {
		t.Fatalf("Unschedule() error = %v", err)
	}

	// 验证任务已移除
	_, err = sched.GetTaskInfo("test-task")
	if err == nil {
		t.Error("GetTaskInfo() should return error for unscheduled task")
	}
}

func TestScheduler_GetScheduledTasks(t *testing.T) {
	opts := Options{
		Name: "test-scheduler",
	}

	sched, err := NewScheduler(opts)
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}

	ctx := context.Background()
	if err := sched.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer sched.Stop(ctx)

	// Schedule multiple tasks
	handler := func(ctx context.Context, taskID string) error {
		return nil
	}

	tasks := []string{"task1", "task2", "task3"}
	for _, taskID := range tasks {
		if err := sched.Schedule(ctx, taskID, "* * * * *", handler); err != nil {
			t.Fatalf("Schedule() error = %v", err)
		}
	}

	// Get all scheduled tasks
	scheduled := sched.GetScheduledTasks()
	if len(scheduled) != len(tasks) {
		t.Errorf("GetScheduledTasks() returned %d tasks, want %d", len(scheduled), len(tasks))
	}
}

func TestScheduler_WithSeconds(t *testing.T) {
	opts := Options{
		Name:          "test-scheduler-seconds",
		EnableSeconds: true, // 启用秒级精度
	}

	sched, err := NewScheduler(opts)
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}

	ctx := context.Background()
	if err := sched.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer sched.Stop(ctx)

	// Schedule a task with seconds
	var executed atomic.Bool
	handler := func(ctx context.Context, taskID string) error {
		executed.Store(true)
		return nil
	}

	// 每2秒执行一次
	expr := "*/2 * * * * *"
	err = sched.Schedule(ctx, "test-task-seconds", expr, handler)
	if err != nil {
		t.Fatalf("Schedule() error = %v", err)
	}

	// 等待任务执行（最多5秒）
	time.Sleep(5 * time.Second)

	if !executed.Load() {
		t.Error("Task should have been executed")
	}
}

func TestScheduler_GetNextRunTime(t *testing.T) {
	opts := Options{
		Name: "test-scheduler",
	}

	sched, err := NewScheduler(opts)
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}

	expr := "30 14 * * *" // 每天 14:30
	nextRun, err := sched.GetNextRunTime(expr)
	if err != nil {
		t.Fatalf("GetNextRunTime() error = %v", err)
	}

	if nextRun.IsZero() {
		t.Error("GetNextRunTime() returned zero time")
	}

	// 验证是 14:30
	if nextRun.Hour() != 14 || nextRun.Minute() != 30 {
		t.Errorf("GetNextRunTime() = %v, want hour=14 minute=30", nextRun)
	}
}
