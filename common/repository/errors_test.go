package repository

import (
	"errors"
	"fmt"
	"testing"

	commonapi "github.com/addp/common/api"
	"gorm.io/gorm"
)

func TestWrapDBError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want error
	}{
		{name: "nil", err: nil, want: nil},
		{name: "not found", err: gorm.ErrRecordNotFound, want: commonapi.ErrNotFound},
		{name: "duplicate key", err: gorm.ErrDuplicatedKey, want: commonapi.ErrConflict},
		{name: "foreign key", err: gorm.ErrForeignKeyViolated, want: commonapi.ErrConflict},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.want == nil {
				got := WrapDBError(tt.err)
				if got != nil {
					t.Fatalf("WrapDBError() = %v, want nil", got)
				}
				return
			}
			got := WrapDBError(fmt.Errorf("database operation: %w", tt.err))
			if !errors.Is(got, tt.want) {
				t.Fatalf("WrapDBError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWrapDBErrorPreservesUnknownError(t *testing.T) {
	want := errors.New("database unavailable")
	if got := WrapDBError(want); !errors.Is(got, want) {
		t.Fatalf("WrapDBError() = %v, want original error", got)
	}
}
