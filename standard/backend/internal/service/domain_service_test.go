package service

import (
	"errors"
	"testing"

	commonapi "github.com/addp/common/api"
)

func TestMapDeleteConflict(t *testing.T) {
	if err := mapDeleteConflict(commonapi.ErrConflict, ErrDomainReferenced); !errors.Is(err, ErrDomainReferenced) {
		t.Fatalf("mapDeleteConflict(ErrConflict) = %v, want ErrDomainReferenced", err)
	}

	other := errors.New("database unavailable")
	if err := mapDeleteConflict(other, ErrDomainReferenced); !errors.Is(err, other) {
		t.Fatalf("mapDeleteConflict(other) = %v, want original error", err)
	}
}
