package engineaccess

import (
	"context"
	"errors"
	"strings"
	"testing"

	commonapi "github.com/addp/common/api"
	"github.com/google/uuid"
)

func TestGrantRevocationRejectsInvalidCommandBeforeDatabase(t *testing.T) {
	service := NewService(NewRepository(nil), nil)
	for _, input := range []RevokeGrantInput{
		{Reason: "missing Grant identity"},
		{RequestID: uuid.New(), Reason: " "},
		{RequestID: uuid.New(), Reason: strings.Repeat("字", 2001)},
		{RequestID: uuid.New(), Reason: "no authenticated actor"},
	} {
		if result, err := service.RevokeGrant(context.Background(), input); result != nil || !errors.Is(err, commonapi.ErrBadRequest) {
			t.Fatalf("invalid input reached database: %+v %v", result, err)
		}
	}
}
