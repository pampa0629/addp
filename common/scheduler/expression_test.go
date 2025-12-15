package scheduler

import (
	"testing"
	"time"
)

func TestExpressionBuilder_BuildFromScheduleConfig(t *testing.T) {
	builder := NewExpressionBuilder()

	tests := []struct {
		name       string
		config     ScheduleConfig
		wantExpr   string
		wantErr    bool
		errContains string
	}{
		{
			name: "manual type",
			config: ScheduleConfig{
				Type: "manual",
			},
			wantExpr: "",
			wantErr:  false,
		},
		{
			name: "daily at 14:30",
			config: ScheduleConfig{
				Type: "daily",
				Time: "14:30",
			},
			wantExpr: "30 14 * * *",
			wantErr:  false,
		},
		{
			name: "weekly on Mon/Wed/Fri at 10:00",
			config: ScheduleConfig{
				Type:  "weekly",
				Time:  "10:00",
				Value: []int{1, 3, 5},
			},
			wantExpr: "0 10 * * 1,3,5",
			wantErr:  false,
		},
		{
			name: "monthly on 1st and 15th at 09:00",
			config: ScheduleConfig{
				Type:  "monthly",
				Time:  "09:00",
				Value: []int{1, 15},
			},
			wantExpr: "0 9 1,15 * *",
			wantErr:  false,
		},
		{
			name: "custom cron expression",
			config: ScheduleConfig{
				Type: "cron",
				Expr: "*/5 * * * *",
			},
			wantExpr: "*/5 * * * *",
			wantErr:  false,
		},
		{
			name: "invalid time format",
			config: ScheduleConfig{
				Type: "daily",
				Time: "25:00",
			},
			wantErr:     true,
			errContains: "小时格式非法",
		},
		{
			name: "empty cron expression",
			config: ScheduleConfig{
				Type: "cron",
				Expr: "",
			},
			wantErr:     true,
			errContains: "不能为空",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr, schedConfig, err := builder.BuildFromScheduleConfig(tt.config)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error but got none")
					return
				}
				if tt.errContains != "" && !contains(err.Error(), tt.errContains) {
					t.Errorf("error = %v, want contains %v", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if expr != tt.wantExpr {
				t.Errorf("expr = %v, want %v", expr, tt.wantExpr)
			}

			if schedConfig == nil {
				t.Errorf("scheduleConfig is nil")
			}
		})
	}
}

func TestExpressionBuilder_Validate(t *testing.T) {
	builder := NewExpressionBuilder()

	tests := []struct {
		name    string
		expr    string
		wantErr bool
	}{
		{
			name:    "valid daily expression",
			expr:    "30 14 * * *",
			wantErr: false,
		},
		{
			name:    "valid every 5 minutes",
			expr:    "*/5 * * * *",
			wantErr: false,
		},
		{
			name:    "invalid expression - too many fields",
			expr:    "0 0 0 0 0 0 0",
			wantErr: true,
		},
		{
			name:    "invalid expression - invalid minute",
			expr:    "60 14 * * *",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := builder.Validate(tt.expr)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestExpressionBuilder_NextRunTime(t *testing.T) {
	builder := NewExpressionBuilder()

	// 每天 14:30
	expr := "30 14 * * *"
	now := time.Date(2025, 12, 13, 10, 0, 0, 0, time.UTC)

	next, err := builder.NextRunTime(expr, now)
	if err != nil {
		t.Fatalf("NextRunTime() error = %v", err)
	}

	// 应该是今天的 14:30
	expected := time.Date(2025, 12, 13, 14, 30, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Errorf("NextRunTime() = %v, want %v", next, expected)
	}

	// 如果当前时间已经过了 14:30，应该返回明天的 14:30
	now = time.Date(2025, 12, 13, 15, 0, 0, 0, time.UTC)
	next, err = builder.NextRunTime(expr, now)
	if err != nil {
		t.Fatalf("NextRunTime() error = %v", err)
	}

	expected = time.Date(2025, 12, 14, 14, 30, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Errorf("NextRunTime() = %v, want %v", next, expected)
	}
}

func TestExpressionBuilder_NextRunTimes(t *testing.T) {
	builder := NewExpressionBuilder()

	expr := "0 */2 * * *" // 每2小时
	times, err := builder.NextRunTimes(expr, 3)
	if err != nil {
		t.Fatalf("NextRunTimes() error = %v", err)
	}

	if len(times) != 3 {
		t.Errorf("NextRunTimes() returned %d times, want 3", len(times))
	}

	// 验证时间间隔
	for i := 1; i < len(times); i++ {
		diff := times[i].Sub(times[i-1])
		if diff != 2*time.Hour {
			t.Errorf("time interval = %v, want 2h", diff)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
