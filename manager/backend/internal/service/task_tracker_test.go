package service

import "testing"

func TestTaskTrackerUsesSuccessAsTerminalExecutionStatus(t *testing.T) {
	tracker := NewTaskTracker()
	task := tracker.CreateTask("task-1", "created")
	if task.Status != TaskStatusPending {
		t.Fatalf("initial status = %s, want pending", task.Status)
	}

	tracker.UpdateStatus("task-1", TaskStatusSuccess, "done")

	got, ok := tracker.GetTask("task-1")
	if !ok {
		t.Fatal("task not found")
	}
	if got.Status != TaskStatusSuccess {
		t.Fatalf("status = %s, want success", got.Status)
	}
	if got.EndTime == nil {
		t.Fatal("end time is nil for success status")
	}
}
