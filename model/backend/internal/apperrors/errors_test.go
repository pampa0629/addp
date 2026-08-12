package apperrors

import (
	"errors"
	"testing"
)

func TestDomainErrorCanBeUnwrappedAndInspected(t *testing.T) {
	cause := errors.New("database unavailable")
	err := Wrap(KindConflict, "entity_code_conflict", "model.entity.code_conflict", cause)

	domainErr, ok := As(err)
	if !ok {
		t.Fatal("expected DomainError")
	}
	if domainErr.Kind != KindConflict || domainErr.Code != "entity_code_conflict" {
		t.Fatalf("unexpected domain error: %#v", domainErr)
	}
	if !errors.Is(err, cause) {
		t.Fatal("expected cause to be preserved")
	}
}
